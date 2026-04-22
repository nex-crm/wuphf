package team

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// newPlaybookFixture spins up a wiki repo + wiki worker + execution log
// isolated to t.TempDir(). Used by the compiler and auto-recompile tests
// so each case gets a fresh filesystem.
func newPlaybookFixture(t *testing.T) (*Repo, *WikiWorker, *ExecutionLog, func()) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "wiki")
	backup := filepath.Join(t.TempDir(), "wiki.bak")
	repo := NewRepoAt(root, backup)
	if err := repo.Init(context.Background()); err != nil {
		t.Fatalf("repo init: %v", err)
	}
	worker := NewWikiWorker(repo, noopPublisher{})
	ctx, cancel := context.WithCancel(context.Background())
	worker.Start(ctx)
	execLog := NewExecutionLog(worker)
	return repo, worker, execLog, func() {
		cancel()
		worker.Stop()
	}
}

// writePlaybookSource writes a source playbook article via the wiki write
// queue (same path production hits) and waits briefly so the post-commit
// auto-recompile goroutine has a chance to run.
func writePlaybookSource(t *testing.T, worker *WikiWorker, slug, body string) {
	t.Helper()
	path := "team/playbooks/" + slug + ".md"
	if _, _, err := worker.Enqueue(context.Background(), "pm", path, body, "replace", "seed playbook"); err != nil {
		t.Fatalf("enqueue playbook source: %v", err)
	}
}

func TestPlaybookSlugFromPath(t *testing.T) {
	cases := []struct {
		in       string
		wantSlug string
		wantOK   bool
	}{
		{"team/playbooks/churn-prevention.md", "churn-prevention", true},
		{"team/playbooks/mid-market-onboarding.md", "mid-market-onboarding", true},
		{"team/people/nazz.md", "", false},
		{"team/playbooks/.compiled/churn/SKILL.md", "", false},
		{"team/playbooks/churn.executions.jsonl", "", false},
		{"team/playbooks/NotAKebab.md", "", false},
		{"", "", false},
	}
	for _, tc := range cases {
		gotSlug, gotOK := PlaybookSlugFromPath(tc.in)
		if gotSlug != tc.wantSlug || gotOK != tc.wantOK {
			t.Errorf("PlaybookSlugFromPath(%q) = (%q,%v), want (%q,%v)", tc.in, gotSlug, gotOK, tc.wantSlug, tc.wantOK)
		}
	}
}

