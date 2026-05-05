package team

// wiki_maintenance.go computes AI-assisted maintenance suggestions for a wiki
// article. Suggestions are *proposals only* — they never auto-write. The user
// must accept a suggestion explicitly through the WikiEditor save path before
// any change lands on disk.
//
// Suggestion types (mirrors phase-03-wiki-ux.md PR 7 scope):
//
//   summarize          — propose a TL;DR / lead paragraph
//   add_citation       — propose [needs citation] markers on un-sourced claims
//   extract_facts      — propose structured facts (subject/predicate/object)
//                        for review *before* commit to fact log
//   resolve_contradiction — link to existing WikiLint contradiction surface
//   split_long_page    — propose a split when the page is large
//   link_related       — propose a "Related" section based on co-occurring
//                        entities, backlinks, and graph edges
//   refresh_stale      — propose a "Recent activity" pointer for stale pages
//
// All actions are pure functions of (article content, on-disk catalog, fact
// index, lint report). No LLM call is required for v1 — every suggestion is
// derived from existing structured signals already in the broker.

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// MaintenanceAction is the discriminator for the 7 supported actions.
type MaintenanceAction string

const (
	MaintActionSummarize            MaintenanceAction = "summarize"
	MaintActionAddCitation          MaintenanceAction = "add_citation"
	MaintActionExtractFacts         MaintenanceAction = "extract_facts"
	MaintActionResolveContradiction MaintenanceAction = "resolve_contradiction"
	MaintActionSplitLong            MaintenanceAction = "split_long_page"
	MaintActionLinkRelated          MaintenanceAction = "link_related"
	MaintActionRefreshStale         MaintenanceAction = "refresh_stale"
)

// AllMaintenanceActions enumerates the supported actions, in display order.
var AllMaintenanceActions = []MaintenanceAction{
	MaintActionSummarize,
	MaintActionAddCitation,
	MaintActionExtractFacts,
	MaintActionLinkRelated,
	MaintActionSplitLong,
	MaintActionRefreshStale,
	MaintActionResolveContradiction,
}

// MaintenanceEvidence is one piece of source material the suggestion was
// derived from. The UI links each item back to its origin.
type MaintenanceEvidence struct {
	// Kind is "wiki_article" | "fact" | "lint_finding" | "edit_log".
	Kind string `json:"kind"`
	// Label is the short human-readable name (article title, predicate,
	// finding type).
	Label string `json:"label"`
	// Path is the wiki path or fact id the evidence points at. Empty when
	// the evidence is purely textual.
	Path string `json:"path,omitempty"`
	// Snippet is a short excerpt the UI can render verbatim.
	Snippet string `json:"snippet,omitempty"`
}

// MaintenanceFactProposal is one proposed structured fact for the
// extract_facts action. The user reviews each one in the side panel; only
// accepted facts go to the fact log on commit.
type MaintenanceFactProposal struct {
	Subject    string  `json:"subject"`
	Predicate  string  `json:"predicate"`
	Object     string  `json:"object"`
	Confidence float64 `json:"confidence"`
	// SourceLine is the article-relative line index the fact was extracted
	// from (1-based for display). Lets the UI highlight context.
	SourceLine int `json:"source_line,omitempty"`
}

// MaintenanceDiff is the proposed change to the article body. v1 carries the
// whole proposed content plus added / removed line counts so the UI can
// render a small unified-diff-style preview.
type MaintenanceDiff struct {
	// ProposedContent is the full new article body. Empty for actions that
	// do not modify the article body (extract_facts, resolve_contradiction).
	ProposedContent string `json:"proposed_content,omitempty"`
	// Added is the list of newly-introduced lines (in order).
	Added []string `json:"added,omitempty"`
	// Removed is the list of removed lines (in order).
	Removed []string `json:"removed,omitempty"`
}

