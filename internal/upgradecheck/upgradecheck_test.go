package upgradecheck

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"0.79.10", "0.79.15", -1},
		{"v0.79.15", "v0.79.15", 0},
		{"0.79.15", "0.79.10", 1},
		{"0.79.10", "0.79.10.1", -1},
		{"0.80.0", "0.79.99", 1},
		{"dev", "0.79.10", -1}, // "dev" → 0, so any real version wins
		// Pre-release suffixes are stripped, so an -rc on the same base
		// version sorts equal (NOT below — splitVersion previously had a
		// bug where "10-rc" parsed to 0 and inverted ordering).
		{"0.79.10-rc.1", "0.79.10", 0},
		{"0.79.10", "0.79.10-rc.1", 0},
		{"0.79.10-rc.1", "0.79.11", -1},
	}
	for _, c := range cases {
		got := compareVersions(c.a, c.b)
		if got != c.want {
			t.Errorf("compareVersions(%q,%q)=%d want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestIsDevVersion(t *testing.T) {
	cases := map[string]bool{
		"":         true,
		"dev":      true,
		"  dev  ":  true,
		"0.79.10":  false,
		"v0.79.10": false,
	}
	for in, want := range cases {
		if got := IsDevVersion(in); got != want {
			t.Errorf("IsDevVersion(%q)=%v want %v", in, got, want)
		}
	}
}

func TestParseCommit(t *testing.T) {
	cases := []struct {
		msg             string
		wantType        string
		wantScope       string
		wantDescription string
		wantPR          string
		wantBreaking    bool
	}{
		{
			msg:             "feat(wiki): inline citations on hover (#310)",
			wantType:        "feat",
			wantScope:       "wiki",
			wantDescription: "inline citations on hover",
			wantPR:          "310",
		},
		{
			msg:             "fix: broken link\n\nbody text here",
			wantType:        "fix",
			wantScope:       "",
			wantDescription: "broken link",
			wantPR:          "",
		},
		{
			msg:             "Random subject without conventional prefix (#42)",
			wantType:        "other",
			wantScope:       "",
			wantDescription: "Random subject without conventional prefix (#42)",
			wantPR:          "42",
		},
		{
			// Breaking-change marker (`!`) must be captured AND surface as
			// Breaking=true so renderers can route it to a dedicated group.
			msg:             "feat(api)!: drop legacy /v1 endpoints (#400)",
			wantType:        "feat",
			wantScope:       "api",
			wantDescription: "drop legacy /v1 endpoints",
			wantPR:          "400",
			wantBreaking:    true,
		},
		{
			// Inline `(#42)` inside the description must NOT be stripped — only
			// a trailing PR ref should be removed. Mirrors the TS parser so
			// the CLI and web banner render the same text for the same input.
			msg:             "fix(api): handle (#42) properly (#310)",
			wantType:        "fix",
			wantScope:       "api",
			wantDescription: "handle (#42) properly",
			wantPR:          "310",
		},
	}
	for _, c := range cases {
		got := parseCommit(c.msg, "abcdef")
		if got.Type != c.wantType || got.Scope != c.wantScope ||
			got.Description != c.wantDescription || got.PR != c.wantPR ||
			got.Breaking != c.wantBreaking {
			t.Errorf("parseCommit(%q) = %+v", c.msg, got)
		}
	}
}

func TestFormatChangelogPromotesBreakingChanges(t *testing.T) {
	entries := []CommitEntry{
		{Type: "feat", Description: "alpha"},
		{Type: "fix", Description: "beta", Breaking: true},
		{Type: "docs", Description: "gamma"},
	}
	out := FormatChangelog(entries)
	breakingIdx := strings.Index(out, "Breaking changes")
	featIdx := strings.Index(out, "New features")
	if breakingIdx < 0 || featIdx < 0 {
		t.Fatalf("missing groups in output:\n%s", out)
	}
	if breakingIdx > featIdx {
		t.Errorf("Breaking changes should appear before New features:\n%s", out)
	}
	if !strings.Contains(out, "beta") {
		t.Errorf("breaking entry not rendered:\n%s", out)
	}
}

func TestCheckHitsRegistry(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/wuphf/latest") {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"version": "99.0.0"})
	}))
	defer srv.Close()

	// Inject a per-test client whose Transport rewrites the npm registry URL
	// to our httptest server. Avoids mutating http.DefaultTransport, which
	// would leak between concurrent tests in this package.
	host := strings.TrimPrefix(srv.URL, "http://")
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		req.URL.Scheme = "http"
		req.URL.Host = host
		return http.DefaultTransport.RoundTrip(req)
	})}

	res, err := Check(context.Background(), client)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if res.Latest != "99.0.0" {
		t.Errorf("Latest = %q want 99.0.0", res.Latest)
	}
	if !res.UpgradeAvailable {
		t.Errorf("expected UpgradeAvailable=true (current=%q)", res.Current)
	}
	if res.CompareURL == "" {
		t.Errorf("expected non-empty CompareURL when UpgradeAvailable=true")
	}
}

// pinCurrentVersion swaps the package-level currentVersion seam for the
// duration of the test. Restores the original on cleanup so parallel tests
// don't observe each other's pin.
func pinCurrentVersion(t *testing.T, v string) {
	t.Helper()
	prev := currentVersion
	currentVersion = func() string { return v }
	t.Cleanup(func() { currentVersion = prev })
}

// mockNPMRegistry returns an http.Client whose Transport rewrites the npm
// registry URL to a httptest server that always responds with the given
// version. Cleans up the server on test end.
func mockNPMRegistry(t *testing.T, latest string) *http.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"version": latest})
	}))
	t.Cleanup(srv.Close)
	host := strings.TrimPrefix(srv.URL, "http://")
	return &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		req.URL.Scheme = "http"
		req.URL.Host = host
		return http.DefaultTransport.RoundTrip(req)
	})}
}

func TestCheckShortCircuitsForDevBuild(t *testing.T) {
	// On a dev binary, Check must NOT compute UpgradeAvailable or
	// CompareURL regardless of what npm `latest` says — otherwise
	// `dev < anything` would always tell the user to npm-install over
	// their source build.
	pinCurrentVersion(t, "dev")
	res, err := Check(context.Background(), mockNPMRegistry(t, "99.0.0"))
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !res.IsDevBuild {
		t.Errorf("expected IsDevBuild=true for current=%q", res.Current)
	}
	if res.UpgradeAvailable {
		t.Errorf("dev build must never report UpgradeAvailable=true")
	}
	if res.CompareURL != "" {
		t.Errorf("CompareURL should be empty for dev build, got %q", res.CompareURL)
	}
}

func TestCheckOmitsCompareURLWhenVersionsEqual(t *testing.T) {
	// True up-to-date path (non-dev current matches latest). CompareURL
	// must be empty so JSON consumers don't surface a degenerate
	// `compare/vX...vX` link.
	pinCurrentVersion(t, "1.2.3")
	res, err := Check(context.Background(), mockNPMRegistry(t, "1.2.3"))
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if res.IsDevBuild {
		t.Errorf("expected IsDevBuild=false for current=%q", res.Current)
	}
	if res.UpgradeAvailable {
		t.Errorf("expected UpgradeAvailable=false when current==latest")
	}
	if res.CompareURL != "" {
		t.Errorf("CompareURL should be empty when up-to-date, got %q", res.CompareURL)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
