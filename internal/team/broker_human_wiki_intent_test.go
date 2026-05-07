package team

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// brokerWithHumanWikiWriter wires a real WikiWorker on a temp git repo and
// installs a HumanWikiIntentWriter on the broker. Mirrors
// brokerWithAutoNotebookWriter from PR 1.
func brokerWithHumanWikiWriter(t *testing.T) (*Broker, *Repo, func()) {
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

	humanWriter := NewHumanWikiIntentWriter(worker)
	humanWriter.Start(ctx)

	b.mu.Lock()
	b.wikiWorker = worker
	b.humanWikiWriter = humanWriter
	b.mu.Unlock()

	return b, repo, func() {
		humanWriter.Stop(2 * time.Second)
		cancel()
		<-worker.Done()
	}
}

// listTeamWikiFiles returns paths (relative to repo root) of every .md file
// under team/ that was written by the human wiki intent writer.
func listTeamWikiFiles(t *testing.T, repo *Repo) []string {
	t.Helper()
	teamDir := repo.TeamDir()
	var paths []string
	err := filepath.Walk(teamDir, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(p, ".md") {
			return nil
		}
		rel, relErr := filepath.Rel(repo.Root(), p)
		if relErr != nil {
			return relErr
		}
		paths = append(paths, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		t.Fatalf("walk team dir: %v", err)
	}
	return paths
}

// Real broker + real WikiWorker. PostMessage from a human containing a
// remember-intent phrase must produce a wiki entry under team/ within seconds.
func TestHumanWikiIntent_PostMessage_LandsInTeamWiki(t *testing.T) {
	b, repo, teardown := brokerWithHumanWikiWriter(t)
	defer teardown()

	if _, err := b.PostMessage("human", "general",
		"remember this: the retro deadline is every Friday", nil, ""); err != nil {
		t.Fatalf("PostMessage: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := b.humanWikiWriter.WaitForCondition(ctx, func() bool {
		return b.humanWikiWriter.Counters().Written >= 1
	}); err != nil {
		t.Fatalf("expected wiki entry to be written: %v (counters=%+v)",
			err, b.humanWikiWriter.Counters())
	}

	paths := listTeamWikiFiles(t, repo)
	found := false
	for _, p := range paths {
		lower := strings.ToLower(p)
		if strings.HasPrefix(p, "team/") &&
			(strings.Contains(lower, "retro") ||
				strings.Contains(lower, "friday") ||
				strings.Contains(lower, "deadline")) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected a team/ entry referencing the topic; got: %v", paths)
	}
}

// Agent senders must NOT trigger a human-wiki write — the hook is human-only.
func TestHumanWikiIntent_AgentSenderProducesNoWrite(t *testing.T) {
	b, _, teardown := brokerWithHumanWikiWriter(t)
	defer teardown()

	if !b.IsAgentMemberSlug("ceo") {
		t.Skip("default manifest missing 'ceo'; cannot exercise agent-sender path")
	}

	if _, err := b.PostMessage("ceo", "general",
		"remember this: agents must not trigger this path", nil, ""); err != nil {
		t.Fatalf("PostMessage: %v", err)
	}

	// Drain by posting a human sentinel that DOES match — when its write
	// lands, every prior PostMessage hook has run to completion.
	if _, err := b.PostMessage("human", "general",
		"remember this: human sentinel", nil, ""); err != nil {
		t.Fatalf("PostMessage human sentinel: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := b.humanWikiWriter.WaitForCondition(ctx, func() bool {
		return b.humanWikiWriter.Counters().Written >= 1
	}); err != nil {
		t.Fatalf("sentinel never wrote: %v", err)
	}

	// Exactly one Written — from the sentinel only. Enqueued must also be 1
	// because the agent message was filtered at the hook site, never reaching
	// Handle.
	c := b.humanWikiWriter.Counters()
	if c.Written != 1 {
		t.Fatalf("expected exactly 1 wiki write (sentinel only); got %d (counters=%+v)",
			c.Written, c)
	}
	if c.Enqueued != 1 {
		t.Fatalf("expected Enqueued=1 (agent filtered at hook); got %d", c.Enqueued)
	}
}

// Human messages without an intent phrase enqueue but the writer goroutine
// classifies and skips — Skipped counter increments, no wiki write happens.
func TestHumanWikiIntent_NonIntentMessageSkipped(t *testing.T) {
	b, _, teardown := brokerWithHumanWikiWriter(t)
	defer teardown()

	if _, err := b.PostMessage("human", "general",
		"the dashboard is loading slowly today", nil, ""); err != nil {
		t.Fatalf("PostMessage: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := b.humanWikiWriter.WaitForCondition(ctx, func() bool {
		return b.humanWikiWriter.Counters().Skipped >= 1
	}); err != nil {
		t.Fatalf("expected Skipped counter to fire: %v (counters=%+v)",
			err, b.humanWikiWriter.Counters())
	}
	if b.humanWikiWriter.Counters().Written != 0 {
		t.Fatalf("non-intent message must not write; got Written=%d",
			b.humanWikiWriter.Counters().Written)
	}
}
