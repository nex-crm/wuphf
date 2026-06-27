package team

// broker_inbox_handler_phase2_test.go exercises GET /inbox/items over
// HTTP: filter routing, kind narrowing, owner vs human-session auth.
// Builds on the same fixtures as broker_inbox_phase2_test.go but goes
// through the real httptest.NewServer + requireAuth pipeline so any
// regression in the middleware composition surfaces here.

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHandleInboxItems_OwnerSeesAllKinds(t *testing.T) {
	b := newTestBroker(t)
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
	if payload.RefreshedAt == "" {
		t.Fatal("refreshedAt must be populated")
	}
}

func TestHandleInboxItems_KindFilterNarrowsResponse(t *testing.T) {
	b := newTestBroker(t)
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

// TestHandleInboxItems_StampsUnreadAndCounts confirms /inbox/items
// computes IsUnread from the actor's cursor and reflects the same count
// in payload.Counts.Unread. With no cursor written, every item is
// unread; after the cursor moves past the items' UpdatedAt, all items
// flip to read.
func TestHandleInboxItems_StampsUnreadAndCounts(t *testing.T) {
	b := newTestBroker(t)
	now := time.Now().UTC()
	older := now.Add(-time.Hour).Format(time.RFC3339)
	b.mu.Lock()
	b.tasks = []teamTask{
		{ID: "task-a", Title: "Refactor", CreatedAt: older, UpdatedAt: older, Reviewers: []string{"owner"}},
	}
	if _, err := b.transitionLifecycleLocked("task-a", LifecycleStateDecision, "seed"); err != nil {
		b.mu.Unlock()
		t.Fatalf("seed: %v", err)
	}
	b.mu.Unlock()

	mux := http.NewServeMux()
	mux.HandleFunc("/inbox/items", b.requireAuth(b.handleInboxItems))
	mux.HandleFunc("/inbox/cursor", b.requireAuth(b.handleInboxCursor))
	srv := httptest.NewServer(mux)
	defer srv.Close()

	get := func() unifiedInboxResponse {
		t.Helper()
		req, _ := http.NewRequest(http.MethodGet, srv.URL+"/inbox/items?filter=all", nil)
		req.Header.Set("Authorization", "Bearer "+b.Token())
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		defer resp.Body.Close()
		var payload unifiedInboxResponse
		if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
			t.Fatalf("decode: %v", err)
		}
		return payload
	}

	// No cursor → every authorized item reads as unread, count matches.
	before := get()
	if len(before.Items) != 1 {
		t.Fatalf("seed item count = %d, want 1", len(before.Items))
	}
	if !before.Items[0].IsUnread {
		t.Fatal("item should be unread before any cursor is written")
	}
	if before.Counts.Unread != 1 {
		t.Fatalf("counts.unread = %d, want 1", before.Counts.Unread)
	}

	// Advance the cursor past the items' UpdatedAt; everything goes
	// to read and the counts unread drops to zero.
	body := strings.NewReader(`{"lastSeenAt":"` + now.Add(time.Minute).UTC().Format(time.RFC3339Nano) + `"}`)
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/inbox/cursor", body)
	req.Header.Set("Authorization", "Bearer "+b.Token())
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("cursor post: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("cursor status = %d, want 204", resp.StatusCode)
	}

	after := get()
	if after.Items[0].IsUnread {
		t.Fatal("item should be read after cursor advances")
	}
	if after.Counts.Unread != 0 {
		t.Fatalf("counts.unread after = %d, want 0", after.Counts.Unread)
	}
}

