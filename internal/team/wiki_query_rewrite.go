package team

// wiki_query_rewrite.go — query rewriter for Slice 2 Thread A.
//
// Pulls structured spans out of natural-language /lookup queries so the
// typed-predicate graph walk has a company + project + person to resolve.
// Pure text parsing — no LLM, no dictionary — so it runs at BM25 speed
// and never falsifies the BM25-only fallback path.
//
// Three extractors here:
//
//   parseMultiHopSpans(q) → (companyDisplay, projectDisplay, ok)
//     "who at <X> championed [the] <Y> [project]?"
//     Handles plain words, [[wikilinks]], and [[slug|Display]].
//
//   parseCounterfactualSubject(q) → (personDisplay, ok)
//     "what would have happened if <P> had (not) ..."
//     "if <P> hadn't ..." / "without <P> ..."
//
//   parseRelationshipSingle(q) → (predicate, projectDisplay, ok)
//     "who champions <Y>?"      → predicate="champions"
//     "who leads <Y>?"          → predicate="leads"
//     "who is involved in <Y>?" → predicate="involved_in"
//     Single-predicate shape. Added in Slice 2 Thread A v2 to close the
//     five bench queries (q_028/030/033/034/035) that failed with BM25
//     alone because surface rankings scattered across 20 topK slots.
//
// The returned display strings are trimmed + punctuation-free but keep the
// original casing so callers can hand them to the slug resolver without
// a second normalisation pass.

import (
	"regexp"
	"strings"
)

// Multi-hop: "who at <COMPANY> championed [the] <PROJECT> [project]?"
//
// Capture groups:
//
//	1 — company display span (word chars, spaces, [[wikilink]] body)
//	2 — project display span (same alphabet)
//
// Both groups stop at punctuation or the trailing "project" literal.
var multiHopRE = regexp.MustCompile(`(?i)who\s+at\s+([^?.!,]+?)\s+champion(?:ed|s)?\s+(?:the\s+)?([^?.!,]+?)(?:\s+project)?[?.!]*$`)

// Relationship (single-predicate) patterns. Anchored at start-of-query so
// the multi_hop "who at <X> championed <Y>" shape never matches here —
// the presence of "at <company>" in multi_hop queries means the regex
// below (which requires no filler between "who" and the verb) won't fire.
//
// Capture group 1 is the project display span. The predicate label is
// fixed per-regex. Present + past tense both accepted so "who led X?"
// and "who championed X?" also route here.
var relationshipSingleREs = []struct {
	predicate string
	re        *regexp.Regexp
}{
	// "who champions <Y>?" / "who championed <Y>?"
	{
		predicate: "champions",
		re:        regexp.MustCompile(`(?i)^who\s+champion(?:ed|s)?\s+(?:the\s+)?([^?.!,]+?)(?:\s+project)?[?.!]*$`),
	},
	// "who leads <Y>?" / "who led <Y>?"
	{
		predicate: "leads",
		re:        regexp.MustCompile(`(?i)^who\s+(?:lead|leads|led)\s+(?:the\s+)?([^?.!,]+?)(?:\s+project)?[?.!]*$`),
	},
	// "who is involved in <Y>?" / "who's involved in <Y>?"
	{
		predicate: "involved_in",
		re:        regexp.MustCompile(`(?i)^who(?:\s+is|'s)?\s+involved\s+in\s+(?:the\s+)?([^?.!,]+?)(?:\s+project)?[?.!]*$`),
	},
}

// Counterfactual subject patterns. Order matters — most specific first so a
// generic "without X" never swallows "what would have happened if X…".
var counterfactualSubjectREs = []*regexp.Regexp{
	// "what would have happened if <P> had (not) ..."
	regexp.MustCompile(`(?i)what\s+would\s+have\s+happened\s+if\s+([^,?.!]+?)\s+(?:had\s+not|had\s+n't|hadn't|had)\b`),
	// "what if <P> had (not) ..."
	regexp.MustCompile(`(?i)what\s+if\s+([^,?.!]+?)\s+(?:had\s+not|had\s+n't|hadn't|had)\b`),
	// "suppose <P> had (not) ..."
	regexp.MustCompile(`(?i)suppose(?:\s+that)?\s+([^,?.!]+?)\s+(?:had\s+not|had\s+n't|hadn't|had)\b`),
	// "if <P> hadn't ..." / "if <P> had not ..."
	regexp.MustCompile(`(?i)\bif\s+([^,?.!]+?)\s+(?:had\s+not|had\s+n't|hadn't)\b`),
	// "without <P>..."
	regexp.MustCompile(`(?i)\bwithout\s+([^,?.!]+?)(?:,|\s+(?:would|the)\b|[?.!])`),
	// "if not for <P>..."
	regexp.MustCompile(`(?i)\bif\s+not\s+for\s+([^,?.!]+?)(?:,|[?.!])`),
}

