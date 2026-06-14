package team

// context_assembler_test.go — the mandatory task-start retrieval helpers
// (anti-fabrication fix family #2): term extraction from the task's own
// contract text, and the file-level wiki search that scores team/ articles
// by distinct-term containment. The packet-level rendering is covered in
// notification_context_test.go; the end-to-end office behavior in
// office_eval.go's grounding job.

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestTaskRetrievalTerms_DefinitionTitleDetailsOrderedDeduped(t *testing.T) {
	t.Parallel()
	task := teamTask{
		Title:   "Prepare the Acme Corp QBR one-pager",
		Details: "Use the account brief and renewal playbook for Acme Corp.",
		Definition: &TaskDefinition{
			Goal: "Ship the Acme Corp QBR brief grounded in the wiki",
		},
	}
	terms := taskRetrievalTerms(task)
	if len(terms) == 0 || len(terms) > taskRetrievalTermCap {
		t.Fatalf("terms out of bounds: %v", terms)
	}
	// Definition goal leads; stopwords and short tokens are filtered;
	// duplicates collapse to first occurrence.
	if terms[0] != "ship" {
		t.Errorf("definition goal must lead the terms; got %v", terms)
	}
	seen := map[string]int{}
	for _, term := range terms {
		seen[term]++
		if seen[term] > 1 {
			t.Errorf("duplicate term %q in %v", term, terms)
		}
	}
	for _, want := range []string{"acme", "corp", "qbr"} {
		if seen[want] == 0 {
			t.Errorf("terms missing %q: %v", want, terms)
		}
	}
	if got := taskRetrievalTerms(teamTask{}); got != nil {
		t.Errorf("empty task must yield no terms; got %v", got)
	}
}

func TestSearchWikiArticlesByTerms_ScoresAndSkipsArchived(t *testing.T) {
	t.Parallel()
	repo := newTestRepo(t)
	if err := repo.Init(context.Background()); err != nil {
		t.Fatalf("repo init: %v", err)
	}
	write := func(rel, content string) {
		t.Helper()
		path := filepath.Join(repo.Root(), filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("team/accounts/acme-corp.md", "# Acme Corp — renewal brief\n\nRED risk. Owner: Dana Whitfield. Renewal Q3.\n")
	write("team/playbooks/renewal-outreach.md", "# Renewal outreach playbook\n\nEscalation-first for accounts with open tickets.\n")
	write("team/accounts/old-acme.md", "---\narchived: true\n---\n# Acme Corp legacy\n\nAcme Corp renewal history.\n")

	hits := searchWikiArticlesByTerms(repo, []string{"acme", "corp", "renewal"}, 5)
	if len(hits) != 2 {
		t.Fatalf("want 2 active hits (archived skipped); got %+v", hits)
	}
	// The 3-term match outranks the 1-term match.
	if hits[0].Path != "team/accounts/acme-corp.md" || hits[0].Title != "Acme Corp — renewal brief" {
		t.Errorf("top hit must be the full-match article with its heading title; got %+v", hits[0])
	}
	if hits[1].Path != "team/playbooks/renewal-outreach.md" {
		t.Errorf("second hit must be the partial match; got %+v", hits[1])
	}

	if got := searchWikiArticlesByTerms(repo, []string{"quokka", "zephyr"}, 5); len(got) != 0 {
		t.Errorf("unmatched terms must yield no hits; got %+v", got)
	}
	if got := searchWikiArticlesByTerms(nil, []string{"acme"}, 5); got != nil {
		t.Errorf("nil repo must be nil-safe; got %+v", got)
	}
}
