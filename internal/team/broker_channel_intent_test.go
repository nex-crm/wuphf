package team

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

// brokerWithChannelIntentDispatcher wires a real WikiWorker on a temp git
// repo plus a NotebookDemandIndex and a ChannelIntentDispatcher. Mirrors
// the brokerWithHumanWikiWriter pattern used by PR 2's integration test.
//
// Cleanup must stop the dispatcher BEFORE cancelling the wiki worker's
// context so the goroutine drains cleanly without races.
func brokerWithChannelIntentDispatcher(t *testing.T) (*Broker, *Repo, func()) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "wiki")
	backup := filepath.Join(t.TempDir(), "wiki.bak")
	repo := NewRepoAt(root, backup)
	if err := repo.Init(context.Background()); err != nil {
		t.Fatalf("repo init: %v", err)
	}
	b := newTestBroker(t)
	worker := NewWikiWorker(repo, b)
	ctx, cancel := context.WithCancel(context.Background())
	worker.Start(ctx)

	demandPath := filepath.Join(t.TempDir(), "events.jsonl")
	idx, err := NewNotebookDemandIndex(demandPath)
	if err != nil {
		t.Fatalf("NewNotebookDemandIndex: %v", err)
	}

	dispatcher := NewChannelIntentDispatcher(b)
	dispatcher.Start(ctx)

	b.mu.Lock()
	b.wikiWorker = worker
	b.demandIndex = idx
	b.channelIntentDispatcher = dispatcher
	b.mu.Unlock()

	return b, repo, func() {
		dispatcher.Stop(2 * time.Second)
		cancel()
		<-worker.Done()
	}
}

// writeNotebookEntryDirect populates a notebook entry by going through the
// wiki worker's NotebookWrite path. The integration test needs a real entry
// on disk for NotebookSearch to find.
func writeNotebookEntryDirect(t *testing.T, b *Broker, slug, path, content string) {
	t.Helper()
	worker := b.WikiWorker()
	if worker == nil {
		t.Fatalf("wiki worker not wired")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, _, err := worker.NotebookWrite(ctx, slug, path, content, "create", "test entry"); err != nil {
		t.Fatalf("NotebookWrite slug=%s path=%s: %v", slug, path, err)
	}
}

// TestChannelIntent_ContextAskRecordsDemand drives the end-to-end flow:
// human posts "who has context on X", an agent's notebook has an entry that
// matches X, and the dispatcher records a DemandSignalChannelContextAsk
// for the matched (path, owner) pair.
func TestChannelIntent_ContextAskRecordsDemand(t *testing.T) {
	b, _, teardown := brokerWithChannelIntentDispatcher(t)
	defer teardown()

	notebookPath := "agents/pm/notebook/2026-05-06-icp.md"
	writeNotebookEntryDirect(t, b, "pm", notebookPath,
		"# our ICP\n\nfounders running 3+ AI agents are the wedge.\n")

	if _, err := b.PostMessage("human", "general",
		"who has context on our ICP", nil, ""); err != nil {
		t.Fatalf("PostMessage: %v", err)
	}

	// Wait for the dispatcher to process the message and the demand index
	// to record at least one event for the matched path.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := b.channelIntentDispatcher.WaitForCondition(ctx, func() bool {
		return b.channelIntentDispatcher.Counters().DemandFired >= 1
	}); err != nil {
		t.Fatalf("expected DemandFired >= 1 (counters=%+v): %v",
			b.channelIntentDispatcher.Counters(), err)
	}

	// Demand index should reflect the channel-context-ask weight (2.0).
	if got := b.demandIndex.Score(notebookPath); got < 2.0 {
		t.Fatalf("demand score for %q = %v, want >= 2.0", notebookPath, got)
	}

	// Counters: one classified, at least one search, and at least one hit.
	c := b.channelIntentDispatcher.Counters()
	if c.Classified < 1 {
		t.Fatalf("expected Classified >= 1, got %d (counters=%+v)", c.Classified, c)
	}
	if c.Searched < 1 {
		t.Fatalf("expected Searched >= 1, got %d (counters=%+v)", c.Searched, c)
	}
	if c.HitsFound < 1 {
		t.Fatalf("expected HitsFound >= 1, got %d (counters=%+v)", c.HitsFound, c)
	}
}