// parseMultiHopSpans returns the company + project display spans from a
// multi_hop query. If the regex doesn't match it returns ("", "", false) and
// the caller falls back to the BM25-only path.
//
// Display strings are trimmed and stripped of surrounding [[ ]] wikilink
// syntax but preserve internal casing.
func parseMultiHopSpans(query string) (companyDisplay, projectDisplay string, ok bool) {
	m := multiHopRE.FindStringSubmatch(strings.TrimSpace(query))
	if len(m) != 3 {
		return "", "", false
	}
	companyDisplay = cleanSpan(m[1])
	projectDisplay = cleanSpan(m[2])
	if companyDisplay == "" || projectDisplay == "" {
		return "", "", false
	}
	return companyDisplay, projectDisplay, true
}

// parseCounterfactualSubject returns the person display span from a
// counterfactual query. Tries several regex forms in specificity order; the
// first match wins.
func parseCounterfactualSubject(query string) (personDisplay string, ok bool) {
	q := strings.TrimSpace(query)
	for _, re := range counterfactualSubjectREs {
		m := re.FindStringSubmatch(q)
		if len(m) >= 2 {
			if span := cleanSpan(m[1]); span != "" {
				return span, true
			}
		}
	}
	return "", false
}

// parseRelationshipSingle returns the predicate + project display span from
// a single-predicate relationship query. Tries each pattern in order; the
// first match wins. If none match, returns ("", "", false) and the caller
// falls back to the BM25-only path.
//
// Predicate values match the wiki fact schema exactly: "champions", "leads",
// "involved_in". Any downstream consumer (the typed graph walk) can pass
// them straight through to FactStore.ListFactsByPredicateObject without
// remapping.
func parseRelationshipSingle(query string) (predicate, projectDisplay string, ok bool) {
	q := strings.TrimSpace(query)
	for _, r := range relationshipSingleREs {
		m := r.re.FindStringSubmatch(q)
		if len(m) < 2 {
			continue
		}
		span := cleanSpan(m[1])
		if span == "" {
			continue
		}
		return r.predicate, span, true
	}
	return "", "", false
}

// cleanSpan trims whitespace and strips [[...]] wikilink brackets, keeping
// the Display half of [[slug|Display]]. Leaves internal whitespace + casing
// alone so the slug resolver gets the original span.
func cleanSpan(s string) string {
	s = strings.TrimSpace(s)
	// If the span is entirely wrapped in [[...]], unwrap it.
	if strings.HasPrefix(s, "[[") && strings.HasSuffix(s, "]]") {
		inner := strings.TrimSuffix(strings.TrimPrefix(s, "[["), "]]")
		if idx := strings.Index(inner, "|"); idx >= 0 {
			// [[slug|Display]] → prefer the Display half.
			inner = inner[idx+1:]
		}
		s = strings.TrimSpace(inner)
	} else {
		// Remove inline [[...]] wikilinks within a larger span by keeping their
		// visible text only.
		s = wikilinkInSpanRE.ReplaceAllStringFunc(s, func(match string) string {
			inner := strings.TrimSuffix(strings.TrimPrefix(match, "[["), "]]")
			if idx := strings.Index(inner, "|"); idx >= 0 {
				return strings.TrimSpace(inner[idx+1:])
			}
			return strings.TrimSpace(inner)
		})
	}
	// Strip trailing possessive 's.
	s = strings.TrimSuffix(s, "'s")
	s = strings.TrimSuffix(s, "’s") // unicode right single quote
	return strings.TrimSpace(s)
}

// wikilinkInSpanRE matches an inline [[...]] wikilink for replacement inside
// a larger captured span.
var wikilinkInSpanRE = regexp.MustCompile(`\[\[([^\[\]]+)\]\]`)

// displayToSlugCandidates returns slug candidates to try against the typed
// store, in order of likelihood:
//
//  1. Full normalised display (e.g. "Acme Corp" → "acme-corp").
//  2. First word only (e.g. "Vandelay Industries" → "vandelay").
//  3. First two words joined (e.g. "Q2 Pilot Program" → "q2-pilot").
//
// The callers try each candidate until one returns hits; the rest are
// harmless no-ops.
func displayToSlugCandidates(display string) []string {
	full := NormalizeForFactID(display)
	if full == "" {
		return nil
	}
	out := []string{full}

	// Split on normalised dashes to get word tokens.
	tokens := strings.Split(full, "-")
	if len(tokens) >= 2 {
		// First two words joined.
		twoWords := tokens[0] + "-" + tokens[1]
		if twoWords != full {
			out = append(out, twoWords)
		}
	}
	if len(tokens) >= 1 {
		// First word only.
		if tokens[0] != full && !slugCandidatesContain(out, tokens[0]) {
			out = append(out, tokens[0])
		}
	}
	return out
}

func slugCandidatesContain(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
