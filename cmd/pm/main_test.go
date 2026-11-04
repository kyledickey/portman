package main

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kdicusb/portman/internal/config"
	"github.com/kdicusb/portman/internal/proxy"
	"github.com/kdicusb/portman/internal/ui"
)

func TestAddListDelete(t *testing.T) {
	dir := home(t)

	out := run(t, dir, "add", "API", "8080")
	mustContain(t, out, "http://api.localhost:", "localhost:8080")
	mustContain(t, out, "isn't running")

	out = run(t, dir, "add", "web", "10.0.0.5:3000")
	mustContain(t, out, "http://web.localhost", "10.0.0.5:3000")

	out = run(t, dir, "add", "api", "9090")
	mustContain(t, out, "localhost:9090", "was localhost:8080")

	out = run(t, dir, "list")
	mustContain(t, out, "api", "localhost:9090", "web", "10.0.0.5:3000", "isn't running")

	out = run(t, dir, "rm", "web")
	mustContain(t, out, "removed web")
	c := load(t, dir)
	if len(c.Apps) != 1 || c.Apps["api"] != "localhost:9090" {
		t.Fatalf("apps = %v", c.Apps)
	}
}

func TestBadInputLeavesConfigAlone(t *testing.T) {
	dir := home(t)
	for _, args := range [][]string{
		{"add", "api"},
		{"add", "bad_name", "8080"},
		{"add", "api", "http://localhost:3000"},
		{"add", "api", "99999"},
		{"delete", "nope"},
		{"forward"},
		{"forward", "host:nope"},
	} {
		if _, err := runErr(dir, args...); err == nil {
			t.Errorf("pm %v succeeded", args)
		}
	}
	if c := load(t, dir); len(c.Apps) != 0 || c.Forward != "" {
		t.Fatalf("bad input changed the config: %+v", c)
	}
}

func TestErrorsCarryHints(t *testing.T) {
	_, err := runErr(home(t), "delete", "nope")
	var buf bytes.Buffer
	ui.Error(&buf, err)
	mustContain(t, buf.String(), "no app named nope", "pm list")
}

func TestForward(t *testing.T) {
	dir := home(t)
	out := run(t, dir, "forward", "my-vm.mshome.net")
	mustContain(t, out, "my-vm.mshome.net:80")
	if c := load(t, dir); c.Forward != "my-vm.mshome.net:80" {
		t.Fatalf("forward = %q", c.Forward)
	}
	mustContain(t, run(t, dir, "list"), "other *.localhost names", "my-vm.mshome.net:80")

	mustContain(t, run(t, dir, "forward", "--off"), "forwarding off")
	if c := load(t, dir); c.Forward != "" {
		t.Fatalf("forward = %q after --off", c.Forward)
	}
}

func TestListEmpty(t *testing.T) {
	mustContain(t, run(t, home(t), "list"), "no apps yet", "pm add")
}

