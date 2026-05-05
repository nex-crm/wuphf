package team

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestHandleWikiMaintenanceSuggest_RejectsGet rejects non-POST requests so
// callers don't accidentally trigger expensive suggestion compute via prefetch.
func TestHandleWikiMaintenanceSuggest_RejectsGet(t *testing.T) {
	b := newTestBroker(t)
	srv := httptest.NewServer(http.HandlerFunc(b.handleWikiMaintenanceSuggest))
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("status: want 405, got %d", resp.StatusCode)
	}
}

// TestHandleWikiMaintenanceSuggest_NoWiki returns 503 when the markdown
// backend is not enabled. The frontend should treat this as "feature off"
// and gracefully hide the panel.
func TestHandleWikiMaintenanceSuggest_NoWiki(t *testing.T) {
	b := newTestBroker(t)
	srv := httptest.NewServer(http.HandlerFunc(b.handleWikiMaintenanceSuggest))
	defer srv.Close()

	body := bytes.NewBufferString(`{"action":"summarize","path":"team/people/x.md"}`)
	resp, err := http.Post(srv.URL, "application/json", body)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status: want 503, got %d", resp.StatusCode)
	}
}

// TestHandleWikiMaintenanceSuggest_E2E exercises the full handler with a
// real WikiWorker — verifies the JSON response shape matches what the
// frontend's WikiMaintenanceSuggestion type expects.
func TestHandleWikiMaintenanceSuggest_E2E(t *testing.T) {
	worker, cleanup := newMaintenanceFixture(t)
	defer cleanup()

	body := strings.Repeat("Sarah Chen leads product at Acme Corp. ", 30) +
		"\n\n# Sarah Chen\n\nSarah Chen leads product at Acme Corp.\n"
	seedArticle(t, worker, "team/people/sarah-chen.md", body)

	b := newTestBroker(t)
	b.mu.Lock()
	b.wikiWorker = worker
	b.mu.Unlock()

	srv := httptest.NewServer(http.HandlerFunc(b.handleWikiMaintenanceSuggest))
	defer srv.Close()

	reqBody := bytes.NewBufferString(`{"action":"summarize","path":"team/people/sarah-chen.md"}`)
	resp, err := http.Post(srv.URL, "application/json", reqBody)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: want 200, got %d", resp.StatusCode)
	}

	var got MaintenanceSuggestion
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Action != MaintActionSummarize {
		t.Errorf("action: want summarize, got %q", got.Action)
	}
	if got.Skipped {
		t.Errorf("expected non-skipped, got skipped: %s", got.SkippedReason)
	}
	if got.Diff == nil || got.Diff.ProposedContent == "" {
		t.Errorf("expected diff with proposed content, got nil")
	}
}
