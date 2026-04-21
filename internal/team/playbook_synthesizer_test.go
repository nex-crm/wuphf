package team

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// playbookPublisherStub captures synthesis events for assertions.
type playbookPublisherStub struct {
	mu     sync.Mutex
	events []PlaybookSynthesizedEvent
}

func (p *playbookPublisherStub) PublishPlaybookSynthesized(evt PlaybookSynthesizedEvent) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.events = append(p.events, evt)
}

func (p *playbookPublisherStub) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.events)
}

// newPlaybookSynthFixture wires repo + worker + execution log + synth.
// The llm stub is injected via PlaybookSynthesizerConfig.LLMCall.
func newPlaybookSynthFixture(
	t *testing.T,
	llmStub func(ctx context.Context, sys, user string) (string, error),
) (*PlaybookSynthesizer, *ExecutionLog, *WikiWorker, *playbookPublisherStub, func()) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "wiki")
	backup := filepath.Join(t.TempDir(), "wiki.bak")
	repo := NewRepoAt(root, backup)
	if err := repo.Init(context.Background()); err != nil {
		t.Fatalf("init: %v", err)
	}
	worker := NewWikiWorker(repo, noopPublisher{})
	ctx, cancel := context.WithCancel(context.Background())
	worker.Start(ctx)

	execLog := NewExecutionLog(worker)
	pub := &playbookPublisherStub{}
	synth := NewPlaybookSynthesizer(worker, execLog, pub, PlaybookSynthesizerConfig{
		Threshold: 2,
		Timeout:   5 * time.Second,
		LLMCall:   llmStub,
	})
	synth.Start(context.Background())

	teardown := func() {
		synth.Stop()
		cancel()
		<-worker.Done()
	}
	return synth, execLog, worker, pub, teardown
}

