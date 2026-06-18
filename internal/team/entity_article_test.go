package team

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// completionFact builds a fact in the exact shape the B1 hook writes
// (taskCompletionFactText), so citation parsing is exercised end-to-end.
func completionFact(kind EntityKind, slug, taskID, artifact, wikilink, recordedBy string, at time.Time) Fact {
	text := "Completed task " + taskID + " (\"Close the renewal\") involved this entity. Goal: Renew Acme."
	if artifact != "" {
		text += " Produced artifact: " + artifact + "."
	}
	if wikilink != "" {
		text += " Co-occurring entities: " + wikilink + "."
	}
	return Fact{
		ID: "f-" + taskID, Kind: kind, Slug: slug, Text: text,
		SourcePath: artifact, RecordedBy: recordedBy, CreatedAt: at,
	}
}

// The deterministic skeleton: marker, H1, bold lead, infobox definition
// list, themed sections, footnote citations with task + artifact, wikilinks,
// Associated section from graph edges, References — same input, same bytes.
func TestBuildEntityArticle_Skeleton(t *testing.T) {
	t0 := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	data := entityArticleData{
		Kind:  EntityKindCompanies,
		Slug:  "acme-corp",
		Title: "Acme Corp",
		Facts: []Fact{
			completionFact(EntityKindCompanies, "acme-corp", "TASK-3", "team/playbooks/acme-renewal.md", "[[people/eng]]", "eng", t0),
			{ID: "f2", Kind: EntityKindCompanies, Slug: "acme-corp", Text: "Prefers quarterly billing.", SourcePath: "agents/eng/notes.md", RecordedBy: "ceo", CreatedAt: t0.Add(time.Hour)},
		},
		Associated: []entityAssociation{{Kind: EntityKindPeople, Slug: "eng", SharedFacts: 2}},
	}
	got := buildEntityArticle(data)

	for _, want := range []string{
		entityArticleMarker,
		"# Acme Corp\n",
		// Substance-first lead: states what it is, no count boilerplate.
		"**Acme Corp** is a company.\n",
		"## Summary",
		"Kind\n: company",
		"Tasks\n: TASK-3",
		"Artifacts\n: [team/playbooks/acme-renewal.md](team/playbooks/acme-renewal.md)",
		"Associated\n: [[people/eng]]",
		"## Work history",
		"[^1]",
		"## Observations",
		"- Prefers quarterly billing.[^2]",
		"## Associated",
		"- [[people/eng]] — 2 shared facts",
		"## References",
		"[^1]: Task TASK-3 — artifact: [team/playbooks/acme-renewal.md](team/playbooks/acme-renewal.md); recorded by eng on 2026-06-10.",
		"[^2]: artifact: [agents/eng/notes.md](agents/eng/notes.md); recorded by ceo on 2026-06-10.",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("article missing %q\n--- article ---\n%s", want, got)
		}
	}
	// The metadata-feeling boilerplate the customer flagged must be gone: no
	// "knowledge graph" framing, no fact-count clause, no "Facts on record"
	// infobox row, and no on-disk file path masquerading as content.
	for _, banned := range []string{
		// The lead's old count clause ("…in the team knowledge graph, with N
		// recorded facts…"). The header HTML comment still references the
		// knowledge graph, so match the reader-visible phrasing specifically.
		"in the team knowledge graph",
		"recorded fact",
		"Facts on record",
		"Article\n: team/companies/acme-corp.md",
	} {
		if strings.Contains(got, banned) {
			t.Errorf("article still contains metadata boilerplate %q\n--- article ---\n%s", banned, got)
		}
	}
	if again := buildEntityArticle(data); again != got {
		t.Fatalf("article assembly must be deterministic")
	}
	// No frontmatter — the regenerator stamps it.
	if strings.HasPrefix(got, "---\n") {
		t.Fatalf("builder must not emit frontmatter")
	}
}

