package config

import (
	"os"
	"strings"
	"testing"
)

func TestLoadMissingFileGivesDefaults(t *testing.T) {
	c, err := Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if c.Domain != "localhost" || c.Port != 80 || c.Share || c.Forward != "" || c.LogLevel != "info" || len(c.Apps) != 0 {
		t.Fatalf("defaults = %+v", c)
	}
	if c.Apps == nil {
		t.Fatal("Apps must be a usable map")
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	want := Config{
		Domain: "test", Port: 8080, Share: true, Forward: "my-vm.mshome.net:80", LogLevel: "debug",
		Apps: map[string]string{"api": "localhost:8080", "web-2": "10.0.0.5:3000"},
	}
	if err := Save(dir, want); err != nil {
		t.Fatal(err)
	}
	got, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.Domain != want.Domain || got.Port != want.Port || got.Share != want.Share ||
		got.Forward != want.Forward || got.LogLevel != want.LogLevel || len(got.Apps) != 2 ||
		got.Apps["api"] != "localhost:8080" || got.Apps["web-2"] != "10.0.0.5:3000" {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestSaveCreatesDirAndLeavesNoTempFiles(t *testing.T) {
	dir := t.TempDir() + "/nested/portman"
	if err := Save(dir, Default()); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "config.toml" {
		t.Fatalf("dir holds %v, want only config.toml", entries)
	}
}

func TestLoadUsesFileOverDefaults(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "port = 8000\n\n[apps]\napi = \"localhost:9000\"\n")
	c, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if c.Port != 8000 || c.Domain != "localhost" || c.Apps["api"] != "localhost:9000" {
		t.Fatalf("got %+v", c)
	}
}

func TestLoadRejectsInvalidFileNamingIt(t *testing.T) {
	for name, body := range map[string]string{
		"bad toml":   "port = \n",
		"bad port":   "port = 70000\n",
		"bad domain": "domain = \"no spaces\"\n",
		"bad level":  "log_level = \"loud\"\n",
		"bad app":    "[apps]\n\"Not_OK\" = \"localhost:1\"\n",
		"bad target": "[apps]\napi = \"localhost\"\n",
		"bad fwd":    "forward = \"host:notaport\"\n",
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			write(t, dir, body)
			_, err := Load(dir)
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.Contains(err.Error(), File(dir)) {
				t.Fatalf("error %q does not name the file", err)
			}
		})
	}
}

func TestParseName(t *testing.T) {
	for in, want := range map[string]string{"api": "api", "My-App": "my-app", "a1": "a1", "9": "9"} {
		if got, err := ParseName(in); err != nil || got != want {
			t.Errorf("ParseName(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"", "-api", "api-", "a.b", "a_b", "a b", strings.Repeat("a", 64)} {
		if _, err := ParseName(in); err == nil {
			t.Errorf("ParseName(%q) accepted", in)
		}
	}
}

func TestParseDomain(t *testing.T) {
	for in, want := range map[string]string{"localhost": "localhost", ".Test": "test", "dev.example.com.": "dev.example.com"} {
		if got, err := ParseDomain(in); err != nil || got != want {
			t.Errorf("ParseDomain(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"", "a..b", "has space", "x_y"} {
		if _, err := ParseDomain(in); err == nil {
			t.Errorf("ParseDomain(%q) accepted", in)
		}
	}
}

func TestParsePort(t *testing.T) {
	for _, in := range []string{"0", "65536", "-1", "http", ""} {
		if _, err := ParsePort(in); err == nil {
			t.Errorf("ParsePort(%q) accepted", in)
		}
	}
	if n, err := ParsePort("443"); err != nil || n != 443 {
		t.Errorf("ParsePort(443) = %d, %v", n, err)
	}
}

func TestParseTarget(t *testing.T) {
	for in, want := range map[string]string{
		"3000":             "localhost:3000",
		"127.0.0.1:8080":   "127.0.0.1:8080",
		"api.internal:443": "api.internal:443",
		"[::1]:5173":       "[::1]:5173",
	} {
		if got, err := ParseTarget(in); err != nil || got != want {
			t.Errorf("ParseTarget(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"", "localhost", ":3000", "host:0", "host:99999", "70000", "http://localhost:3000"} {
		if _, err := ParseTarget(in); err == nil {
			t.Errorf("ParseTarget(%q) accepted", in)
		}
	}
}

func TestParseForward(t *testing.T) {
	for in, want := range map[string]string{
		"my-vm.mshome.net":  "my-vm.mshome.net:80",
		"192.168.1.20:8080": "192.168.1.20:8080",
		"::1":               "[::1]:80",
		"devbox.lan":        "devbox.lan:80",
	} {
		if got, err := ParseForward(in); err != nil || got != want {
			t.Errorf("ParseForward(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	if _, err := ParseForward("host:nope"); err == nil {
		t.Error("ParseForward accepted a bad port")
	}
}

func write(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.WriteFile(File(dir), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
