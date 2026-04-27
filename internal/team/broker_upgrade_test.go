package team

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nex-crm/wuphf/internal/upgradecheck"
)

// resetUpgradeCaches wipes the package-level upgrade cache state between
// tests so a stale entry from one test can't satisfy another.
func resetUpgradeCaches(t *testing.T) {
	t.Helper()
	upgradeCheckCache.Store(nil)
	upgradeChangelog.Range(func(k, _ any) bool {
		upgradeChangelog.Delete(k)
		return true
	})
}

func TestHandleUpgradeChangelog_RejectsPathTraversalParams(t *testing.T) {
	// The version-param regex MUST reject anything that isn't a clean
	// dotted-numeric (with optional v / pre-release) so a crafted call
	// can't inject path segments into the upstream GitHub compare URL.
	resetUpgradeCaches(t)
	b := newTestBroker(t)
	srv := httptest.NewServer(b.requireAuth(b.handleUpgradeChangelog))
	defer srv.Close()

	cases := []string{
		"../../etc/passwd",
		"v0.79.10/extra",
		"v0.79.10;rm",
		"",
		"latest",
	}
	for _, bad := range cases {
		req, _ := http.NewRequest(http.MethodGet, srv.URL+"?from="+bad+"&to=v0.79.15", nil)
		req.Header.Set("Authorization", "Bearer "+b.Token())
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("from=%q: request: %v", bad, err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("from=%q: expected 400, got %d", bad, resp.StatusCode)
		}
	}
}

func TestHandleUpgradeChangelog_BadParamMatchesUpstreamErrorShape(t *testing.T) {
	// Callers should be able to render a bad-param response with the
	// same .catch path as an upstream-failure response — both must
	// expose `commits: []` + `error: "..."`. This locks the contract.
	resetUpgradeCaches(t)
	b := newTestBroker(t)
	srv := httptest.NewServer(b.requireAuth(b.handleUpgradeChangelog))
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"?from=garbage&to=v0.79.15", nil)
	req.Header.Set("Authorization", "Bearer "+b.Token())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
	var body struct {
		Commits []any  `json:"commits"`
		Error   string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Error == "" {
		t.Errorf("expected non-empty error field on 400, got %+v", body)
	}
	// Lock the documented contract: the bad-param path returns the
	// SAME shape as the upstream-failure path so the banner's .catch
	// can render one message instead of branching on HTTP status. A
	// future change that removes commits or returns null must fail
	// the test loudly.
	if body.Commits == nil {
		t.Errorf("expected commits to be an empty array, got nil")
	}
	if len(body.Commits) != 0 {
		t.Errorf("expected empty commits, got %d entries", len(body.Commits))
	}
}

func TestUpgradeEndpoints_RequireAuthToken(t *testing.T) {
	// Both endpoints MUST be behind requireAuth — they hit upstream
	// network and an unauthenticated caller could trivially exhaust
	// our shared rate-limit budget.
	resetUpgradeCaches(t)
	b := newTestBroker(t)
	cases := []struct {
		name    string
		handler http.HandlerFunc
		path    string
	}{
		{"upgrade-check", b.requireAuth(b.handleUpgradeCheck), ""},
		{"upgrade-changelog", b.requireAuth(b.handleUpgradeChangelog), "?from=v0.1.0&to=v0.1.1"},
	}
	for _, ep := range cases {
		t.Run(ep.name, func(t *testing.T) {
			srv := httptest.NewServer(ep.handler)
			defer srv.Close()
			resp, err := http.Get(srv.URL + ep.path) // no Authorization header
			if err != nil {
				t.Fatalf("request: %v", err)
			}
			_ = resp.Body.Close()
			if resp.StatusCode != http.StatusUnauthorized {
				t.Errorf("expected 401 without token, got %d", resp.StatusCode)
			}
		})
	}
}

func TestUpgradeEndpoints_RejectNonGetMethods(t *testing.T) {
	// Defense-in-depth: the upgrade endpoints are read-only and should
	// not respond to POST/PUT/DELETE — keeps the HTTP contract tight
	// for proxies that gate on method.
	resetUpgradeCaches(t)
	b := newTestBroker(t)
	cases := []struct {
		name    string
		handler http.HandlerFunc
	}{
		{"upgrade-check", b.requireAuth(b.handleUpgradeCheck)},
		{"upgrade-changelog", b.requireAuth(b.handleUpgradeChangelog)},
	}
	for _, ep := range cases {
		t.Run(ep.name, func(t *testing.T) {
			srv := httptest.NewServer(ep.handler)
			defer srv.Close()
			req, _ := http.NewRequest(http.MethodPost, srv.URL, nil)
			req.Header.Set("Authorization", "Bearer "+b.Token())
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("request: %v", err)
			}
			_ = resp.Body.Close()
			if resp.StatusCode != http.StatusMethodNotAllowed {
				t.Errorf("POST: expected 405, got %d", resp.StatusCode)
			}
			if got := resp.Header.Get("Allow"); got != "GET" {
				t.Errorf("expected Allow: GET, got %q", got)
			}
		})
	}
}

func TestHandleUpgradeChangelog_ResponseShapeMatchesBannerExpectations(t *testing.T) {
	// Lock in the JSON wire shape the React banner reads — without
	// JSON struct tags on CommitEntry, Go's default capitalised
	// encoding silently broke the banner (every entry rendered as
	// `{type: "other"}` because c.type was undefined). This test
	// short-circuits the upstream GitHub call by pre-seeding the
	// changelog cache, then asserts the served JSON uses lowercase
	// keys exactly matching what UpgradeBanner.tsx reads.
	resetUpgradeCaches(t)
	// Use the production key helper so the test stays in sync if the
	// broker's encoding ever changes.
	upgradeChangelog.Store(upgradeChangelogKey("v0.1.0", "v0.1.1"), &upgradeChangelogCacheEntry{
		entries: []upgradecheck.CommitEntry{{
			Type: "feat", Scope: "wiki", Description: "x",
			PR: "1", SHA: "abc", Breaking: true,
		}},
		storeAt: time.Now(),
	})
	b := newTestBroker(t)
	srv := httptest.NewServer(b.requireAuth(b.handleUpgradeChangelog))
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"?from=v0.1.0&to=v0.1.1", nil)
	req.Header.Set("Authorization", "Bearer "+b.Token())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status %d, body=%s", resp.StatusCode, string(body))
	}

	// Decode into a struct using the LOWERCASE field names the React
	// banner reads. If JSON tags ever drift back to defaults, all
	// fields decode to zero values and the test fails loudly.
	var payload struct {
		Commits []struct {
			Type        string `json:"type"`
			Scope       string `json:"scope"`
			Description string `json:"description"`
			PR          string `json:"pr"`
			SHA         string `json:"sha"`
			Breaking    bool   `json:"breaking"`
		} `json:"commits"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(payload.Commits) != 1 {
		t.Fatalf("expected 1 commit, got %d", len(payload.Commits))
	}
	c := payload.Commits[0]
	if c.Type != "feat" || c.Scope != "wiki" || c.Description != "x" ||
		c.PR != "1" || c.SHA != "abc" || !c.Breaking {
		t.Errorf("wire shape drifted: %+v", c)
	}
}
