package team

// broker_inbox_handler_phase2_test.go exercises GET /inbox/items over
// HTTP: filter routing, kind narrowing, owner vs human-session auth.
// Builds on the same fixtures as broker_inbox_phase2_test.go but goes
// through the real httptest.NewServer + requireAuth pipeline so any
// regression in the middleware composition surfaces here.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHandleInboxItems_OwnerSeesAllKinds(t *testing.T) {
	b := newTestBrokerForReview(t)
	rl := b.ReviewLog()
	now := time.Now().UTC()

	b.mu.Lock()
	b.tasks = []teamTask{
		{ID: "task-a", Title: "Decide refactor", CreatedAt: now.Format(time.RFC3339), Reviewers: []string{"owner"}},
	}
	if _, err := b.transitionLifecycleLocked("task-a", LifecycleStateDecision, "phase 2 handler seed"); err != nil {
		b.mu.Unlock()
		t.Fatalf("seed task: %v", err)
	}
	b.requests = []humanInterview{
		{ID: "req-1", From: "owner", Channel: "general", Question: "Approve?", CreatedAt: now.Format(time.RFC3339), Kind: "approval"},
	}
	b.mu.Unlock()
	seedPromotion(t, rl, &Promotion{
		ID:           "rev-1",
		State:        PromotionPending,
		SourceSlug:   "ada",
		SourcePath:   "notebook/ada/draft.md",
		TargetPath:   "wiki/draft.md",
		ReviewerSlug: "owner",
		CreatedAt:    now,
		UpdatedAt:    now,
	})

	mux := http.NewServeMux()
	mux.HandleFunc("/inbox/items", b.requireAuth(b.handleInboxItems))
	srv := httptest.NewServer(mux)
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/inbox/items?filter=all", nil)
	req.Header.Set("Authorization", "Bearer "+b.Token())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("inbox/items request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var payload unifiedInboxResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode: %v", err)
	}

	counts := map[InboxItemKind]int{}
	for _, item := range payload.Items {
		counts[item.Kind]++
	}
	if counts[InboxItemKindTask] != 1 {
		t.Fatalf("task count = %d, want 1; items = %+v", counts[InboxItemKindTask], payload.Items)
	}
	if counts[InboxItemKindRequest] != 1 {
		t.Fatalf("request count = %d, want 1; items = %+v", counts[InboxItemKindRequest], payload.Items)
	}
	if counts[InboxItemKindReview] != 1 {
		t.Fatalf("review count = %d, want 1; items = %+v", counts[InboxItemKindReview], payload.Items)
	}
	if payload.RefreshedAt == "" {
		t.Fatal("refreshedAt must be populated")
	}
}

func TestHandleInboxItems_KindFilterNarrowsResponse(t *testing.T) {
	b := newTestBrokerForReview(t)
	now := time.Now().UTC()

	b.mu.Lock()
	b.tasks = []teamTask{
		{ID: "task-a", Title: "Decide", CreatedAt: now.Format(time.RFC3339)},
	}
	if _, err := b.transitionLifecycleLocked("task-a", LifecycleStateDecision, "seed"); err != nil {
		b.mu.Unlock()
		t.Fatalf("seed task: %v", err)
	}
	b.requests = []humanInterview{
		{ID: "req-1", From: "owner", Channel: "general", Question: "Approve?", CreatedAt: now.Format(time.RFC3339), Kind: "approval"},
	}
	b.mu.Unlock()

	mux := http.NewServeMux()
	mux.HandleFunc("/inbox/items", b.requireAuth(b.handleInboxItems))
	srv := httptest.NewServer(mux)
	defer srv.Close()

	for _, tc := range []struct {
		name     string
		kind     string
		wantKind InboxItemKind
		wantLen  int
	}{
		{name: "request only", kind: "request", wantKind: InboxItemKindRequest, wantLen: 1},
		{name: "task only", kind: "task", wantKind: InboxItemKindTask, wantLen: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req, _ := http.NewRequest(http.MethodGet, srv.URL+"/inbox/items?filter=all&kind="+tc.kind, nil)
			req.Header.Set("Authorization", "Bearer "+b.Token())
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("request: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status = %d, want 200", resp.StatusCode)
			}
			var payload unifiedInboxResponse
			if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if len(payload.Items) != tc.wantLen {
				t.Fatalf("items len = %d, want %d (items=%+v)", len(payload.Items), tc.wantLen, payload.Items)
			}
			for _, item := range payload.Items {
				if item.Kind != tc.wantKind {
					t.Fatalf("item.Kind = %q, want %q", item.Kind, tc.wantKind)
				}
			}
		})
	}
}

