package team

// entity_article.go — the B2 entity wiki articles
// (docs/specs/core-loop.md, Core Loop step 7.2).
//
// Entity facts become rich, linked, cited wiki articles — update-first.
// The article generator extends the existing entity-brief system:
//
//   - Stable path: one article per entity at the EXISTING brief path
//     team/{kind}/{slug}.md (briefPath). Slug collision = update, never a
//     duplicate file. This is the flat per-kind namespace B5 will build on.
//   - Deterministic skeleton: lead paragraph, infobox-style Summary block
//     (definition list), sections grouped by fact theme, footnote-style
//     citations ([^n]) tying every fact-sourced claim back to its task and
//     artifact, [[kind/slug]] wikilinks, and an "Associated" section
//     rendered from BOTH directions of the cross-entity graph so A↔B
//     backlinks appear on both articles. Pure template assembly — no LLM,
//     so self-hosted installs without a synth provider still get articles.
//   - Trigger: deterministic only. The B1 completion hook regenerates the
//     articles for every entity it just recorded facts for, from the same
//     queued distillation goroutine (off the broker hot path, commits ride
//     the WikiWorker queue under the archivist identity). No agent-whim
//     writes.
//   - Update-first: regeneration reuses the brief system's conventions —
//     synthesis frontmatter bookkeeping (fact_count_at_synthesis), the
//     last_human_edit_ts edit guard (a human-edited body is preserved
//     verbatim and the generated article lands in a sentinel-wrapped
//     managed block instead), and sentinel-wrapped managed regions. A
//     pre-existing LLM-synthesized brief (the entity_synthesizer.go path)
//     is folded into the article's "## Background" prose section, so the
//     optional LLM enrichment the briefs already have is preserved rather
//     than stomped.

import (
	"context"
	"fmt"
	"log"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// entityArticleMarker identifies a generated entity article. Kept as a
// prefix of the full header comment so detection survives wording tweaks
// to the human-readable half.
const entityArticleMarker = "<!-- wuphf:entity-article"

// entityArticleHeaderComment is the managed-content notice at the top of
// every generated article body.
const entityArticleHeaderComment = entityArticleMarker + ` — generated from the team knowledge graph (fact log + entity graph); regenerated deterministically when completed tasks record new facts. The generated body is fully managed: human edits are detected via last_human_edit_ts and preserved by moving the generated article into a managed block. -->`

// Sentinels for the managed regions inside / around an entity article.
//
//	entity-prose:  the optional enrichment prose ("## Background") carried
//	               forward across deterministic regenerations.
//	entity-article: the whole generated article when it must coexist with a
//	               human-edited body (same pattern as the synthesizer's
//	               learned-section sentinels).
const (
	entityProseSentinelStart   = "<!-- wuphf:entity-prose:start -->"
	entityProseSentinelEnd     = "<!-- wuphf:entity-prose:end -->"
	entityArticleSentinelStart = "<!-- wuphf:entity-article:start -->"
	entityArticleSentinelEnd   = "<!-- wuphf:entity-article:end -->"
)

// factTaskRefPattern / factArtifactPattern recover the task and artifact
// associations from the deterministic fact text the B1 completion hook
// writes (taskCompletionFactText): "Completed task <id> (...)" and
// "Produced artifact: <path>.".
var (
	factTaskRefPattern  = regexp.MustCompile(`Completed task (\S+)`)
	factArtifactPattern = regexp.MustCompile(`Produced artifact: (\S+)\.`)
)

// entityArticleLocks serializes regeneration per entity so two tasks
// completing concurrently cannot interleave read-build-commit and lose one
// another's facts. The WikiWorker queue serializes the commits themselves;
// this lock serializes the read-modify-write envelope around them.
var (
	entityArticleLocksMu sync.Mutex
	entityArticleLocks   = map[string]*sync.Mutex{}
)

func entityArticleLock(kind EntityKind, slug string) *sync.Mutex {
	key := entityKey(kind, slug)
	entityArticleLocksMu.Lock()
	defer entityArticleLocksMu.Unlock()
	mu, ok := entityArticleLocks[key]
	if !ok {
		mu = &sync.Mutex{}
		entityArticleLocks[key] = mu
	}
	return mu
}

// entityAssociation is one related entity rendered in the article — the
// coalesced union of out-edges and in-edges (backlinks) for the subject.
type entityAssociation struct {
	Kind        EntityKind
	Slug        string
	SharedFacts int
}

// entityArticleData is the deterministic input to buildEntityArticle.
type entityArticleData struct {
	Kind  EntityKind
	Slug  string
	Title string
	// Facts in chronological order (oldest first) — footnote numbering is
	// stable as the log grows because the log is append-only.
	Facts      []Fact
	Associated []entityAssociation
	// Prose is the optional enrichment section ("## Background"). Carried
	// forward across regenerations; populated from a prior LLM-synthesized
	// brief when one exists. Empty in pure-deterministic installs.
	Prose string
}

// entityKindNoun renders the kind as the singular noun used in prose.
func entityKindNoun(kind EntityKind) string {
	switch kind {
	case EntityKindPeople:
		return "person"
	case EntityKindCompanies:
		return "company"
	case EntityKindCustomers:
		return "customer"
	}
	return "entity"
}

// countNoun renders "1 fact" / "3 facts" style phrases deterministically.
func countNoun(n int, singular, plural string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, singular)
	}
	return fmt.Sprintf("%d %s", n, plural)
}

