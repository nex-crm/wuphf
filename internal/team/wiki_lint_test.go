package team

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeQueryProvider returns deterministic contradict judgments for testing.
type fakeQueryProvider struct {
	// response maps "subject/predicate" to the desired JSON response.
	// Defaults to contradicts=false if not found.
	responses map[string]string
}

func (f *fakeQueryProvider) Query(_ context.Context, _, userPrompt string) (string, error) {
	for key, resp := range f.responses {
		if strings.Contains(userPrompt, key) {
			return resp, nil
		}
	}
	return `{"contradicts": false, "reason": "no conflict detected by test fake"}`, nil
}

func newLintFixture(t *testing.T) (*Lint, *WikiWorker, *WikiIndex, func()) {
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

	idx := NewWikiIndex(root)

	prov := &fakeQueryProvider{
		responses: map[string]string{
			// Any cluster mentioning "reports_to" will be judged as contradicting.
			"reports_to": `{"contradicts": true, "reason": "Fact A says reports to Michael; Fact B says reports to David — mutually exclusive."}`,
		},
	}
	l := NewLint(idx, worker, prov)
	// Pin the clock so report filenames are deterministic.
	l.now = func() time.Time {
		return time.Date(2026, 4, 22, 9, 0, 0, 0, time.UTC)
	}

	teardown := func() {
		cancel()
		<-worker.Done()
	}
	return l, worker, idx, teardown
}

// seedEntityBrief writes a minimal entity brief so allEntitySlugs() picks it up.
func seedEntityBrief(t *testing.T, worker *WikiWorker, slug, kind string) {
	t.Helper()
	path := fmt.Sprintf("team/%s/%s.md", kind, slug)
	content := fmt.Sprintf("---\ncanonical_slug: %s\nkind: %s\n---\n# %s\n\nStub.\n", slug, kind, slug)
	_, _, err := worker.Enqueue(context.Background(), ArchivistAuthor, path, content, "replace",
		fmt.Sprintf("archivist: seed brief %s", slug))
	if err != nil {
		t.Fatalf("seed brief %s: %v", slug, err)
	}
}

// seedTypedFact upserts a TypedFact directly into the in-memory index.
func seedTypedFact(t *testing.T, idx *WikiIndex, f TypedFact) {
	t.Helper()
	if err := idx.store.UpsertFact(context.Background(), f); err != nil {
		t.Fatalf("seed fact %s: %v", f.ID, err)
	}
}

func TestLintRunEmptyWiki(t *testing.T) {
	l, _, _, teardown := newLintFixture(t)
	defer teardown()

	report, err := l.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if report.Date != "2026-04-22" {
		t.Errorf("Date = %q, want 2026-04-22", report.Date)
	}
	if len(report.Findings) != 0 {
		t.Errorf("expected 0 findings on empty wiki, got %d", len(report.Findings))
	}
}

func TestLintDetectsContradiction(t *testing.T) {
	l, worker, idx, teardown := newLintFixture(t)
	defer teardown()

	seedEntityBrief(t, worker, "sarah-jones", "people")
	worker.WaitForIdle()

	validFrom := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	seedTypedFact(t, idx, TypedFact{
		ID:         "factA001",
		EntitySlug: "sarah-jones",
		Type:       "relationship",
		Triplet:    &Triplet{Subject: "sarah-jones", Predicate: "reports_to", Object: "michael"},
		Text:       "Sarah reports to Michael.",
		Confidence: 0.9,
		ValidFrom:  validFrom,
		CreatedAt:  validFrom,
		CreatedBy:  "archivist",
	})
	seedTypedFact(t, idx, TypedFact{
		ID:         "factB001",
		EntitySlug: "sarah-jones",
		Type:       "relationship",
		Triplet:    &Triplet{Subject: "sarah-jones", Predicate: "reports_to", Object: "david"},
		Text:       "Sarah reports to David.",
		Confidence: 0.9,
		ValidFrom:  validFrom,
		CreatedAt:  validFrom,
		CreatedBy:  "archivist",
	})

	report, err := l.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	found := false
	for _, f := range report.Findings {
		if f.Type == "contradictions" && f.EntitySlug == "sarah-jones" {
			found = true
			if f.Severity != "critical" {
				t.Errorf("contradiction severity = %q, want critical", f.Severity)
			}
			if len(f.FactIDs) < 2 {
				t.Errorf("contradiction finding has %d fact IDs, want ≥ 2", len(f.FactIDs))
			}
			if len(f.ResolveActions) != 3 {
				t.Errorf("ResolveActions length = %d, want 3", len(f.ResolveActions))
			}
			break
		}
	}
	if !found {
		t.Errorf("expected a contradictions finding for sarah-jones, got %+v", report.Findings)
	}
}

