package team

// wiki_compile_page.go — Phase 2 of the compile engine: write ONE
// Wikipedia-shaped article for a MergedConcept, drawing facts ONLY from the
// concept's source excerpts and citing each with a ^[source-id] marker. The
// LLM call is the second (and last) narrow seam; renderCompiledArticle wraps
// the returned body in deterministic YAML frontmatter.

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// pageSystemPrompt is the verbatim Phase-2 author contract.
const pageSystemPrompt = "You are a wiki author for an internal encyclopedia. Write ONE Wikipedia-shaped article about the given concept, drawing facts ONLY from the provided source excerpts. RULES: (1) every factual sentence MUST end with a citation marker of the form ^[source-id] naming the source it came from; use the exact source ids given. (2) Do NOT invent facts that are not in the sources. (3) Genuine synthesis/inference is allowed but must be hedged and left uncited. (4) Start with a one-paragraph lead summary, then use ## sections. (5) Do NOT write an H1 title — the title is rendered separately. (6) Output GitHub-flavored markdown only, no frontmatter."

// maxPageSourceChars budgets the total source-excerpt characters fed into one
// Phase-2 prompt so a concept with many large sources cannot blow the context
// window. When over budget, the longest excerpts are truncated first.
const maxPageSourceChars = 24000

// sourceTruncationNote marks where an over-budget source excerpt was cut.
const sourceTruncationNote = "\n\n[...source excerpt truncated for length...]"

// compiledFrontmatter is the YAML block prepended to every compiled article.
// Encoded via yaml.v3 (like RenderSourceMarkdown) so titles with colons and
// other YAML-significant characters stay valid + Obsidian-readable.
type compiledFrontmatter struct {
	Title      string    `yaml:"title"`
	Kind       string    `yaml:"kind"`
	Categories []string  `yaml:"categories,omitempty"`
	Sources    []string  `yaml:"sources"`
	Compiled   bool      `yaml:"compiled"`
	UpdatedAt  time.Time `yaml:"updated_at"`
}

// buildPagePrompt renders the (system, user) pair for one MergedConcept.
// existing is the current article body (empty when creating); relatedTitles
// are other compiled page titles offered as [[wikilink]] context.
func buildPagePrompt(mc MergedConcept, existing string, relatedTitles []string) (system, user string) {
	var b strings.Builder
	b.WriteString("Concept title: ")
	b.WriteString(mc.Title)
	b.WriteString("\nConcept kind: ")
	b.WriteString(mc.Kind)
	if strings.TrimSpace(mc.Summary) != "" {
		b.WriteString("\nWorking summary: ")
		b.WriteString(mc.Summary)
	}
	b.WriteString("\n\nSource excerpts (cite these by id with ^[source-id]):\n\n")
	b.WriteString(renderSourcesForPage(mc.Sources))

	if strings.TrimSpace(existing) != "" {
		b.WriteString("\n\nHere is the current article; revise and extend it, preserving still-valid cited claims:\n\n")
		b.WriteString(EscapeForPromptBody(existing))
	}

	if titles := relatedPageTitleList(relatedTitles, mc.Title); len(titles) > 0 {
		b.WriteString("\n\nRelated pages you may reference with [[wikilink]] syntax: ")
		b.WriteString(strings.Join(titles, ", "))
	}

	b.WriteString("\n\nWrite the article body now.")
	return pageSystemPrompt, b.String()
}

// renderSourcesForPage formats each source as `### source: {id}\n{content}`,
// applying the maxPageSourceChars budget by truncating the longest excerpts
// first. The untrusted source bodies flow through EscapeForPromptBody.
func renderSourcesForPage(sources []SourceRecord) string {
	contents := budgetSourceContents(sources)
	var b strings.Builder
	for i, src := range sources {
		if i > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString("### source: ")
		b.WriteString(src.ID)
		b.WriteString("\n")
		b.WriteString(EscapeForPromptBody(contents[i]))
	}
	return b.String()
}

