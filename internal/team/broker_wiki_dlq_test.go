package team

// broker_wiki_dlq_test.go — HTTP-surface tests for GET /wiki/dlq.

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestHandleWikiDLQ exercises the inspection endpoint against a seeded DLQ.
// It verifies the endpoint is auth-gated, returns both pending and
// permanent-failure rows in JSON, and that a nil DLQ returns 503.
func TestHandleWikiDLQ(t *testing.T) {
	b := NewBroker()

	// Seed a DLQ with 2 pending + 1 promoted (via RecordAttempt past the
	// validation max_retries ceiling).
	dlq := newTestDLQ(t)
	ctx := t.Context()

	now := time.Now().UTC()
	pending1 := DLQEntry{
		ArtifactSHA:   "pending-aaaa",
		ArtifactPath:  "wiki/artifacts/chat/pending-aaaa.md",
		Kind:          "chat",
		LastError:     "provider timeout",
		ErrorCategory: DLQCategoryProviderTimeout,
		FirstFailedAt: now.Add(-30 * time.Minute),
	}
	pending2 := DLQEntry{
		ArtifactSHA:   "pending-bbbb",
		ArtifactPath:  "wiki/artifacts/email/pending-bbbb.md",
		Kind:          "email",
		LastError:     "provider timeout 2",
		ErrorCategory: DLQCategoryProviderTimeout,
		FirstFailedAt: now.Add(-10 * time.Minute),
	}
	promoted := DLQEntry{
		ArtifactSHA:   "promoted-cccc",
		ArtifactPath:  "wiki/artifacts/meeting/promoted-cccc.md",
		Kind:          "meeting",
		LastError:     "bad triplet",
		ErrorCategory: DLQCategoryValidation,
	}

	if err := dlq.Enqueue(ctx, pending1); err != nil {
		t.Fatalf("enqueue pending1: %v", err)
	}
	if err := dlq.Enqueue(ctx, pending2); err != nil {
		t.Fatalf("enqueue pending2: %v", err)
	}
	if err := dlq.Enqueue(ctx, promoted); err != nil {
		t.Fatalf("enqueue promoted: %v", err)
	}
	// Promote the third entry by recording a validation-class failure
	// (validation max_retries = 1, so the first RecordAttempt promotes).
	if err := dlq.RecordAttempt(ctx, "promoted-cccc", nil, string(DLQCategoryValidation)); err != nil {
		t.Fatalf("record attempt to promote: %v", err)
	}

	b.mu.Lock()
	b.wikiDLQ = dlq
	b.mu.Unlock()

	mux := http.NewServeMux()
	mux.HandleFunc("/wiki/dlq", b.requireAuth(b.handleWikiDLQ))
	srv := httptest.NewServer(mux)
	defer srv.Close()

	// Unauthenticated → 401.
	resp, err := http.Get(srv.URL + "/wiki/dlq")
	if err != nil {
		t.Fatalf("unauth GET: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 without auth; got %d", resp.StatusCode)
	}

	// Authenticated GET returns 200 + JSON snapshot.
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/wiki/dlq", nil)
	req.Header.Set("Authorization", "Bearer "+b.Token())
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("auth GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200; got %d: %s", resp.StatusCode, body)
	}

	var snap Snapshot
	if err := json.NewDecoder(resp.Body).Decode(&snap); err != nil {
		t.Fatalf("decode snapshot: %v", err)
	}

	if len(snap.Pending) != 2 {
		t.Errorf("pending len = %d; want 2\npending: %+v", len(snap.Pending), snap.Pending)
	}
	if len(snap.PermanentFailures) != 1 {
		t.Errorf("permanent_failures len = %d; want 1\npermanent: %+v",
			len(snap.PermanentFailures), snap.PermanentFailures)
	}

	// Pending ordered by FirstFailedAt ascending (older first).
	if len(snap.Pending) == 2 && snap.Pending[0].ArtifactSHA != "pending-aaaa" {
		t.Errorf("pending[0] = %q; want pending-aaaa (older)", snap.Pending[0].ArtifactSHA)
	}

	// Permanent entries carry their validation category through.
	if len(snap.PermanentFailures) == 1 {
		if snap.PermanentFailures[0].ArtifactSHA != "promoted-cccc" {
			t.Errorf("permanent[0] = %q; want promoted-cccc", snap.PermanentFailures[0].ArtifactSHA)
		}
		if snap.PermanentFailures[0].ErrorCategory != DLQCategoryValidation {
			t.Errorf("permanent[0].ErrorCategory = %q; want validation",
				snap.PermanentFailures[0].ErrorCategory)
		}
	}

	// Wrong method → 405.
	req2, _ := http.NewRequest(http.MethodPost, srv.URL+"/wiki/dlq", nil)
	req2.Header.Set("Authorization", "Bearer "+b.Token())
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatalf("POST /wiki/dlq: %v", err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405 on POST; got %d", resp2.StatusCode)
	}
}

// TestHandleWikiDLQ_NilDLQReturns503 verifies the handler degrades
// gracefully when the worker has not been booted.
func TestHandleWikiDLQ_NilDLQReturns503(t *testing.T) {
	b := NewBroker()
	// Explicitly leave b.wikiDLQ = nil.

	mux := http.NewServeMux()
	mux.HandleFunc("/wiki/dlq", b.requireAuth(b.handleWikiDLQ))
	srv := httptest.NewServer(mux)
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/wiki/dlq", nil)
	req.Header.Set("Authorization", "Bearer "+b.Token())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 503; got %d: %s", resp.StatusCode, body)
	}
}