// Prose enrichment renders sentinel-wrapped under "## Background" and is
// recoverable for the next regeneration.
func TestBuildEntityArticle_ProseSectionRoundTrips(t *testing.T) {
	data := entityArticleData{
		Kind: EntityKindPeople, Slug: "eng", Title: "Eng",
		Facts: []Fact{{ID: "f1", Kind: EntityKindPeople, Slug: "eng", Text: "Owns the broker.", RecordedBy: "ceo", CreatedAt: time.Now().UTC()}},
		Prose: "Eng has led the renewal motion since spring.",
	}
	got := buildEntityArticle(data)
	if !strings.Contains(got, entityProseSentinelStart+"\n## Background\n\nEng has led the renewal motion since spring.\n"+entityProseSentinelEnd) {
		t.Fatalf("prose block malformed:\n%s", got)
	}
	if extracted := extractEntityArticleProse(got); extracted != "Eng has led the renewal motion since spring." {
		t.Fatalf("prose extraction = %q", extracted)
	}
}

// A legacy (pre-article) brief — e.g. an LLM-synthesized brief or a ghost
// placeholder — folds into the prose section: H1, managed Related section,
// disclaimer, and comments are dropped; substance survives.
func TestExtractEntityArticleProse_FoldsLegacyBrief(t *testing.T) {
	legacy := "---\nslug: acme-corp\nkind: companies\n---\n\n# Acme Corp\n\nAcme is a mid-market design platform.\n\n" +
		relatedSentinelStart + "\n## Related\n\n- [[people/eng]]\n" + relatedSentinelEnd + "\n"
	got := extractEntityArticleProse(legacy)
	if got != "Acme is a mid-market design platform." {
		t.Fatalf("legacy fold = %q", got)
	}

	ghost := "---\nslug: acme-corp\nkind: companies\nghost: true\n---\n\n# Acme Corp\n\n## Signals\n\n- (none)\n\n" + minimalBriefDisclaimer + "\n"
	if got := extractEntityArticleProse(ghost); got != "" {
		t.Fatalf("empty ghost placeholder must fold to nothing, got %q", got)
	}
}

// entityAssociations merges out- and in-edges (backlinks), dedupes the
// other end, elides self, and orders deterministically.
func TestEntityAssociations_MergesBothDirections(t *testing.T) {
	out := []CoalescedEdge{
		{FromKind: EntityKindCompanies, FromSlug: "acme-corp", ToKind: EntityKindPeople, ToSlug: "eng", OccurrenceCount: 2},
	}
	in := []CoalescedEdge{
		{FromKind: EntityKindPeople, FromSlug: "eng", ToKind: EntityKindCompanies, ToSlug: "acme-corp", OccurrenceCount: 1},
		{FromKind: EntityKindCompanies, FromSlug: "acme-corp", ToKind: EntityKindCompanies, ToSlug: "acme-corp", OccurrenceCount: 9}, // self — elided
		{FromKind: EntityKindCustomers, FromSlug: "globex", ToKind: EntityKindCompanies, ToSlug: "acme-corp", OccurrenceCount: 1},
	}
	got := entityAssociations(EntityKindCompanies, "acme-corp", out, in)
	if len(got) != 2 {
		t.Fatalf("expected 2 associations, got %+v", got)
	}
	// Deterministic order: customers < people (kind sort).
	if got[0].Kind != EntityKindCustomers || got[0].Slug != "globex" {
		t.Errorf("order[0] = %+v", got[0])
	}
	if got[1].Kind != EntityKindPeople || got[1].Slug != "eng" || got[1].SharedFacts != 3 {
		t.Errorf("eng association must merge counts from both directions: %+v", got[1])
	}
}