// entityFactCitation is the task/artifact provenance parsed from one fact.
type entityFactCitation struct {
	TaskID   string
	Artifact string
}

// citeFact recovers the citation for a fact: the task ID and artifact path
// embedded by the B1 hook's fact text, with the fact log's source_path as
// the artifact fallback for agent-recorded facts.
func citeFact(f Fact) entityFactCitation {
	c := entityFactCitation{}
	if m := factTaskRefPattern.FindStringSubmatch(f.Text); m != nil {
		c.TaskID = strings.Trim(m[1], ".,;:()\"")
	}
	if m := factArtifactPattern.FindStringSubmatch(f.Text); m != nil {
		c.Artifact = m[1]
	} else if sp := strings.TrimSpace(f.SourcePath); sp != "" {
		c.Artifact = sp
	}
	return c
}

// factTaskIDs returns the distinct task IDs cited across facts, in
// first-seen (chronological) order.
func factTaskIDs(facts []Fact) []string {
	out := make([]string, 0, len(facts))
	for _, f := range facts {
		if id := citeFact(f).TaskID; id != "" {
			out = append(out, id)
		}
	}
	return dedupePreserveOrder(out)
}

// factArtifacts returns the distinct artifact paths cited across facts, in
// first-seen (chronological) order.
func factArtifacts(facts []Fact) []string {
	out := make([]string, 0, len(facts))
	for _, f := range facts {
		if a := citeFact(f).Artifact; a != "" {
			out = append(out, a)
		}
	}
	return dedupePreserveOrder(out)
}

// splitFactThemes groups fact indices by theme: facts written by the task
// completion hook form the "Work history" section; everything else lands
// under "Observations". Deterministic string classification, no LLM.
func splitFactThemes(facts []Fact) (work, observations []int) {
	for i, f := range facts {
		if strings.HasPrefix(strings.TrimSpace(f.Text), "Completed task ") {
			work = append(work, i)
			continue
		}
		observations = append(observations, i)
	}
	return work, observations
}

// renderFactFootnote renders the markdown footnote definition for fact n.
func renderFactFootnote(n int, f Fact) string {
	c := citeFact(f)
	var b strings.Builder
	fmt.Fprintf(&b, "[^%d]: ", n)
	parts := make([]string, 0, 2)
	if c.TaskID != "" {
		parts = append(parts, "Task "+c.TaskID)
	}
	if c.Artifact != "" {
		parts = append(parts, fmt.Sprintf("artifact: [%s](%s)", c.Artifact, c.Artifact))
	}
	if len(parts) > 0 {
		b.WriteString(strings.Join(parts, " — "))
		b.WriteString("; ")
	}
	fmt.Fprintf(&b, "recorded by %s on %s.", f.RecordedBy, f.CreatedAt.UTC().Format("2006-01-02"))
	return b.String()
}