// waitForSynthCount polls for n events.
func waitForSynthCount(t *testing.T, pub *playbookPublisherStub, n int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if pub.count() >= n {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d synth events; got %d", n, pub.count())
}

const seededPlaybookBody = `---
author: pm
---

# Churn prevention

## What to do

1. Pull the account's ARR.
2. Page the CSM.
3. Draft a save-offer DM.
`

func TestPlaybookSynthesizer_PreservesFrontmatterAndAuthorBody(t *testing.T) {
	stub := func(ctx context.Context, sys, user string) (string, error) {
		// LLM returns a draft that includes the learnings section. The
		// synthesizer must splice learnings onto the original body, NOT
		// adopt the LLM's rewrite.
		return "# I rewrote the body\n\nAll new steps.\n\n## What we've learned\n\n- The CSM is the fastest path.\n", nil
	}
	synth, execLog, worker, pub, teardown := newPlaybookSynthFixture(t, stub)
	defer teardown()
	ctx := context.Background()

	writePlaybookSource(t, worker, "churn-prevention", seededPlaybookBody)
	_, err := execLog.Append(ctx, "churn-prevention", PlaybookOutcomeSuccess, "Saved the account.", "", "cmo")
	if err != nil {
		t.Fatalf("append: %v", err)
	}
	_, err = execLog.Append(ctx, "churn-prevention", PlaybookOutcomePartial, "Blocked on legal.", "", "cmo")
	if err != nil {
		t.Fatalf("append: %v", err)
	}

	if _, err := synth.SynthesizeNow(ctx, "churn-prevention", "human"); err != nil {
		t.Fatalf("synthesize: %v", err)
	}
	waitForSynthCount(t, pub, 1, 3*time.Second)

	bytes, err := readArticle(worker.Repo(), playbookSourceRel("churn-prevention"))
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	got := string(bytes)

	// Frontmatter: original author key preserved, synthesis keys stamped.
	if !strings.HasPrefix(got, "---\n") {
		t.Fatalf("missing frontmatter: %s", got)
	}
	if !strings.Contains(got, "author: pm") {
		t.Errorf("original author key lost: %s", got)
	}
	for _, key := range []string{lastSHAKey, lastTSKey, factCntKey} {
		if !strings.Contains(got, key+":") {
			t.Errorf("missing synth key %q: %s", key, got)
		}
	}

	// Author body preserved verbatim.
	if !strings.Contains(got, "1. Pull the account's ARR.") {
		t.Errorf("author step 1 missing: %s", got)
	}
	if !strings.Contains(got, "3. Draft a save-offer DM.") {
		t.Errorf("author step 3 missing: %s", got)
	}
	// LLM's body rewrite must NOT have replaced the author body.
	if strings.Contains(got, "I rewrote the body") {
		t.Errorf("author body was replaced by the LLM rewrite: %s", got)
	}
	// Learnings section landed.
	if !strings.Contains(got, WhatWeveLearnedHeading) {
		t.Errorf("missing learnings section: %s", got)
	}
	if !strings.Contains(got, "The CSM is the fastest path.") {
		t.Errorf("learnings bullet missing: %s", got)
	}
}

func TestPlaybookSynthesizer_ReplacesExistingLearningsSection(t *testing.T) {
	stub := func(ctx context.Context, sys, user string) (string, error) {
		return "## What we've learned\n\n- New lesson from the latest run.\n", nil
	}
	synth, execLog, worker, pub, teardown := newPlaybookSynthFixture(t, stub)
	defer teardown()
	ctx := context.Background()

	seeded := seededPlaybookBody + "\n## What we've learned\n\n- Stale lesson from before.\n"
	writePlaybookSource(t, worker, "retention", seeded)
	_, _ = execLog.Append(ctx, "retention", PlaybookOutcomeSuccess, "closed it.", "", "cmo")

	_, _ = synth.SynthesizeNow(ctx, "retention", "human")
	waitForSynthCount(t, pub, 1, 3*time.Second)

	bytes, err := readArticle(worker.Repo(), playbookSourceRel("retention"))
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	got := string(bytes)
	if strings.Contains(got, "Stale lesson from before.") {
		t.Errorf("stale learnings bullet was not replaced: %s", got)
	}
	if !strings.Contains(got, "New lesson from the latest run.") {
		t.Errorf("new learnings bullet missing: %s", got)
	}
	// Heading appears exactly once — no duplication.
	if c := strings.Count(got, WhatWeveLearnedHeading); c != 1 {
		t.Errorf("expected %q heading exactly once; got %d", WhatWeveLearnedHeading, c)
	}
}

func TestPlaybookSynthesizer_ContradictionCalloutsPassThrough(t *testing.T) {
	stub := func(ctx context.Context, sys, user string) (string, error) {
		return "## What we've learned\n\n- **Contradiction:** run 1 succeeded skipping step 2; run 3 failed skipping step 2.\n", nil
	}
	synth, execLog, worker, pub, teardown := newPlaybookSynthFixture(t, stub)
	defer teardown()
	ctx := context.Background()

	writePlaybookSource(t, worker, "dispute", seededPlaybookBody)
	_, _ = execLog.Append(ctx, "dispute", PlaybookOutcomeSuccess, "worked without step 2.", "", "cmo")
	_, _ = execLog.Append(ctx, "dispute", PlaybookOutcomeAborted, "failed without step 2.", "", "ceo")

	_, _ = synth.SynthesizeNow(ctx, "dispute", "human")
	waitForSynthCount(t, pub, 1, 3*time.Second)

	bytes, _ := readArticle(worker.Repo(), playbookSourceRel("dispute"))
	if !strings.Contains(string(bytes), "**Contradiction:**") {
		t.Errorf("contradiction callout dropped: %s", string(bytes))
	}
}

func TestPlaybookSynthesizer_CommitAuthorIsArchivist(t *testing.T) {
	stub := func(ctx context.Context, sys, user string) (string, error) {
		return "## What we've learned\n\n- ok\n", nil
	}
	synth, execLog, worker, pub, teardown := newPlaybookSynthFixture(t, stub)
	defer teardown()
	ctx := context.Background()

	writePlaybookSource(t, worker, "attrib", seededPlaybookBody)
	_, _ = execLog.Append(ctx, "attrib", PlaybookOutcomeSuccess, "ran.", "", "cmo")

	_, _ = synth.SynthesizeNow(ctx, "attrib", "human")
	waitForSynthCount(t, pub, 1, 3*time.Second)

	entries, err := worker.Repo().AuditLog(ctx, time.Time{}, 50)
	if err != nil {
		t.Fatalf("audit: %v", err)
	}
	// Find the synthesis commit.
	var found bool
	for _, e := range entries {
		if strings.HasPrefix(e.Message, "archivist: synthesize learnings into playbook attrib") {
			if e.Author != ArchivistAuthor {
				t.Errorf("synth commit author = %q, want %q", e.Author, ArchivistAuthor)
			}
			found = true
			break
		}
	}
	if !found {
		t.Errorf("synthesis commit not found in audit log")
	}
}

func TestPlaybookSynthesizer_MissingSourceIsSkip(t *testing.T) {
	var calls atomic.Int32
	stub := func(ctx context.Context, sys, user string) (string, error) {
		calls.Add(1)
		return "## What we've learned\n\n- ok\n", nil
	}
	synth, execLog, _, pub, teardown := newPlaybookSynthFixture(t, stub)
	defer teardown()
	ctx := context.Background()
	// Don't seed a source — jump straight to logging + synth.
	_, _ = execLog.Append(ctx, "ghost", PlaybookOutcomeSuccess, "ran.", "", "cmo")
	_, _ = synth.SynthesizeNow(ctx, "ghost", "human")
	time.Sleep(300 * time.Millisecond)
	if pub.count() != 0 {
		t.Errorf("expected no synth event for missing source")
	}
	if calls.Load() != 0 {
		t.Errorf("expected LLM to NOT be called when source is missing; got %d", calls.Load())
	}
}

func TestPlaybookSynthesizer_NoNewExecutionsIsSkip(t *testing.T) {
	stub := func(ctx context.Context, sys, user string) (string, error) {
		return "## What we've learned\n\n- ok\n", nil
	}
	synth, execLog, worker, pub, teardown := newPlaybookSynthFixture(t, stub)
	defer teardown()
	ctx := context.Background()

	writePlaybookSource(t, worker, "stable", seededPlaybookBody)
	_, _ = execLog.Append(ctx, "stable", PlaybookOutcomeSuccess, "ran.", "", "cmo")
	_, _ = synth.SynthesizeNow(ctx, "stable", "human")
	waitForSynthCount(t, pub, 1, 3*time.Second)

	// Second synth with no new executions should skip.
	_, _ = synth.SynthesizeNow(ctx, "stable", "human")
	time.Sleep(300 * time.Millisecond)
	if pub.count() != 1 {
		t.Errorf("expected exactly 1 synth event; got %d", pub.count())
	}
}

func TestPlaybookSynthesizer_LLMErrorDoesNotCommit(t *testing.T) {
	stub := func(ctx context.Context, sys, user string) (string, error) {
		return "", fmt.Errorf("llm boom")
	}
	synth, execLog, _, pub, teardown := newPlaybookSynthFixture(t, stub)
	defer teardown()
	ctx := context.Background()
	_, _ = execLog.Append(ctx, "boom", PlaybookOutcomeSuccess, "ran.", "", "cmo")
	_, _ = synth.SynthesizeNow(ctx, "boom", "human")
	time.Sleep(300 * time.Millisecond)
	if pub.count() != 0 {
		t.Errorf("expected no synth event on LLM error")
	}
}

func TestPlaybookSynthesizer_MissingHeadingRejected(t *testing.T) {
	// LLM draft without the required heading must be rejected.
	stub := func(ctx context.Context, sys, user string) (string, error) {
		return "Just some text without the required heading.\n", nil
	}
	synth, execLog, worker, pub, teardown := newPlaybookSynthFixture(t, stub)
	defer teardown()
	ctx := context.Background()
	writePlaybookSource(t, worker, "noheading", seededPlaybookBody)
	_, _ = execLog.Append(ctx, "noheading", PlaybookOutcomeSuccess, "ran.", "", "cmo")
	_, _ = synth.SynthesizeNow(ctx, "noheading", "human")
	time.Sleep(300 * time.Millisecond)
	if pub.count() != 0 {
		t.Errorf("expected no synth event when LLM omits the required heading")
	}
}

func TestPlaybookSynthesizer_StopPreventsNewJobs(t *testing.T) {
	synth, execLog, _, _, teardown := newPlaybookSynthFixture(t, func(context.Context, string, string) (string, error) {
		return "## What we've learned\n\n- ok\n", nil
	})
	defer teardown()
	synth.Stop()

	_, _ = execLog.Append(context.Background(), "stopped", PlaybookOutcomeSuccess, "ran.", "", "cmo")
	if _, err := synth.SynthesizeNow(context.Background(), "stopped", "human"); err != ErrPlaybookSynthesizerStopped {
		t.Fatalf("expected ErrPlaybookSynthesizerStopped; got %v", err)
	}
}

func TestPlaybookSynthesizer_ThresholdAutoTriggers(t *testing.T) {
	// When OnExecutionRecorded is called with enough executions to cross the
	// threshold, synthesis is auto-enqueued. Fixture threshold=2.
	stub := func(ctx context.Context, sys, user string) (string, error) {
		return "## What we've learned\n\n- ok\n", nil
	}
	synth, execLog, worker, pub, teardown := newPlaybookSynthFixture(t, stub)
	defer teardown()
	ctx := context.Background()
	writePlaybookSource(t, worker, "auto", seededPlaybookBody)

	_, _ = execLog.Append(ctx, "auto", PlaybookOutcomeSuccess, "run 1.", "", "cmo")
	synth.OnExecutionRecorded("auto")
	// Threshold not crossed yet (1 < 2); no event expected.
	time.Sleep(150 * time.Millisecond)
	if pub.count() != 0 {
		t.Errorf("expected 0 syntheses at count=1 < threshold=2; got %d", pub.count())
	}

	_, _ = execLog.Append(ctx, "auto", PlaybookOutcomeSuccess, "run 2.", "", "cmo")
	synth.OnExecutionRecorded("auto")
	waitForSynthCount(t, pub, 1, 3*time.Second)
}

// TestPlaybookSynthesizer_IntegrationTriggersAutoRecompile verifies the
// full end-to-end loop: writing a synthesis commit to the source playbook
// causes the existing auto-recompile hook (wired in wiki_worker.go) to
// regenerate SKILL.md. Observable via the compiled file's mtime bumping.
func TestPlaybookSynthesizer_IntegrationTriggersAutoRecompile(t *testing.T) {
	stub := func(ctx context.Context, sys, user string) (string, error) {
		return "## What we've learned\n\n- compiled again.\n", nil
	}
	synth, execLog, worker, pub, teardown := newPlaybookSynthFixture(t, stub)
	defer teardown()
	ctx := context.Background()

	writePlaybookSource(t, worker, "recompile", seededPlaybookBody)
	// Force an initial compile so SKILL.md exists before synthesis.
	if _, _, err := worker.EnqueuePlaybookCompile(ctx, "recompile", ArchivistAuthor); err != nil {
		t.Fatalf("initial compile: %v", err)
	}
	skillFull := filepath.Join(worker.Repo().Root(), filepath.FromSlash(CompiledSkillRelPath("recompile")))
	initialStat, err := os.Stat(skillFull)
	if err != nil {
		t.Fatalf("initial stat: %v", err)
	}

	// Wait a bit so mtimes can change observably on filesystems with
	// second precision.
	time.Sleep(1100 * time.Millisecond)

	_, _ = execLog.Append(ctx, "recompile", PlaybookOutcomeSuccess, "ran.", "", "cmo")
	_, _ = synth.SynthesizeNow(ctx, "recompile", "human")
	waitForSynthCount(t, pub, 1, 3*time.Second)

	// Auto-recompile is async (side goroutine). Poll.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		st, err := os.Stat(skillFull)
		if err == nil && st.ModTime().After(initialStat.ModTime()) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Errorf("auto-recompile did not bump SKILL.md mtime; initial=%s", initialStat.ModTime())
}
