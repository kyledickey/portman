package proxy

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/kdicusb/portman/internal/config"
)

func TestRouting(t *testing.T) {
	api, web := backend(t, "api"), backend(t, "web")
	remote := backend(t, "remote")
	c := config.Default()
	c.Port = freePort(t)
	c.Apps = map[string]string{"api": api, "web": web, "down": closedAddr(t)}
	dir := start(t, c)

	for _, tc := range []struct {
		host, want string
		status     int
	}{
		{"api.localhost", "api", 200},
		{"API.Localhost", "api", 200},
		{"v1.api.localhost", "api", 200},
		{"a.b.c.web.localhost", "web", 200},
		{"myapi.localhost", "", 404},
		{"api.localhost.evil", "", 404},
		{"nope.localhost", "", 404},
		{"down.localhost", "", 502},
	} {
		status, body, hdr := get(t, c.Port, tc.host)
		if status != tc.status || !strings.HasPrefix(body, tc.want) {
			t.Errorf("%s: got %d %q, want %d %q", tc.host, status, body, tc.status, tc.want)
		}
		if tc.status != 200 && hdr.Get(Header) != "test" {
			t.Errorf("%s: portman page lacks %s header", tc.host, Header)
		}
		if tc.status != 200 && !strings.Contains(body, strings.ToLower(tc.host)) && !strings.Contains(body, tc.host) {
			t.Errorf("%s: page does not name the host: %q", tc.host, body)
		}
	}

	c.Forward = remote
	if err := config.Save(dir, c); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { _, body, _ := get(t, c.Port, "other.localhost"); return strings.HasPrefix(body, "remote") })
	for host, want := range map[string]string{
		"api.localhost":     "api",
		"other.localhost":   "remote host=other.localhost",
		"x.other.localhost": "remote host=x.other.localhost",
	} {
		if _, body, _ := get(t, c.Port, host); body != want && !strings.HasPrefix(body, want+" ") {
			t.Errorf("forwarding %s: got %q, want %q", host, body, want)
		}
	}
	if status, _, _ := get(t, c.Port, "example.com"); status != 404 {
		t.Errorf("forward must only take *.%s, example.com got %d", c.Domain, status)
	}
}

func TestCustomDomain(t *testing.T) {
	c := config.Default()
	c.Domain, c.Port = "test", freePort(t)
	c.Apps = map[string]string{"api": backend(t, "api")}
	start(t, c)
	if _, body, _ := get(t, c.Port, "api.test"); !strings.HasPrefix(body, "api") {
		t.Fatalf("api.test got %q", body)
	}
	if status, _, _ := get(t, c.Port, "api.localhost"); status != 404 {
		t.Fatalf("api.localhost must not match domain test, got %d", status)
	}
}

func TestProbe(t *testing.T) {
	c := config.Default()
	c.Port = freePort(t)
	if _, err := Probe(c.Port); err == nil {
		t.Fatal("Probe found portman on an empty port")
	}
	start(t, c)
	if v, err := Probe(c.Port); err != nil || v != "test" {
		t.Fatalf("Probe = %q, %v", v, err)
	}

	other := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(other.Close)
	port, _ := strconv.Atoi(other.URL[strings.LastIndex(other.URL, ":")+1:])
	if _, err := Probe(port); err == nil || !strings.Contains(err.Error(), "another program") {
		t.Fatalf("Probe of a foreign server = %v", err)
	}
}

func TestStartFollowsConfigFile(t *testing.T) {
	dir := t.TempDir()
	c := config.Default()
	c.Port = freePort(t)
	if err := config.Save(dir, c); err != nil {
		t.Fatal(err)
	}
	logf, err := os.Create(dir + "/pm.log")
	if err != nil {
		t.Fatal(err)
	}
	defer logf.Close()
	stop, err := Start(dir, "test", logf)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()

	c.Apps["api"] = backend(t, "api")
	if err := config.Save(dir, c); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { _, body, _ := get(t, c.Port, "api.localhost"); return strings.HasPrefix(body, "api") })

	if err := os.WriteFile(config.File(dir), []byte("port = \n"), 0o644); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	if _, body, _ := get(t, c.Port, "api.localhost"); !strings.HasPrefix(body, "api") {
		t.Fatalf("a broken config dropped the routes, got %q", body)
	}
	eventually(t, func() bool {
		b, _ := os.ReadFile(dir + "/pm.log")
		return strings.Contains(string(b), "config reload failed")
	})
	if b, _ := os.ReadFile(dir + "/pm.log"); strings.Contains(string(b), "server running") {
		t.Error("Caddy's startup chatter reached the log at info level")
	}

	delete(c.Apps, "api")
	if err := config.Save(dir, c); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { status, _, _ := get(t, c.Port, "api.localhost"); return status == 404 })
}

func TestStartFailsOnBusyPort(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	c := config.Default()
	c.Port = ln.Addr().(*net.TCPAddr).Port
	dir := t.TempDir()
	if err := config.Save(dir, c); err != nil {
		t.Fatal(err)
	}
	if stop, err := Start(dir, "test", io.Discard); err == nil {
		stop()
		t.Fatal("Start succeeded on a busy port")
	}
}

func TestStartRefusesSecondPortman(t *testing.T) {
	c := config.Default()
	c.Port = freePort(t)
	start(t, c)
	dir := t.TempDir()
	if err := config.Save(dir, c); err != nil {
		t.Fatal(err)
	}
	stop, err := Start(dir, "test", io.Discard)
	if err == nil {
		stop()
		t.Fatal("a second portman started on the same port")
	}
	if !errors.Is(err, ErrRunning) {
		t.Fatalf("err = %v, want ErrRunning", err)
	}
}

func start(t *testing.T, c config.Config) string {
	t.Helper()
	dir := t.TempDir()
	if err := config.Save(dir, c); err != nil {
		t.Fatal(err)
	}
	stop, err := Start(dir, "test", io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(stop)
	return dir
}

func backend(t *testing.T, name string) string {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "%s host=%s", name, r.Host)
	}))
	t.Cleanup(s.Close)
	return strings.TrimPrefix(s.URL, "http://")
}

func closedAddr(t *testing.T) string {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ln.Close()
	return ln.Addr().String()
}

func freePort(t *testing.T) int {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func get(t *testing.T, port int, host string) (int, string, http.Header) {
	t.Helper()
	req, _ := http.NewRequest("GET", "http://127.0.0.1:"+strconv.Itoa(port)+"/", nil)
	req.Host = host
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", host, err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b), resp.Header
}

func eventually(t *testing.T, ok func() bool) {
	t.Helper()
	for range 50 {
		if ok() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("condition not met within 2.5s")
}
