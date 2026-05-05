package team

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// newMaintenanceFixture wires a Repo + WikiWorker for maintenance-assistant
// tests. The returned cleanup stops the worker before the TempDir is removed.
func newMaintenanceFixture(t *testing.T) (*WikiWorker, func()) {
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
	return worker, func() {
		cancel()
		<-worker.Done()
	}
}

func seedArticle(t *testing.T, worker *WikiWorker, path, body string) {
	t.Helper()
	if _, _, err := worker.Enqueue(context.Background(), "tester", path, body, "replace", "seed "+path); err != nil {
		t.Fatalf("seed %s: %v", path, err)
	}
}

func TestMaintenance_Summarize_ProposesTLDR(t *testing.T) {
	worker, cleanup := newMaintenanceFixture(t)
	defer cleanup()

	body := strings.Repeat("This is a long article about Sarah Chen. ", 30) +
		"\n\n# Sarah Chen\n\nSarah Chen leads product at Acme Corp.\n\n## Background\n\nShe has been at Acme since 2024.\n"
	seedArticle(t, worker, "team/people/sarah-chen.md", body)

	a := NewMaintenanceAssistant(worker, nil, nil)
	s, err := a.Suggest(context.Background(), MaintActionSummarize, "team/people/sarah-chen.md")
	if err != nil {
		t.Fatalf("suggest: %v", err)
	}
	if s.Skipped {
		t.Fatalf("expected suggestion, got skipped: %s", s.SkippedReason)
	}
	if s.Diff == nil || s.Diff.ProposedContent == "" {
		t.Fatalf("expected diff with proposed content, got nil")
	}
	if !strings.Contains(s.Diff.ProposedContent, "TL;DR") {
		t.Errorf("expected TL;DR in proposed content, got: %s", s.Diff.ProposedContent[:200])
	}
	if len(s.Evidence) == 0 {
		t.Errorf("expected evidence to be present")
	}
}

func TestMaintenance_Summarize_SkipsShortArticles(t *testing.T) {
	worker, cleanup := newMaintenanceFixture(t)
	defer cleanup()

	seedArticle(t, worker, "team/people/short.md", "# Short\n\nJust a stub.\n")

	a := NewMaintenanceAssistant(worker, nil, nil)
	s, err := a.Suggest(context.Background(), MaintActionSummarize, "team/people/short.md")
	if err != nil {
		t.Fatalf("suggest: %v", err)
	}
	if !s.Skipped {
		t.Fatalf("expected skipped, got %+v", s)
	}
}

func TestMaintenance_AddCitation_FlagsNumericClaims(t *testing.T) {
	worker, cleanup := newMaintenanceFixture(t)
	defer cleanup()

	body := "# Acme Corp\n\nAcme Corp raised 50 million in 2024.\n\nThe company has 200 employees.\n\nOnly some words here.\n"
	seedArticle(t, worker, "team/companies/acme.md", body)

	a := NewMaintenanceAssistant(worker, nil, nil)
	s, err := a.Suggest(context.Background(), MaintActionAddCitation, "team/companies/acme.md")
	if err != nil {
		t.Fatalf("suggest: %v", err)
	}
	if s.Skipped {
		t.Fatalf("expected suggestion, got skipped: %s", s.SkippedReason)
	}
	if len(s.Diff.Added) == 0 {
		t.Fatalf("expected added lines, got none")
	}
	for _, ln := range s.Diff.Added {
		if !strings.Contains(ln, "[needs citation]") {
			t.Errorf("expected [needs citation] mark, got: %q", ln)
		}
	}
}

func TestMaintenance_AddCitation_SkipsWhenAllSourced(t *testing.T) {
	worker, cleanup := newMaintenanceFixture(t)
	defer cleanup()

	body := "# Stub\n\nNo strong claims here today.\n"
	seedArticle(t, worker, "team/companies/stub.md", body)

	a := NewMaintenanceAssistant(worker, nil, nil)
	s, err := a.Suggest(context.Background(), MaintActionAddCitation, "team/companies/stub.md")
	if err != nil {
		t.Fatalf("suggest: %v", err)
	}
	if !s.Skipped {
		t.Fatalf("expected skipped, got %+v", s)
	}
}

