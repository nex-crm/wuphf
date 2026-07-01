package team

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestKnowledgePageGBrainRoundTrip(t *testing.T) {
	page := appKnowledgePage{
		ID:       "world-weather",
		Title:    "World Weather",
		Category: "Weather app",
		Summary:  "Weather for five cities.",
		Infobox:  []appKnowledgeInfoRow{{Label: "Unit", Value: "Celsius"}},
		Lead:     "Shows temperatures in Celsius.[[1]]",
		Sections: []appKnowledgeSection{
			{Heading: "What it shows", Paras: []string{"Temp in degrees C.[[1]]"}},
		},
		References: []appKnowledgeRef{
			{N: 1, Title: "App brief", Detail: "spec", Kind: "document", Snippet: "…", Why: "states the unit"},
		},
		Categories: []string{"Weather"},
		SeeAlso:    []string{"other"},
	}
	content, err := renderKnowledgePageForGBrain("app_d50e34194a87a5ed", page)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	// Frontmatter carries the app-scope tag for ListPages filtering.
	if !strings.Contains(content, "wuphf-app-d50e34194a87a5ed") {
		t.Fatalf("missing app-scope tag in frontmatter:\n%s", content)
	}
	// The readable body strips citation markers (the exact form is in base64).
	body := content[strings.LastIndex(content, "---\n\n")+len("---\n\n"):]
	readable := body[:strings.Index(body, "<!--")]
	if strings.Contains(readable, "[[1]]") {
		t.Fatalf("readable body should strip [[n]] citations:\n%s", readable)
	}
	if !strings.Contains(readable, "World Weather") {
		t.Fatalf("readable body should contain the title")
	}
	// The structured page round-trips exactly.
	got, ok := decodeKnowledgePageFromBody(content)
	if !ok {
		t.Fatalf("decode failed")
	}
	a, _ := json.Marshal(page)
	b, _ := json.Marshal(got)
	if string(a) != string(b) {
		t.Fatalf("round-trip mismatch:\n want %s\n got  %s", a, b)
	}
}

func TestAppKnowledgeSlugAndTag(t *testing.T) {
	if got := appKnowledgeScopeTag("app_abc123"); got != "wuphf-app-abc123" {
		t.Fatalf("scope tag = %q", got)
	}
	if got := appKnowledgeSlug("app_abc123", "world-weather"); got != "k-abc123-world-weather" {
		t.Fatalf("slug = %q", got)
	}
}

func TestDecodeKnowledgePageRejectsGarbage(t *testing.T) {
	if _, ok := decodeKnowledgePageFromBody("no marker here"); ok {
		t.Fatalf("should reject body with no marker")
	}
	if _, ok := decodeKnowledgePageFromBody("<!--wuphf-knowledge-b64:%%%notb64%%%-->"); ok {
		t.Fatalf("should reject invalid base64")
	}
}

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