// budgetSourceContents returns each source's content, truncating the longest
// entries first until the total is within maxPageSourceChars. Each truncated
// entry gets a visible note so the LLM knows the excerpt is partial.
func budgetSourceContents(sources []SourceRecord) []string {
	contents := make([]string, len(sources))
	total := 0
	for i, s := range sources {
		contents[i] = s.Content
		total += len(s.Content)
	}
	// Bounded loop: each iteration strictly reduces the longest entry that is
	// larger than the note, so total decreases. The guard cap is belt-and-
	// braces against a pathological all-tiny-sources input.
	for guard := 0; total > maxPageSourceChars && guard < len(contents)*2+4; guard++ {
		longest := -1
		for i := range contents {
			if len(contents[i]) > len(sourceTruncationNote) &&
				(longest == -1 || len(contents[i]) > len(contents[longest])) {
				longest = i
			}
		}
		if longest == -1 {
			break // nothing left big enough to usefully truncate
		}
		over := total - maxPageSourceChars
		cur := contents[longest]
		newBody := len(cur) - over - len(sourceTruncationNote)
		if newBody < 0 {
			newBody = 0
		}
		truncated := cur[:newBody] + sourceTruncationNote
		total = total - len(cur) + len(truncated)
		contents[longest] = truncated
	}
	return contents
}

// relatedPageTitleList drops the concept's own title and any blanks from the
// related-titles list, preserving order.
func relatedPageTitleList(titles []string, self string) []string {
	out := make([]string, 0, len(titles))
	for _, t := range titles {
		t = strings.TrimSpace(t)
		if t == "" || t == self {
			continue
		}
		out = append(out, t)
	}
	return out
}

// renderCompiledArticle prepends the deterministic YAML frontmatter to the
// LLM-authored body. now is the compile timestamp (injected so the caller
// controls the clock and tests stay deterministic).
func renderCompiledArticle(mc MergedConcept, body string, now time.Time) string {
	fm := compiledFrontmatter{
		Title:      mc.Title,
		Kind:       mc.Kind,
		Categories: mc.Tags,
		Sources:    sourceIDs(mc.Sources),
		Compiled:   true,
		UpdatedAt:  now.UTC(),
	}
	var buf bytes.Buffer
	buf.WriteString("---\n")
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(fm); err != nil {
		// The struct is plain scalars + string slices; encoding cannot fail
		// in practice. Fall back to a body-only article rather than dropping
		// the page entirely.
		_ = enc.Close()
		return strings.TrimRight(body, "\n") + "\n"
	}
	_ = enc.Close()
	buf.WriteString("---\n\n")
	// Prepend the H1 title here (the LLM body deliberately omits it). The wiki
	// catalog's extractTitle reads the first "# " heading and otherwise falls
	// back to the filename slug, so a rendered H1 makes list/catalog views show
	// the real title ("Reciprocal Rank Fusion") rather than the kebab slug. The
	// reader UI (S6) strips this leading H1 to avoid a duplicate heading.
	if t := strings.TrimSpace(mc.Title); t != "" {
		buf.WriteString("# ")
		buf.WriteString(t)
		buf.WriteString("\n\n")
	}
	buf.WriteString(strings.TrimRight(body, "\n"))
	buf.WriteString("\n")
	return buf.String()
}

// sourceIDs lifts the ids from a source record slice in order.
func sourceIDs(sources []SourceRecord) []string {
	out := make([]string, len(sources))
	for i, s := range sources {
		out[i] = s.ID
	}
	return out
}

// compilePage runs the Phase-2 LLM call for one MergedConcept and returns the
// full article text (frontmatter + body). now is the compile timestamp.
func compilePage(ctx context.Context, runner PamRunner, mc MergedConcept, existing string, relatedTitles []string, now time.Time) (string, error) {
	system, user := buildPagePrompt(mc, existing, relatedTitles)
	body, err := runner.Run(ctx, system, user)
	if err != nil {
		return "", fmt.Errorf("compile page %s: %w", mc.Slug, err)
	}
	body = strings.TrimSpace(body)
	if body == "" {
		return "", fmt.Errorf("compile page %s: runner returned empty body", mc.Slug)
	}
	return renderCompiledArticle(mc, body, now), nil
}
