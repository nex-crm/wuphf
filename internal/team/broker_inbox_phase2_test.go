package team

// broker_inbox_phase2_test.go locks the Phase 2 unified-inbox contract
// via four tests written RED before any GREEN implementation lands.
// The spec these tests encode comes from /plan-design-review and
// /plan-eng-review on 2026-05-11 (artifact /tmp/wuphf-unified-inbox-plan.md):
//
//   1. tasksForInbox  — owner sees all; human session sees only tasks
//      where Reviewers contains the slug.
//   2. requestsForInbox — owner sees all; human session sees only
//      requests where From == slug.
//   3. reviewsForInbox — owner sees all; human session sees only
//      promotions where ReviewerSlug == slug.
//   4. SetInboxCursor / InboxCursor read-write race: concurrent writes
//      from one goroutine + concurrent reads from another must not
//      race under `go test -race`, and the final state must be the
//      most recent write.
//   5. Fan-out merge at 1000 mixed-kind items keeps P95 under 100ms.
//
// All five tests fail today because the helpers in
// broker_inbox_phase2.go return zero values. The GREEN pass fills the
// bodies in and the tests turn green without their assertions
// changing.

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestInboxItem_TaskKind_AuthByReviewers exercises the task half of
// the fan-out auth boundary. Three tasks, one assigned to "mira" as
// reviewer. The owner-token actor sees all three; mira's human-session
// actor sees only the one she reviews.
func TestInboxItem_TaskKind_AuthByReviewers(t *testing.T) {
	b := newTestBroker(t)
	now := time.Now().UTC().Format(time.RFC3339)

	b.mu.Lock()
	b.tasks = []teamTask{
		{ID: "task-a", Title: "Mira reviews", CreatedAt: now, Reviewers: []string{"mira"}},
		{ID: "task-b", Title: "Nobody assigned", CreatedAt: now},
		{ID: "task-c", Title: "Alex reviews", CreatedAt: now, Reviewers: []string{"alex"}},
	}
	for _, id := range []string{"task-a", "task-b", "task-c"} {
		if _, err := b.transitionLifecycleLocked(id, LifecycleStateDecision, "phase 2 auth seed"); err != nil {
			b.mu.Unlock()
			t.Fatalf("seed %s: %v", id, err)
		}
	}
	b.mu.Unlock()

	owner := requestActor{Kind: requestActorKindBroker}
	mira := requestActor{Kind: requestActorKindHuman, Slug: "mira", DisplayName: "Mira"}

	ownerRows := b.tasksForInbox(owner)
	if len(ownerRows) != 3 {
		t.Fatalf("ownerRows len = %d, want 3", len(ownerRows))
	}
	for _, row := range ownerRows {
		if row.Kind != InboxItemKindTask {
			t.Fatalf("ownerRows[%q].Kind = %q, want %q", row.TaskID, row.Kind, InboxItemKindTask)
		}
	}

	miraRows := b.tasksForInbox(mira)
	if len(miraRows) != 1 {
		t.Fatalf("miraRows len = %d, want 1 (task-a only)", len(miraRows))
	}
	if miraRows[0].TaskID != "task-a" {
		t.Fatalf("miraRows[0].TaskID = %q, want %q", miraRows[0].TaskID, "task-a")
	}
}

