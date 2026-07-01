package team

import "testing"

func testSources() []knowledgeSource {
	return []knowledgeSource{
		{N: 1, Kind: "document", Title: "App: X", Detail: "spec", Snippet: "does a thing"},
		{N: 2, Kind: "roster", Title: "Office roster", Detail: "team", Snippet: "Maya — RevOps"},
	}
}

func TestSanitizeKnowledgePagesDropsUngroundedRefsAndPages(t *testing.T) {
	pages := []appKnowledgePage{
		{
			Title: "Good Page",
			Lead:  "A fact.[[1]]",
			References: []appKnowledgeRef{
				{N: 1, Title: "App: X", Kind: "document", Snippet: "does a thing", Why: "it says so"},
				{N: 9, Title: "Made up", Kind: "document", Snippet: "hallucinated"}, // no such source → dropped
			},
			SeeAlso:    []string{"ghost-page"}, // no such page → dropped
			Categories: []string{"Ops", "  "},
		},
		{
			// No grounded references at all → whole page dropped.
			Title:      "Empty Refs",
			Lead:       "Unsupported claim.",
			References: []appKnowledgeRef{{N: 42, Title: "nope"}},
		},
	}
	out := sanitizeKnowledgePages(pages, testSources())
	if len(out) != 1 {
		t.Fatalf("want 1 page (ungrounded dropped), got %d", len(out))
	}
	p := out[0]
	if p.ID != "good-page" {
		t.Fatalf("id should slugify from title, got %q", p.ID)
	}
	if len(p.References) != 1 || p.References[0].N != 1 {
		t.Fatalf("want only the grounded ref [1], got %+v", p.References)
	}
	if len(p.SeeAlso) != 0 {
		t.Fatalf("seeAlso to a non-existent page should be dropped, got %v", p.SeeAlso)
	}
	if len(p.Categories) != 1 || p.Categories[0] != "Ops" {
		t.Fatalf("blank categories should be trimmed, got %v", p.Categories)
	}
	if p.UpdatedAt == "" {
		t.Fatalf("updatedAt should be stamped server-side")
	}
}

func TestSanitizeRefsNormalizesKindAndFillsFromSource(t *testing.T) {
	refs := []appKnowledgeRef{
		{N: 1, Kind: "bogus"},           // unknown kind → source kind (document)
		{N: 2, Title: "", Kind: "chat"}, // valid kind kept; title filled from source
		{N: 1, Kind: "document"},        // duplicate n → dropped
	}
	byN := map[int]knowledgeSource{}
	for _, s := range testSources() {
		byN[s.N] = s
	}
	out := sanitizeRefs(refs, byN)
	if len(out) != 2 {
		t.Fatalf("want 2 refs (dup dropped), got %d", len(out))
	}
	if out[0].Kind != "document" {
		t.Fatalf("unknown kind should normalize to source kind, got %q", out[0].Kind)
	}
	if out[1].Title != "Office roster" {
		t.Fatalf("empty title should fill from source, got %q", out[1].Title)
	}
	// A ref with an empty snippet gets one from the source.
	if out[0].Snippet == "" {
		t.Fatalf("empty snippet should fall back to the source excerpt")
	}
}

func TestSanitizeKnowledgePagesCapsAtThree(t *testing.T) {
	mk := func(id string) appKnowledgePage {
		return appKnowledgePage{
			Title:      id,
			References: []appKnowledgeRef{{N: 1, Kind: "document"}},
		}
	}
	pages := []appKnowledgePage{mk("a"), mk("b"), mk("c"), mk("d")}
	out := sanitizeKnowledgePages(pages, testSources())
	if len(out) != 3 {
		t.Fatalf("want at most 3 pages, got %d", len(out))
	}
}
