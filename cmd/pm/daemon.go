package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/kardianos/service"
	"github.com/kdicusb/portman/internal/config"
	"github.com/kdicusb/portman/internal/proxy"
	"github.com/kdicusb/portman/internal/ui"
	"github.com/urfave/cli/v3"
	"gopkg.in/natefinch/lumberjack.v2"
)

// unit replaces kardianos' systemd template to grant :80 without root and to
// restart in seconds rather than minutes.
const unit = `[Unit]
Description={{Description}}
After=network.target

[Service]
ExecStart={{Path | cmdEscape}}{{range Arguments}} {{. | cmd}}{{end}}
{{if UserName}}User={{UserName}}
{{end}}AmbientCapabilities=CAP_NET_BIND_SERVICE
Restart=always
RestartSec=2

[Install]
WantedBy=multi-user.target
`

func newService(dir string, d *daemon) (service.Service, error) {
	return service.New(d, &service.Config{
		Name:        "portman",
		DisplayName: "portman",
		Description: "Maps NAME.localhost to local ports",
		Arguments:   []string{"serve", "--home", dir},
		// On Linux, run as whoever ran sudo so files in dir stay theirs.
		UserName: os.Getenv("SUDO_USER"),
		Option: service.KeyValue{
			"SystemdScript": unit,
			"UserService":   runtime.GOOS == "darwin",
			"RunAtLoad":     true,
			"OnFailure":     "restart",
		},
	})
}

type daemon struct {
	dir  string
	logw io.Writer
	out  io.Writer
	stop func()
}

func (d *daemon) Start(s service.Service) error {
	stop, err := proxy.Start(d.dir, version, d.logw)
	if err != nil {
		return explain(err)
	}
	d.stop = stop
	if service.Interactive() {
		c, _ := config.Load(d.dir)
		ui.Success(d.out, "portman "+version+" serving "+ui.Accent(appURL(c, "*"))+ui.Faint(" · ctrl+c to stop"))
	}
	return nil
}

func (d *daemon) Stop(s service.Service) error {
	d.stop()
	return nil
}

// explain turns the ways listening fails into the fix for each.
func explain(err error) error {
	switch msg := err.Error(); {
	case errors.Is(err, proxy.ErrRunning):
		return ui.Hint(err, "watch it with pm logs -f, or stop it with pm stop")
	case strings.Contains(msg, "permission denied"):
		return ui.Hint(err, "ports below 1024 need pm start, or pick a higher port with pm config")
	case strings.Contains(msg, "address already in use"), strings.Contains(msg, "Only one usage"):
		return ui.Hint(err, "another program has this port: stop it, or pick another port with pm config")
	}
	return err
}

var serveCmd = &cli.Command{
	Name:  "serve",
	Usage: "run portman in the foreground, logging to the terminal",
	Action: func(ctx context.Context, cmd *cli.Command) error {
		dir := cmd.String("home")
		out := cmd.Root().Writer
		if c, err := config.Load(dir); err == nil {
			if v, err := proxy.Probe(c.Port); err == nil {
				return explain(fmt.Errorf("%w on port %d (%s)", proxy.ErrRunning, c.Port, v))
			}
			if rerun, err := withBindRights(cmd, c); rerun {
				return err
			}
		}
		lj := &lumberjack.Logger{Filename: config.LogFile(dir), MaxSize: 10, MaxBackups: 3}
		defer lj.Close()
		var logw io.Writer = lj
		if service.Interactive() {
			logw = io.MultiWriter(lj, &prettyLog{w: out})
		}
		s, err := newService(dir, &daemon{dir: dir, logw: logw, out: out})
		if err != nil {
			return err
		}
		return s.Run()
	},
}

// withBindRights reruns pm serve through sudo on Linux when the port needs
// privilege. setpriv keeps you as the user, so files in dir stay yours, and
// adds only the right to bind low ports, like the service unit does.
func withBindRights(cmd *cli.Command, c config.Config) (bool, error) {
	if runtime.GOOS != "linux" || os.Geteuid() == 0 || os.Getenv("SUDO_UID") != "" {
		return false, nil
	}
	ln, err := net.Listen("tcp", proxy.Listen(c))
	if err == nil {
		ln.Close()
		return false, nil
	}
	if !errors.Is(err, os.ErrPermission) {
		return false, nil
	}
	if _, err := exec.LookPath("setpriv"); err != nil {
		return true, ui.Hint(fmt.Errorf("port %d needs extra rights", c.Port), "run pm start to use the service, or pick a port above 1023 with pm config")
	}
	exe, err := os.Executable()
	if err != nil {
		return true, err
	}
	ui.Info(cmd.Root().Writer, fmt.Sprintf("port %d needs extra rights on Linux: asking sudo to grant them", c.Port))
	return true, sudo("setpriv",
		"--reuid", strconv.Itoa(os.Getuid()), "--regid", strconv.Itoa(os.Getgid()), "--init-groups",
		"--inh-caps=+net_bind_service", "--ambient-caps=+net_bind_service",
		"--", exe, "--home", cmd.String("home"), "serve")
}

var startCmd = &cli.Command{
	Name:  "start",
	Usage: "run portman in the background, now and whenever your machine starts",
	Action: func(ctx context.Context, cmd *cli.Command) error {
		if rerun, err := asRoot(cmd); rerun {
			return err
		}
		dir := cmd.String("home")
		s, err := newService(dir, &daemon{})
		if err != nil {
			return err
		}
		if _, err := s.Status(); !errors.Is(err, service.ErrNotInstalled) {
			_ = s.Stop()
			if err := s.Uninstall(); err != nil {
				return elevated(err)
			}
		}
		if err := s.Install(); err != nil {
			return elevated(err)
		}
		if err := s.Start(); err != nil {
			return elevated(err)
		}
		c, err := config.Load(dir)
		if err != nil {
			return err
		}
		for range 30 {
			if v, err := proxy.Probe(c.Port); err == nil {
				ui.Success(cmd.Root().Writer, "portman "+v+" is running at "+ui.Accent(appURL(c, "*")))
				return nil
			}
			time.Sleep(100 * time.Millisecond)
		}
		return ui.Hint(fmt.Errorf("portman started but isn't answering on port %d", c.Port), "see why with pm logs")
	},
}

var stopCmd = &cli.Command{
	Name:  "stop",
	Usage: "stop portman and keep it from starting with your machine (keeps your config)",
	Action: func(ctx context.Context, cmd *cli.Command) error {
		dir := cmd.String("home")
		s, err := newService(dir, &daemon{})
		if err != nil {
			return err
		}
		out := cmd.Root().Writer
		if _, err := s.Status(); errors.Is(err, service.ErrNotInstalled) {
			if c, err := config.Load(dir); err == nil {
				if _, err := proxy.Probe(c.Port); err == nil {
					ui.Info(out, "portman is running in the foreground: press ctrl+c where pm serve runs")
					return nil
				}
			}
			ui.Success(out, "portman isn't running")
			return nil
		}
		if rerun, err := asRoot(cmd); rerun {
			return err
		}
		_ = s.Stop()
		if err := s.Uninstall(); err != nil {
			return elevated(err)
		}
		ui.Success(out, "portman stopped")
		return nil
	},
}

func elevated(err error) error {
	if runtime.GOOS == "windows" {
		return ui.Hint(err, "Windows services need an Administrator terminal: right-click your terminal, Run as administrator")
	}
	return err
}