func TestHandleTaskDecision_PersistsComment(t *testing.T) {
	b := newTestBroker(t)
	now := time.Now().UTC().Format(time.RFC3339)

	b.mu.Lock()
	b.tasks = []teamTask{
		{ID: "task-a", Title: "Decide me", CreatedAt: now},
	}
	if _, err := b.transitionLifecycleLocked("task-a", LifecycleStateDecision, "seed"); err != nil {
		b.mu.Unlock()
		t.Fatalf("seed: %v", err)
	}
	b.mu.Unlock()

	mux := http.NewServeMux()
	mux.HandleFunc("/tasks/", b.requireAuth(b.handleTaskByID))
	srv := httptest.NewServer(mux)
	defer srv.Close()

	body := strings.NewReader(`{"action":"approve","comment":"LGTM ship it"}`)
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/tasks/task-a/decision", body)
	req.Header.Set("Authorization", "Bearer "+b.Token())
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("decision request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	// Pull the packet and confirm the FeedbackItem was appended with
	// the broker-token author "owner".
	b.mu.Lock()
	packet, _ := b.findDecisionPacketLocked("task-a")
	b.mu.Unlock()
	if packet == nil {
		t.Fatal("expected packet to exist after decision")
	}
	if len(packet.Spec.Feedback) != 1 {
		t.Fatalf("spec.feedback len = %d, want 1; got %+v", len(packet.Spec.Feedback), packet.Spec.Feedback)
	}
	fb := packet.Spec.Feedback[0]
	if fb.Body != "LGTM ship it" {
		t.Fatalf("feedback body = %q, want %q", fb.Body, "LGTM ship it")
	}
	if fb.Author != "owner" {
		t.Fatalf("feedback author = %q, want %q (broker-token caller)", fb.Author, "owner")
	}
	if fb.AppendedAt.IsZero() {
		t.Fatal("feedback appendedAt must be populated")
	}
}

func TestHandleTaskDecision_NoCommentLeavesFeedbackEmpty(t *testing.T) {
	b := newTestBroker(t)
	now := time.Now().UTC().Format(time.RFC3339)

	b.mu.Lock()
	b.tasks = []teamTask{
		{ID: "task-b", Title: "Decide me", CreatedAt: now},
	}
	if _, err := b.transitionLifecycleLocked("task-b", LifecycleStateDecision, "seed"); err != nil {
		b.mu.Unlock()
		t.Fatalf("seed: %v", err)
	}
	b.mu.Unlock()

	mux := http.NewServeMux()
	mux.HandleFunc("/tasks/", b.requireAuth(b.handleTaskByID))
	srv := httptest.NewServer(mux)
	defer srv.Close()

	// No comment field → backward-compatible with pre-Phase-3 callers
	// that POST {action} only.
	body := strings.NewReader(`{"action":"approve"}`)
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/tasks/task-b/decision", body)
	req.Header.Set("Authorization", "Bearer "+b.Token())
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("decision request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	b.mu.Lock()
	packet, _ := b.findDecisionPacketLocked("task-b")
	b.mu.Unlock()
	if packet == nil {
		t.Fatal("expected packet to exist after decision")
	}
	if len(packet.Spec.Feedback) != 0 {
		t.Fatalf("spec.feedback len = %d, want 0 when no comment posted", len(packet.Spec.Feedback))
	}
}

func TestHandleInboxItems_BadFilterReturns400(t *testing.T) {
	b := newTestBroker(t)
	mux := http.NewServeMux()
	mux.HandleFunc("/inbox/items", b.requireAuth(b.handleInboxItems))
	srv := httptest.NewServer(mux)
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/inbox/items?filter=nonsense", nil)
	req.Header.Set("Authorization", "Bearer "+b.Token())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}