func TestMaintenance_ExtractFacts_ProposesTriples(t *testing.T) {
	worker, cleanup := newMaintenanceFixture(t)
	defer cleanup()

	body := "# Sarah Chen\n\nSarah Chen works at Acme Corp.\n\nShe is based in Seattle.\n"
	seedArticle(t, worker, "team/people/sarah-chen.md", body)

	a := NewMaintenanceAssistant(worker, nil, nil)
	s, err := a.Suggest(context.Background(), MaintActionExtractFacts, "team/people/sarah-chen.md")
	if err != nil {
		t.Fatalf("suggest: %v", err)
	}
	if s.Skipped {
		t.Fatalf("expected suggestion, got skipped: %s", s.SkippedReason)
	}
	if len(s.Facts) < 2 {
		t.Fatalf("expected at least 2 fact proposals, got %d", len(s.Facts))
	}
	for _, f := range s.Facts {
		if f.Confidence >= 0.7 {
			t.Errorf("confidence too high (review-bypass risk): %v", f.Confidence)
		}
		if f.Subject != "sarah-chen" {
			t.Errorf("subject should anchor on slug, got %q", f.Subject)
		}
	}
}

func TestMaintenance_ExtractFacts_SkipsNonEntityPaths(t *testing.T) {
	worker, cleanup := newMaintenanceFixture(t)
	defer cleanup()

	body := "# Random Doc\n\nFoo bar baz.\n"
	seedArticle(t, worker, "team/notes/random.md", body)

	a := NewMaintenanceAssistant(worker, nil, nil)
	s, err := a.Suggest(context.Background(), MaintActionExtractFacts, "team/notes/random.md")
	if err != nil {
		t.Fatalf("suggest: %v", err)
	}
	if !s.Skipped {
		t.Fatalf("expected skipped for non-entity path, got %+v", s)
	}
}

func TestMaintenance_SplitLong_SkipsShortArticles(t *testing.T) {
	worker, cleanup := newMaintenanceFixture(t)
	defer cleanup()

	seedArticle(t, worker, "team/people/sarah.md", "# Sarah\n\nA short note.\n")

	a := NewMaintenanceAssistant(worker, nil, nil)
	s, err := a.Suggest(context.Background(), MaintActionSplitLong, "team/people/sarah.md")
	if err != nil {
		t.Fatalf("suggest: %v", err)
	}
	if !s.Skipped {
		t.Fatalf("expected skipped, got %+v", s)
	}
}

func TestMaintenance_SplitLong_ProposesWhenLong(t *testing.T) {
	worker, cleanup := newMaintenanceFixture(t)
	defer cleanup()

	long := "# Acme\n\n"
	for i := 0; i < 5; i++ {
		long += "## Section " + string(rune('A'+i)) + "\n\n"
		long += strings.Repeat("Long words and phrases describing the section in detail. ", 80)
		long += "\n\n"
	}
	seedArticle(t, worker, "team/companies/acme.md", long)

	a := NewMaintenanceAssistant(worker, nil, nil)
	s, err := a.Suggest(context.Background(), MaintActionSplitLong, "team/companies/acme.md")
	if err != nil {
		t.Fatalf("suggest: %v", err)
	}
	if s.Skipped {
		t.Fatalf("expected split suggestion, got skipped: %s", s.SkippedReason)
	}
	if s.Diff == nil || len(s.Diff.Added) < 2 {
		t.Fatalf("expected at least 2 split outline entries, got %+v", s.Diff)
	}
}