// TestChannelIntent_NoHitsNoDemand asserts that a context-ask which finds
// nothing in any notebook records ZERO demand events. The classifier still
// matched (Classified increments), but with no notebook entries to point
// at, the demand index stays empty.
func TestChannelIntent_NoHitsNoDemand(t *testing.T) {
	b, _, teardown := brokerWithChannelIntentDispatcher(t)
	defer teardown()

	if _, err := b.PostMessage("human", "general",
		"who has context on the unrelated billing migration", nil, ""); err != nil {
		t.Fatalf("PostMessage: %v", err)
	}

	// Wait for classification to happen — the dispatcher must have
	// processed the message to a terminal state. Searched will increment
	// even on an empty corpus (NotebookSearchAll runs over zero slugs but
	// is still a "we searched" event).
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := b.channelIntentDispatcher.WaitForCondition(ctx, func() bool {
		return b.channelIntentDispatcher.Counters().Classified >= 1
	}); err != nil {
		t.Fatalf("classifier never fired (counters=%+v): %v",
			b.channelIntentDispatcher.Counters(), err)
	}
	// And wait for the search to complete so we can assert on hits.
	if err := b.channelIntentDispatcher.WaitForCondition(ctx, func() bool {
		return b.channelIntentDispatcher.Counters().Searched >= 1
	}); err != nil {
		t.Fatalf("search never ran (counters=%+v): %v",
			b.channelIntentDispatcher.Counters(), err)
	}

	c := b.channelIntentDispatcher.Counters()
	if c.HitsFound != 0 {
		t.Fatalf("expected HitsFound=0 with no matching entries, got %d", c.HitsFound)
	}
	if c.DemandFired != 0 {
		t.Fatalf("expected DemandFired=0 with no matching entries, got %d", c.DemandFired)
	}
}

// TestChannelIntent_NonQuestionMessageSkipped exercises the question-form
// guard: a statement-form message ("I have context on X") must NOT trigger
// the search/record path.
func TestChannelIntent_NonQuestionMessageSkipped(t *testing.T) {
	b, _, teardown := brokerWithChannelIntentDispatcher(t)
	defer teardown()

	// First post a non-question message: must NOT classify.
	if _, err := b.PostMessage("human", "general",
		"I have context on the billing migration", nil, ""); err != nil {
		t.Fatalf("PostMessage: %v", err)
	}
	// Then a sentinel question that DOES classify, used as a drain barrier.
	if _, err := b.PostMessage("human", "general",
		"who has context on the billing migration", nil, ""); err != nil {
		t.Fatalf("PostMessage sentinel: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := b.channelIntentDispatcher.WaitForCondition(ctx, func() bool {
		return b.channelIntentDispatcher.Counters().Classified >= 1
	}); err != nil {
		t.Fatalf("sentinel never classified (counters=%+v): %v",
			b.channelIntentDispatcher.Counters(), err)
	}

	c := b.channelIntentDispatcher.Counters()
	// Two messages were enqueued; exactly one classified (the sentinel)
	// and exactly one was skipped (the statement-form first message).
	if c.Enqueued != 2 {
		t.Fatalf("expected Enqueued=2, got %d (counters=%+v)", c.Enqueued, c)
	}
	if c.Classified != 1 {
		t.Fatalf("expected Classified=1 (sentinel only), got %d (counters=%+v)", c.Classified, c)
	}
	if c.Skipped != 1 {
		t.Fatalf("expected Skipped=1 (statement form), got %d (counters=%+v)", c.Skipped, c)
	}
}

// TestChannelIntent_AgentSenderAlsoTriggers verifies the hook fires for
// agent senders as well as humans: a roster agent posting "who has context
// on X" must trigger classification and demand recording the same way a
// human does. The classifier inside the dispatcher is what filters; the
// hook itself does NOT pre-filter on sender role.
//
// This guards against a regression where a future change accidentally
// routes the dispatcher only on isHumanMessageSender, losing agent-to-agent
// context-asks.
func TestChannelIntent_AgentSenderAlsoTriggers(t *testing.T) {
	b, _, teardown := brokerWithChannelIntentDispatcher(t)
	defer teardown()

	if !b.IsAgentMemberSlug("ceo") {
		t.Skip("default manifest missing 'ceo'; cannot exercise agent-sender path")
	}

	// Plant a notebook entry on the PM's shelf for the agent to ask about.
	notebookPath := "agents/pm/notebook/2026-05-06-onboarding.md"
	writeNotebookEntryDirect(t, b, "pm", notebookPath,
		"# onboarding gotchas\n\nthe bun version pin is required.\n")

	if _, err := b.PostMessage("ceo", "general",
		"who has context on onboarding gotchas", nil, ""); err != nil {
		t.Fatalf("PostMessage: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := b.channelIntentDispatcher.WaitForCondition(ctx, func() bool {
		return b.channelIntentDispatcher.Counters().DemandFired >= 1
	}); err != nil {
		t.Fatalf("agent-sender context-ask did not fire demand (counters=%+v): %v",
			b.channelIntentDispatcher.Counters(), err)
	}
	if got := b.demandIndex.Score(notebookPath); got < 2.0 {
		t.Fatalf("demand score = %v, want >= 2.0", got)
	}
}