func TestLintDetectsOrphan(t *testing.T) {
	l, worker, _, teardown := newLintFixture(t)
	defer teardown()

	// Seed an entity with no facts and no edges — it should be an orphan.
	seedEntityBrief(t, worker, "forgotten-entity", "people")
	worker.WaitForIdle()

	report, err := l.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	found := false
	for _, f := range report.Findings {
		if f.Type == "orphans" && f.EntitySlug == "forgotten-entity" {
			found = true
			if f.Severity != "warning" {
				t.Errorf("orphan severity = %q, want warning", f.Severity)
			}
			break
		}
	}
	if !found {
		t.Errorf("expected orphan finding for forgotten-entity, got %+v", report.Findings)
	}
}

func TestLintDetectsStale(t *testing.T) {
	l, worker, idx, teardown := newLintFixture(t)
	defer teardown()

	seedEntityBrief(t, worker, "old-company", "companies")
	worker.WaitForIdle()

	// Seed a very old fact with no reinforcement.
	oldTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC) // >2 years ago
	seedTypedFact(t, idx, TypedFact{
		ID:         "staleF001",
		EntitySlug: "old-company",
		Type:       "status",
		Text:       "Old Company is in stealth mode.",
		Confidence: 0.9,
		ValidFrom:  oldTime,
		CreatedAt:  oldTime,
		CreatedBy:  "archivist",
		// ReinforcedAt is nil
	})

	report, err := l.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	found := false
	for _, f := range report.Findings {
		if f.Type == "stale" && f.EntitySlug == "old-company" {
			found = true
			if f.Severity != "warning" {
				t.Errorf("stale severity = %q, want warning", f.Severity)
			}
			break
		}
	}
	if !found {
		t.Errorf("expected stale finding for old-company, got %+v", report.Findings)
	}
}