// RegenerateEntityArticle writes the article at the stable brief path on
// first run and UPDATES the same file on the next — no duplicate, the new
// fact appended, frontmatter fact count restamped.
func TestRegenerateEntityArticle_CreateThenUpdate(t *testing.T) {
	factLog, worker, teardown := newFactLogFixture(t)
	defer teardown()
	graph := NewEntityGraph(worker)
	ctx := context.Background()

	record := func(slug string, kind EntityKind, text, source string) {
		t.Helper()
		fact, err := factLog.Append(ctx, kind, slug, text, source, "eng")
		if err != nil {
			t.Fatalf("append: %v", err)
		}
		if _, err := graph.RecordFactRefs(ctx, fact); err != nil {
			t.Fatalf("graph: %v", err)
		}
	}
	record("acme-corp", EntityKindCompanies,
		"Completed task TASK-3 (\"Close the renewal\") involved this entity. Goal: Renew. Produced artifact: team/playbooks/acme-renewal.md. Co-occurring entities: [[people/eng]].",
		"team/playbooks/acme-renewal.md")
	if err := RegenerateEntityArticle(ctx, worker, factLog, graph, EntityKindCompanies, "acme-corp"); err != nil {
		t.Fatalf("regenerate 1: %v", err)
	}

	relPath := briefPath(EntityKindCompanies, "acme-corp")
	read := func() string {
		t.Helper()
		body, err := os.ReadFile(filepath.Join(worker.Repo().Root(), filepath.FromSlash(relPath)))
		if err != nil {
			t.Fatalf("read article: %v", err)
		}
		return string(body)
	}
	first := read()
	if !strings.Contains(first, entityArticleMarker) || !strings.Contains(first, "TASK-3") {
		t.Fatalf("first article malformed:\n%s", first)
	}
	if !strings.Contains(first, "[[people/eng]]") {
		t.Fatalf("first article must wikilink the associated entity:\n%s", first)
	}
	if _, _, factCount := parseSynthesisFrontmatter(first); factCount != 1 {
		t.Fatalf("fact_count_at_synthesis = %d, want 1", factCount)
	}

	record("acme-corp", EntityKindCompanies,
		"Completed task TASK-9 (\"Expansion\") involved this entity. Goal: Expand. Produced artifact: team/playbooks/acme-expansion.md.",
		"team/playbooks/acme-expansion.md")
	if err := RegenerateEntityArticle(ctx, worker, factLog, graph, EntityKindCompanies, "acme-corp"); err != nil {
		t.Fatalf("regenerate 2: %v", err)
	}
	second := read()
	if !strings.Contains(second, "TASK-3") || !strings.Contains(second, "TASK-9") {
		t.Fatalf("update must keep the old fact and append the new:\n%s", second)
	}
	if strings.Count(second, "# Acme Corp\n") != 1 {
		t.Fatalf("update must not duplicate the article:\n%s", second)
	}
	if _, _, factCount := parseSynthesisFrontmatter(second); factCount != 2 {
		t.Fatalf("fact_count_at_synthesis = %d, want 2", factCount)
	}

	// Only one article file exists for the slug.
	entries, err := os.ReadDir(filepath.Join(worker.Repo().Root(), "team", "companies"))
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	count := 0
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "acme-corp") {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected exactly one article file, got %d", count)
	}
}

// A body edited by a human since the last synthesis is preserved verbatim;
// the generated article lands in a sentinel-wrapped managed block that is
// replaced (not duplicated) on the next regeneration.
func TestRegenerateEntityArticle_PreservesHumanEditedBody(t *testing.T) {
	factLog, worker, teardown := newFactLogFixture(t)
	defer teardown()
	ctx := context.Background()

	human := "---\nlast_synthesized_ts: 2026-01-01T00:00:00Z\nlast_human_edit_ts: 2026-06-01T00:00:00Z\nfact_count_at_synthesis: 0\n---\n\n# Acme Corp\n\nHand-written institutional knowledge that must survive.\n"
	relPath := briefPath(EntityKindCompanies, "acme-corp")
	if _, _, err := worker.Enqueue(ctx, "human", relPath, human, "replace", "human brief"); err != nil {
		t.Fatalf("seed human brief: %v", err)
	}
	if _, err := factLog.Append(ctx, EntityKindCompanies, "acme-corp", "Completed task TASK-3 (\"R\") involved this entity. Goal: G.", "", "eng"); err != nil {
		t.Fatalf("append: %v", err)
	}

	for i := 0; i < 2; i++ { // run twice — the managed block must not duplicate
		if err := RegenerateEntityArticle(ctx, worker, factLog, nil, EntityKindCompanies, "acme-corp"); err != nil {
			t.Fatalf("regenerate %d: %v", i+1, err)
		}
	}
	body, err := os.ReadFile(filepath.Join(worker.Repo().Root(), filepath.FromSlash(relPath)))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	got := string(body)
	if !strings.Contains(got, "Hand-written institutional knowledge that must survive.") {
		t.Fatalf("human body lost:\n%s", got)
	}
	if strings.Count(got, entityArticleSentinelStart) != 1 || strings.Count(got, entityArticleSentinelEnd) != 1 {
		t.Fatalf("managed article block must appear exactly once:\n%s", got)
	}
	if !strings.Contains(got, "TASK-3") {
		t.Fatalf("managed block must carry the generated article:\n%s", got)
	}
	// The human-edit timestamp must survive the frontmatter restamp.
	if parseLastHumanEditTS(got).IsZero() {
		t.Fatalf("last_human_edit_ts lost:\n%s", got)
	}
}

