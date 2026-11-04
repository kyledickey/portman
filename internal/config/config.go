// Package config owns portman's settings file and the directory it shares
// with the log and Caddy's data.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/knadh/koanf/parsers/toml/v2"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/providers/structs"
	"github.com/knadh/koanf/v2"
)

type Config struct {
	Domain   string            `koanf:"domain"`
	Port     int               `koanf:"port"`
	Share    bool              `koanf:"share"`
	Forward  string            `koanf:"forward"`
	LogLevel string            `koanf:"log_level"`
	Apps     map[string]string `koanf:"apps"`
}

func Default() Config {
	return Config{
		Domain:   "localhost",
		Port:     80,
		LogLevel: "info",
		Apps:     map[string]string{},
	}
}

func DefaultDir() string {
	base, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(base, "portman")
}

func File(dir string) string    { return filepath.Join(dir, "config.toml") }
func LogFile(dir string) string { return filepath.Join(dir, "pm.log") }

// Load reads dir's config file over the defaults. A missing file is not an error.
func Load(dir string) (Config, error) {
	k := koanf.New(".")
	if err := k.Load(structs.Provider(Default(), "koanf"), nil); err != nil {
		return Config{}, err
	}
	if err := k.Load(file.Provider(File(dir)), toml.Parser()); err != nil &&
		!errors.Is(err, fs.ErrNotExist) {
		return Config{}, fmt.Errorf("read %s: %w", File(dir), err)
	}

	var c Config
	if err := k.Unmarshal("", &c); err != nil {
		return Config{}, fmt.Errorf("read %s: %w", File(dir), err)
	}
	if c.Apps == nil {
		c.Apps = map[string]string{}
	}
	if err := c.validate(); err != nil {
		return Config{}, fmt.Errorf("%s: %w", File(dir), err)
	}

	return c, nil
}

// Save writes c atomically so a running daemon never reads half a file.
func Save(dir string, c Config) error {
	k := koanf.New(".")
	if err := k.Load(structs.Provider(c, "koanf"), nil); err != nil {
		return err
	}
	b, err := k.Marshal(toml.Parser())
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".config-*.toml")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), File(dir))
}

func (c Config) validate() error {
	if _, err := ParseDomain(c.Domain); err != nil {
		return err
	}
	if _, err := ParsePort(strconv.Itoa(c.Port)); err != nil {
		return err
	}
	if c.Forward != "" {
		if _, err := ParseForward(c.Forward); err != nil {
			return err
		}
	}
	var level slog.Level
	if err := level.UnmarshalText([]byte(c.LogLevel)); err != nil {
		return fmt.Errorf("log_level %q must be debug, info, warn, or error", c.LogLevel)
	}
	for name, target := range c.Apps {
		if _, err := ParseName(name); err != nil {
			return err
		}
		if _, err := ParseTarget(target); err != nil {
			return fmt.Errorf("app %s: %w", name, err)
		}
	}
	return nil
}

var label = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

// ParseName accepts an app name: one DNS label, any case.
func ParseName(s string) (string, error) {
	s = strings.ToLower(s)
	if !label.MatchString(s) {
		return "", fmt.Errorf("%q is not a valid app name: use a-z 0-9 and '-' only", s)
	}
	return s, nil
}

// ParseDomain accepts the suffix apps live under, like "localhost" or "test"
func ParseDomain(s string) (string, error) {
	s = strings.Trim(strings.ToLower(s), ".")
	for _, part := range strings.Split(s, ".") {
		if !label.MatchString(part) {
			return "", fmt.Errorf("%q is not a valid domain", s)
		}
	}
	return s, nil
}

func ParsePort(s string) (int, error) {
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 || n > 65535 {
		return 0, fmt.Errorf("%q is not a port between 1 and 65535", s)
	}
	return n, nil
}

// ParseTarget turns "3000" or "host:3000" into a dialable host:port. A bare
// port means localhost, which reaches apps bbound to either 127.0.0.1 or ::1.
func ParseTarget(s string) (string, error) {
	if _, err := strconv.Atoi(s); err == nil {
		s = "localhost:" + s
	}
	host, port, err := net.SplitHostPort(s)
	if err != nil || host == "" {
		return "", fmt.Errorf("%q is not a port or a host:port", s)
	}
	if _, err := ParsePort(port); err != nil {
		return "", err
	}
	return net.JoinHostPort(host, port), nil
}

// ParseForward is ParseTarget for another portman machine: the port defaults to 80.
func ParseForward(s string) (string, error) {
	if _, _, err := net.SplitHostPort(s); err != nil {
		s = net.JoinHostPort(s, "80")
	}
	return ParseTarget(s)
}