// renderFactBullet renders one cited claim line. The full fact text is kept
// (newlines collapsed) so the co-occurrence [[kind/slug]] wikilinks inside
// it stay in the article body and feed the wiki's backlink index.
func renderFactBullet(f Fact, n int) string {
	text := strings.TrimSpace(strings.Join(strings.Fields(f.Text), " "))
	return fmt.Sprintf("- %s[^%d]", text, n)
}

// entityAssociations merges the out-edges and in-edges touching the subject
// into one deduplicated association list. Rendering both directions is what
// makes backlinks bidirectional: when A's facts link B, both A's article
// (out-edge) and B's article (in-edge) list the other side. Deterministic
// order: kind, then slug.
func entityAssociations(kind EntityKind, slug string, out, in []CoalescedEdge) []entityAssociation {
	idx := map[string]*entityAssociation{}
	order := make([]string, 0, len(out)+len(in))
	add := func(k EntityKind, s string, count int) {
		if (k == kind && s == slug) || s == "" {
			return
		}
		key := string(k) + "/" + s
		if row, ok := idx[key]; ok {
			row.SharedFacts += count
			return
		}
		idx[key] = &entityAssociation{Kind: k, Slug: s, SharedFacts: count}
		order = append(order, key)
	}
	for _, e := range out {
		add(e.ToKind, e.ToSlug, e.OccurrenceCount)
	}
	for _, e := range in {
		add(e.FromKind, e.FromSlug, e.OccurrenceCount)
	}
	res := make([]entityAssociation, 0, len(order))
	for _, k := range order {
		res = append(res, *idx[k])
	}
	sort.SliceStable(res, func(i, j int) bool {
		if res[i].Kind != res[j].Kind {
			return res[i].Kind < res[j].Kind
		}
		return res[i].Slug < res[j].Slug
	})
	return res
}

