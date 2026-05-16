package team

// broker_inbox_threads_test.go exercises Phase 3 thread grouping:
// per-agent buckets, preview enrichment from DM messages, and the
// interleaved event stream returned by /inbox/threads/{slug}.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestInboxThreads_GroupsItemsByAgent(t *testing.T) {
	b := newTestBrokerForReview(t)
	rl := b.ReviewLog()
	now := time.Now().UTC()

	b.mu.Lock()
	b.members = []officeMember{
		{Slug: "mira", Name: "Mira", Role: "Backend lead"},
		{Slug: "ada", Name: "Ada", Role: "Wiki curator"},
	}
	b.tasks = []teamTask{
		{ID: "task-a", Title: "Mira refactor", CreatedAt: now.Add(-30 * time.Minute).Format(time.RFC3339), Owner: "mira"},
		{ID: "task-b", Title: "Mira second task", CreatedAt: now.Add(-15 * time.Minute).Format(time.RFC3339), Owner: "mira"},
	}
	for _, id := range []string{"task-a", "task-b"} {
		if _, err := b.transitionLifecycleLocked(id, LifecycleStateDecision, "phase 3 seed"); err != nil {
			b.mu.Unlock()
			t.Fatalf("seed %s: %v", id, err)
		}
	}
	b.requests = []humanInterview{
		{ID: "req-1", From: "ada", Channel: "general", Question: "Bump dep?", Kind: "approval", CreatedAt: now.Add(-10 * time.Minute).Format(time.RFC3339)},
	}
	b.mu.Unlock()
	seedPromotion(t, rl, &Promotion{
		ID:           "rev-1",
		State:        PromotionPending,
		SourceSlug:   "ada",
		SourcePath:   "notebook/ada/draft.md",
		TargetPath:   "wiki/draft.md",
		ReviewerSlug: "owner",
		CreatedAt:    now.Add(-5 * time.Minute),
		UpdatedAt:    now.Add(-5 * time.Minute),
	})

	payload, err := b.inboxThreadsForActor(requestActor{Kind: requestActorKindBroker})
	if err != nil {
		t.Fatalf("inboxThreadsForActor: %v", err)
	}
	if len(payload.Threads) != 2 {
		t.Fatalf("threads = %d, want 2 (mira + ada); got %+v", len(payload.Threads), payload.Threads)
	}

	byAgent := map[string]InboxThread{}
	for _, th := range payload.Threads {
		byAgent[th.AgentSlug] = th
	}
	mira, ok := byAgent["mira"]
	if !ok {
		t.Fatal("expected thread for mira")
	}
	if mira.PendingCount != 2 || len(mira.Items) != 2 {
		t.Fatalf("mira: pending=%d items=%d, want 2/2", mira.PendingCount, len(mira.Items))
	}
	if mira.AgentName != "Mira" {
		t.Fatalf("mira AgentName = %q, want %q", mira.AgentName, "Mira")
	}
	if mira.DMChannel == "" {
		t.Fatal("mira DMChannel should be populated for non-system threads")
	}

	ada, ok := byAgent["ada"]
	if !ok {
		t.Fatal("expected thread for ada")
	}
	if ada.PendingCount != 2 || len(ada.Items) != 2 {
		t.Fatalf("ada: pending=%d items=%d, want 2/2 (1 request + 1 review)", ada.PendingCount, len(ada.Items))
	}
	kinds := map[InboxItemKind]int{}
	for _, item := range ada.Items {
		kinds[item.Kind]++
	}
	if kinds[InboxItemKindRequest] != 1 || kinds[InboxItemKindReview] != 1 {
		t.Fatalf("ada kinds = %+v, want 1 request + 1 review", kinds)
	}
}