// TestInboxItem_RequestKind_AuthByFrom exercises the request half of
// the fan-out auth boundary. Three pending requests, one from "mira".
// The owner-token actor sees all three; mira's human-session actor
// sees only the one she sent. (Phase 2 OSS-local scope: From is the
// proxy for "who needs to see this"; channel / tagged / assignedTo
// fields land in a v1.1 follow-up that extends the helper without
// changing this contract.)
func TestInboxItem_RequestKind_AuthByFrom(t *testing.T) {
	b := newTestBroker(t)
	now := time.Now().UTC().Format(time.RFC3339)

	b.mu.Lock()
	b.requests = []humanInterview{
		{ID: "req-1", From: "mira", Channel: "general", Question: "Approve?", CreatedAt: now, Kind: "approval"},
		{ID: "req-2", From: "alex", Channel: "general", Question: "Confirm?", CreatedAt: now, Kind: "confirm"},
		{ID: "req-3", From: "alex", Channel: "general", Question: "Choice?", CreatedAt: now, Kind: "choice"},
	}
	b.mu.Unlock()

	owner := requestActor{Kind: requestActorKindBroker}
	mira := requestActor{Kind: requestActorKindHuman, Slug: "mira", DisplayName: "Mira"}

	ownerRows := b.requestsForInbox(owner)
	if len(ownerRows) != 3 {
		t.Fatalf("ownerRows len = %d, want 3", len(ownerRows))
	}
	for _, row := range ownerRows {
		if row.Kind != InboxItemKindRequest {
			t.Fatalf("ownerRows[%q].Kind = %q, want %q", row.RequestID, row.Kind, InboxItemKindRequest)
		}
		if row.Request == nil {
			t.Fatalf("ownerRows[%q].Request peek must be populated", row.RequestID)
		}
	}

	miraRows := b.requestsForInbox(mira)
	if len(miraRows) != 1 {
		t.Fatalf("miraRows len = %d, want 1 (req-1 only)", len(miraRows))
	}
	if miraRows[0].RequestID != "req-1" {
		t.Fatalf("miraRows[0].RequestID = %q, want %q", miraRows[0].RequestID, "req-1")
	}
}

// TestInboxItem_ReviewKind_AuthByAssignedReviewer exercises the review
// half of the fan-out auth boundary. Three pending promotions, one
// assigned to "mira" as the reviewer. The owner-token actor sees all
// three; mira's human-session actor sees only the one assigned to her.
//
// This wires the existing ReviewLog (broker.reviewLog) into the
// fan-out — the helper must call through the same scope filter that
// the existing /review/list?scope=<slug> handler uses.
func TestInboxItem_ReviewKind_AuthByAssignedReviewer(t *testing.T) {
	b := newTestBrokerForReview(t)
	rl := b.ReviewLog()
	if rl == nil {
		t.Skip("ReviewLog not initialized in test broker; skipping until wiki worker harness lands")
	}

	now := time.Now().UTC()
	seedPromotion(t, rl, &Promotion{
		ID:           "rev-1",
		State:        PromotionPending,
		SourceSlug:   "ada",
		SourcePath:   "notebook/ada/draft-1.md",
		TargetPath:   "wiki/draft-1.md",
		ReviewerSlug: "mira",
		CreatedAt:    now,
		UpdatedAt:    now,
	})
	seedPromotion(t, rl, &Promotion{
		ID:           "rev-2",
		State:        PromotionPending,
		SourceSlug:   "ada",
		SourcePath:   "notebook/ada/draft-2.md",
		TargetPath:   "wiki/draft-2.md",
		ReviewerSlug: "alex",
		CreatedAt:    now,
		UpdatedAt:    now,
	})
	seedPromotion(t, rl, &Promotion{
		ID:           "rev-3",
		State:        PromotionPending,
		SourceSlug:   "ada",
		SourcePath:   "notebook/ada/draft-3.md",
		TargetPath:   "wiki/draft-3.md",
		ReviewerSlug: "alex",
		CreatedAt:    now,
		UpdatedAt:    now,
	})

	owner := requestActor{Kind: requestActorKindBroker}
	mira := requestActor{Kind: requestActorKindHuman, Slug: "mira", DisplayName: "Mira"}

	ownerRows := b.reviewsForInbox(owner)
	if len(ownerRows) != 3 {
		t.Fatalf("ownerRows len = %d, want 3", len(ownerRows))
	}
	for _, row := range ownerRows {
		if row.Kind != InboxItemKindReview {
			t.Fatalf("ownerRows[%q].Kind = %q, want %q", row.ReviewID, row.Kind, InboxItemKindReview)
		}
		if row.Review == nil {
			t.Fatalf("ownerRows[%q].Review peek must be populated", row.ReviewID)
		}
		if row.Review.ReviewerSlug == "" {
			t.Fatalf("ownerRows[%q].Review.ReviewerSlug must be populated", row.ReviewID)
		}
	}

	miraRows := b.reviewsForInbox(mira)
	if len(miraRows) != 1 {
		t.Fatalf("miraRows len = %d, want 1 (rev-1 only)", len(miraRows))
	}
	if miraRows[0].ReviewID != "rev-1" {
		t.Fatalf("miraRows[0].ReviewID = %q, want %q", miraRows[0].ReviewID, "rev-1")
	}
}