func TestCompilePlaybook_RejectsNonPlaybookPaths(t *testing.T) {
	repo, _, _, teardown := newPlaybookFixture(t)
	defer teardown()
	_, _, err := CompilePlaybook(repo, "team/people/nazz.md")
	if err == nil {
		t.Fatalf("expected error for non-playbook path")
	}
	if !strings.Contains(err.Error(), "playbook: path must be") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestCompilePlaybook_ProducesFrontmatterAndBody(t *testing.T) {
	repo, worker, _, teardown := newPlaybookFixture(t)
	defer teardown()

	body := "# Churn prevention\n\nA tight-loop playbook for rescuing accounts showing churn risk.\n\n## What to do\n\n1. Pull the last 30 days of usage.\n2. Schedule a save call with the account owner.\n"
	writePlaybookSource(t, worker, "churn-prevention", body)
	// Wait for the auto-recompile to land so we're asserting the on-disk
	// compiled skill, not just the CompilePlaybook return value.
	waitForCompiledSkill(t, repo, "churn-prevention", 2*time.Second)

	compiled := readCompiled(t, repo, "churn-prevention")
	// Frontmatter sanity.
	for _, want := range []string{
		"---\n",
		"name: churn-prevention\n",
		"description: ",
		"allowed-tools: team_wiki_read, playbook_list, playbook_execution_record\n",
		"source_path: team/playbooks/churn-prevention.md\n",
		"compiled_by: archivist\n",
	} {
		if !strings.Contains(compiled, want) {
			t.Errorf("compiled skill missing %q", want)
		}
	}
	// Body sanity.
	for _, want := range []string{
		"# Playbook: Churn prevention",
		"team_wiki_read",
		"playbook_execution_record",
		"team/playbooks/churn-prevention.executions.jsonl",
	} {
		if !strings.Contains(compiled, want) {
			t.Errorf("compiled skill missing body piece %q", want)
		}
	}
}

func TestCompilePlaybook_IsIdempotent(t *testing.T) {
	repo, worker, _, teardown := newPlaybookFixture(t)
	defer teardown()

	body := "# Mid-market onboarding\n\nFast, opinionated onboarding for mid-market accounts.\n"
	writePlaybookSource(t, worker, "mid-market-onboarding", body)
	waitForCompiledSkill(t, repo, "mid-market-onboarding", 2*time.Second)

	first := readCompiled(t, repo, "mid-market-onboarding")
	// Recompile without changing the source.
	if _, _, err := CompilePlaybook(repo, "team/playbooks/mid-market-onboarding.md"); err != nil {
		t.Fatalf("recompile: %v", err)
	}
	second := readCompiled(t, repo, "mid-market-onboarding")
	if first != second {
		t.Errorf("compilation is not deterministic — byte-identical re-run produced different output")
	}
}

func TestWikiWrite_TriggersAutoRecompile(t *testing.T) {
	repo, worker, _, teardown := newPlaybookFixture(t)
	defer teardown()

	body := "# Pricing negotiations\n\nShort body.\n"
	writePlaybookSource(t, worker, "pricing-negotiations", body)
	waitForCompiledSkill(t, repo, "pricing-negotiations", 2*time.Second)

	// Capture the initial compiled body.
	first := readCompiled(t, repo, "pricing-negotiations")

	// Now change the source and re-queue — the worker should auto-recompile.
	newBody := "# Pricing negotiations v2\n\nUpdated body reflecting last quarter's deals.\n"
	if _, _, err := worker.Enqueue(context.Background(), "pm", "team/playbooks/pricing-negotiations.md", newBody, "replace", "update"); err != nil {
		t.Fatalf("second enqueue: %v", err)
	}
	// Poll until the compiled skill reflects the new H1.
	deadline := time.Now().Add(3 * time.Second)
	var latest string
	for time.Now().Before(deadline) {
		latest = readCompiled(t, repo, "pricing-negotiations")
		if strings.Contains(latest, "Pricing negotiations v2") {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !strings.Contains(latest, "Pricing negotiations v2") {
		t.Errorf("auto-recompile did not pick up new source — got %q, first=%q", latest, first)
	}
}

func TestExecutionLog_AppendAndListNewestFirst(t *testing.T) {
	repo, worker, execLog, teardown := newPlaybookFixture(t)
	defer teardown()

	writePlaybookSource(t, worker, "churn-prevention", "# Churn\n\nBody.\n")
	waitForCompiledSkill(t, repo, "churn-prevention", 2*time.Second)

	ctx := context.Background()
	if _, err := execLog.Append(ctx, "churn-prevention", PlaybookOutcomeSuccess, "Saved account.", "", "cmo"); err != nil {
		t.Fatalf("append 1: %v", err)
	}
	if _, err := execLog.Append(ctx, "churn-prevention", PlaybookOutcomePartial, "Partial — need CEO follow-up.", "Blocker: legal review.", "cmo"); err != nil {
		t.Fatalf("append 2: %v", err)
	}

	entries, err := execLog.List("churn-prevention")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[0].Outcome != PlaybookOutcomePartial {
		t.Errorf("expected newest first; got %s", entries[0].Outcome)
	}
	if entries[1].Outcome != PlaybookOutcomeSuccess {
		t.Errorf("expected oldest last; got %s", entries[1].Outcome)
	}
	// Every entry must carry an ID.
	for i, e := range entries {
		if e.ID == "" {
			t.Errorf("entry %d missing ID", i)
		}
	}
}

func TestExecutionLog_ValidationErrors(t *testing.T) {
	_, worker, execLog, teardown := newPlaybookFixture(t)
	defer teardown()
	// Seed a playbook so the path exists; not strictly required for
	// validation errors but mirrors the production shape.
	writePlaybookSource(t, worker, "pb", "# PB\n\nBody.\n")

	cases := []struct {
		name    string
		slug    string
		out     PlaybookOutcome
		summary string
		notes   string
		by      string
		want    string
	}{
		{"bad slug", "BAD", PlaybookOutcomeSuccess, "x", "", "pm", "slug must match"},
		{"bad outcome", "pb", PlaybookOutcome("yikes"), "x", "", "pm", "outcome must be one of"},
		{"empty summary", "pb", PlaybookOutcomeSuccess, "   ", "", "pm", "summary is required"},
		{"too long summary", "pb", PlaybookOutcomeSuccess, strings.Repeat("x", MaxExecutionSummaryLen+1), "", "pm", "summary must be <="},
		{"too long notes", "pb", PlaybookOutcomeSuccess, "ok", strings.Repeat("x", MaxExecutionNotesLen+1), "pm", "notes must be <="},
		{"empty by", "pb", PlaybookOutcomeSuccess, "x", "", "", "recorded_by is required"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			_, err := execLog.Append(context.Background(), tc.slug, tc.out, tc.summary, tc.notes, tc.by)
			if err == nil {
				t.Fatalf("expected error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("want error containing %q, got %q", tc.want, err.Error())
			}
		})
	}
}

func TestExecutionLog_MalformedLinesAreSkipped(t *testing.T) {
	repo, worker, execLog, teardown := newPlaybookFixture(t)
	defer teardown()

	writePlaybookSource(t, worker, "pb", "# PB\n\nBody.\n")
	if _, err := execLog.Append(context.Background(), "pb", PlaybookOutcomeSuccess, "Good entry.", "", "pm"); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Inject a bad line directly — bypass the queue to simulate a partially
	// corrupted file.
	logPath := filepath.Join(repo.Root(), "team", "playbooks", "pb.executions.jsonl")
	existing, _ := os.ReadFile(logPath)
	withBad := append(append([]byte{}, existing...), []byte("not-json\n")...)
	if err := os.WriteFile(logPath, withBad, 0o600); err != nil {
		t.Fatalf("inject bad: %v", err)
	}
	entries, err := execLog.List("pb")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("expected 1 valid entry (bad one skipped), got %d", len(entries))
	}
}

// ── helpers ─────────────────────────────────────────────────────────

func readCompiled(t *testing.T, repo *Repo, slug string) string {
	t.Helper()
	p := filepath.Join(repo.Root(), "team", "playbooks", ".compiled", slug, "SKILL.md")
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read compiled: %v", err)
	}
	return string(b)
}

func waitForCompiledSkill(t *testing.T, repo *Repo, slug string, timeout time.Duration) {
	t.Helper()
	p := filepath.Join(repo.Root(), "team", "playbooks", ".compiled", slug, "SKILL.md")
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(p); err == nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("compiled skill for %q did not appear within %s", slug, timeout)
}
