package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"runtime/debug"
	"strconv"

	"github.com/kdicusb/portman/internal/config"
	"github.com/kdicusb/portman/internal/ui"
	"github.com/urfave/cli/v3"
	"golang.org/x/mod/semver"
)

// version is set by release builds; go install builds read it from the module.
var version = "dev"

func main() {
	if bi, ok := debug.ReadBuildInfo(); ok && version == "dev" && semver.IsValid(bi.Main.Version) {
		version = bi.Main.Version
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := app().Run(ctx, os.Args); err != nil {
		ui.Error(os.Stderr, err)
		os.Exit(1)
	}
}

func app() *cli.Command {
	return &cli.Command{
		Name:            "pm",
		Usage:           "give local ports friendly names like api.localhost",
		Version:         version,
		HideHelpCommand: true,
		Flags: []cli.Flag{&cli.StringFlag{
			Name:    "home",
			Usage:   "directory for config, logs, and data",
			Value:   config.DefaultDir(),
			Sources: cli.EnvVars("PORTMAN_HOME"),
		}},
		Commands: []*cli.Command{
			addCmd, deleteCmd, listCmd, forwardCmd, configCmd,
			startCmd, stopCmd, serveCmd, logsCmd, updateCmd,
		},
	}
}

func appURL(c config.Config, name string) string {
	host := name + "." + c.Domain
	if c.Port != 80 {
		host += ":" + strconv.Itoa(c.Port)
	}
	return "http://" + host
}

// asRoot reruns this command under sudo on Linux and macOS, where the system
// service needs root. It reports whether it reran.
func asRoot(cmd *cli.Command) (bool, error) {
	if (runtime.GOOS != "linux" && runtime.GOOS != "darwin") || os.Geteuid() == 0 {
		return false, nil
	}
	exe, err := os.Executable()
	if err != nil {
		return true, err
	}
	return true, sudo(append([]string{exe, "--home", cmd.String("home")}, os.Args[1:]...)...)
}

// sudo runs args as root in this terminal and exits with its status on failure.
func sudo(args ...string) error {
	c := exec.Command("sudo", args...)
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := c.Run(); err != nil {
		// The elevated run already printed its own error.
		if ee := (*exec.ExitError)(nil); errors.As(err, &ee) {
			os.Exit(ee.ExitCode())
		}
		return err
	}
	return nil
}