// A pre-existing ghost placeholder upgrades in place: the ghost flag is
// dropped and the placeholder body does not pollute the article.
func TestRegenerateEntityArticle_UpgradesGhostPlaceholder(t *testing.T) {
	factLog, worker, teardown := newFactLogFixture(t)
	defer teardown()
	ctx := context.Background()

	ghost := MinimalBrief(IndexEntity{Slug: "acme-corp", Kind: "companies", CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)})
	ghost = "---\nghost: true\n" + strings.TrimPrefix(ghost, "---\n")
	relPath := briefPath(EntityKindCompanies, "acme-corp")
	if _, _, err := worker.Enqueue(ctx, ArchivistAuthor, relPath, ghost, "replace", "ghost brief"); err != nil {
		t.Fatalf("seed ghost: %v", err)
	}
	if _, err := factLog.Append(ctx, EntityKindCompanies, "acme-corp", "Completed task TASK-1 (\"T\") involved this entity. Goal: G.", "", "eng"); err != nil {
		t.Fatalf("append: %v", err)
	}
	if err := RegenerateEntityArticle(ctx, worker, factLog, nil, EntityKindCompanies, "acme-corp"); err != nil {
		t.Fatalf("regenerate: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(worker.Repo().Root(), filepath.FromSlash(relPath)))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	got := string(body)
	if parseGhostFrontmatter(got) {
		t.Fatalf("ghost flag must be dropped on upgrade:\n%s", got)
	}
	if strings.Contains(got, minimalBriefDisclaimer) {
		t.Fatalf("placeholder disclaimer must not survive:\n%s", got)
	}
	if !strings.Contains(got, entityArticleMarker) || !strings.Contains(got, "TASK-1") {
		t.Fatalf("article body malformed:\n%s", got)
	}
}

// No facts → no write. The article generator never mints empty articles.
func TestRegenerateEntityArticle_NoFactsNoWrite(t *testing.T) {
	factLog, worker, teardown := newFactLogFixture(t)
	defer teardown()
	if err := RegenerateEntityArticle(context.Background(), worker, factLog, nil, EntityKindCompanies, "acme-corp"); err != nil {
		t.Fatalf("regenerate: %v", err)
	}
	if _, err := os.Stat(filepath.Join(worker.Repo().Root(), "team", "companies", "acme-corp.md")); !os.IsNotExist(err) {
		t.Fatalf("no-fact regeneration must not create a file (stat err=%v)", err)
	}
}

// The deterministic trigger: regenerateTaskEntityArticles writes one
// article per entity the completed task touched, mutually wikilinked.
func TestRegenerateTaskEntityArticles_MutualBacklinks(t *testing.T) {
	factLog, worker, teardown := newFactLogFixture(t)
	defer teardown()
	graph := NewEntityGraph(worker)
	ctx := context.Background()

	task := teamTask{
		ID: "TASK-7", Title: "Close the Acme Corp renewal with @eng",
		Details:  "Loop in @eng.",
		Artifact: "team/playbooks/acme-renewal.md",
		Definition: &TaskDefinition{
			Goal:         "Renew the Acme Corp account",
			Deliverables: []TaskDeliverable{{Name: "renewal brief"}},
		},
	}
	recordTaskCompletionEntityFacts(ctx, factLog, graph, task)
	regenerateTaskEntityArticles(ctx, worker, factLog, graph, task)

	read := func(rel string) string {
		t.Helper()
		body, err := os.ReadFile(filepath.Join(worker.Repo().Root(), filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		return string(body)
	}
	company := read("team/companies/acme-corp.md")
	person := read("team/people/eng.md")
	if !strings.Contains(company, "[[people/eng]]") {
		t.Errorf("company article missing backlink to person:\n%s", company)
	}
	if !strings.Contains(person, "[[companies/acme-corp]]") {
		t.Errorf("person article missing backlink to company:\n%s", person)
	}
	for name, body := range map[string]string{"company": company, "person": person} {
		if !strings.Contains(body, "[^1]") || !strings.Contains(body, "Task TASK-7") {
			t.Errorf("%s article missing footnote citation:\n%s", name, body)
		}
	}
}