// MaintenanceSuggestion is the single-action response from the assistant.
type MaintenanceSuggestion struct {
	Action      MaintenanceAction `json:"action"`
	Title       string            `json:"title"`
	Description string            `json:"description"`
	// Diff is populated for body-mutating actions.
	Diff *MaintenanceDiff `json:"diff,omitempty"`
	// Facts is populated for extract_facts.
	Facts []MaintenanceFactProposal `json:"facts,omitempty"`
	// Evidence is the list of source pointers the suggestion drew from.
	Evidence []MaintenanceEvidence `json:"evidence,omitempty"`
	// LintFinding is populated for resolve_contradiction. The UI uses this
	// to redirect into the existing ResolveContradictionModal flow.
	LintFinding *LintFinding `json:"lint_finding,omitempty"`
	// LintReportDate, LintFindingIdx pair the finding with its report.
	LintReportDate string `json:"lint_report_date,omitempty"`
	LintFindingIdx int    `json:"lint_finding_idx,omitempty"`
	// ExpectedSHA is the article SHA at the time the suggestion was
	// computed. Sent back when the user accepts so the WikiEditor save
	// path can detect stale suggestions exactly like a stale editor open.
	ExpectedSHA string `json:"expected_sha,omitempty"`
	// Skipped is true when no suggestion was warranted (e.g. page is short
	// enough that split_long is not useful, or no contradictions exist).
	// The UI shows a "nothing to do" state rather than an empty diff.
	Skipped       bool   `json:"skipped,omitempty"`
	SkippedReason string `json:"skipped_reason,omitempty"`
}

// MaintenanceAssistant computes suggestions for one article + action pair.
// All inputs are existing broker subsystems; no new state is introduced.
type MaintenanceAssistant struct {
	worker *WikiWorker
	index  *WikiIndex
	lint   *Lint
	now    func() time.Time
}

// NewMaintenanceAssistant wires the assistant to its dependencies. worker is
// required (provides on-disk article reads + repo head SHA). index and lint
// are optional — when nil, actions that need them return Skipped responses.
func NewMaintenanceAssistant(worker *WikiWorker, index *WikiIndex, lint *Lint) *MaintenanceAssistant {
	return &MaintenanceAssistant{
		worker: worker,
		index:  index,
		lint:   lint,
		now:    time.Now,
	}
}

// ErrMaintenanceNoWorker is returned when the assistant is constructed without
// a wiki worker (markdown backend disabled).
var ErrMaintenanceNoWorker = errors.New("wiki maintenance: no worker")

// Suggest dispatches to the action-specific computer.
func (m *MaintenanceAssistant) Suggest(ctx context.Context, action MaintenanceAction, articlePath string) (MaintenanceSuggestion, error) {
	if m.worker == nil {
		return MaintenanceSuggestion{}, ErrMaintenanceNoWorker
	}

	body, err := m.worker.ReadArticle(articlePath)
	if err != nil {
		return MaintenanceSuggestion{}, fmt.Errorf("read article: %w", err)
	}

	sha, _ := m.worker.repo.HeadSHA(ctx)

	switch action {
	case MaintActionSummarize:
		return m.suggestSummarize(articlePath, string(body), sha), nil
	case MaintActionAddCitation:
		return m.suggestAddCitation(articlePath, string(body), sha), nil
	case MaintActionExtractFacts:
		return m.suggestExtractFacts(articlePath, string(body), sha), nil
	case MaintActionLinkRelated:
		return m.suggestLinkRelated(ctx, articlePath, string(body), sha), nil
	case MaintActionSplitLong:
		return m.suggestSplitLong(articlePath, string(body), sha), nil
	case MaintActionRefreshStale:
		return m.suggestRefreshStale(ctx, articlePath, string(body), sha), nil
	case MaintActionResolveContradiction:
		return m.suggestResolveContradiction(ctx, articlePath, sha), nil
	default:
		return MaintenanceSuggestion{}, fmt.Errorf("unknown action: %q", action)
	}
}

// ── summarize ────────────────────────────────────────────────────────────────

const summarizeMinWords = 80