func TestLintResolveContradictionWinnerA(t *testing.T) {
	l, worker, idx, teardown := newLintFixture(t)
	defer teardown()

	seedEntityBrief(t, worker, "jane-doe", "people")
	worker.WaitForIdle()

	validFrom := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	// Seed facts directly into the fact log on disk so ResolveContradiction
	// can find and mutate them.
	root := worker.repo.Root()
	factPath := filepath.Join(root, "wiki", "facts", "people", "jane-doe.jsonl")
	if err := os.MkdirAll(filepath.Dir(factPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	factA := TypedFact{
		ID:         "resolveA1",
		EntitySlug: "jane-doe",
		Type:       "status",
		Triplet:    &Triplet{Subject: "jane-doe", Predicate: "reports_to", Object: "alice"},
		Text:       "Jane reports to Alice.",
		Confidence: 0.9,
		ValidFrom:  validFrom,
		CreatedAt:  validFrom,
		CreatedBy:  "archivist",
	}
	factB := TypedFact{
		ID:         "resolveB1",
		EntitySlug: "jane-doe",
		Type:       "status",
		Triplet:    &Triplet{Subject: "jane-doe", Predicate: "reports_to", Object: "bob"},
		Text:       "Jane reports to Bob.",
		Confidence: 0.9,
		ValidFrom:  validFrom,
		CreatedAt:  validFrom,
		CreatedBy:  "archivist",
	}

	seedTypedFact(t, idx, factA)
	seedTypedFact(t, idx, factB)

	// Write the facts to disk so mutateFact can find them.
	writeFactsToJSONL(t, factPath, []TypedFact{factA, factB})

	// Commit the file through the worker so git knows about it.
	data, _ := os.ReadFile(factPath)
	_, _, err := worker.EnqueueFactLog(context.Background(), ArchivistAuthor,
		"wiki/facts/people/jane-doe.jsonl", string(data),
		"test: seed facts for resolve test")
	if err != nil {
		t.Fatalf("enqueue fact file: %v", err)
	}
	worker.WaitForIdle()

	report := LintReport{
		Date: "2026-04-22",
		Findings: []LintFinding{
			{
				Type:       "contradictions",
				Severity:   "critical",
				EntitySlug: "jane-doe",
				FactIDs:    []string{"resolveA1", "resolveB1"},
				Summary:    "test contradiction",
			},
		},
	}

	identity := HumanIdentity{Slug: "nazz", Name: "Test User", Email: "test@example.com"}
	if err := l.ResolveContradiction(context.Background(), report, 0, "A", identity); err != nil {
		t.Fatalf("ResolveContradiction: %v", err)
	}
	worker.WaitForIdle()

	// Re-read the fact file and verify the mutations.
	updated, err := os.ReadFile(filepath.Join(root, "wiki/facts/people/jane-doe.jsonl"))
	if err != nil {
		t.Fatalf("read updated fact file: %v", err)
	}
	if !strings.Contains(string(updated), "resolveB1") || !strings.Contains(string(updated), "supersedes") {
		t.Errorf("expected winner A to have supersedes:[resolveB1], got:\n%s", string(updated))
	}
	if !strings.Contains(string(updated), "valid_until") {
		t.Errorf("expected loser B to have valid_until set, got:\n%s", string(updated))
	}
}

func TestLintResolveContradictionBoth(t *testing.T) {
	l, worker, idx, teardown := newLintFixture(t)
	defer teardown()

	seedEntityBrief(t, worker, "mark-lee", "people")
	worker.WaitForIdle()

	root := worker.repo.Root()
	factPath := filepath.Join(root, "wiki", "facts", "people", "mark-lee.jsonl")
	if err := os.MkdirAll(filepath.Dir(factPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	validFrom := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	factA := TypedFact{
		ID: "bothA1", EntitySlug: "mark-lee", Type: "status",
		Triplet: &Triplet{Subject: "mark-lee", Predicate: "role", Object: "cto"},
		Text: "Mark is CTO.", Confidence: 0.8, ValidFrom: validFrom, CreatedAt: validFrom, CreatedBy: "archivist",
	}
	factB := TypedFact{
		ID: "bothB1", EntitySlug: "mark-lee", Type: "status",
		Triplet: &Triplet{Subject: "mark-lee", Predicate: "role", Object: "coo"},
		Text: "Mark is COO.", Confidence: 0.8, ValidFrom: validFrom, CreatedAt: validFrom, CreatedBy: "archivist",
	}

	seedTypedFact(t, idx, factA)
	seedTypedFact(t, idx, factB)
	writeFactsToJSONL(t, factPath, []TypedFact{factA, factB})

	data, _ := os.ReadFile(factPath)
	_, _, err := worker.EnqueueFactLog(context.Background(), ArchivistAuthor,
		"wiki/facts/people/mark-lee.jsonl", string(data), "test: seed both facts")
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	worker.WaitForIdle()

	report := LintReport{
		Date: "2026-04-22",
		Findings: []LintFinding{{
			Type: "contradictions", Severity: "critical",
			EntitySlug: "mark-lee",
			FactIDs:    []string{"bothA1", "bothB1"},
		}},
	}

	identity := HumanIdentity{Slug: "nazz", Name: "Test User", Email: "test@example.com"}
	if err := l.ResolveContradiction(context.Background(), report, 0, "Both", identity); err != nil {
		t.Fatalf("ResolveContradiction Both: %v", err)
	}
	worker.WaitForIdle()

	updated, err := os.ReadFile(filepath.Join(root, "wiki/facts/people/mark-lee.jsonl"))
	if err != nil {
		t.Fatalf("read updated: %v", err)
	}
	content := string(updated)
	if !strings.Contains(content, "contradicts_with") {
		t.Errorf("expected contradicts_with in both facts, got:\n%s", content)
	}
}

func TestLintReportCommitted(t *testing.T) {
	l, worker, _, teardown := newLintFixture(t)
	defer teardown()

	_, err := l.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	worker.WaitForIdle()

	// The report file should exist in the wiki repo.
	root := worker.repo.Root()
	reportPath := filepath.Join(root, "wiki", ".lint", "report-2026-04-22.md")
	data, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatalf("report not committed at %s: %v", reportPath, err)
	}
	if !strings.Contains(string(data), "Lint report") {
		t.Errorf("report content looks wrong: %s", string(data))
	}
}

// writeFactsToJSONL writes typed facts as JSONL to a file for test seeding.
func writeFactsToJSONL(t *testing.T, path string, facts []TypedFact) {
	t.Helper()
	var sb strings.Builder
	for _, f := range facts {
		b, err := marshalFactJSON(f)
		if err != nil {
			t.Fatalf("marshal fact %s: %v", f.ID, err)
		}
		sb.Write(b)
		sb.WriteByte('\n')
	}
	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
		t.Fatalf("write facts file %s: %v", path, err)
	}
}

// marshalFactJSON encodes a TypedFact to JSON.
func marshalFactJSON(f TypedFact) ([]byte, error) {
	return json.Marshal(f)
}
