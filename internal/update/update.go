// Package update replaces the running pm binary with a GitHub release.
package update

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"golang.org/x/mod/semver"
)

// API is GitHub's REST root; tests point it at a fake.
var API = "https://api.github.com/repos/kdicusb/portman"

type Release struct {
	Tag        string  `json:"tag_name"`
	Prerelease bool    `json:"prerelease"`
	Draft      bool    `json:"draft"`
	Assets     []Asset `json:"assets"`
}

type Asset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
}

// Latest returns the newest stable release, or the newest of any kind when beta is set.
func Latest(ctx context.Context, beta bool) (Release, error) {
	var all []Release
	if err := getJSON(ctx, API+"/releases?per_page=50", &all); err != nil {
		return Release{}, err
	}
	var best Release
	for _, r := range all {
		if r.Draft || (r.Prerelease && !beta) || !semver.IsValid(r.Tag) {
			continue
		}
		if best.Tag == "" || semver.Compare(r.Tag, best.Tag) > 0 {
			best = r
		}
	}
	if best.Tag == "" {
		return Release{}, fmt.Errorf("no releases found")
	}
	return best, nil
}

// Newer reports whether release tag is newer than the running version.
func Newer(tag, current string) bool {
	return !semver.IsValid(current) || semver.Compare(tag, current) > 0
}

// AssetName is the binary a release publishes for this OS and arch.
func AssetName(tag string) string {
	name := fmt.Sprintf("pm_%s_%s_%s", tag, runtime.GOOS, runtime.GOARCH)
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return name
}

// Install downloads r's binary, checks it against the releases SHA256SUMS,
// and swaps it in for exe.
func Install(ctx context.Context, r Release, exe string) error {
	urls := map[string]string{}
	for _, a := range r.Assets {
		urls[a.Name] = a.URL
	}
	name := AssetName(r.Tag)
	if urls[name] == "" || urls["SHA256SUMS"] == "" {
		return fmt.Errorf("release %s has no %s", r.Tag, name)
	}
	sums, err := get(ctx, urls["SHA256SUMS"])
	if err != nil {
		return err
	}
	want, err := sumFor(sums, name)
	if err != nil {
		return err
	}
	bin, err := get(ctx, urls[name])
	if err != nil {
		return err
	}
	if got := sha256.Sum256(bin); hex.EncodeToString(got[:]) != want {
		return fmt.Errorf("checksums mismatch for %s", name)
	}

	tmp := exe + ".new"
	if err := os.WriteFile(tmp, bin, 0o755); err != nil {
		return err
	}
	// Windows can't replace a running executable, but it can rename one.
	old := exe + ".old"
	if runtime.GOOS == "windows" {
		os.Remove(old)
		if err := os.Rename(exe, old); err != nil {
			os.Remove(tmp)
			return err
		}
	}
	if err := os.Rename(tmp, exe); err != nil {
		os.Remove(tmp)
		if runtime.GOOS == "windows" {
			os.Rename(old, exe)
		}
		return err
	}
	return nil
}

// Writable reports whether pm can replace exe without elevated rights.
func Writable(exe string) bool {
	f, err := os.CreateTemp(filepath.Dir(exe), ".pm-update-*")
	if err != nil {
		return false
	}
	f.Close()
	os.Remove(f.Name())
	return true
}

func sumFor(sums []byte, name string) (string, error) {
	sc := bufio.NewScanner(strings.NewReader(string(sums)))
	for sc.Scan() {
		if f := strings.Fields(sc.Text()); len(f) == 2 && strings.TrimPrefix(f[1], "*") == name {
			return f[0], nil
		}
	}
	return "", fmt.Errorf("SHA256SUMS has no entry for %s", name)
}

func get(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	return io.ReadAll(resp.Body)
}

func getJSON(ctx context.Context, url string, v any) error {
	b, err := get(ctx, url)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}
