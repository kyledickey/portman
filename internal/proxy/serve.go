package proxy

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"github.com/caddyserver/caddy/v2"
	_ "github.com/caddyserver/caddy/v2/modules/caddyhttp/reverseproxy"
	"github.com/fsnotify/fsnotify"

	"github.com/kdicusb/portman/internal/config"
)

// Start serves dir's config and reloads it each time the file changes. Hand
// edits that don't parse are logged and the previous routes stay up.
func Start(dir, version string, logw io.Writer) (stop func(), err error) {
	setSink(logw)
	level := new(slog.LevelVar)
	log := slog.New(slog.NewJSONHandler(logw, &slog.HandlerOptions{Level: level}))
	// Keep Caddy's own bookkeeping files inside dir too.
	os.Setenv("XDG_DATA_HOME", dir)
	caddy.DefaultStorage.Path = filepath.Join(dir, "caddy")

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	load := func() error {
		c, err := config.Load(dir)
		if err != nil {
			return err
		}
		_ = level.UnmarshalText([]byte(c.LogLevel))
		b, err := Build(c, version)
		if err != nil {
			return err
		}
		if err := caddy.Load(b, false); err != nil {
			return err
		}
		log.Info("routes loaded",
			"apps", len(c.Apps),
			"forward", c.Forward,
			"port", c.Port,
			"share", c.Share,
		)
		return nil
	}
	// Caddy binds with SO_REUSEPORT, so a second portman would bind as well
	// and silently split traffic with the first.
	if c, err := config.Load(dir); err == nil {
		if v, err := Probe(c.Port); err == nil {
			return nil, fmt.Errorf("%w on port %d (%s)", ErrRunning, c.Port, v)
		}
	}
	if err := load(); err != nil {
		return nil, err
	}
	w, err := fsnotify.NewWatcher()
	if err == nil {
		err = w.Add(dir)
	}
	if err != nil {
		caddy.Stop()
		return nil, err
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case ev, ok := <-w.Events:
				if !ok {
					return
				}
				if filepath.Base(ev.Name) == "config.toml" &&
					ev.Has(fsnotify.Create|fsnotify.Write) {
					if err := load(); err != nil {
						log.Error("config reload failed, keeping previous routes", "err", err)
					}
				}
			case err, ok := <-w.Errors:
				if !ok {
					return
				}
				log.Error("watching config", "err", err)
			}
		}
	}()
	log.Info("portman started", "version", version, "dir", dir)
	return func() {
		w.Close()
		<-done
		caddy.Stop()
		log.Info("portman stopped")
	}, nil
}

// sink receives Caddy's own logs, so both share one rotated file. Caddy
// reuses an opened writer across reloads, so the writer reads sink per write.
var (
	sinkMu sync.Mutex
	sink   io.Writer = io.Discard
)

func setSink(w io.Writer) {
	sinkMu.Lock()
	defer sinkMu.Unlock()
	sink = w
}

func init() { caddy.RegisterModule(logWriter{}) }

type logWriter struct{}

func (logWriter) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "caddy.logging.writers.portman",
		New: func() caddy.Module { return new(logWriter) },
	}
}

func (logWriter) String() string { return "portman" }

// WriterKey matches Caddy's stderr writer only so Caddy skips printing
// "redirected default logger" to stderr on startup.
func (logWriter) WriterKey() string { return "std:err" }

func (logWriter) OpenWriter() (io.WriteCloser, error) { return logWriter{}, nil }
func (logWriter) Close() error                        { return nil }

func (logWriter) Write(p []byte) (int, error) {
	sinkMu.Lock()
	defer sinkMu.Unlock()
	return sink.Write(p)
}