// TestInboxCursor_ReadWriteRace forces concurrent SetInboxCursor +
// InboxCursor calls on the same slug. Run with `go test -race` — any
// data race in the per-user cursor map surfaces as a runtime failure.
// The assertion at the end pins the final state to the most recent
// write so the no-op stub fails (it always returns IsZero() == true).
func TestInboxCursor_ReadWriteRace(t *testing.T) {
	b := newTestBroker(t)

	const writers = 16
	const reads = 64

	var wg sync.WaitGroup
	wg.Add(writers + 1)

	go func() {
		defer wg.Done()
		for i := 0; i < reads; i++ {
			_ = b.InboxCursor("mira")
		}
	}()

	base := time.Now().UTC()
	for w := 0; w < writers; w++ {
		w := w
		go func() {
			defer wg.Done()
			cursor := InboxCursor{LastSeenAt: base.Add(time.Duration(w) * time.Millisecond)}
			b.SetInboxCursor("mira", cursor)
		}()
	}
	wg.Wait()

	final := b.InboxCursor("mira")
	if final.IsZero() {
		t.Fatal("final cursor for 'mira' is zero; SetInboxCursor must persist at least one write")
	}
	// "Most recent write wins" + rewind guard: the surviving cursor
	// MUST be the highest-LastSeenAt writer (writer N-1 here), no
	// matter the goroutine scheduling order. A weaker stub that keeps
	// "whatever wrote last in wall time" would pass `>= base` but
	// fail this exact equality.
	want := base.Add(time.Duration(writers-1) * time.Millisecond)
	if !final.LastSeenAt.Equal(want) {
		t.Fatalf("final cursor LastSeenAt = %s, want %s (newest writer must win)", final.LastSeenAt, want)
	}
}