// suggestSummarize proposes a TL;DR block at the top of the article, just
// after the H1, when the body is long enough to benefit. The summary itself
// is the first non-empty paragraph trimmed to 240 chars. The proposal hands
// the user a starting point; the editor tab is where they refine it.
func (m *MaintenanceAssistant) suggestSummarize(path, body, sha string) MaintenanceSuggestion {
	wc := countWords([]byte(body))
	if wc < summarizeMinWords {
		return MaintenanceSuggestion{
			Action:        MaintActionSummarize,
			Title:         "Summarize page",
			Skipped:       true,
			SkippedReason: fmt.Sprintf("Article is only %d words — a summary would not help.", wc),
			ExpectedSHA:   sha,
		}
	}
	lead := firstParagraph(body)
	if lead == "" {
		return MaintenanceSuggestion{
			Action:        MaintActionSummarize,
			Title:         "Summarize page",
			Skipped:       true,
			SkippedReason: "No paragraph found to summarize.",
			ExpectedSHA:   sha,
		}
	}
	tldr := truncateChars(strings.ReplaceAll(lead, "\n", " "), 240)
	block := fmt.Sprintf("> **TL;DR:** %s\n\n", tldr)

	proposed, added, removed := insertAfterTitle(body, block)
	return MaintenanceSuggestion{
		Action:      MaintActionSummarize,
		Title:       "Summarize page",
		Description: "Insert a one-line TL;DR derived from the article's lead paragraph.",
		Diff: &MaintenanceDiff{
			ProposedContent: proposed,
			Added:           added,
			Removed:         removed,
		},
		Evidence: []MaintenanceEvidence{
			{Kind: "wiki_article", Label: "Article body lead", Path: path, Snippet: tldr},
		},
		ExpectedSHA: sha,
	}
}

// ── add citation ─────────────────────────────────────────────────────────────

// citationClaimRe matches sentence-ish lines that look like a strong claim
// (contain a number, percentage, year, or one of a small list of strong
// verbs) but have no explicit source link or footnote on the same line.
var citationStrongVerbs = []string{
	"announced", "launched", "raised", "shipped", "acquired", "merged",
	"reported", "achieved", "doubled", "tripled", "increased", "decreased",
}

// suggestAddCitation flags lines that look like load-bearing claims without a
// source. v1 is conservative — only proposes appending `[needs citation]` to
// numeric/strong-verb sentences that lack a link or a footnote-style anchor.
func (m *MaintenanceAssistant) suggestAddCitation(path, body, sha string) MaintenanceSuggestion {
	lines := strings.Split(body, "\n")
	var changed []string
	added := make([]string, 0)
	removed := make([]string, 0)

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, ">") {
			changed = append(changed, line)
			continue
		}
		if !claimNeedsCitation(trimmed) {
			changed = append(changed, line)
			continue
		}
		if strings.Contains(line, "[needs citation]") {
			changed = append(changed, line)
			continue
		}
		new := strings.TrimRight(line, " \t") + " [needs citation]"
		changed = append(changed, new)
		removed = append(removed, line)
		added = append(added, new)
	}

	if len(added) == 0 {
		return MaintenanceSuggestion{
			Action:        MaintActionAddCitation,
			Title:         "Add missing citation",
			Skipped:       true,
			SkippedReason: "No un-sourced numeric or strong-claim sentences found.",
			ExpectedSHA:   sha,
		}
	}
	return MaintenanceSuggestion{
		Action:      MaintActionAddCitation,
		Title:       "Add missing citation",
		Description: fmt.Sprintf("Mark %d claim(s) as needing a citation. The mark is reversible — replace it with the actual source link before saving.", len(added)),
		Diff: &MaintenanceDiff{
			ProposedContent: strings.Join(changed, "\n"),
			Added:           added,
			Removed:         removed,
		},
		Evidence: []MaintenanceEvidence{
			{Kind: "wiki_article", Label: "Article body — un-sourced claims", Path: path},
		},
		ExpectedSHA: sha,
	}
}

func claimNeedsCitation(line string) bool {
	if strings.Contains(line, "http://") || strings.Contains(line, "https://") {
		return false
	}
	if strings.Contains(line, "[") && strings.Contains(line, "](") {
		return false // already has a markdown link
	}
	if strings.Contains(line, "[needs citation]") {
		return false
	}
	if hasNumericClaim(line) {
		return true
	}
	lower := strings.ToLower(line)
	for _, v := range citationStrongVerbs {
		if strings.Contains(lower, " "+v+" ") || strings.HasPrefix(lower, v+" ") {
			return true
		}
	}
	return false
}

func hasNumericClaim(line string) bool {
	digits := 0
	for _, r := range line {
		if r >= '0' && r <= '9' {
			digits++
		}
	}
	return digits >= 2
}

// ── extract facts ────────────────────────────────────────────────────────────