// TestHandleInboxItems_UnreadFilterTrimsReadItems exercises the
// post-fetch trim that powers the "Unread" sidebar filter.
func TestHandleInboxItems_UnreadFilterTrimsReadItems(t *testing.T) {
	b := newTestBroker(t)
	now := time.Now().UTC()
	older := now.Add(-time.Hour).Format(time.RFC3339)
	b.mu.Lock()
	b.tasks = []teamTask{
		{ID: "task-old", Title: "Older", CreatedAt: older, UpdatedAt: older, Reviewers: []string{"owner"}},
		{ID: "task-new", Title: "Newer", CreatedAt: now.Format(time.RFC3339), UpdatedAt: now.Format(time.RFC3339), Reviewers: []string{"owner"}},
	}
	if _, err := b.transitionLifecycleLocked("task-old", LifecycleStateDecision, "seed"); err != nil {
		b.mu.Unlock()
		t.Fatalf("seed old: %v", err)
	}
	if _, err := b.transitionLifecycleLocked("task-new", LifecycleStateDecision, "seed"); err != nil {
		b.mu.Unlock()
		t.Fatalf("seed new: %v", err)
	}
	b.mu.Unlock()

	// Cursor between the two: task-old becomes read, task-new stays unread.
	b.SetInboxCursor(inboxCursorOwnerKey, InboxCursor{LastSeenAt: now.Add(-30 * time.Minute)})

	mux := http.NewServeMux()
	mux.HandleFunc("/inbox/items", b.requireAuth(b.handleInboxItems))
	srv := httptest.NewServer(mux)
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/inbox/items?filter=unread", nil)
	req.Header.Set("Authorization", "Bearer "+b.Token())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	var payload unifiedInboxResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(payload.Items) != 1 {
		t.Fatalf("unread filter items = %d, want 1; got %+v", len(payload.Items), payload.Items)
	}
	if payload.Items[0].TaskID != "task-new" {
		t.Fatalf("unread item taskId = %q, want task-new", payload.Items[0].TaskID)
	}
}

// TestHandleInboxItems_TaskRowCarriesBlockerAndDetails confirms the
// blocked-card payload extension (details + blockedOn) actually surfaces
// on /inbox/items so the PacketPending fallback can render real info.
func TestHandleInboxItems_TaskRowCarriesBlockerAndDetails(t *testing.T) {
	b := newTestBroker(t)
	now := time.Now().UTC().Format(time.RFC3339)
	b.mu.Lock()
	b.tasks = []teamTask{
		{
			ID:        "task-blocked",
			Title:     "Waits on upstream",
			Details:   "Automatic timeout recovery: @mira timed out after 5m.",
			CreatedAt: now,
			UpdatedAt: now,
			Reviewers: []string{"owner"},
			BlockedOn: []string{"task-upstream"},
		},
	}
	if _, err := b.transitionLifecycleLocked("task-blocked", LifecycleStateBlocked, "seed"); err != nil {
		b.mu.Unlock()
		t.Fatalf("seed: %v", err)
	}
	b.mu.Unlock()

	mux := http.NewServeMux()
	mux.HandleFunc("/inbox/items", b.requireAuth(b.handleInboxItems))
	srv := httptest.NewServer(mux)
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/inbox/items?filter=all", nil)
	req.Header.Set("Authorization", "Bearer "+b.Token())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	var payload unifiedInboxResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(payload.Items) != 1 || payload.Items[0].TaskRow == nil {
		t.Fatalf("expected 1 task item with task row; got %+v", payload.Items)
	}
	row := payload.Items[0].TaskRow
	if row.Details == "" || !strings.Contains(row.Details, "timed out") {
		t.Fatalf("row details should carry the broker reason; got %q", row.Details)
	}
	if len(row.BlockedOn) != 1 || row.BlockedOn[0] != "task-upstream" {
		t.Fatalf("row.BlockedOn = %v, want [task-upstream]", row.BlockedOn)
	}
	if row.UpdatedAt == "" {
		t.Fatal("row.UpdatedAt must be populated for the unread cursor to work")
	}
}