func TestListAgainstRunningPortman(t *testing.T) {
	dir := home(t)
	live := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(live.Close)
	run(t, dir, "add", "live", strings.TrimPrefix(live.URL, "http://"))
	run(t, dir, "add", "dead", closedAddr(t))

	stop, err := proxy.Start(dir, version, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()

	out := run(t, dir, "list")
	mustContain(t, out, "portman "+version, "127.0.0.1:")
	lines := strings.Split(out, "\n")
	for _, want := range []struct{ app, state string }{{"live", "up"}, {"dead", "down"}} {
		found := false
		for _, l := range lines {
			if strings.Contains(l, want.app+".localhost") {
				found = true
				if !strings.HasSuffix(strings.TrimSpace(l), want.state) {
					t.Errorf("%s row %q should end in %s", want.app, l, want.state)
				}
			}
		}
		if !found {
			t.Errorf("no row for %s in:\n%s", want.app, out)
		}
	}

	if out := run(t, dir, "add", "another", "3000"); strings.Contains(out, "isn't running") {
		t.Errorf("add warned while portman runs: %s", out)
	}
}

func TestConfigNeedsTerminal(t *testing.T) {
	_, err := runErr(home(t), "config")
	if err == nil || !strings.Contains(err.Error(), "terminal") {
		t.Fatalf("err = %v", err)
	}
}

// home gives each test its own portman dir on a free port, so nothing it
// starts collides with a real portman on :80.
func home(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	c := config.Default()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	c.Port = ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	if err := config.Save(dir, c); err != nil {
		t.Fatal(err)
	}
	return dir
}

func run(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := runErr(dir, args...)
	if err != nil {
		t.Fatalf("pm %v: %v\n%s", args, err, out)
	}
	return out
}

func runErr(dir string, args ...string) (string, error) {
	var buf bytes.Buffer
	cmd := app()
	cmd.Writer, cmd.ErrWriter = &buf, &buf
	err := cmd.Run(context.Background(), append([]string{"pm", "--home", dir}, args...))
	return buf.String(), err
}

func load(t *testing.T, dir string) config.Config {
	t.Helper()
	c, err := config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func mustContain(t *testing.T, s string, subs ...string) {
	t.Helper()
	for _, sub := range subs {
		if !strings.Contains(s, sub) {
			t.Errorf("output lacks %q:\n%s", sub, s)
		}
	}
}

func closedAddr(t *testing.T) string {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ln.Close()
	return ln.Addr().String()
}

func TestLogsPrettyPrintsTail(t *testing.T) {
	dir := home(t)
	writeLog(t, dir,
		`{"time":"2026-11-04T11:12:57-07:00","level":"INFO","msg":"first"}`,
		`{"time":"2026-11-04T11:12:58-07:00","level":"INFO","msg":"routes loaded","apps":2,"forward":""}`,
		`{"level":"error","ts":1793815977.25,"logger":"http.log.error","msg":"dial tcp: connection refused","status":502}`,
		`not json`,
	)
	out := run(t, dir, "logs", "-n", "3")
	if strings.Contains(out, "first") {
		t.Errorf("-n 3 showed the 4th-newest line:\n%s", out)
	}
	mustContain(t, out,
		"INFO   routes loaded apps=2 forward=",
		"ERROR  http.log.error: dial tcp: connection refused status=502",
		"not json",
	)
	if strings.Contains(out, `"msg"`) {
		t.Errorf("raw JSON leaked into the output:\n%s", out)
	}
}

func TestLogsWithoutLog(t *testing.T) {
	_, err := runErr(home(t), "logs")
	if err == nil || !strings.Contains(err.Error(), "no log yet") {
		t.Fatalf("err = %v", err)
	}
}

func TestLogsFollow(t *testing.T) {
	dir := home(t)
	writeLog(t, dir, `{"level":"INFO","msg":"before"}`)
	var buf syncBuffer
	cmd := app()
	cmd.Writer, cmd.ErrWriter = &buf, &buf
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error)
	go func() { done <- cmd.Run(ctx, []string{"pm", "--home", dir, "logs", "-f"}) }()

	f, err := os.OpenFile(config.LogFile(dir), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString(`{"level":"INFO","msg":"after`)
	f.WriteString(`ward"}` + "\n")
	f.Close()
	for i := 0; i < 40 && !strings.Contains(buf.String(), "afterward"); i++ {
		time.Sleep(50 * time.Millisecond)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	mustContain(t, buf.String(), "before", "afterward")
}

func TestSettingsSaveOnlyValidChanges(t *testing.T) {
	dir := home(t)
	c := load(t, dir)
	s := newSettings(dir, c)

	*s.port = "nope"
	s.c.Share = true
	s.save()
	if load(t, dir).Share {
		t.Fatal("saved while the port was invalid")
	}

	*s.port = "8080"
	s.c.Forward = "devbox.lan"
	s.save()
	got := load(t, dir)
	if got.Port != 8080 || !got.Share || got.Forward != "devbox.lan:80" || !s.changed {
		t.Fatalf("saved %+v", got)
	}
}

func writeLog(t *testing.T, dir string, lines ...string) {
	t.Helper()
	if err := os.WriteFile(config.LogFile(dir), []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}
