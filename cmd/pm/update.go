package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"

	"charm.land/huh/v2/spinner"
	"github.com/kardianos/service"
	"github.com/urfave/cli/v3"

	"github.com/kdicusb/portman/internal/ui"
	"github.com/kdicusb/portman/internal/update"
)

var updateCmd = &cli.Command{
	Name:  "update",
	Usage: "replace pm with the newest release",
	Flags: []cli.Flag{&cli.BoolFlag{Name: "beta", Usage: "include beta releases"}},
	Action: func(ctx context.Context, cmd *cli.Command) error {
		out := cmd.Root().Writer
		rel, err := update.Latest(ctx, cmd.Bool("beta"))
		if err != nil {
			return ui.Hint(err, "check your connection, or download a release from github.com/kdicusb/portman/releases")
		}
		if !update.Newer(rel.Tag, version) {
			ui.Success(out, "pm "+version+" is up to date")
			return nil
		}
		exe, err := os.Executable()
		if err == nil {
			exe, err = filepath.EvalSymlinks(exe)
		}
		if err != nil {
			return err
		}
		if !update.Writable(exe) {
			if rerun, err := asRoot(cmd); rerun {
				return err
			}
			return ui.Hint(errors.New("can't write "+exe), "rerun pm update as the user who owns it")
		}
		err = spinner.New().Title(" downloading pm " + rel.Tag).
			ActionWithErr(func(ctx context.Context) error { return update.Install(ctx, rel, exe) }).
			Run()
		if err != nil {
			return err
		}
		ui.Success(out, "updated pm "+version+" → "+ui.Accent(rel.Tag))

		s, err := newService(cmd.String("home"), &daemon{})
		if err != nil {
			return err
		}
		if _, err := s.Status(); errors.Is(err, service.ErrNotInstalled) {
			return nil
		}
		// The new binary restarts the service through the same path as pm start.
		c := exec.Command(exe, "--home", cmd.String("home"), "start")
		c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
		return c.Run()
	},
}
