package team

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// waitUntilNextSecond sleeps to the next wall-clock second boundary.
// Used in tests that depend on git's second-precision commit timestamps.
func waitUntilNextSecond(t *testing.T) {
	t.Helper()
	now := time.Now()
	next := now.Truncate(time.Second).Add(time.Second).Add(10 * time.Millisecond)
	time.Sleep(time.Until(next))
}

// newFactLogFixture spins up a wiki repo + worker + fact log isolated to
// t.TempDir(). Caller must defer the returned teardown.
func newFactLogFixture(t *testing.T) (*FactLog, *WikiWorker, func()) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "wiki")
	backup := filepath.Join(t.TempDir(), "wiki.bak")
	repo := NewRepoAt(root, backup)
	if err := repo.Init(context.Background()); err != nil {
		t.Fatalf("init repo: %v", err)
	}
	worker := NewWikiWorker(repo, noopPublisher{})
	ctx, cancel := context.WithCancel(context.Background())
	worker.Start(ctx)
	log := NewFactLog(worker)
	return log, worker, func() {
		cancel()
		<-worker.Done()
	}
}

func TestFactLog_AppendAndListReturnsNewestFirst(t *testing.T) {
	log, _, teardown := newFactLogFixture(t)
	defer teardown()
	ctx := context.Background()

	if _, err := log.Append(ctx, EntityKindPeople, "nazz", "First fact", "", "pm"); err != nil {
		t.Fatalf("append 1: %v", err)
	}
	if _, err := log.Append(ctx, EntityKindPeople, "nazz", "Second fact", "agents/pm/notebook/retro.md", "pm"); err != nil {
		t.Fatalf("append 2: %v", err)
	}
	facts, err := log.List(EntityKindPeople, "nazz")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(facts) != 2 {
		t.Fatalf("expected 2 facts, got %d", len(facts))
	}
	// Newest first.
	if !strings.Contains(facts[0].Text, "Second") {
		t.Errorf("expected newest first; got %q", facts[0].Text)
	}
	if facts[1].Text != "First fact" {
		t.Errorf("expected oldest last; got %q", facts[1].Text)
	}
	if facts[0].RecordedBy != "pm" {
		t.Errorf("recorded_by=%q", facts[0].RecordedBy)
	}
	if facts[0].ID == "" {
		t.Errorf("fact missing ID")
	}
}

func TestFactLog_ValidationErrors(t *testing.T) {
	log, _, teardown := newFactLogFixture(t)
	defer teardown()
	ctx := context.Background()

	cases := []struct {
		name       string
		kind       EntityKind
		slug       string
		text       string
		sourcePath string
		recordedBy string
		wantErr    string
	}{
		{"bad kind", "orgs", "acme", "x", "", "pm", "entity_kind must be"},
		{"bad slug uppercase", EntityKindPeople, "Nazz", "x", "", "pm", "entity_slug must match"},
		{"bad slug leading dash", EntityKindPeople, "-nazz", "x", "", "pm", "entity_slug must match"},
		{"empty text", EntityKindPeople, "nazz", "   ", "", "pm", "fact text is required"},
		{"too long text", EntityKindPeople, "nazz", strings.Repeat("x", MaxFactTextLen+1), "", "pm", "fact text must be <="},
		{"bad source", EntityKindPeople, "nazz", "x", "../etc/passwd", "pm", "source_path must start with"},
		{"empty recordedBy", EntityKindPeople, "nazz", "x", "", "", "recorded_by is required"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			_, err := log.Append(ctx, tc.kind, tc.slug, tc.text, tc.sourcePath, tc.recordedBy)
			if err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("expected error containing %q; got %q", tc.wantErr, err.Error())
			}
		})
	}
}

