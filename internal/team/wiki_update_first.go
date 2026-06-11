package team

// wiki_update_first.go — B2 knowledge-integrity: update-first enforcement at
// the wiki WRITE boundary for agent article writes.
//
// The v3 live run compounded duplicates into the knowledge base: two Corti
// briefs on disk ("Corti Labs — Account Brief" + "Account Brief: Corti
// Labs"), "Playbook: Renewal Outreach Playbook" next to "Renewal Outreach
// Playbook" ([17:39:51], [18:19:45], [20:15] disk truth). Every duplicate
// was a CREATE of a new file whose slug was near-identical to an existing
// article in the same directory.
//
// routeAgentCreateToSimilarSlug runs inside the wiki worker's drain loop
// (single goroutine, off the broker hot path) before a mode="create"
// standard wiki write commits. When an existing .md in the SAME directory
// has a Jaro-Winkler-similar slug (the same tier-1 gate the playbook draft
// and skill dedup use), the write is rerouted to append onto the existing
// article instead of creating a near-duplicate file.
//
// Scope guards:
//   - agent writes only: author slugs "system" and "human" are exempt —
//     system writes include team/decisions/<TASK-ID>.md where sequential
//     ids (OFFICE-295 / OFFICE-296) are similar BY DESIGN, and human writes
//     ride their own identity path with optimistic concurrency.
//   - exact-slug matches fall through untouched so Repo.Commit's existing
//     "already exists" contract for mode=create stays intact.

import (
	"os"
	"path/filepath"
	"strings"
)

// wikiCreateSlugSimilarityThreshold is the Jaro-Winkler gate for routing a
// create onto an existing similar-slug article. Same tier-1 default as the
// playbook draft + skill dedup gates.
const wikiCreateSlugSimilarityThreshold = 0.85

// updateFirstExemptAuthor reports whether the author slug is exempt from
// update-first routing (system plumbing and the human identity path).
func updateFirstExemptAuthor(slug string) bool {
	switch strings.ToLower(strings.TrimSpace(slug)) {
	case "", "system", "human":
		return true
	}
	return false
}

// slugTokens splits a slug into its dash-separated tokens.
func slugTokens(slug string) []string {
	parts := strings.Split(slug, "-")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// isNumericToken reports whether a slug token is purely digits (a date,
// week number, or sequence id).
func isNumericToken(t string) bool {
	if t == "" {
		return false
	}
	for _, r := range t {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// numericTokensDiffer reports whether the two token lists carry different
// numeric tokens — "investor-update-week-23" vs "-24" is an intentional
// series, never a duplicate to fold.
func numericTokensDiffer(a, b []string) bool {
	numsOf := func(tokens []string) map[string]int {
		out := map[string]int{}
		for _, t := range tokens {
			if isNumericToken(t) {
				out[t]++
			}
		}
		return out
	}
	na, nb := numsOf(a), numsOf(b)
	if len(na) != len(nb) {
		return true
	}
	for k, v := range na {
		if nb[k] != v {
			return true
		}
	}
	return false
}

// tokenSubset reports whether every token of the smaller slug appears in
// the larger one — the v3 duplicate shape ("renewal-outreach-playbook"
// inside "playbook-renewal-outreach-playbook", "corti-labs" inside
// "account-brief-corti-labs").
func tokenSubset(small, big []string) bool {
	if len(small) < 2 || len(small) > len(big) {
		return false
	}
	counts := map[string]int{}
	for _, t := range big {
		counts[t]++
	}
	for _, t := range small {
		if counts[t] == 0 {
			return false
		}
		counts[t]--
	}
	return true
}

// slugsSimilarForUpdateFirst is the routing predicate. Both slugs must be
// multi-token (single-token slugs like "agenta"/"agentb" are too short to
// disambiguate and score artificially high on Jaro-Winkler), and slugs
// whose numeric tokens differ are an intentional series, never folded.
// Past those guards, similarity is Jaro-Winkler at the shared tier-1 gate,
// or the token-subset shape the v3 duplicates actually had.
func slugsSimilarForUpdateFirst(a, b string) bool {
	ta, tb := slugTokens(a), slugTokens(b)
	if len(ta) < 2 || len(tb) < 2 {
		return false
	}
	if numericTokensDiffer(ta, tb) {
		return false
	}
	if JaroWinkler(a, b) >= wikiCreateSlugSimilarityThreshold {
		return true
	}
	if len(ta) <= len(tb) {
		return tokenSubset(ta, tb)
	}
	return tokenSubset(tb, ta)
}

// findSimilarArticleSlug scans dir (absolute) for an existing .md article
// whose slug is similar to slug per slugsSimilarForUpdateFirst, excluding
// the exact slug itself. Returns the best match (highest Jaro-Winkler
// among the similar candidates), or "".
func findSimilarArticleSlug(dir, slug string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	best := ""
	bestScore := -1.0
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasPrefix(name, ".") || !strings.HasSuffix(strings.ToLower(name), ".md") {
			continue
		}
		existing := strings.TrimSuffix(name, ".md")
		if existing == slug || !slugsSimilarForUpdateFirst(slug, existing) {
			continue
		}
		if score := JaroWinkler(slug, existing); score > bestScore {
			best = existing
			bestScore = score
		}
	}
	return best
}

// routeAgentCreateToSimilarSlug decides the final (path, mode) for a
// standard wiki write. For an agent-authored mode="create" whose target
// directory already holds a similar-slug article, it reroutes to
// (existing article path, "append_section") — update, never duplicate.
// All other writes pass through unchanged.
func routeAgentCreateToSimilarSlug(repoRoot, authorSlug, relPath, mode string) (string, string) {
	if mode != "create" || updateFirstExemptAuthor(authorSlug) || strings.TrimSpace(repoRoot) == "" {
		return relPath, mode
	}
	cleaned := filepath.ToSlash(filepath.Clean(strings.TrimSpace(relPath)))
	if !strings.HasSuffix(strings.ToLower(cleaned), ".md") {
		return relPath, mode
	}
	dirRel, base := filepath.Split(cleaned)
	slug := strings.TrimSuffix(base, ".md")
	if slug == "" {
		return relPath, mode
	}
	dirAbs := filepath.Join(repoRoot, filepath.FromSlash(dirRel))
	existing := findSimilarArticleSlug(dirAbs, slug)
	if existing == "" {
		return relPath, mode
	}
	return filepath.ToSlash(filepath.Join(dirRel, existing+".md")), "append_section"
}