// TestInboxFanout_1000MixedItems_P95Under100ms seeds 333 tasks +
// 333 requests + 333 promotions and asserts the fan-out merge stays
// under the 100ms ceiling on a developer laptop. Builds on the
// existing 1000-task perf test but exercises the new merge path
// rather than the indexed-bucket lookup alone.
func TestInboxFanout_1000MixedItems_P95Under100ms(t *testing.T) {
	if testing.Short() {
		t.Skip("perf test; runs in full mode only")
	}

	const taskCount = 333
	const requestCount = 333
	const reviewCount = 334
	const ceiling = 100 * time.Millisecond

	b := newTestBrokerForReview(t)
	rl := b.ReviewLog()

	now := time.Now().UTC()
	b.mu.Lock()
	for i := 0; i < taskCount; i++ {
		id := fmt.Sprintf("task-%04d", i)
		task := teamTask{
			ID:        id,
			Title:     fmt.Sprintf("Task %d", i),
			CreatedAt: now.Add(-time.Duration(i) * time.Minute).Format(time.RFC3339),
		}
		b.tasks = append(b.tasks, task)
		if _, err := b.transitionLifecycleLocked(id, LifecycleStateDecision, "fanout seed"); err != nil {
			b.mu.Unlock()
			t.Fatalf("seed task %s: %v", id, err)
		}
	}
	for i := 0; i < requestCount; i++ {
		b.requests = append(b.requests, humanInterview{
			ID:        fmt.Sprintf("req-%04d", i),
			From:      "owner",
			Channel:   "general",
			Question:  fmt.Sprintf("Question %d", i),
			Kind:      "approval",
			CreatedAt: now.Add(-time.Duration(i) * time.Minute).Format(time.RFC3339),
		})
	}
	b.mu.Unlock()

	if rl != nil {
		for i := 0; i < reviewCount; i++ {
			seedPromotion(t, rl, &Promotion{
				ID:           fmt.Sprintf("rev-%04d", i),
				State:        PromotionPending,
				SourceSlug:   "ada",
				SourcePath:   fmt.Sprintf("notebook/ada/n-%d.md", i),
				TargetPath:   fmt.Sprintf("wiki/n-%d.md", i),
				ReviewerSlug: "owner",
				CreatedAt:    now,
				UpdatedAt:    now,
			})
		}
	}

	owner := requestActor{Kind: requestActorKindBroker}

	// Warm-up so the timed run measures steady-state.
	if _, err := b.inboxItemsForActor(owner, InboxFilterAll); err != nil {
		t.Fatalf("warm-up: %v", err)
	}

	const samples = 5
	var slowest time.Duration
	for i := 0; i < samples; i++ {
		start := time.Now()
		rows, err := b.inboxItemsForActor(owner, InboxFilterAll)
		elapsed := time.Since(start)
		if err != nil {
			t.Fatalf("inboxItemsForActor sample %d: %v", i, err)
		}
		if len(rows) < taskCount+requestCount {
			t.Fatalf("rows = %d, want at least %d (tasks+requests, reviews optional)", len(rows), taskCount+requestCount)
		}
		// Smoke: kind distribution must include all three when reviews seeded.
		if rl != nil {
			counts := map[InboxItemKind]int{}
			for _, r := range rows {
				counts[r.Kind]++
			}
			if counts[InboxItemKindTask] == 0 || counts[InboxItemKindRequest] == 0 || counts[InboxItemKindReview] == 0 {
				t.Fatalf("kind distribution missing entries: %+v", counts)
			}
		}
		if elapsed > slowest {
			slowest = elapsed
		}
	}
	t.Logf("inboxItemsForActor slowest across %d samples: %s (ceiling %s)", samples, slowest, ceiling)
	if slowest > ceiling {
		t.Fatalf("inboxItemsForActor P95-proxy = %s > ceiling %s", slowest, ceiling)
	}
}

// newTestBrokerForReview returns a test broker with the ReviewLog
// initialized so the review-kind tests can seed promotions directly.
// Bypasses the wiki-worker dependency by constructing the ReviewLog
// with a tmpfile path — the in-memory promotions map is the only
// state the per-kind auth filter reads.
func newTestBrokerForReview(t *testing.T) *Broker {
	t.Helper()
	b := newTestBroker(t)
	rl, err := NewReviewLog(filepath.Join(t.TempDir(), "review-log.jsonl"), nil, nil)
	if err != nil {
		t.Fatalf("init review log: %v", err)
	}
	b.mu.Lock()
	b.reviewLog = rl
	b.mu.Unlock()
	return b
}

// seedPromotion inserts a Promotion directly into the ReviewLog's
// in-memory map for tests. Bypasses the JSONL append path because
// the per-kind auth filter only reads the in-memory list.
func seedPromotion(t *testing.T, rl *ReviewLog, p *Promotion) {
	t.Helper()
	if rl == nil {
		return
	}
	rl.mu.Lock()
	defer rl.mu.Unlock()
	if rl.promotions == nil {
		rl.promotions = map[string]*Promotion{}
	}
	rl.promotions[p.ID] = p
	// Strip any leading whitespace the test fixtures may carry.
	p.ID = strings.TrimSpace(p.ID)
}
