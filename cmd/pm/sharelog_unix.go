//go:build !windows

package main

import (
	"errors"
	"io/fs"
	"os"
	"syscall"

	"github.com/kdicusb/portman/internal/config"
)

// shareLog keeps pm.log readable by dir's owner when the service runs as root,
// as it does on macOS. lumberjack would create it 0600 and owned by root, and
// it keeps that mode when it rotates.
func shareLog(dir string) error {
	if os.Geteuid() != 0 {
		return nil
	}
	fi, err := os.Stat(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok || st.Uid == 0 {
		return nil
	}
	path := config.LogFile(dir)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	f.Close()
	if err := os.Chmod(path, 0o644); err != nil {
		return err
	}
	return os.Chown(path, int(st.Uid), int(st.Gid))
}