// suggestExtractFacts proposes structured fact triples from the article. v1
// scans for "X is the Y of Z" / "X works at Y" / "X joined Y on DATE" shapes
// using a tiny pattern set — every proposal is conservative and confidence
// is capped so the user reviews before any commit.
func (m *MaintenanceAssistant) suggestExtractFacts(path, body, _ string) MaintenanceSuggestion {
	subject := slugFromPath(path)
	if subject == "" {
		// Without a subject anchor, refuse to propose facts — extraction
		// without an entity context tends to be noise.
		return MaintenanceSuggestion{
			Action:        MaintActionExtractFacts,
			Title:         "Extract facts",
			Skipped:       true,
			SkippedReason: "Article path does not map to an entity (people/companies/customers).",
		}
	}
	proposals := extractTriples(subject, body)
	if len(proposals) == 0 {
		return MaintenanceSuggestion{
			Action:        MaintActionExtractFacts,
			Title:         "Extract facts",
			Skipped:       true,
			SkippedReason: "No clear triples found in the article body.",
		}
	}

	return MaintenanceSuggestion{
		Action:      MaintActionExtractFacts,
		Title:       "Extract facts",
		Description: fmt.Sprintf("Propose %d structured fact(s) for review. Nothing is committed to the fact log until you accept individual proposals.", len(proposals)),
		Facts:       proposals,
		Evidence: []MaintenanceEvidence{
			{Kind: "wiki_article", Label: "Article body — pattern-extracted triples", Path: path},
		},
	}
}

// ── link related ─────────────────────────────────────────────────────────────

// suggestLinkRelated proposes appending a "Related" section listing entities
// that co-occur with this one in the fact log or share graph edges. The
// section is only proposed when at least one related entity exists *and* the
// article does not already have a "Related" heading.
func (m *MaintenanceAssistant) suggestLinkRelated(ctx context.Context, path, body, sha string) MaintenanceSuggestion {
	related := m.relatedEntities(ctx, path)
	if len(related) == 0 {
		return MaintenanceSuggestion{
			Action:        MaintActionLinkRelated,
			Title:         "Link related pages",
			Skipped:       true,
			SkippedReason: "No related entities found in the fact log or graph.",
			ExpectedSHA:   sha,
		}
	}
	if hasHeading(body, "Related") {
		return MaintenanceSuggestion{
			Action:        MaintActionLinkRelated,
			Title:         "Link related pages",
			Skipped:       true,
			SkippedReason: "Article already has a Related section. Edit it manually if you want to refresh.",
			ExpectedSHA:   sha,
		}
	}

	var sb strings.Builder
	sb.WriteString("\n## Related\n\n")
	for _, slug := range related {
		fmt.Fprintf(&sb, "- [[%s]]\n", slug)
	}
	block := sb.String()

	proposed := strings.TrimRight(body, "\n") + "\n" + block
	added := strings.Split(strings.TrimRight(block, "\n"), "\n")

	evidence := make([]MaintenanceEvidence, 0, len(related))
	for _, slug := range related {
		evidence = append(evidence, MaintenanceEvidence{
			Kind:  "wiki_article",
			Label: slug,
			Path:  slug,
		})
	}

	return MaintenanceSuggestion{
		Action:      MaintActionLinkRelated,
		Title:       "Link related pages",
		Description: fmt.Sprintf("Append a Related section linking %d co-occurring entit%s.", len(related), pluralY(len(related))),
		Diff: &MaintenanceDiff{
			ProposedContent: proposed,
			Added:           added,
		},
		Evidence:    evidence,
		ExpectedSHA: sha,
	}
}

// ── split long ───────────────────────────────────────────────────────────────

const splitLongMinWords = 1500

