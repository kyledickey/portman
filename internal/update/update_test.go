package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestLatestPicksNewestOnChannel(t *testing.T) {
	serve(t, []Release{
		{Tag: "v0.2.0-beta.10", Prerelease: true},
		{Tag: "v0.2.0-beta.9", Prerelease: true},
		{Tag: "v0.1.1"},
		{Tag: "v0.3.0", Draft: true},
		{Tag: "nonsense"},
		{Tag: "v0.1.0"},
	}, nil)
	for beta, want := range map[bool]string{false: "v0.1.1", true: "v0.2.0-beta.10"} {
		r, err := Latest(context.Background(), beta)
		if err != nil || r.Tag != want {
			t.Errorf("Latest(beta=%v) = %q, %v; want %q", beta, r.Tag, err, want)
		}
	}
}

func TestLatestWithNoReleases(t *testing.T) {
	serve(t, []Release{{Tag: "v1.0.0-beta.1", Prerelease: true}}, nil)
	if _, err := Latest(context.Background(), false); err == nil {
		t.Fatal("expected an error with no stable release")
	}
}

func TestNewer(t *testing.T) {
	for _, tc := range []struct {
		tag, current string
		want         bool
	}{
		{"v0.1.1", "v0.1.0", true},
		{"v0.1.0", "v0.1.0", false},
		{"v0.1.0", "v0.2.0-beta.1", false},
		{"v0.2.0", "v0.2.0-beta.3", true},
		{"v0.1.0", "dev", true},
	} {
		if got := Newer(tc.tag, tc.current); got != tc.want {
			t.Errorf("Newer(%q, %q) = %v", tc.tag, tc.current, got)
		}
	}
}

func TestInstallVerifiesAndReplaces(t *testing.T) {
	bin := []byte("new pm binary")
	sum := sha256.Sum256(bin)
	name := AssetName("v1.0.0")
	assets := map[string][]byte{
		name:         bin,
		"SHA256SUMS": fmt.Appendf(nil, "%s  %s\n%s  other\n", hex.EncodeToString(sum[:]), name, strings.Repeat("0", 64)),
	}
	srv := serve(t, nil, assets)
	rel := Release{Tag: "v1.0.0", Assets: []Asset{{name, srv.URL + "/" + name}, {"SHA256SUMS", srv.URL + "/SHA256SUMS"}}}

	exe := filepath.Join(t.TempDir(), "pm")
	if err := os.WriteFile(exe, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Install(context.Background(), rel, exe); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(exe); string(got) != string(bin) {
		t.Fatalf("exe holds %q", got)
	}
	if info, _ := os.Stat(exe); runtime.GOOS != "windows" && info.Mode().Perm()&0o100 == 0 {
		t.Fatalf("exe is not executable: %v", info.Mode())
	}

	assets[name] = []byte("tampered")
	if err := os.WriteFile(exe, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Install(context.Background(), rel, exe); err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("tampered binary: err = %v", err)
	}
	if got, _ := os.ReadFile(exe); string(got) != "old" {
		t.Fatalf("a failed update changed exe to %q", got)
	}
}

func TestInstallNeedsAssetForThisPlatform(t *testing.T) {
	rel := Release{Tag: "v1.0.0", Assets: []Asset{{"pm_v1.0.0_plan9_mips", "x"}, {"SHA256SUMS", "y"}}}
	if err := Install(context.Background(), rel, filepath.Join(t.TempDir(), "pm")); err == nil {
		t.Fatal("expected an error for a missing asset")
	}
}

func TestWritable(t *testing.T) {
	dir := t.TempDir()
	if !Writable(filepath.Join(dir, "pm")) {
		t.Fatal("temp dir should be writable")
	}
	if Writable(filepath.Join(dir, "missing", "pm")) {
		t.Fatal("a missing dir is not writable")
	}
}

// serve fakes the GitHub API and asset downloads for one test.
func serve(t *testing.T, releases []Release, assets map[string][]byte) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/releases" {
			json.NewEncoder(w).Encode(releases)
			return
		}
		b, ok := assets[strings.TrimPrefix(r.URL.Path, "/")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Write(b)
	}))
	t.Cleanup(srv.Close)
	old := API
	API = srv.URL
	t.Cleanup(func() { API = old })
	return srv
}
