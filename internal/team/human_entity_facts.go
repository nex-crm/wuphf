package team

// human_entity_facts.go enrolls the office's HUMANS into the same entity
// context-graph + wiki pipeline the agents already flow through, so every
// human teammate gets a per-person wiki article at team/people/<slug>.md the
// CEO can read before deciding who to loop into a task.
//
// Why this exists
// ===============
//
// slack_entity_facts.go already accretes people/<slug> articles for humans it
// can SEE on a bridged Slack channel. But a human who joins the office through
// a share invite (broker_human_share.go) — the host and any teammate admitted
// via /humans/invite — never appears on Slack, so without this file they would
// have no entity and no article. This enrolls those humans from the broker's
// own session roster, on the SAME substrate:
//
//   - facts land via FactLog.Append (append-only, content-hash dedup makes a
//     repeat enrollment idempotent), and
//   - the article regenerates via RegenerateEntityArticle, the identical
//     deterministic generator the agent + Slack paths use.
//
// No parallel human-only pipeline: humans share the people kind with agents
// exactly as slack_entity_facts.go documents. What an entity IS (human
// teammate vs office agent) is stated in its facts, not split across kinds.

import (
	"context"
	"fmt"
	"log"
	"strings"
)

// humanEntityRecordedBy attributes facts this enrollment records. Matches the
// slack pass's "system" attribution so footnotes read consistently across both
// human-enrollment paths.
const humanEntityRecordedBy = "system"

// enrollHumanEntity records identity facts for one human and regenerates that
// human's people/<slug> wiki article. Reuses the agent entity pipeline
// end-to-end; it only appends facts and triggers the shared regenerator.
//
// slug is the stable people-kind slug (the human session slug). displayName is
// the human-readable name shown on the article. source is a short phrase naming
// how the office knows this human (e.g. "joined the office via a share invite")
// so the article states what the entity IS.
//
// Fails closed when the wiki backend is not active (any sink nil) — there is
// nowhere to write the article, so there is no point recording facts that no
// regeneration would ever render. Errors are logged, never fatal: enrollment
// is best-effort knowledge accretion, never on a request's critical path.
func (b *Broker) enrollHumanEntity(ctx context.Context, slug, displayName, source string) {
	if b == nil {
		return
	}
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return
	}
	displayName = strings.TrimSpace(displayName)
	if displayName == "" {
		displayName = slug
	}

	factLog, graph, worker := b.entityFactSinks()
	if factLog == nil || graph == nil || worker == nil {
		// Wiki backend off (self-hosted minimal install) — no entity wiki to
		// write. Slack-bridged humans are handled separately; here there is
		// simply no sink, so skip cleanly.
		return
	}

	facts := []string{
		fmt.Sprintf("Human teammate in the office — display name %q.", displayName),
	}
	if s := strings.TrimSpace(source); s != "" {
		facts = append(facts, fmt.Sprintf("Known to the office because they %s.", s))
	}
	for _, text := range facts {
		if _, err := factLog.Append(ctx, EntityKindPeople, slug, text, "", humanEntityRecordedBy); err != nil {
			log.Printf("[human-entity] fact append failed for %s: %v", slug, err)
			return
		}
	}

	if err := RegenerateEntityArticle(ctx, worker, factLog, graph, EntityKindPeople, slug); err != nil {
		log.Printf("[human-entity] article regen failed for %s: %v", slug, err)
	}
}

// humanPromptEntry is one human surfaced to the CEO's planning context: the
// slug it would assign, the display name, and a short context line drawn from
// the human's wiki article (its lead paragraph) when one exists.
type humanPromptEntry struct {
	Slug    string
	Name    string
	Context string
}

// HumansForPrompt returns the distinct humans the office knows about, each with
// a short context line for the CEO's HUMANS planning block. The set is the
// active (non-revoked) human sessions; the context line is the lead paragraph
// of the human's people/<slug> wiki article when the wiki backend has one,
// otherwise empty. Deterministic order (by slug) so the prompt prefix stays
// byte-stable for prompt caching.
func (b *Broker) HumansForPrompt() []humanPromptEntry {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	type human struct{ name string }
	known := map[string]human{}
	for _, s := range b.humanSessions {
		if s.RevokedAt != "" {
			continue
		}
		slug := strings.TrimSpace(s.HumanSlug)
		if slug == "" {
			continue
		}
		name := strings.TrimSpace(s.DisplayName)
		if name == "" {
			name = slug
		}
		// First session wins the display name; later sessions for the same
		// human do not clobber it.
		if _, ok := known[slug]; !ok {
			known[slug] = human{name: name}
		}
	}
	worker := b.wikiWorker
	b.mu.Unlock()

	out := make([]humanPromptEntry, 0, len(known))
	for slug, h := range known {
		out = append(out, humanPromptEntry{
			Slug:    slug,
			Name:    h.name,
			Context: humanArticleContextLine(worker, slug),
		})
	}
	sortHumanPromptEntries(out)
	return out
}

// humanArticleContextLine reads the human's people/<slug> wiki article and
// returns its lead paragraph (the first bold-subject sentence the entity
// article generator writes) as a one-line context summary. Returns "" when the
// wiki backend is off or the article does not exist yet — the prompt block
// then falls back to the bare name.
func humanArticleContextLine(worker *WikiWorker, slug string) string {
	if worker == nil {
		return ""
	}
	relPath := briefPath(EntityKindPeople, slug)
	raw, err := readArticle(worker.Repo(), relPath)
	if err != nil || len(raw) == 0 {
		return ""
	}
	return leadParagraph(stripFrontmatter(string(raw)))
}

// leadParagraph extracts the first non-empty prose paragraph from an entity
// article body: it skips the managed-content HTML comment header and the H1
// title line, then returns the first prose line (the bold-subject lead the
// generator writes). Returns "" when no prose line is found.
func leadParagraph(body string) string {
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "<!--") {
			continue
		}
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		// Collapse internal whitespace so a wrapped paragraph renders on one
		// prompt line.
		return strings.Join(strings.Fields(trimmed), " ")
	}
	return ""
}

// sortHumanPromptEntries orders entries by slug for prompt-cache byte
// stability. Kept as a named helper so the ordering rationale lives next to
// the data it sorts.
func sortHumanPromptEntries(entries []humanPromptEntry) {
	for i := 1; i < len(entries); i++ {
		for j := i; j > 0 && entries[j-1].Slug > entries[j].Slug; j-- {
			entries[j-1], entries[j] = entries[j], entries[j-1]
		}
	}
}