// suggestSplitLong proposes splitting a long page on its top-level (H2)
// sections. The split itself is described — the user accepts to apply the
// rewrite that turns each H2 into its own sub-page with a stub-link footer.
func (m *MaintenanceAssistant) suggestSplitLong(path, body, sha string) MaintenanceSuggestion {
	wc := countWords([]byte(body))
	if wc < splitLongMinWords {
		return MaintenanceSuggestion{
			Action:        MaintActionSplitLong,
			Title:         "Split long page",
			Skipped:       true,
			SkippedReason: fmt.Sprintf("Article is %d words — short enough to keep as one page.", wc),
			ExpectedSHA:   sha,
		}
	}
	headings := extractH2Headings(body)
	if len(headings) < 2 {
		return MaintenanceSuggestion{
			Action:        MaintActionSplitLong,
			Title:         "Split long page",
			Skipped:       true,
			SkippedReason: "Article does not have enough top-level sections to split cleanly.",
			ExpectedSHA:   sha,
		}
	}
	// v1: produce a description-only proposal. The Diff carries an
	// `Added` outline so the UI can render the proposed sub-page list.
	added := make([]string, 0, len(headings))
	for _, h := range headings {
		added = append(added, fmt.Sprintf("- New page: [[%s/%s]] (from H2 \"%s\")",
			strings.TrimSuffix(path, ".md"), slugify(h), h))
	}
	return MaintenanceSuggestion{
		Action:      MaintActionSplitLong,
		Title:       "Split long page",
		Description: fmt.Sprintf("Article is %d words across %d sections. Propose splitting each H2 into its own sub-page with a cross-link.", wc, len(headings)),
		Diff: &MaintenanceDiff{
			Added: added,
		},
		Evidence: []MaintenanceEvidence{
			{Kind: "wiki_article", Label: "Article body — H2 sections", Path: path},
		},
		ExpectedSHA: sha,
	}
}

// ── refresh stale ────────────────────────────────────────────────────────────

const refreshStaleDays = 30

// suggestRefreshStale points at recent edits + recent fact log entries so the
// user can decide whether the page needs a content refresh. v1 does not
// auto-rewrite — it surfaces a "review activity since X" suggestion with
// links to the supporting evidence.
func (m *MaintenanceAssistant) suggestRefreshStale(ctx context.Context, path, body, sha string) MaintenanceSuggestion {
	lastEdited := lastEditedTimeFromBody(body)
	cutoff := m.now().AddDate(0, 0, -refreshStaleDays)
	if !lastEdited.IsZero() && lastEdited.After(cutoff) {
		return MaintenanceSuggestion{
			Action:        MaintActionRefreshStale,
			Title:         "Refresh stale page",
			Skipped:       true,
			SkippedReason: fmt.Sprintf("Article was edited within the last %d days.", refreshStaleDays),
			ExpectedSHA:   sha,
		}
	}

	subject := slugFromPath(path)
	evidence := []MaintenanceEvidence{}
	if m.index != nil && subject != "" {
		facts, _ := m.index.ListFactsForEntity(ctx, subject)
		for _, f := range facts {
			anchor := f.CreatedAt
			if !f.ValidFrom.IsZero() {
				anchor = f.ValidFrom
			}
			if anchor.After(cutoff) {
				evidence = append(evidence, MaintenanceEvidence{
					Kind:    "fact",
					Label:   f.Triplet.predicateOrText(f),
					Path:    f.ID,
					Snippet: shortText(f.Text, 120),
				})
			}
		}
	}

	if len(evidence) == 0 {
		return MaintenanceSuggestion{
			Action:        MaintActionRefreshStale,
			Title:         "Refresh stale page",
			Skipped:       true,
			SkippedReason: "Page is stale but no recent fact-log activity to draw a refresh from.",
			ExpectedSHA:   sha,
		}
	}

	return MaintenanceSuggestion{
		Action:      MaintActionRefreshStale,
		Title:       "Refresh stale page",
		Description: fmt.Sprintf("Page has not been edited in %d+ days but %d new fact(s) have landed since. Review and merge into the body.", refreshStaleDays, len(evidence)),
		Evidence:    evidence,
		ExpectedSHA: sha,
	}
}

// ── resolve contradiction ────────────────────────────────────────────────────

