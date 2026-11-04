package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/urfave/cli/v3"

	"github.com/kdicusb/portman/internal/config"
	"github.com/kdicusb/portman/internal/ui"
)

var logsCmd = &cli.Command{
	Name:  "logs",
	Usage: "show portman's log",
	Flags: []cli.Flag{
		&cli.BoolFlag{Name: "follow", Aliases: []string{"f"}, Usage: "keep printing new lines until ctrl+c"},
		&cli.IntFlag{Name: "lines", Aliases: []string{"n"}, Value: 40, Usage: "how many recent lines to show"},
	},
	Action: func(ctx context.Context, cmd *cli.Command) error {
		path := config.LogFile(cmd.String("home"))
		f, err := os.Open(path)
		if errors.Is(err, fs.ErrNotExist) {
			return ui.Hint(errors.New("no log yet"), "portman writes %s once it runs: start it with pm start", path)
		}
		if err != nil {
			return err
		}
		defer func() { f.Close() }()

		out := &prettyLog{w: cmd.Root().Writer}
		// shortcut: reads the whole file, which lumberjack caps at 10 MB.
		all, err := io.ReadAll(f)
		if err != nil {
			return err
		}
		lines := bytes.SplitAfter(all, []byte("\n"))
		var partial []byte
		if last := lines[len(lines)-1]; !bytes.HasSuffix(last, []byte("\n")) {
			partial, lines = last, lines[:len(lines)-1]
		}
		for _, l := range lines[max(0, len(lines)-int(cmd.Int("lines"))):] {
			out.Write(l)
		}
		if !cmd.Bool("follow") {
			return nil
		}

		r := bufio.NewReader(f)
		for {
			chunk, err := r.ReadBytes('\n')
			partial = append(partial, chunk...)
			if err == nil {
				out.Write(partial)
				partial = nil
				continue
			}
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(250 * time.Millisecond):
			}
			// lumberjack rotates by renaming the file and starting a new one.
			if now, err := os.Stat(path); err == nil {
				if was, err := f.Stat(); err == nil && !os.SameFile(now, was) {
					if next, err := os.Open(path); err == nil {
						f.Close()
						f, r = next, bufio.NewReader(next)
					}
				}
			}
		}
	},
}

// prettyLog writes portman's and Caddy's JSON log lines as aligned, colored text.
type prettyLog struct {
	mu sync.Mutex
	w  io.Writer
}

func (p *prettyLog) Write(b []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, line := range bytes.Split(bytes.TrimRight(b, "\n"), []byte("\n")) {
		lipgloss.Fprintln(p.w, formatLog(line))
	}
	return len(b), nil
}

// formatLog reads both formats in pm.log: slog's ("time", "level", "msg")
// and Caddy's ("ts", "level", "logger", "msg"). Other lines pass through.
func formatLog(line []byte) string {
	var e map[string]any
	if json.Unmarshal(line, &e) != nil {
		return string(line)
	}
	var t time.Time
	switch v := e["time"].(type) {
	case string:
		t, _ = time.Parse(time.RFC3339Nano, v)
	}
	if ts, ok := e["ts"].(float64); ok {
		t = time.UnixMilli(int64(ts * 1000))
	}
	level := strings.ToUpper(fmt.Sprint(e["level"]))
	msg := fmt.Sprint(e["msg"])
	if logger, ok := e["logger"].(string); ok {
		msg = logger + ": " + msg
	}
	for _, k := range []string{"time", "ts", "level", "msg", "logger"} {
		delete(e, k)
	}
	var attrs strings.Builder
	for _, k := range slices.Sorted(maps.Keys(e)) {
		v, ok := e[k].(string)
		if !ok {
			b, _ := json.Marshal(e[k])
			v = string(b)
		}
		fmt.Fprintf(&attrs, " %s=%s", k, v)
	}

	lvl := fmt.Sprintf("%-5s", level)
	switch level {
	case "ERROR", "WARN":
		lvl = ui.Red(lvl)
	case "DEBUG":
		lvl = ui.Faint(lvl)
	default:
		lvl = ui.Accent(lvl)
	}
	return ui.Faint(t.Local().Format("Jan 02 15:04:05")) + "  " + lvl + "  " + msg + ui.Faint(attrs.String())
}
