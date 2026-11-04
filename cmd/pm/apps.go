package main

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net"
	"slices"
	"sync"
	"time"

	"github.com/kdicusb/portman/internal/config"
	"github.com/kdicusb/portman/internal/proxy"
	"github.com/kdicusb/portman/internal/ui"
	"github.com/urfave/cli/v3"
)

var addCmd = &cli.Command{
	Name:      "add",
	Usage:     "map NAME.localhost to a port or host:port",
	ArgsUsage: "NAME PORT",
	Action: func(ctx context.Context, cmd *cli.Command) error {
		if cmd.NArg() != 2 {
			return ui.Hint(errors.New("add needs a name and a port"), "example: pm add api 8080")
		}
		name, err := config.ParseName(cmd.Args().Get(0))
		if err != nil {
			return err
		}
		target, err := config.ParseTarget(cmd.Args().Get(1))
		if err != nil {
			return ui.Hint(err, "use a port like 8080 or a host:port like 10.0.0.5:8080")
		}
		dir := cmd.String("home")
		c, err := config.Load(dir)
		if err != nil {
			return err
		}
		old := c.Apps[name]
		c.Apps[name] = target
		if err := config.Save(dir, c); err != nil {
			return err
		}
		out := cmd.Root().Writer
		ui.Success(out, ui.Accent(appURL(c, name))+ui.Faint(" → "+target))
		if old != "" && old != target {
			ui.Info(out, "was "+old)
		}
		if _, err := proxy.Probe(c.Port); err != nil {
			ui.Info(out, "portman isn't running yet: start it with pm start")
		}
		return nil
	},
}

var deleteCmd = &cli.Command{
	Name:      "delete",
	Aliases:   []string{"rm"},
	Usage:     "remove an app",
	ArgsUsage: "NAME",
	Action: func(ctx context.Context, cmd *cli.Command) error {
		if cmd.NArg() != 1 {
			return ui.Hint(errors.New("delete needs an app name"), "example: pm delete api")
		}
		dir := cmd.String("home")
		c, err := config.Load(dir)
		if err != nil {
			return err
		}
		name := cmd.Args().First()
		if _, ok := c.Apps[name]; !ok {
			return ui.Hint(fmt.Errorf("no app named %s", name), "run pm list to see your apps")
		}
		delete(c.Apps, name)
		if err := config.Save(dir, c); err != nil {
			return err
		}
		ui.Success(cmd.Root().Writer, "removed "+name)
		return nil
	},
}

var listCmd = &cli.Command{
	Name:    "list",
	Aliases: []string{"ls"},
	Usage:   "show apps and whether they're reachable",
	Action: func(ctx context.Context, cmd *cli.Command) error {
		c, err := config.Load(cmd.String("home"))
		if err != nil {
			return err
		}
		out := cmd.Root().Writer
		fmt.Fprintln(out)
		running, err := proxy.Probe(c.Port)
		switch {
		case err == nil:
			where := fmt.Sprintf("127.0.0.1:%d", c.Port)
			if c.Share {
				where = fmt.Sprintf("port %d, shared with other machines", c.Port)
			}
			ui.Line(out, ui.Up("portman "+running+" on "+where))
			if running != version {
				ui.Info(out, "  this pm is "+version+": run pm start to restart portman on it")
			}
		case errors.Is(err, proxy.ErrNotRunning):
			ui.Line(out, ui.Down("portman isn't running: start it with pm start"))
		default:
			ui.Line(out, ui.Down(err.Error()))
		}
		fmt.Fprintln(out)

		names := slices.Sorted(maps.Keys(c.Apps))
		targets := make([]string, len(names))
		for i, n := range names {
			targets[i] = c.Apps[n]
		}
		if c.Forward != "" {
			targets = append(targets, c.Forward)
		}
		up := reachable(targets)

		if len(names) == 0 {
			ui.Info(out, "no apps yet: add one with pm add NAME PORT")
		} else {
			rows := make([][]string, len(names))
			for i, n := range names {
				rows[i] = []string{n, ui.Accent(appURL(c, n)), c.Apps[n], status(up[i])}
			}
			ui.Table(out, []string{"APP", "URL", "TARGET", ""}, rows)
		}
		if c.Forward != "" {
			fmt.Fprintln(out)
			ui.Line(out,
				ui.Faint("other *."+c.Domain+" names → ")+c.Forward+"   "+status(up[len(up)-1]),
			)
		}
		fmt.Fprintln(out)
		return nil
	},
}

var forwardCmd = &cli.Command{
	Name:      "forward",
	Usage:     "send names with no local app to portman on another machine",
	ArgsUsage: "HOST[:PORT]",
	Flags:     []cli.Flag{&cli.BoolFlag{Name: "off", Usage: "stop forwarding"}},
	Action: func(ctx context.Context, cmd *cli.Command) error {
		dir := cmd.String("home")
		c, err := config.Load(dir)
		if err != nil {
			return err
		}
		out := cmd.Root().Writer
		if cmd.Bool("off") {
			c.Forward = ""
			if err := config.Save(dir, c); err != nil {
				return err
			}
			ui.Success(out, "forwarding off")
			return nil
		}
		if cmd.NArg() != 1 {
			return ui.Hint(errors.New("forward needs a host"),
				"example: pm forward my-vm.local, or pm forward --off",
			)
		}
		if c.Forward, err = config.ParseForward(cmd.Args().First()); err != nil {
			return err
		}
		if err := config.Save(dir, c); err != nil {
			return err
		}
		ui.Success(out, "other *."+c.Domain+" names → "+ui.Accent(c.Forward))
		if !reachable([]string{c.Forward})[0] {
			ui.Info(out, "can't reach "+c.Forward+" yet: is portman running there with sharing on?")
		}
		return nil
	},
}

func status(up bool) string {
	if up {
		return ui.Up("up")
	}
	return ui.Down("down")
}

// reachable dials every target at once so a slow host doesn't stall the list.
func reachable(targets []string) []bool {
	up := make([]bool, len(targets))
	var wg sync.WaitGroup
	for i, t := range targets {
		wg.Go(func() {
			if conn, err := net.DialTimeout("tcp", t, 500*time.Millisecond); err == nil {
				conn.Close()
				up[i] = true
			}
		})
	}
	wg.Wait()
	return up
}
