package team

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

// brokerWithAutoNotebookWriter wires a real WikiWorker on a temp git repo
// and installs an AutoNotebookWriter on the broker so tests can prove the
// broker never feeds it events. The writer is started but should remain
// idle: notebooks now only accept properly drafted entries authored via
// the notebook_write MCP tool, so no broker hook should reach the writer.
// Returns a teardown that cancels the worker's context and stops the writer.
func brokerWithAutoNotebookWriter(t *testing.T) (*Broker, func()) {
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

	writer := NewAutoNotebookWriter(worker, nil)
	writer.Start(ctx)

	b.mu.Lock()
	b.wikiWorker = worker
	b.autoNotebookWriter = writer
	b.mu.Unlock()

	return b, func() {
		writer.Stop(2 * time.Second)
		cancel()
		<-worker.Done()
	}
}

// noNotebookEntries asserts the agent shelf is empty. The auto-write
// hooks are gone, so PostMessage / MutateTask never spawn writer
// goroutine work; we can read the shelf and the writer counter
// synchronously, with a short WaitForCondition fallback that exits as
// soon as the writer signals no progress (since no progress is the
// expected state). The predicate intentionally never returns true —
// it is a deterministic deadline-bound wait, not a poll.
func noNotebookEntries(t *testing.T, b *Broker, slug string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	_ = b.autoNotebookWriter.WaitForCondition(ctx, func() bool { return false })
	entries, _ := b.wikiWorker.NotebookList(slug)
	if len(entries) > 0 {
		t.Fatalf("expected no notebook entries on %s shelf; got %d: %+v",
			slug, len(entries), entries)
	}
}

// PostMessage from a roster agent must NOT auto-populate a notebook entry.
// Notebooks accept only properly drafted notes authored via notebook_write.
func TestAutoNotebookWriter_PostMessage_DoesNotLandOnShelf(t *testing.T) {
	b, teardown := brokerWithAutoNotebookWriter(t)
	defer teardown()

	if !b.IsAgentMemberSlug("ceo") {
		t.Skip("default manifest missing 'ceo'; cannot exercise integration path")
	}

	if _, err := b.PostMessage("ceo", "general", "shipping pr 1 of the auto-notebook writer", nil, ""); err != nil {
		t.Fatalf("PostMessage: %v", err)
	}

	noNotebookEntries(t, b, "ceo")
	if got := b.autoNotebookWriter.Counters().Enqueued; got != 0 {
		t.Fatalf("expected zero enqueued events from PostMessage; got %d", got)
	}
}

// Human-authored messages also must not land on a shelf — the previous
// roster filter is moot because the hook itself is gone.
func TestAutoNotebookWriter_HumanMessage_DoesNotLandOnShelf(t *testing.T) {
	b, teardown := brokerWithAutoNotebookWriter(t)
	defer teardown()

	if _, err := b.PostMessage("human", "general", "hi from a human", nil, ""); err != nil {
		t.Fatalf("PostMessage human: %v", err)
	}
	if !b.IsAgentMemberSlug("ceo") {
		t.Skip("default manifest missing 'ceo'")
	}
	if _, err := b.PostMessage("ceo", "general", "drain sentinel", nil, ""); err != nil {
		t.Fatalf("PostMessage ceo sentinel: %v", err)
	}

	slugs, err := b.wikiWorker.AgentsWithNotebooks()
	if err != nil {
		t.Fatalf("AgentsWithNotebooks: %v", err)
	}
	for _, s := range slugs {
		entries, _ := b.wikiWorker.NotebookList(s)
		if len(entries) > 0 {
			t.Fatalf("PostMessage produced an entry on %s shelf: %+v", s, entries)
		}
	}
}

// Task creations and transitions must not auto-populate the owner's
// shelf. Notebooks are reserved for drafted notes; status deltas are
// already recorded in the broker task log.
func TestAutoNotebookWriter_TaskMutation_DoesNotLandOnShelf(t *testing.T) {
	b, teardown := brokerWithAutoNotebookWriter(t)
	defer teardown()
	if !b.IsAgentMemberSlug("ceo") {
		t.Skip("default manifest missing 'ceo'")
	}

	resp, err := b.MutateTask(TaskPostRequest{
		Action:    "create",
		Channel:   "general",
		Title:     "Ship PR 1",
		Owner:     "ceo",
		CreatedBy: "ceo",
	})
	if err != nil {
		t.Fatalf("MutateTask create: %v", err)
	}
	if resp.Task.ID == "" {
		t.Fatalf("created task missing id")
	}

	noNotebookEntries(t, b, "ceo")
	if got := b.autoNotebookWriter.Counters().Enqueued; got != 0 {
		t.Fatalf("expected zero enqueued events from MutateTask; got %d", got)
	}
}