func TestMaintenance_RefreshStale_SkipsWhenRecentlyEdited(t *testing.T) {
	worker, cleanup := newMaintenanceFixture(t)
	defer cleanup()

	// Article newly seeded — frontmatter has no last_edited_ts but the
	// repo HEAD shows a fresh edit. v1 reads frontmatter; missing ts
	// means we fall through to evidence (and find none for a non-entity
	// path), so we expect Skipped.
	body := "# Stale\n\nSome content.\n"
	seedArticle(t, worker, "team/notes/stale.md", body)

	a := NewMaintenanceAssistant(worker, nil, nil)
	s, err := a.Suggest(context.Background(), MaintActionRefreshStale, "team/notes/stale.md")
	if err != nil {
		t.Fatalf("suggest: %v", err)
	}
	if !s.Skipped {
		t.Fatalf("expected skipped, got %+v", s)
	}
}

func TestMaintenance_LinkRelated_SkipsWithoutIndex(t *testing.T) {
	worker, cleanup := newMaintenanceFixture(t)
	defer cleanup()

	seedArticle(t, worker, "team/people/sarah.md", "# Sarah\n\nworks at acme\n")

	a := NewMaintenanceAssistant(worker, nil, nil)
	s, err := a.Suggest(context.Background(), MaintActionLinkRelated, "team/people/sarah.md")
	if err != nil {
		t.Fatalf("suggest: %v", err)
	}
	if !s.Skipped {
		t.Fatalf("expected skipped without index, got %+v", s)
	}
}

func TestMaintenance_ResolveContradiction_SkipsWithoutLint(t *testing.T) {
	worker, cleanup := newMaintenanceFixture(t)
	defer cleanup()

	seedArticle(t, worker, "team/people/sarah.md", "# Sarah\n\nrole.\n")

	a := NewMaintenanceAssistant(worker, nil, nil)
	s, err := a.Suggest(context.Background(), MaintActionResolveContradiction, "team/people/sarah.md")
	if err != nil {
		t.Fatalf("suggest: %v", err)
	}
	if !s.Skipped {
		t.Fatalf("expected skipped without lint, got %+v", s)
	}
}

func TestMaintenance_UnknownAction_ReturnsError(t *testing.T) {
	worker, cleanup := newMaintenanceFixture(t)
	defer cleanup()

	seedArticle(t, worker, "team/notes/x.md", "# X\n")

	a := NewMaintenanceAssistant(worker, nil, nil)
	if _, err := a.Suggest(context.Background(), "no-such-action", "team/notes/x.md"); err == nil {
		t.Fatal("expected error for unknown action")
	}
}

func TestMaintenance_NilWorker_ReturnsError(t *testing.T) {
	a := NewMaintenanceAssistant(nil, nil, nil)
	if _, err := a.Suggest(context.Background(), MaintActionSummarize, "x.md"); err == nil {
		t.Fatal("expected error when worker is nil")
	}
}

func TestSlugFromPath(t *testing.T) {
	tt := []struct {
		path string
		want string
	}{
		{"team/people/nazz.md", "nazz"},
		{"team/companies/acme-corp.md", "acme-corp"},
		{"team/customers/wayne-industries.md", "wayne-industries"},
		{"people/nazz.md", "nazz"},
		{"team/notes/random.md", ""},
		{"random", ""},
	}
	for _, c := range tt {
		got := slugFromPath(c.path)
		if got != c.want {
			t.Errorf("slugFromPath(%q) = %q, want %q", c.path, got, c.want)
		}
	}
}

func TestClaimNeedsCitation(t *testing.T) {
	tt := []struct {
		line string
		want bool
	}{
		{"Acme raised 50 million in 2024.", true},
		{"They acquired BigCo last year.", true},
		{"It feels small.", false},
		{"See [the report](https://example.com/report) for details.", false},
		{"Already noted [needs citation]", false},
		{"Their team is great.", false},
	}
	for _, c := range tt {
		got := claimNeedsCitation(c.line)
		if got != c.want {
			t.Errorf("claimNeedsCitation(%q) = %v, want %v", c.line, got, c.want)
		}
	}
}