// TestHandleTaskResume covers the POST /tasks/{id}/resume route that
// the blocked-card "Resume" button posts to. Verifies the lifecycle
// transitions away from blocked and that the JSON envelope
// carries changed=true.
func TestHandleTaskResume(t *testing.T) {
	b := newTestBroker(t)
	now := time.Now().UTC().Format(time.RFC3339)
	b.mu.Lock()
	b.tasks = []teamTask{
		{ID: "task-blocked", Title: "Blocked", CreatedAt: now, UpdatedAt: now, Reviewers: []string{"owner"}, status: "blocked"},
	}
	if _, err := b.transitionLifecycleLocked("task-blocked", LifecycleStateBlocked, "seed"); err != nil {
		b.mu.Unlock()
		t.Fatalf("seed: %v", err)
	}
	b.mu.Unlock()

	mux := http.NewServeMux()
	mux.HandleFunc("/tasks/", b.requireAuth(b.handleTaskByID))
	srv := httptest.NewServer(mux)
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/tasks/task-blocked/resume", strings.NewReader(`{"reason":"manual ping"}`))
	req.Header.Set("Authorization", "Bearer "+b.Token())
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("resume request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var envelope struct {
		Changed bool `json:"changed"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !envelope.Changed {
		t.Fatal("first resume should report changed=true")
	}

	b.mu.Lock()
	state := LifecycleStateUnknown
	for _, task := range b.tasks {
		if task.ID == "task-blocked" {
			state = task.LifecycleState
		}
	}
	b.mu.Unlock()
	if state == LifecycleStateBlocked {
		t.Fatalf("task should have left blocked state after resume; still %s", state)
	}
}

// TestHandleTaskResume_NonReviewerForbidden locks the authorization
// contract: a human session whose slug is NOT in the task's Reviewers
// list cannot resume the task. The broker-token happy path is exercised
// in TestHandleTaskResume above; this case fills the other half of the
// auth matrix.
func TestHandleTaskResume_NonReviewerForbidden(t *testing.T) {
	b := newTestBroker(t)
	now := time.Now().UTC().Format(time.RFC3339)
	b.mu.Lock()
	b.tasks = []teamTask{
		{ID: "task-blocked", Title: "Blocked", CreatedAt: now, UpdatedAt: now, Reviewers: []string{"mira"}, status: "blocked"},
	}
	if _, err := b.transitionLifecycleLocked("task-blocked", LifecycleStateBlocked, "seed"); err != nil {
		b.mu.Unlock()
		t.Fatalf("seed: %v", err)
	}
	b.mu.Unlock()

	// Mint a human session for Alex — not in the Reviewers list.
	inviteToken, _, err := b.createHumanInvite()
	if err != nil {
		t.Fatalf("create invite: %v", err)
	}
	sessionToken, _, err := b.acceptHumanInvite(inviteToken, "Alex", "browser")
	if err != nil {
		t.Fatalf("accept invite: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/tasks/", b.requireAuth(b.handleTaskByID))
	srv := httptest.NewServer(mux)
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/tasks/task-blocked/resume", strings.NewReader(`{}`))
	req.AddCookie(&http.Cookie{Name: humanSessionCookie, Value: sessionToken})
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("resume request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("non-reviewer status = %d, want 403", resp.StatusCode)
	}

	// Lifecycle state must not have moved — auth gate runs before the
	// ResumeTask call, so the task should still be blocked.
	b.mu.Lock()
	state := LifecycleStateUnknown
	for _, task := range b.tasks {
		if task.ID == "task-blocked" {
			state = task.LifecycleState
		}
	}
	b.mu.Unlock()
	if state != LifecycleStateBlocked {
		t.Fatalf("state should remain blocked after 403; got %s", state)
	}
}

// TestHandleTaskResume_RejectsMalformedJSON guards the decoder
// hardening: a non-empty body that fails to parse must yield 400 and
// must not mutate the task.
func TestHandleTaskResume_RejectsMalformedJSON(t *testing.T) {
	b := newTestBroker(t)
	now := time.Now().UTC().Format(time.RFC3339)
	b.mu.Lock()
	b.tasks = []teamTask{
		{ID: "task-blocked", Title: "Blocked", CreatedAt: now, UpdatedAt: now, Reviewers: []string{"owner"}, status: "blocked"},
	}
	if _, err := b.transitionLifecycleLocked("task-blocked", LifecycleStateBlocked, "seed"); err != nil {
		b.mu.Unlock()
		t.Fatalf("seed: %v", err)
	}
	b.mu.Unlock()

	mux := http.NewServeMux()
	mux.HandleFunc("/tasks/", b.requireAuth(b.handleTaskByID))
	srv := httptest.NewServer(mux)
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/tasks/task-blocked/resume", strings.NewReader(`{not json}`))
	req.Header.Set("Authorization", "Bearer "+b.Token())
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("resume request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("malformed body status = %d, want 400", resp.StatusCode)
	}

	b.mu.Lock()
	state := LifecycleStateUnknown
	for _, task := range b.tasks {
		if task.ID == "task-blocked" {
			state = task.LifecycleState
		}
	}
	b.mu.Unlock()
	if state != LifecycleStateBlocked {
		t.Fatalf("state should remain blocked after 400; got %s", state)
	}
}

// TestHandleInboxItems_SortsByUpdatedAtDescending guards the contract
// that recently-updated items surface first regardless of their
// CreatedAt. An older task that just transitioned must outrank a newer
// task that has not been touched since creation.
func TestHandleInboxItems_SortsByUpdatedAtDescending(t *testing.T) {
	b := newTestBroker(t)
	now := time.Now().UTC()
	older := now.Add(-2 * time.Hour).Format(time.RFC3339)
	newer := now.Add(-1 * time.Hour).Format(time.RFC3339)
	justNow := now.Format(time.RFC3339)
	b.mu.Lock()
	b.tasks = []teamTask{
		// task-old was created longest ago AND just transitioned now.
		{ID: "task-old-but-active", Title: "Old, just updated", CreatedAt: older, UpdatedAt: justNow, Reviewers: []string{"owner"}},
		// task-fresh was created more recently but has not moved.
		{ID: "task-fresh-but-quiet", Title: "Newer, untouched", CreatedAt: newer, UpdatedAt: newer, Reviewers: []string{"owner"}},
	}
	for _, id := range []string{"task-old-but-active", "task-fresh-but-quiet"} {
		if _, err := b.transitionLifecycleLocked(id, LifecycleStateDecision, "seed"); err != nil {
			b.mu.Unlock()
			t.Fatalf("seed %s: %v", id, err)
		}
	}
	// The lifecycle transition above stamps UpdatedAt with the wall
	// clock for both, so reset the fixture timestamps explicitly to
	// recreate the "old created, just-updated" vs "newer created,
	// untouched" contrast the sort is meant to honor.
	for i := range b.tasks {
		switch b.tasks[i].ID {
		case "task-old-but-active":
			b.tasks[i].UpdatedAt = justNow
			b.tasks[i].CreatedAt = older
		case "task-fresh-but-quiet":
			b.tasks[i].UpdatedAt = newer
			b.tasks[i].CreatedAt = newer
		}
	}
	b.mu.Unlock()

	mux := http.NewServeMux()
	mux.HandleFunc("/inbox/items", b.requireAuth(b.handleInboxItems))
	srv := httptest.NewServer(mux)
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/inbox/items?filter=all", nil)
	req.Header.Set("Authorization", "Bearer "+b.Token())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("inbox request: %v", err)
	}
	defer resp.Body.Close()
	var payload unifiedInboxResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(payload.Items) < 2 {
		t.Fatalf("expected at least 2 items; got %d", len(payload.Items))
	}
	if payload.Items[0].TaskID != "task-old-but-active" {
		t.Fatalf("most-recent-activity item = %q, want task-old-but-active; full order = %+v", payload.Items[0].TaskID, payload.Items)
	}
}

// TestInboxFilterToStates_RejectsUnreadOnLegacyPath documents the
// guard that prevents /tasks/inbox?filter=unread from silently behaving
// like filter=all. The legacy task-only path has no cursor context, so
// the unread filter must be rejected at the bucket layer. The new
// /inbox/items handler unwraps unread to all-states before this call.
func TestInboxFilterToStates_RejectsUnreadOnLegacyPath(t *testing.T) {
	if _, err := inboxFilterToStates(InboxFilterUnread); !errors.Is(err, ErrInboxFilterUnknown) {
		t.Fatalf("inboxFilterToStates(unread) err = %v, want ErrInboxFilterUnknown", err)
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
