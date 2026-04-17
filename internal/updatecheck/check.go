// Package updatecheck handles the passive "new version available" banner
// that prints on startup when a newer release exists on GitHub.
//
// Design notes:
//
//   - This is the ONE phone-home path wuphf makes to github.com. It
//     exists because we've already shipped broken versions (v0.8.1's 404
//     install) that silently stranded users on a dead binary. The
//     website says "no telemetry" — and this honors that: no payload is
//     sent to github.com, we read the URL GitHub redirects us to and
//     extract the tag from the path. No analytics.
//
//   - The refresh is fire-and-forget on a goroutine with a short
//     timeout. The banner reads from disk cache and is synchronous.
//     First startup after a release lands: refresh writes cache, no
//     banner yet. Next startup: banner shows.
//
//   - Dev builds (buildinfo.Version unset, "dev", or the baked-in
//     "0.1.0") suppress the banner, otherwise `go run` / unreleased
//     snapshots would compare as perpetually behind.
//
//   - WUPHF_NO_UPDATE_CHECK=1 disables everything — both the refresh
//     and the banner.
package updatecheck

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nex-crm/wuphf/internal/buildinfo"
)

const (
	cacheFileName = "update-check.json"
	defaultURL    = "https://github.com/nex-crm/wuphf/releases/latest"
	cacheTTL      = 24 * time.Hour
	httpTimeout   = 5 * time.Second
	envDisable    = "WUPHF_NO_UPDATE_CHECK"
	envOverrideURL = "WUPHF_UPDATE_CHECK_URL" // tests only
)

type cache struct {
	CheckedAt     time.Time `json:"checked_at"`
	LatestVersion string    `json:"latest_version"`
}

// Disabled reports whether the user has opted out of version checks via
// WUPHF_NO_UPDATE_CHECK. Accepts the same truthiness heuristic as the
// rest of wuphf: anything other than "", "0", or "false" is ON.
func Disabled() bool {
	v := strings.TrimSpace(os.Getenv(envDisable))
	if v == "" {
		return false
	}
	switch strings.ToLower(v) {
	case "0", "false", "no", "off":
		return false
	}
	return true
}

func cachePath() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	dir := filepath.Join(home, ".wuphf")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return ""
	}
	return filepath.Join(dir, cacheFileName)
}

func readCache() cache {
	p := cachePath()
	if p == "" {
		return cache{}
	}
	raw, err := os.ReadFile(p)
	if err != nil {
		return cache{}
	}
	var c cache
	_ = json.Unmarshal(raw, &c)
	return c
}

func writeCache(c cache) {
	p := cachePath()
	if p == "" {
		return
	}
	raw, err := json.Marshal(c)
	if err != nil {
		return
	}
	_ = os.WriteFile(p, raw, 0o644)
}

// fetchLatest hits the releases/latest URL, follows GitHub's redirect,
// and extracts the tag from the final URL path. Returns the tag with
// the "v" prefix intact (matches goreleaser's tag format).
func fetchLatest(ctx context.Context) (string, error) {
	url := strings.TrimSpace(os.Getenv(envOverrideURL))
	if url == "" {
		url = defaultURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	client := &http.Client{Timeout: httpTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	path := resp.Request.URL.Path
	i := strings.LastIndex(path, "/")
	if i < 0 || i == len(path)-1 {
		return "", fmt.Errorf("unexpected release URL path: %q", path)
	}
	tag := path[i+1:]
	if !strings.HasPrefix(tag, "v") {
		return "", fmt.Errorf("unexpected tag format (want v-prefix): %q", tag)
	}
	return tag, nil
}

// RefreshAsync kicks off a background check if the cache is stale (older
// than cacheTTL) or missing. Non-blocking; callers must NOT wait on it.
// Returns immediately when disabled, when the cache is fresh, or when
// the cache directory isn't writable.
func RefreshAsync(ctx context.Context) {
	if Disabled() {
		return
	}
	c := readCache()
	if !c.CheckedAt.IsZero() && time.Since(c.CheckedAt) < cacheTTL {
		return
	}
	go func() {
		fetchCtx, cancel := context.WithTimeout(ctx, httpTimeout)
		defer cancel()
		tag, err := fetchLatest(fetchCtx)
		if err != nil {
			return
		}
		writeCache(cache{CheckedAt: time.Now().UTC(), LatestVersion: tag})
	}()
}

// Banner returns a one-line "new version available" notice, or "" when
// nothing needs to be said. Safe to print to stderr verbatim.
func Banner() string {
	if Disabled() {
		return ""
	}
	current := buildinfo.Current().Version
	if isDevVersion(current) {
		return ""
	}
	c := readCache()
	if c.LatestVersion == "" {
		return ""
	}
	latest := strings.TrimPrefix(c.LatestVersion, "v")
	if !isNewer(latest, current) {
		return ""
	}
	return fmt.Sprintf(
		"WUPHF %s available (you're on v%s). Upgrade: curl -fsSL https://raw.githubusercontent.com/nex-crm/wuphf/main/scripts/install.sh | sh",
		c.LatestVersion, current,
	)
}

// isDevVersion returns true for unreleased builds that should never show
// an "upgrade to vX.Y.Z" banner. buildinfo.Version defaults to "0.1.0"
// when the -ldflags stamp isn't applied (i.e. any `go build` or `go run`
// from source), so we treat that as dev alongside the explicit "dev" /
// empty cases.
func isDevVersion(v string) bool {
	switch v {
	case "", "dev", "0.1.0":
		return true
	}
	return false
}

func isNewer(candidate, current string) bool {
	cNum, _, _ := strings.Cut(candidate, "-")
	aNum, _, _ := strings.Cut(current, "-")
	return cmpNumericDotted(cNum, aNum) > 0
}

func cmpNumericDotted(a, b string) int {
	as := strings.Split(a, ".")
	bs := strings.Split(b, ".")
	n := len(as)
	if len(bs) > n {
		n = len(bs)
	}
	for i := 0; i < n; i++ {
		var ai, bi int
		if i < len(as) {
			ai = atoiSafe(as[i])
		}
		if i < len(bs) {
			bi = atoiSafe(bs[i])
		}
		switch {
		case ai < bi:
			return -1
		case ai > bi:
			return 1
		}
	}
	return 0
}

func atoiSafe(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return n
		}
		n = n*10 + int(r-'0')
	}
	return n
}
