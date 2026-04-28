package team

// skill_compile_endpoints_test.go covers the HTTP surface for /skills/compile
// and /skills/compile/stats. We exercise auth, the dry_run pass-through, and
// the coalesce / cooldown response shapes.

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func newCompileTestServer(t *testing.T, b *Broker) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/skills/compile", b.requireAuth(b.handlePostSkillCompile))
	mux.HandleFunc("/skills/compile/stats", b.requireAuth(b.handleGetSkillCompileStats))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestPostSkillCompile_DryRunReturnsScanResult(t *testing.T) {
	b := withScannedTestBroker(t, &instantProvider{})
	srv := newCompileTestServer(t, b)

	body := bytes.NewBufferString(`{"dry_run": true}`)
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/skills/compile", body)
	req.Header.Set("Authorization", "Bearer "+b.Token())
	req.Header.Set("Content-Type", "application/json")

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(res.Body)
		t.Fatalf("expected 200, got %d: %s", res.StatusCode, string(raw))
	}

	var result ScanResult
	if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if result.Trigger != "manual" {
		t.Fatalf("expected trigger=manual, got %q", result.Trigger)
	}
}

func TestPostSkillCompile_RequiresAuth(t *testing.T) {
	b := withScannedTestBroker(t, &instantProvider{})
	srv := newCompileTestServer(t, b)

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/skills/compile", nil)
	// No Authorization header.

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 without auth, got %d", res.StatusCode)
	}
}

func TestPostSkillCompile_CoalescedReturns202(t *testing.T) {
	b := withScannedTestBroker(t, &instantProvider{})
	srv := newCompileTestServer(t, b)

	// Pre-set the inflight flag so the next POST coalesces.
	b.mu.Lock()
	b.skillCompileInflight = true
	b.mu.Unlock()

	body := bytes.NewBufferString(`{"dry_run": true}`)
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/skills/compile", body)
	req.Header.Set("Authorization", "Bearer "+b.Token())
	req.Header.Set("Content-Type", "application/json")

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusAccepted {
		raw, _ := io.ReadAll(res.Body)
		t.Fatalf("expected 202 on coalesce, got %d: %s", res.StatusCode, string(raw))
	}

	var queued struct {
		Queued bool `json:"queued"`
	}
	if err := json.NewDecoder(res.Body).Decode(&queued); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !queued.Queued {
		t.Fatalf("expected queued=true on 202 response")
	}
}

func TestPostSkillCompile_EmptyBodyAccepted(t *testing.T) {
	b := withScannedTestBroker(t, &instantProvider{})
	srv := newCompileTestServer(t, b)

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/skills/compile", nil)
	req.Header.Set("Authorization", "Bearer "+b.Token())

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(res.Body)
		t.Fatalf("expected 200 with empty body, got %d: %s", res.StatusCode, string(raw))
	}
}

func TestGetSkillCompileStats_ReturnsMetrics(t *testing.T) {
	b := withScannedTestBroker(t, &instantProvider{})

	// Run one manual pass to produce a metric.
	if _, err := b.compileWikiSkills(context.Background(), "", true, "manual"); err != nil {
		t.Fatalf("compile: %v", err)
	}

	srv := newCompileTestServer(t, b)
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/skills/compile/stats", nil)
	req.Header.Set("Authorization", "Bearer "+b.Token())

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.StatusCode)
	}

	var stats struct {
		ManualClicksTotal      int64  `json:"manual_clicks_total"`
		LastSkillCompilePassAt string `json:"last_skill_compile_pass_at"`
	}
	if err := json.NewDecoder(res.Body).Decode(&stats); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if stats.ManualClicksTotal < 1 {
		t.Fatalf("expected manual_clicks_total >= 1, got %d", stats.ManualClicksTotal)
	}
	if stats.LastSkillCompilePassAt == "" {
		t.Fatalf("expected last_skill_compile_pass_at to be populated")
	}
	// Sanity: parses as RFC3339.
	if _, err := time.Parse(time.RFC3339, stats.LastSkillCompilePassAt); err != nil {
		t.Fatalf("LastSkillCompilePassAt should be RFC3339: %v", err)
	}
}
