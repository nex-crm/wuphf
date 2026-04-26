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
	}
	for _, c := range cases {
		got := compareVersions(c.a, c.b)
		if got != c.want {
			t.Errorf("compareVersions(%q,%q)=%d want %d", c.a, c.b, got, c.want)
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
	}
	for _, c := range cases {
		got := parseCommit(c.msg, "abcdef")
		if got.Type != c.wantType || got.Scope != c.wantScope || got.Description != c.wantDescription || got.PR != c.wantPR {
			t.Errorf("parseCommit(%q) = %+v", c.msg, got)
		}
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

	// Override only the URL via a custom transport.
	rt := http.DefaultTransport
	defer func() { http.DefaultTransport = rt }()
	http.DefaultTransport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		// Redirect any registry call to our test server.
		req.URL.Scheme = "http"
		req.URL.Host = strings.TrimPrefix(srv.URL, "http://")
		return rt.RoundTrip(req)
	})

	res, err := Check(context.Background(), &http.Client{Transport: http.DefaultTransport})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if res.Latest != "99.0.0" {
		t.Errorf("Latest = %q want 99.0.0", res.Latest)
	}
	if !res.UpgradeAvailable {
		t.Errorf("expected UpgradeAvailable=true (current=%q)", res.Current)
	}
	if res.Notice() == "" {
		t.Errorf("expected non-empty Notice")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