// buildEntityArticle assembles the full Wikipedia-flavored article body
// from facts + graph edges. Pure deterministic template assembly: the same
// input always produces the same output. No frontmatter — the caller stamps
// it via applySynthesisFrontmatter so the brief system's bookkeeping keys
// stay authoritative.
func buildEntityArticle(d entityArticleData) string {
	tasks := factTaskIDs(d.Facts)
	artifacts := factArtifacts(d.Facts)

	var b strings.Builder
	b.WriteString(entityArticleHeaderComment)
	b.WriteString("\n\n# ")
	b.WriteString(d.Title)
	b.WriteString("\n\n")

	// Lead paragraph — subject bolded, Wikipedia style.
	fmt.Fprintf(&b, "**%s** is a %s in the team knowledge graph, with %s",
		d.Title, entityKindNoun(d.Kind), countNoun(len(d.Facts), "recorded fact", "recorded facts"))
	if len(tasks) > 0 {
		fmt.Fprintf(&b, " from %s", countNoun(len(tasks), "completed task", "completed tasks"))
	}
	if len(d.Associated) > 0 {
		fmt.Fprintf(&b, " and %s", countNoun(len(d.Associated), "associated entity", "associated entities"))
	}
	b.WriteString(".\n\n")

	// Infobox-style summary: key facts as a definition list.
	b.WriteString("## Summary\n\n")
	writeDef := func(term, def string) {
		b.WriteString(term)
		b.WriteString("\n: ")
		b.WriteString(def)
		b.WriteString("\n\n")
	}
	writeDef("Kind", entityKindNoun(d.Kind))
	writeDef("Article", briefPath(d.Kind, d.Slug))
	writeDef("Facts on record", strconv.Itoa(len(d.Facts)))
	if len(tasks) > 0 {
		writeDef("Tasks", strings.Join(tasks, ", "))
	}
	if len(artifacts) > 0 {
		links := make([]string, 0, len(artifacts))
		for _, a := range artifacts {
			links = append(links, fmt.Sprintf("[%s](%s)", a, a))
		}
		writeDef("Artifacts", strings.Join(links, ", "))
	}
	if len(d.Associated) > 0 {
		links := make([]string, 0, len(d.Associated))
		for _, a := range d.Associated {
			links = append(links, fmt.Sprintf("[[%s/%s]]", a.Kind, a.Slug))
		}
		writeDef("Associated", strings.Join(links, ", "))
	}

	// Optional enrichment prose, sentinel-wrapped so regenerations carry it
	// forward without re-deriving it.
	if p := strings.TrimSpace(d.Prose); p != "" {
		b.WriteString(entityProseSentinelStart)
		b.WriteString("\n## Background\n\n")
		b.WriteString(p)
		b.WriteString("\n")
		b.WriteString(entityProseSentinelEnd)
		b.WriteString("\n\n")
	}

	// Sections by fact theme, every claim cited with a footnote reference.
	work, observations := splitFactThemes(d.Facts)
	if len(work) > 0 {
		b.WriteString("## Work history\n\n")
		for _, i := range work {
			b.WriteString(renderFactBullet(d.Facts[i], i+1))
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}
	if len(observations) > 0 {
		b.WriteString("## Observations\n\n")
		for _, i := range observations {
			b.WriteString(renderFactBullet(d.Facts[i], i+1))
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	// Associated entities — both graph directions, so backlinks render on
	// each side's article.
	if len(d.Associated) > 0 {
		b.WriteString("## Associated\n\n")
		for _, a := range d.Associated {
			fmt.Fprintf(&b, "- [[%s/%s]] — %s\n", a.Kind, a.Slug, countNoun(a.SharedFacts, "shared fact", "shared facts"))
		}
		b.WriteString("\n")
	}

	// References — one footnote per fact, chronological numbering.
	if len(d.Facts) > 0 {
		b.WriteString("## References\n\n")
		for i, f := range d.Facts {
			b.WriteString(renderFactFootnote(i+1, f))
			b.WriteString("\n")
		}
	}
	return strings.TrimRight(b.String(), "\n") + "\n"
}

// applyEntityArticleSection lands the generated article inside a
// sentinel-wrapped managed block at the end of a human-edited body —
// the same coexistence pattern as the synthesizer's learned section.
// The previous managed block (if any) is replaced, never duplicated.
func applyEntityArticleSection(body, article string) string {
	stripped := strings.TrimRight(stripEntityArticleSection(body), "\n")
	var b strings.Builder
	b.WriteString(stripped)
	if stripped != "" {
		b.WriteString("\n\n")
	}
	b.WriteString(entityArticleSentinelStart)
	b.WriteString("\n")
	b.WriteString(strings.TrimRight(article, "\n"))
	b.WriteString("\n")
	b.WriteString(entityArticleSentinelEnd)
	b.WriteString("\n")
	return b.String()
}

// stripEntityArticleSection removes a previously written sentinel-wrapped
// article block. Returns body unchanged when no sentinels are present.
func stripEntityArticleSection(body string) string {
	start := strings.Index(body, entityArticleSentinelStart)
	if start < 0 {
		return body
	}
	after := body[start+len(entityArticleSentinelStart):]
	end := strings.Index(after, entityArticleSentinelEnd)
	if end < 0 {
		return body
	}
	tail := after[end+len(entityArticleSentinelEnd):]
	return strings.TrimRight(body[:start], "\n") + tail
}

// extractEntityArticleProse recovers the enrichment prose to carry into the
// next regeneration. For a previously generated article that is the content
// of its prose sentinels; for a legacy (pre-article) brief — typically an
// LLM-synthesized brief or a ghost placeholder — the cleaned body is folded
// in wholesale so existing enrichment survives the format upgrade.
func extractEntityArticleProse(existing string) string {
	if strings.TrimSpace(existing) == "" {
		return ""
	}
	body := stripFrontmatter(existing)
	if !strings.Contains(body, entityArticleMarker) {
		return cleanLegacyBriefForProse(body)
	}
	start := strings.Index(body, entityProseSentinelStart)
	if start < 0 {
		return ""
	}
	after := body[start+len(entityProseSentinelStart):]
	end := strings.Index(after, entityProseSentinelEnd)
	if end < 0 {
		return ""
	}
	block := strings.TrimSpace(after[:end])
	block = strings.TrimPrefix(block, "## Background")
	return strings.TrimSpace(block)
}

// cleanLegacyBriefForProse prepares a pre-article brief body for the prose
// section: drops the managed Related section (the article re-renders
// associations from the graph), the H1 (the article has its own), HTML
// comment lines, and the ghost-placeholder disclaimer. Returns "" when
// nothing of substance remains (e.g. an empty ghost placeholder).
func cleanLegacyBriefForProse(body string) string {
	body = stripRelatedSection(body)
	lines := strings.Split(body, "\n")
	kept := make([]string, 0, len(lines))
	droppedH1 := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !droppedH1 && strings.HasPrefix(trimmed, "# ") {
			droppedH1 = true
			continue
		}
		if trimmed == minimalBriefDisclaimer {
			continue
		}
		if strings.HasPrefix(trimmed, "<!--") && strings.HasSuffix(trimmed, "-->") {
			continue
		}
		kept = append(kept, line)
	}
	out := strings.TrimSpace(strings.Join(kept, "\n"))
	// An empty ghost placeholder reduces to its signals stub — no knowledge.
	if out == "## Signals\n\n- (none)" || out == "## Signals\n- (none)" {
		return ""
	}
	return out
}

// dropGhostFrontmatter removes a `ghost: true` frontmatter line. A
// regenerated article is real content, not a placeholder — leaving the
// ghost flag would keep the article rendered as a ghost in ArticleMeta.
func dropGhostFrontmatter(body string) string {
	if !strings.HasPrefix(body, "---\n") {
		return body
	}
	rest := body[len("---\n"):]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return body
	}
	block := rest[:end]
	tail := rest[end:]
	lines := strings.Split(block, "\n")
	kept := make([]string, 0, len(lines))
	changed := false
	for _, line := range lines {
		m := frontmatterKeyLine.FindStringSubmatch(line)
		if m != nil && m[1] == "ghost" && strings.TrimSpace(m[2]) == "true" {
			changed = true
			continue
		}
		kept = append(kept, line)
	}
	if !changed {
		return body
	}
	return "---\n" + strings.Join(kept, "\n") + tail
}

// wikiRepoHeadSHA returns the repo's current short HEAD SHA. Shared by the
// synthesizer and the article generator for frontmatter stamping.
func wikiRepoHeadSHA(ctx context.Context, repo *Repo) (string, error) {
	repo.mu.Lock()
	defer repo.mu.Unlock()
	out, err := repo.runGitLocked(ctx, "system", "rev-parse", "--short", "HEAD")
	if err != nil {
		return "", fmt.Errorf("%w: %s", err, out)
	}
	return strings.TrimSpace(out), nil
}

// RegenerateEntityArticle rebuilds the entity's wiki article from the fact
// log + cross-entity graph and commits it at the stable brief path via the
// WikiWorker queue. Update-first by construction: the path is fixed per
// (kind, slug), the regeneration replaces the managed body, and the brief
// system's fact-count frontmatter is restamped so downstream pending-delta
// bookkeeping (and LLM brief synthesis thresholds) see the facts as
// incorporated. No-op when the entity has no facts. No LLM call.
func RegenerateEntityArticle(ctx context.Context, worker *WikiWorker, factLog *FactLog, graph *EntityGraph, kind EntityKind, slug string) error {
	if worker == nil || factLog == nil {
		return fmt.Errorf("entity article: worker and fact log are required")
	}
	if err := validateListInputs(kind, slug); err != nil {
		return fmt.Errorf("entity article: %w", err)
	}

	mu := entityArticleLock(kind, slug)
	mu.Lock()
	defer mu.Unlock()

	facts, err := factLog.List(kind, slug)
	if err != nil {
		return fmt.Errorf("entity article: list facts: %w", err)
	}
	if len(facts) == 0 {
		return nil
	}
	// List returns newest-first; the article cites chronologically so
	// footnote numbers stay stable as the append-only log grows.
	ordered := make([]Fact, len(facts))
	for i, f := range facts {
		ordered[len(facts)-1-i] = f
	}

	var associated []entityAssociation
	if graph != nil {
		outEdges, outErr := graph.Query(kind, slug, DirectionOut)
		inEdges, inErr := graph.Query(kind, slug, DirectionIn)
		if outErr != nil || inErr != nil {
			// Non-fatal: an article without associations is still an article.
			log.Printf("entity article: graph query %s/%s: out=%v in=%v", kind, slug, outErr, inErr)
		}
		associated = entityAssociations(kind, slug, outEdges, inEdges)
	}

	relPath := briefPath(kind, slug)
	existingBytes, _ := readArticle(worker.Repo(), relPath)
	existing := string(existingBytes)
	_, lastSynthTS, _ := parseSynthesisFrontmatter(existing)

	title := strings.TrimSpace(briefTitleFrom(existing, ""))
	if title == "" {
		title = humanizeSlugForBrief(slug)
	}

	// Coexistence mode is sticky: once a human edit forced the article into
	// a managed block, later regenerations keep replacing that block — the
	// restamped last_synthesized_ts would otherwise out-date the human edit
	// and the next run would stomp the human body.
	humanEdited := strings.TrimSpace(existing) != "" &&
		(humanEditedSince(existing, lastSynthTS) || strings.Contains(existing, entityArticleSentinelStart))
	prose := ""
	if !humanEdited {
		prose = extractEntityArticleProse(existing)
	}

	article := buildEntityArticle(entityArticleData{
		Kind:       kind,
		Slug:       slug,
		Title:      title,
		Facts:      ordered,
		Associated: associated,
		Prose:      prose,
	})

	var body string
	if humanEdited {
		body = applyEntityArticleSection(stripFrontmatter(existing), article)
	} else {
		body = article
	}

	headSHA, headErr := wikiRepoHeadSHA(ctx, worker.Repo())
	if headErr != nil {
		// Non-fatal — the next regeneration recounts every fact.
		log.Printf("entity article: resolve HEAD: %v", headErr)
	}
	newBody := applySynthesisFrontmatter(body, headSHA, time.Now().UTC(), len(ordered), existing)
	newBody = applyTagsFrontmatter(newBody, deriveTagsFromBrief(existing))
	newBody = dropGhostFrontmatter(newBody)

	msg := fmt.Sprintf("archivist: entity article %s/%s (%d facts)", kind, slug, len(ordered))
	if _, _, err := worker.Enqueue(ctx, ArchivistAuthor, relPath, newBody, "replace", msg); err != nil {
		return fmt.Errorf("entity article: commit %s: %w", relPath, err)
	}
	return nil
}

// regenerateTaskEntityArticles is the deterministic B2 trigger: after the
// B1 completion hook records facts for a done task's entities, regenerate
// each touched entity's article. Runs from the queued distillation
// goroutine — never under b.mu. Failures are logged, never fatal.
func regenerateTaskEntityArticles(ctx context.Context, worker *WikiWorker, factLog *FactLog, graph *EntityGraph, task teamTask) {
	if worker == nil || factLog == nil || task.System {
		return
	}
	for _, e := range taskCompletionEntities(task) {
		if err := RegenerateEntityArticle(ctx, worker, factLog, graph, e.Kind, e.Slug); err != nil {
			log.Printf("entity article: regenerate %s/%s after %s: %v", e.Kind, e.Slug, task.ID, err)
		}
	}
}
