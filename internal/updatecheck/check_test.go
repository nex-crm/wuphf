package updatecheck

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nex-crm/wuphf/internal/buildinfo"
)

func TestIsNewer(t *testing.T) {
	cases := []struct {
		candidate string
		current   string
		want      bool
	}{
		{"0.21.0", "0.20.2", true},
		{"0.20.2", "0.20.2", false},
		{"0.20.1", "0.20.2", false},
		{"1.0.0", "0.99.99", true},
		{"0.20.10", "0.20.2", true},      // numeric not lexical
		{"0.21.0-rc.1", "0.20.2", true},   // pre-release on newer numeric
		{"0.20.2", "0.20.2-rc.1", false},  // same numeric core; pre-release tail ignored
	}
	for _, tc := range cases {
		t.Run(tc.candidate+"_vs_"+tc.current, func(t *testing.T) {
			if got := isNewer(tc.candidate, tc.current); got != tc.want {
				t.Fatalf("isNewer(%q, %q) = %v want %v", tc.candidate, tc.current, got, tc.want)
			}
		})
	}
}

func TestDisabledRespectsEnv(t *testing.T) {
	cases := map[string]bool{
		"":      false,
		"0":     false,
		"false": false,
		"no":    false,
		"off":   false,
		"1":     true,
		"true":  true,
		"yes":   true,
	}
	for v, want := range cases {
		t.Run("env="+v, func(t *testing.T) {
			t.Setenv(envDisable, v)
			if got := Disabled(); got != want {
				t.Fatalf("Disabled() with %s=%q: got %v want %v", envDisable, v, got, want)
			}
		})
	}
}

// Banner must stay silent for dev builds — otherwise `go run` /
// unreleased snapshots would flag themselves as behind every release.
func TestBannerSuppressedForDevBuilds(t *testing.T) {
	withIsolatedHome(t)
	writeCacheForTest(t, cache{CheckedAt: time.Now(), LatestVersion: "v99.0.0"})

	prev := buildinfo.Version
	t.Cleanup(func() { buildinfo.Version = prev })

	for _, v := range []string{"", "dev", "0.1.0"} {
		buildinfo.Version = v
		if b := Banner(); b != "" {
			t.Fatalf("expected empty banner for dev version %q, got %q", v, b)
		}
	}
}

func TestBannerShownWhenBehind(t *testing.T) {
	withIsolatedHome(t)
	writeCacheForTest(t, cache{CheckedAt: time.Now(), LatestVersion: "v0.25.0"})

	prev := buildinfo.Version
	buildinfo.Version = "0.20.2"
	t.Cleanup(func() { buildinfo.Version = prev })

	b := Banner()
	if !strings.Contains(b, "v0.25.0") {
		t.Fatalf("banner missing latest version: %q", b)
	}
	if !strings.Contains(b, "v0.20.2") {
		t.Fatalf("banner missing current version: %q", b)
	}
	if !strings.Contains(b, "install.sh") {
		t.Fatalf("banner missing upgrade instructions: %q", b)
	}
}

func TestBannerEmptyWhenUpToDate(t *testing.T) {
	withIsolatedHome(t)
	writeCacheForTest(t, cache{CheckedAt: time.Now(), LatestVersion: "v0.20.2"})

	prev := buildinfo.Version
	buildinfo.Version = "0.20.2"
	t.Cleanup(func() { buildinfo.Version = prev })

	if b := Banner(); b != "" {
		t.Fatalf("expected empty banner when up to date, got %q", b)
	}
}

func TestBannerEmptyWhenDisabled(t *testing.T) {
	withIsolatedHome(t)
	writeCacheForTest(t, cache{CheckedAt: time.Now(), LatestVersion: "v99.0.0"})
	t.Setenv(envDisable, "1")

	prev := buildinfo.Version
	buildinfo.Version = "0.20.2"
	t.Cleanup(func() { buildinfo.Version = prev })

	if b := Banner(); b != "" {
		t.Fatalf("expected empty banner when disabled, got %q", b)
	}
}

// End-to-end refresh against a mock GitHub-like redirect server.
func TestRefreshAsyncWritesCacheFromRedirect(t *testing.T) {
	withIsolatedHome(t)

	// Mock: /releases/latest -> 302 /releases/tag/v0.99.0
	mux := http.NewServeMux()
	mux.HandleFunc("/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/releases/tag/v0.99.0", http.StatusFound)
	})
	mux.HandleFunc("/releases/tag/v0.99.0", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	t.Setenv(envOverrideURL, srv.URL+"/releases/latest")

	// Force the disabled flag off even if the ambient env has it set.
	t.Setenv(envDisable, "")

	RefreshAsync(context.Background())

	// Poll until the cache file appears or fail after a short deadline.
	deadline := time.Now().Add(2 * time.Second)
	var got cache
	for time.Now().Before(deadline) {
		got = readCache()
		if got.LatestVersion != "" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if got.LatestVersion != "v0.99.0" {
		t.Fatalf("expected cache.LatestVersion=v0.99.0, got %+v", got)
	}
}

func TestRefreshAsyncSkipsFreshCache(t *testing.T) {
	withIsolatedHome(t)
	writeCacheForTest(t, cache{CheckedAt: time.Now(), LatestVersion: "v0.1.2"})

	// If the fresh-cache guard is broken this would panic or hit the network.
	// Point the override at an unroutable address so any network call fails
	// loudly with a Dial error.
	t.Setenv(envOverrideURL, "http://127.0.0.1:1") // port 1 typically refuses
	t.Setenv(envDisable, "")

	RefreshAsync(context.Background())
	time.Sleep(100 * time.Millisecond) // let the (suppressed) goroutine not happen

	c := readCache()
	if c.LatestVersion != "v0.1.2" {
		t.Fatalf("fresh cache was overwritten: %+v", c)
	}
}

// withIsolatedHome redirects $HOME so cachePath() writes to a temp
// directory and doesn't touch the developer's real ~/.wuphf/.
func withIsolatedHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	return dir
}

func writeCacheForTest(t *testing.T, c cache) {
	t.Helper()
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".wuphf")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdirall: %v", err)
	}
	raw, _ := json.Marshal(c)
	if err := os.WriteFile(filepath.Join(dir, cacheFileName), raw, 0o644); err != nil {
		t.Fatalf("write cache: %v", err)
	}
}