func TestInboxThreads_PreviewFromLatestMessage(t *testing.T) {
	b := newTestBroker(t)
	now := time.Now().UTC()
	b.mu.Lock()
	b.members = []officeMember{
		{Slug: "mira", Name: "Mira"},
	}
	b.tasks = []teamTask{
		{ID: "task-a", Title: "Refactor X", CreatedAt: now.Add(-30 * time.Minute).Format(time.RFC3339), Owner: "mira"},
	}
	if _, err := b.transitionLifecycleLocked("task-a", LifecycleStateDecision, "preview seed"); err != nil {
		b.mu.Unlock()
		t.Fatalf("seed: %v", err)
	}
	b.messages = []channelMessage{
		{ID: "m-1", From: "mira", Channel: "general", Content: "I just shipped the refactor", Timestamp: now.Add(-1 * time.Minute).Format(time.RFC3339)},
	}
	b.mu.Unlock()

	payload, err := b.inboxThreadsForActor(requestActor{Kind: requestActorKindBroker})
	if err != nil {
		t.Fatalf("inboxThreadsForActor: %v", err)
	}
	if len(payload.Threads) != 1 {
		t.Fatalf("threads = %d, want 1", len(payload.Threads))
	}
	if got, want := payload.Threads[0].Preview, "I just shipped the refactor"; got != want {
		t.Fatalf("preview = %q, want %q (message should beat task title because it's newer)", got, want)
	}
}

func TestInboxThreads_DetailInterleavesMessagesAndItems(t *testing.T) {
	b := newTestBroker(t)
	now := time.Now().UTC()
	b.mu.Lock()
	b.members = []officeMember{{Slug: "mira", Name: "Mira"}}
	b.tasks = []teamTask{
		{ID: "task-a", Title: "Decide", CreatedAt: now.Add(-10 * time.Minute).Format(time.RFC3339), Owner: "mira"},
	}
	if _, err := b.transitionLifecycleLocked("task-a", LifecycleStateDecision, "detail seed"); err != nil {
		b.mu.Unlock()
		t.Fatalf("seed: %v", err)
	}
	b.messages = []channelMessage{
		{ID: "m-1", From: "mira", Channel: "general", Content: "starting work", Timestamp: now.Add(-20 * time.Minute).Format(time.RFC3339)},
		{ID: "m-2", From: "mira", Channel: "general", Content: "almost done", Timestamp: now.Add(-5 * time.Minute).Format(time.RFC3339)},
	}
	b.mu.Unlock()

	detail, err := b.inboxThreadDetailForActor(requestActor{Kind: requestActorKindBroker}, "mira")
	if err != nil {
		t.Fatalf("detail: %v", err)
	}
	// Expect 3 events: message m-1, item task-a, message m-2.
	if len(detail.Events) != 3 {
		t.Fatalf("events = %d, want 3 (got %+v)", len(detail.Events), detail.Events)
	}
	if detail.Events[0].Kind != InboxThreadEventMessage || detail.Events[0].Message.ID != "m-1" {
		t.Fatalf("events[0] = %+v, want message m-1", detail.Events[0])
	}
	if detail.Events[1].Kind != InboxThreadEventItem || detail.Events[1].Item.TaskID != "task-a" {
		t.Fatalf("events[1] = %+v, want item task-a", detail.Events[1])
	}
	if detail.Events[2].Kind != InboxThreadEventMessage || detail.Events[2].Message.ID != "m-2" {
		t.Fatalf("events[2] = %+v, want message m-2", detail.Events[2])
	}
}

func TestHandleInboxThreads_Owner(t *testing.T) {
	b := newTestBroker(t)
	now := time.Now().UTC()
	b.mu.Lock()
	b.members = []officeMember{{Slug: "mira", Name: "Mira"}}
	b.tasks = []teamTask{
		{ID: "task-a", Title: "Decide", CreatedAt: now.Format(time.RFC3339), Owner: "mira"},
	}
	if _, err := b.transitionLifecycleLocked("task-a", LifecycleStateDecision, "handler seed"); err != nil {
		b.mu.Unlock()
		t.Fatalf("seed: %v", err)
	}
	b.mu.Unlock()

	mux := http.NewServeMux()
	mux.HandleFunc("/inbox/threads", b.requireAuth(b.handleInboxThreads))
	srv := httptest.NewServer(mux)
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/inbox/threads", nil)
	req.Header.Set("Authorization", "Bearer "+b.Token())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var payload InboxThreadsResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(payload.Threads) != 1 {
		t.Fatalf("threads = %d, want 1", len(payload.Threads))
	}
	if !strings.EqualFold(payload.Threads[0].AgentSlug, "mira") {
		t.Fatalf("thread slug = %q, want %q", payload.Threads[0].AgentSlug, "mira")
	}
}