// suggestResolveContradiction redirects through the existing lint surface.
// We do not duplicate the resolve flow — the panel hands the user back to
// ResolveContradictionModal with the right finding pre-selected.
func (m *MaintenanceAssistant) suggestResolveContradiction(ctx context.Context, path, sha string) MaintenanceSuggestion {
	if m.lint == nil {
		return MaintenanceSuggestion{
			Action:        MaintActionResolveContradiction,
			Title:         "Resolve contradiction",
			Skipped:       true,
			SkippedReason: "Lint runner not available.",
			ExpectedSHA:   sha,
		}
	}
	report, err := m.lint.Run(ctx)
	if err != nil {
		return MaintenanceSuggestion{
			Action:        MaintActionResolveContradiction,
			Title:         "Resolve contradiction",
			Skipped:       true,
			SkippedReason: fmt.Sprintf("Lint run failed: %v", err),
			ExpectedSHA:   sha,
		}
	}
	subject := slugFromPath(path)
	for i, f := range report.Findings {
		if f.Type != "contradictions" {
			continue
		}
		if subject != "" && f.EntitySlug != "" && f.EntitySlug != subject {
			continue
		}
		copyF := f
		return MaintenanceSuggestion{
			Action:         MaintActionResolveContradiction,
			Title:          "Resolve contradiction",
			Description:    "Open the contradiction in the existing resolve flow.",
			LintFinding:    &copyF,
			LintReportDate: report.Date,
			LintFindingIdx: i,
			Evidence: []MaintenanceEvidence{
				{Kind: "lint_finding", Label: f.Summary},
			},
			ExpectedSHA: sha,
		}
	}
	return MaintenanceSuggestion{
		Action:        MaintActionResolveContradiction,
		Title:         "Resolve contradiction",
		Skipped:       true,
		SkippedReason: "No contradictions involving this article in the latest lint report.",
		ExpectedSHA:   sha,
	}
}

// ── helpers ──────────────────────────────────────────────────────────────────

// relatedEntities returns up to N other entity slugs that co-occur with the
// article's subject in fact triples or share graph edges. Sorted by
// co-occurrence count, descending.
func (m *MaintenanceAssistant) relatedEntities(ctx context.Context, path string) []string {
	if m.index == nil {
		return nil
	}
	subject := slugFromPath(path)
	if subject == "" {
		return nil
	}
	counts := make(map[string]int)
	if facts, err := m.index.ListFactsForEntity(ctx, subject); err == nil {
		for _, f := range facts {
			if f.Triplet == nil {
				continue
			}
			obj := f.Triplet.Object
			if obj == "" || obj == subject {
				continue
			}
			counts[obj]++
		}
	}
	if edges, err := m.index.ListEdgesForEntity(ctx, subject); err == nil {
		for _, e := range edges {
			other := e.Object
			if other == subject {
				other = e.Subject
			}
			if other == "" || other == subject {
				continue
			}
			counts[other]++
		}
	}
	if len(counts) == 0 {
		return nil
	}
	type kv struct {
		k string
		v int
	}
	pairs := make([]kv, 0, len(counts))
	for k, v := range counts {
		pairs = append(pairs, kv{k, v})
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].v != pairs[j].v {
			return pairs[i].v > pairs[j].v
		}
		return pairs[i].k < pairs[j].k
	})
	const max = 8
	out := make([]string, 0, max)
	for i, p := range pairs {
		if i >= max {
			break
		}
		out = append(out, p.k)
	}
	return out
}

// firstParagraph returns the first non-empty, non-heading paragraph from a
// markdown body. Used as a starting point for summarize.
func firstParagraph(body string) string {
	var sb strings.Builder
	for _, ln := range strings.Split(stripFrontmatter(body), "\n") {
		trimmed := strings.TrimSpace(ln)
		if sb.Len() == 0 {
			if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, ">") {
				continue
			}
			sb.WriteString(trimmed)
			continue
		}
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			break
		}
		sb.WriteString(" ")
		sb.WriteString(trimmed)
	}
	return sb.String()
}

// insertAfterTitle places `block` immediately after the article's H1 title
// (or after the frontmatter if there is no H1). Returns the new content and
// the lists of added/removed lines for diff display.
func insertAfterTitle(body, block string) (string, []string, []string) {
	lines := strings.Split(body, "\n")
	insertAt := 0
	inFrontmatter := false
	foundH1 := false
	for i, ln := range lines {
		if i == 0 && strings.TrimSpace(ln) == "---" {
			inFrontmatter = true
			continue
		}
		if inFrontmatter {
			if strings.TrimSpace(ln) == "---" {
				inFrontmatter = false
				insertAt = i + 1
			}
			continue
		}
		if strings.HasPrefix(ln, "# ") {
			insertAt = i + 1
			foundH1 = true
			break
		}
	}
	if !foundH1 && insertAt == 0 {
		// No H1 and no frontmatter — drop the block at the very top.
		insertAt = 0
	}
	out := make([]string, 0, len(lines)+1)
	out = append(out, lines[:insertAt]...)
	blockLines := strings.Split(strings.TrimRight(block, "\n"), "\n")
	out = append(out, blockLines...)
	out = append(out, lines[insertAt:]...)
	return strings.Join(out, "\n"), blockLines, nil
}