func TestFactLog_MalformedLineRecovery(t *testing.T) {
	log, worker, teardown := newFactLogFixture(t)
	defer teardown()
	ctx := context.Background()

	// Append one good fact so the file exists.
	if _, err := log.Append(ctx, EntityKindCompanies, "acme", "founded-1999", "", "pm"); err != nil {
		t.Fatalf("append: %v", err)
	}
	// Inject a malformed line directly at the file level (bypassing the
	// worker so the test deterministically sees the bad line).
	fullPath := filepath.Join(worker.Repo().Root(), "team", "entities", "companies-acme.facts.jsonl")
	existing, _ := os.ReadFile(fullPath)
	junk := []byte("this is not json\n{\"id\":\"x\",\"kind\":\"companies\",\"slug\":\"acme\",\"text\":\"good\",\"recorded_by\":\"pm\",\"created_at\":\"2026-04-20T00:00:00Z\"}\n")
	_ = os.WriteFile(fullPath, append(existing, junk...), 0o600)

	facts, err := log.List(EntityKindCompanies, "acme")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	// Original fact + injected "good" record. Malformed line is skipped.
	if len(facts) != 2 {
		t.Fatalf("expected 2 facts after skipping malformed; got %d", len(facts))
	}
}

func TestFactLog_ConcurrentAppendsAllLand(t *testing.T) {
	log, _, teardown := newFactLogFixture(t)
	defer teardown()
	ctx := context.Background()

	const N = 10
	var wg sync.WaitGroup
	wg.Add(N)
	for i := 0; i < N; i++ {
		i := i
		go func() {
			defer wg.Done()
			agent := []string{"pm", "eng", "sales", "cs", "ceo"}[i%5]
			_, err := log.Append(ctx, EntityKindCustomers, "northstar", "fact-"+string(rune('A'+i)), "", agent)
			if err != nil {
				t.Errorf("concurrent append %d: %v", i, err)
			}
		}()
	}
	wg.Wait()

	facts, err := log.List(EntityKindCustomers, "northstar")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(facts) != N {
		t.Fatalf("expected %d facts, got %d", N, len(facts))
	}
	// All IDs should be unique.
	seen := map[string]bool{}
	for _, f := range facts {
		if seen[f.ID] {
			t.Errorf("duplicate fact id: %s", f.ID)
		}
		seen[f.ID] = true
	}
}

func TestFactLog_CountSinceSHA(t *testing.T) {
	log, worker, teardown := newFactLogFixture(t)
	defer teardown()
	ctx := context.Background()

	if _, err := log.Append(ctx, EntityKindPeople, "ceo", "old fact", "", "pm"); err != nil {
		t.Fatalf("append old: %v", err)
	}
	// Grab the head sha AFTER the first fact committed.
	worker.Repo().mu.Lock()
	shaOut, _ := worker.Repo().runGitLocked(ctx, "system", "rev-parse", "--short", "HEAD")
	worker.Repo().mu.Unlock()
	sha := strings.TrimSpace(shaOut)

	// Git commit timestamps are second-precision. Sleep past the second
	// boundary so the next two appends land unambiguously AFTER `sha`'s
	// commit time.
	waitUntilNextSecond(t)
	if _, err := log.Append(ctx, EntityKindPeople, "ceo", "new fact 1", "", "pm"); err != nil {
		t.Fatalf("append new 1: %v", err)
	}
	if _, err := log.Append(ctx, EntityKindPeople, "ceo", "new fact 2", "", "pm"); err != nil {
		t.Fatalf("append new 2: %v", err)
	}

	n, err := log.CountSinceSHA(ctx, EntityKindPeople, "ceo", sha)
	if err != nil {
		t.Fatalf("count since: %v", err)
	}
	if n != 2 {
		t.Fatalf("expected 2 new facts since sha; got %d", n)
	}

	// Empty sha returns total.
	total, _ := log.CountSinceSHA(ctx, EntityKindPeople, "ceo", "")
	if total != 3 {
		t.Fatalf("expected 3 total; got %d", total)
	}

	// Unknown sha returns everything (safe default — brief has never synthesized).
	all, _ := log.CountSinceSHA(ctx, EntityKindPeople, "ceo", "deadbeef")
	if all != 3 {
		t.Fatalf("expected 3 on unknown sha; got %d", all)
	}
}

func TestFactLog_MissingFileReturnsEmpty(t *testing.T) {
	log, _, teardown := newFactLogFixture(t)
	defer teardown()

	facts, err := log.List(EntityKindPeople, "ghost")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(facts) != 0 {
		t.Fatalf("expected 0 facts for missing file; got %d", len(facts))
	}
}
