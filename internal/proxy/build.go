// Package proxy turns a portman config into a running embedded Caddy.
package proxy

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"time"

	"github.com/kdicusb/portman/internal/config"
)

// Header marks every page portman itself serves, carrying the daemon's version.
const Header = "X-Portman"

type obj = map[string]any

// Listen is the address portman binds: loopback only, unless sharing.
func Listen(c config.Config) string {
	if c.Share {
		return ":" + strconv.Itoa(c.Port)
	}
	return "127.0.0.1:" + strconv.Itoa(c.Port)
}

// Build renders c as Caddy JSON. Each app matches its own name and every name
// under it, so web.localhost and a.b.web.localhost both reach web.
func Build(c config.Config, version string) ([]byte, error) {
	var routes []any
	for _, name := range slices.Sorted(maps.Keys(c.Apps)) {
		routes = append(routes,
			proxyTo(`^(.+\.)?`+regexp.QuoteMeta(name+"."+c.Domain)+`$`,
				c.Apps[name],
			),
		)
	}
	if c.Forward != "" {
		routes = append(routes,
			proxyTo(`^.+\.`+regexp.QuoteMeta(c.Domain)+`$`, c.Forward),
		)
	}
	routes = append(routes, page(404, "{http.request.host} isn't mapped",
		"Run <code>pm list</code> to see your apps, or <code>pm add NAME PORT</code> to map this one.", version,
	))

	return json.Marshal(obj{
		"admin": obj{
			"disabled": true,
			"config": obj{
				"persist": false,
			}},
		"logging": obj{"logs": obj{"default": caddyLog(c.LogLevel)}},
		"apps": obj{"http": obj{"servers": obj{
			"portman": obj{
				"listen": []string{Listen(c)},
				"automatic_https": obj{
					"disable": true,
				},
				"routes": routes,
				"errors": obj{"routes": []any{
					page("{http.error.status_code}",
						"{http.request.host} isn't responding",
						"portman reached no app behind this name. Is it running?",
						version,
					)}}}}}},
	})
}

func proxyTo(hostPattern, upstream string) obj {
	return obj{
		"match": []any{obj{"vars_regexp": obj{
			"{http.request.host}": obj{
				"pattern": "(?i)" + hostPattern}}}},
		"handle": []any{obj{
			"handler":   "reverse_proxy",
			"upstreams": []any{obj{"dial": upstream}},
		}},
	}
}

func page(status any, title, body, version string) obj {
	return obj{"handle": []any{obj{
		"handler":     "static_response",
		"status_code": status,
		"headers": obj{
			"Content-Type": []string{"text/html; charset=utf-8"},
			Header:         []string{version}},
		"body": fmt.Sprintf(pageHTML, title, title, body),
	}}}
}

const pageHTML = `
<!doctype html>
	<meta charset=utf-8>
	<meta name=viewport content="width=device-width">
	<title>%s</title>
<body style="font:16px/1.5 system-ui,sans-serif;max-width:34rem;margin:18vh auto;padding:0 1.5rem;color:#334155">
	<h1 style="font-size:1.35rem;color:#14b8a6">%s</h1>
	<p>%s</p>
</body>`

// caddyLog keeps Caddy's startup chatter (HTTP/2 needs TLS, admin disabled)
// out of the log unless you ask for debug.
func caddyLog(level string) obj {
	log := obj{"writer": obj{"output": "portman"}, "level": levelOf(level)}
	if level != "debug" {
		log["include"] = []string{"http.handlers", "http.log.error"}
	}
	return log
}

// Caddy names its levels in capitals; the config uses slog's lower case.
func levelOf(s string) string {
	switch s {
	case "debug":
		return "DEBUG"
	case "warn":
		return "WARN"
	case "error":
		return "ERROR"
	}
	return "INFO"
}

var (
	ErrNotRunning = errors.New("portman isn't running")
	ErrRunning    = errors.New("portman is already running")
)

// Probe asks whatever listens on port whether it is portman and returns its version.
func Probe(port int) (string, error) {
	client := http.Client{Timeout: 500 * time.Millisecond}
	resp, err := client.Get("http://127.0.0.1:" + strconv.Itoa(port) + "/")
	if err != nil {
		return "", ErrNotRunning
	}
	resp.Body.Close()
	v := resp.Header.Get(Header)
	if v == "" {
		return "", fmt.Errorf("another program is using port %d", port)
	}
	return v, nil
}