func truncateChars(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return strings.TrimRight(s[:n], " ,.;:") + "…"
}

func hasHeading(body, name string) bool {
	target := strings.ToLower(name)
	for _, ln := range strings.Split(body, "\n") {
		t := strings.TrimSpace(ln)
		if strings.HasPrefix(t, "## ") && strings.ToLower(strings.TrimSpace(strings.TrimPrefix(t, "## "))) == target {
			return true
		}
	}
	return false
}

func extractH2Headings(body string) []string {
	out := []string{}
	for _, ln := range strings.Split(body, "\n") {
		t := strings.TrimSpace(ln)
		if strings.HasPrefix(t, "## ") {
			out = append(out, strings.TrimSpace(strings.TrimPrefix(t, "## ")))
		}
	}
	return out
}

// slugFromPath maps a wiki article path to its entity slug for paths under
// people/, companies/, customers/. Returns "" for non-entity paths.
func slugFromPath(path string) string {
	p := strings.TrimPrefix(path, "team/")
	p = strings.TrimSuffix(p, ".md")
	parts := strings.Split(p, "/")
	if len(parts) < 2 {
		return ""
	}
	switch parts[0] {
	case "people", "companies", "customers":
		return parts[len(parts)-1]
	}
	return ""
}

func pluralY(n int) string {
	if n == 1 {
		return "y"
	}
	return "ies"
}

// extractTriples runs a small set of pattern matchers over the body and
// proposes triples anchored on the article subject. v1 is conservative —
// every proposal has confidence < 0.7 so the user is forced to review.
var triplePatterns = []struct {
	pred string
	keys []string
}{
	{"role_at", []string{"works at", "head of", "ceo of", "vp of", "founder of", "founded"}},
	{"based_in", []string{"based in", "lives in", "located in"}},
	{"part_of", []string{"member of", "joined"}},
}

func extractTriples(subject, body string) []MaintenanceFactProposal {
	out := []MaintenanceFactProposal{}
	lines := strings.Split(body, "\n")
	for i, raw := range lines {
		ln := strings.TrimSpace(raw)
		if ln == "" || strings.HasPrefix(ln, "#") || strings.HasPrefix(ln, ">") {
			continue
		}
		lower := strings.ToLower(ln)
		for _, p := range triplePatterns {
			for _, key := range p.keys {
				idx := strings.Index(lower, key)
				if idx < 0 {
					continue
				}
				rest := strings.TrimSpace(ln[idx+len(key):])
				rest = strings.TrimRight(rest, ".,;: ")
				if rest == "" {
					continue
				}
				out = append(out, MaintenanceFactProposal{
					Subject:    subject,
					Predicate:  p.pred,
					Object:     rest,
					Confidence: 0.6,
					SourceLine: i + 1,
				})
				break
			}
		}
	}
	return out
}

// lastEditedTimeFromBody scans the article for a `last_edited_ts:` frontmatter
// field. Returns zero value if not found.
func lastEditedTimeFromBody(body string) time.Time {
	for _, ln := range strings.Split(body, "\n") {
		t := strings.TrimSpace(ln)
		const prefix = "last_edited_ts:"
		if !strings.HasPrefix(t, prefix) {
			continue
		}
		val := strings.TrimSpace(strings.TrimPrefix(t, prefix))
		val = strings.Trim(val, "\"'")
		if ts, err := time.Parse(time.RFC3339, val); err == nil {
			return ts
		}
	}
	return time.Time{}
}

// predicateOrText returns the triplet predicate, or the raw fact text when
// the triplet is missing. Defined on *Triplet so a nil receiver works.
func (t *Triplet) predicateOrText(f TypedFact) string {
	if t != nil && t.Predicate != "" {
		return t.Predicate
	}
	return shortText(f.Text, 60)
}
