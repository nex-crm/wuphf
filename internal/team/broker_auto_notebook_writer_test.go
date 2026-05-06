package team

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// brokerWithAutoNotebookWriter wires a real WikiWorker on a temp git repo and
// installs an AutoNotebookWriter on the broker. Returns a teardown that cancels
// the worker's context and stops the writer.
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

	// Roster filtering happens at the broker hook sites under b.mu; the writer
	// gets nil here so calling Handle from inside a b.mu critical section does
	// not re-enter the broker mutex.
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

func waitForNotebookEntry(t *testing.T, b *Broker, slug string) []NotebookEntry {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := b.autoNotebookWriter.WaitForCondition(ctx, func() bool {
		entries, listErr := b.wikiWorker.NotebookList(slug)
		return listErr == nil && len(entries) > 0
	}); err != nil {
		t.Fatalf("no notebook entries appeared for slug=%s: %v", slug, err)
	}
	entries, err := b.wikiWorker.NotebookList(slug)
	if err != nil {
		t.Fatalf("NotebookList(%s): %v", slug, err)
	}
	return entries
}

// Real broker + real WikiWorker. PostMessage from the ceo agent must produce a
// notebook entry under agents/ceo/notebook/ within seconds.
func TestAutoNotebookWriter_PostMessage_LandsOnShelf(t *testing.T) {
	b, teardown := brokerWithAutoNotebookWriter(t)
	defer teardown()

	if !b.IsAgentMemberSlug("ceo") {
		t.Skip("default manifest missing 'ceo'; cannot exercise integration path")
	}

	if _, err := b.PostMessage("ceo", "general", "shipping pr 1 of the auto-notebook writer", nil, ""); err != nil {
		t.Fatalf("PostMessage: %v", err)
	}

	entries := waitForNotebookEntry(t, b, "ceo")
	if !strings.Contains(entries[0].Path, "agents/ceo/notebook/") {
		t.Fatalf("entry not on ceo shelf: %q", entries[0].Path)
	}
	if !strings.Contains(entries[0].Path, "message-posted") {
		t.Fatalf("expected message-posted in filename: %q", entries[0].Path)
	}
}

// Human-authored messages must NOT populate the agent shelf. The roster filter
// (decision OV6A) gates this at the writer's ingress.
func TestAutoNotebookWriter_HumanMessage_DoesNotLandOnShelf(t *testing.T) {
	b, teardown := brokerWithAutoNotebookWriter(t)
	defer teardown()

	// Post a sentinel agent message AFTER the human one so we have a
	// deterministic signal that the writer pipeline drained: when the agent
	// entry lands on the ceo shelf, every prior PostMessage hook has already
	// run to completion. If the human PostMessage incorrectly produced a
	// shelf entry, NotebookList for any non-ceo agent will be non-empty.
	if _, err := b.PostMessage("human", "general", "hi from a human", nil, ""); err != nil {
		t.Fatalf("PostMessage human: %v", err)
	}
	if !b.IsAgentMemberSlug("ceo") {
		t.Skip("default manifest missing 'ceo'; cannot drain via agent sentinel")
	}
	if _, err := b.PostMessage("ceo", "general", "drain sentinel", nil, ""); err != nil {
		t.Fatalf("PostMessage ceo sentinel: %v", err)
	}
	waitForNotebookEntry(t, b, "ceo")

	slugs, err := b.wikiWorker.AgentsWithNotebooks()
	if err != nil {
		t.Fatalf("AgentsWithNotebooks: %v", err)
	}
	for _, s := range slugs {
		if s == "ceo" {
			continue
		}
		entries, _ := b.wikiWorker.NotebookList(s)
		if len(entries) > 0 {
			t.Fatalf("human message produced an entry on %s shelf", s)
		}
	}
}

// Two consecutive identical messages collapse to one shelf entry via the LRU.
func TestAutoNotebookWriter_DuplicatePostMessageDeduped(t *testing.T) {
	b, teardown := brokerWithAutoNotebookWriter(t)
	defer teardown()
	if !b.IsAgentMemberSlug("ceo") {
		t.Skip("default manifest missing 'ceo'")
	}

	if _, err := b.PostMessage("ceo", "general", "exactly the same thing", nil, ""); err != nil {
		t.Fatalf("PostMessage 1: %v", err)
	}
	waitForNotebookEntry(t, b, "ceo")
	if _, err := b.PostMessage("ceo", "general", "exactly the same thing", nil, ""); err != nil {
		t.Fatalf("PostMessage 2: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := b.autoNotebookWriter.WaitForCondition(ctx, func() bool {
		return b.autoNotebookWriter.Counters().Deduped >= 1
	}); err != nil {
		t.Fatalf("expected dedupe to fire on identical repost: %v", err)
	}
	entries, _ := b.wikiWorker.NotebookList("ceo")
	if len(entries) != 1 {
		t.Fatalf("expected exactly 1 entry after dedupe; got %d", len(entries))
	}
}

// Task transitions populate the owner's shelf, distinct kind in filename.
func TestAutoNotebookWriter_TaskMutationLandsOnOwnerShelf(t *testing.T) {
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
	taskID := resp.Task.ID
	if taskID == "" {
		t.Fatalf("created task missing id")
	}

	entries := waitForNotebookEntry(t, b, "ceo")
	var transitionEntry *NotebookEntry
	for i := range entries {
		if strings.Contains(entries[i].Path, "task-transitioned") {
			transitionEntry = &entries[i]
			break
		}
	}
	if transitionEntry == nil {
		t.Fatalf("expected task-transitioned entry on ceo shelf; got entries: %v", entries)
	}
}
