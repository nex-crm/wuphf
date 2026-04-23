package team

// wiki_classifier.go — heuristic query classifier for the /lookup cited-answer
// loop.
//
// Classifies a natural-language query into one of five buckets so the query
// handler can route it to the cheapest answering path. The classifier itself
// makes NO LLM call — that would invert the cost savings it exists to provide.
//
// Design constraints (from answer_query.tmpl lines 13-15):
//   - Simple queries (status, relationship) → 1 LLM call (~2-3s)
//   - Complex queries (multi_hop, counterfactual) → 2 LLM calls (~5-7s)
//   - Out-of-scope (general) → 0 LLM calls (short-circuit refusal)
//
// Confidence is 0–1. When confidence < 0.8 for the "general" class, the
// query handler falls through to the LLM path rather than refusing.
//
// Heuristic rules (order matters — first match wins):
//  1. Contains counterfactual signal → counterfactual
//  2. "who at <entity>" pattern (including [[wikilink]]) → multi_hop
//  3. Contains entity@entity or "<multi-token> at <entity>" → multi_hop
//  4. Starts with "who" + works/reports verb → relationship
//  5. "what does X do" / "what is X's role" → status
//  6. No entity tokens detected (after filtering non-workspace capitals) → general
//  7. Default → status

import (
	"regexp"
	"strings"
	"unicode"
)

// QueryClass is the classifier output label.
type QueryClass string

const (
	QueryClassStatus         QueryClass = "status"
	QueryClassRelationship   QueryClass = "relationship"
	QueryClassMultiHop       QueryClass = "multi_hop"
	QueryClassCounterfactual QueryClass = "counterfactual"
	QueryClassGeneral        QueryClass = "general"
)

// counterfactualSignals are phrases that indicate a "what if" query.
var counterfactualSignals = []string{
	"what if", "what would have", "what would happen",
	"suppose ", "had not", "hadn't", "never had",
	"if not for", "if they hadn't", "if she hadn't", "if he hadn't",
	"without", "hypothetically", "counterfactual",
}

// rolePatterns match "what does X do" or "what is X's role".
var rolePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)^what does\s+\S+\s+do\b`),
	regexp.MustCompile(`(?i)\brole\b`),
	regexp.MustCompile(`(?i)^what is .+['']s (role|job|title|position)\b`),
	regexp.MustCompile(`(?i)\bjob title\b`),
	regexp.MustCompile(`(?i)\bposition\b`),
}

// wikilinkRE matches [[slug]] or [[slug|Display]] syntax.
var wikilinkRE = regexp.MustCompile(`\[\[([^\[\]|]+)(?:\|[^\[\]|]*)?\]\]`)

// whoAtEntityRE matches "who at <word-or-wikilink>" — multi-hop bridging.
// Handles both plain words (\w) and wikilinks ([[...).
var whoAtEntityRE = regexp.MustCompile(`(?i)\bwho\s+at\s+(?:\w|\[\[)`)

// entityAtEntityRE covers "X at Y" where both X and Y are capitalized token
// sequences (minimum 2 chars each to avoid stop-word false positives).
var entityAtEntityRE = regexp.MustCompile(`[A-Z][a-zA-Z0-9]{1,} at [A-Z][a-zA-Z0-9]{1,}`)

// commonFirstNames is a small set of common given names used as entity signals.
// Not exhaustive — heuristic only.
var commonFirstNames = map[string]bool{
	"alice": true, "bob": true, "carol": true, "dave": true, "david": true,
	"emily": true, "eve": true, "frank": true, "grace": true, "henry": true,
	"ivan": true, "jane": true, "john": true, "kate": true, "kevin": true,
	"lisa": true, "mark": true, "mary": true, "mike": true, "michael": true,
	"nancy": true, "nina": true, "oscar": true, "paul": true, "peter": true,
	"rachel": true, "rob": true, "robert": true, "sarah": true, "sue": true,
	"susan": true, "tom": true, "tony": true, "victor": true, "wendy": true,
	"anna": true, "james": true, "alex": true, "emma": true, "liam": true,
	"olivia": true, "noah": true, "ava": true, "sophia": true, "william": true,
	"nazz": true, "naz": true, "sam": true, "max": true, "leo": true,
}

// ClassifyQuery returns a QueryClass + confidence for the given query string.
//
// Confidence is expressed as a float64 in [0.0, 1.0]:
//   - 1.0  strong heuristic signal (wikilink, clear counterfactual phrase)
//   - 0.8+ medium signal (role pattern, who-at construction)
//   - 0.5  default (entity-adjacent but ambiguous)
func ClassifyQuery(q string) (QueryClass, float64) {
	q = strings.TrimSpace(q)
	if q == "" {
		return QueryClassGeneral, 1.0
	}

	lower := strings.ToLower(q)

	// Rule 1: counterfactual signals take highest priority.
	for _, sig := range counterfactualSignals {
		if strings.Contains(lower, sig) {
			return QueryClassCounterfactual, 0.85
		}
	}

	// Rule 2: "who at <entity/wikilink>" → multi_hop.
	if whoAtEntityRE.MatchString(q) {
		return QueryClassMultiHop, 0.9
	}

	// Rule 3: entity@entity pattern or explicit wikilink + at-context → multi_hop.
	if entityAtEntityRE.MatchString(q) {
		return QueryClassMultiHop, 0.8
	}
	if strings.Contains(lower, "@") && hasWorkspaceEntityToken(q) {
		return QueryClassMultiHop, 0.75
	}

	// Rule 4: relationship queries (who works/reports).
	if startsWithWhoAndRelationship(lower) {
		return QueryClassRelationship, 0.85
	}
	// Broader relationship: "who <verb> ..." with workspace entity.
	if strings.HasPrefix(lower, "who ") && hasWorkspaceEntityToken(q) {
		return QueryClassRelationship, 0.75
	}

	// Rule 5: role/status patterns.
	for _, pat := range rolePatterns {
		if pat.MatchString(q) {
			return QueryClassStatus, 0.8
		}
	}

	// Explicit wikilink → always entity-adjacent.
	if wikilinkRE.MatchString(q) {
		return QueryClassStatus, 1.0
	}

	// Rule 6: no workspace entity token → general (out-of-scope).
	if !hasWorkspaceEntityToken(q) {
		return QueryClassGeneral, 0.85
	}

	// Rule 7: default — entity tokens present, query shape is ambiguous → status.
	return QueryClassStatus, 0.5
}

// startsWithWhoAndRelationship returns true when the query leads with "who"
// and contains a relationship verb.
func startsWithWhoAndRelationship(lower string) bool {
	if !strings.HasPrefix(lower, "who") {
		return false
	}
	relationshipVerbs := []string{
		"reports to", "works for", "works at", "works with",
		"manages", "leads", "owns", "runs", "heads",
		"does", "is the", "is responsible",
	}
	for _, verb := range relationshipVerbs {
		if strings.Contains(lower, verb) {
			return true
		}
	}
	return false
}

// hasWorkspaceEntityToken returns true when the query contains at least one
// token that looks like a workspace entity (person or company name):
//   - a [[wikilink]] reference (strongest signal)
//   - a known first name (case-insensitive)
//   - a capitalized multi-char word that is not a stop-word, location, or
//     well-known non-workspace proper noun
func hasWorkspaceEntityToken(q string) bool {
	// Wikilink syntax is the strongest signal.
	if wikilinkRE.MatchString(q) {
		return true
	}
	for _, word := range strings.Fields(q) {
		clean := stripPunct(word)
		if clean == "" {
			continue
		}
		lower := strings.ToLower(clean)
		// Known first name always counts.
		if commonFirstNames[lower] {
			return true
		}
		// Capitalized word: exclude stop-words and non-workspace proper nouns.
		r := []rune(clean)
		if len(r) > 1 && unicode.IsUpper(r[0]) {
			if isStopWord(lower) || isNonWorkspaceNoun(lower) {
				continue
			}
			return true
		}
	}
	return false
}

// stopWords lists common English words that start sentences and are uppercased
// only because of grammar, not because they are named entities.
var stopWords = map[string]bool{
	"what": true, "who": true, "where": true, "when": true, "why": true,
	"how": true, "is": true, "are": true, "was": true, "were": true,
	"the": true, "a": true, "an": true, "in": true, "on": true,
	"at": true, "to": true, "for": true, "of": true, "and": true,
	"or": true, "but": true, "can": true, "will": true, "does": true,
	"do": true, "did": true, "has": true, "have": true, "had": true,
	"should": true, "would": true, "could": true, "my": true, "your": true,
	"their": true, "our": true, "his": true, "her": true, "its": true,
	"which": true, "that": true, "this": true, "if": true, "then": true,
	"tell": true, "show": true, "give": true, "find": true, "get": true,
	"with": true, "about": true, "from": true, "by": true, "as": true,
	"i": true, "me": true, "we": true, "you": true, "they": true,
	"write": true, "read": true, "please": true, "help": true,
}

// nonWorkspaceNouns are well-known proper nouns that are not business entities
// in a typical workspace context. This blocks false positives when someone
// asks about pop culture, geography, or programming languages.
var nonWorkspaceNouns = map[string]bool{
	// Geography
	"london": true, "paris": true, "new": true, "york": true, "berlin": true,
	"tokyo": true, "sydney": true, "dubai": true, "singapore": true,
	"america": true, "europe": true, "asia": true, "africa": true,
	"england": true, "france": true, "germany": true, "japan": true,
	"uk": true, "usa": true, "india": true, "china": true, "brazil": true,
	// Pop culture / entertainment
	"oscars": true, "grammy": true, "emmy": true, "bafta": true,
	"netflix": true, "disney": true, "marvel": true, "dc": true,
	"oscar": true, "academy": true, "golden": true, "globe": true,
	"hollywood": true, "broadway": true, "superbowl": true, "nfl": true,
	"nba": true, "fifa": true, "olympics": true, "wimbledon": true,
	// Programming languages / tech platforms (generic queries)
	"go": true, "python": true, "javascript": true, "typescript": true,
	"java": true, "rust": true, "swift": true, "kotlin": true,
	"react": true, "vue": true, "angular": true, "kubernetes": true,
	"docker": true, "aws": true, "azure": true, "gcp": true,
	"linux": true, "macos": true, "windows": true, "android": true, "ios": true,
	// Time / calendar
	"january": true, "february": true, "march": true, "april": true,
	"may": true, "june": true, "july": true, "august": true, "september": true,
	"october": true, "november": true, "december": true,
	"monday": true, "tuesday": true, "wednesday": true, "thursday": true,
	"friday": true, "saturday": true, "sunday": true,
	"today": true, "yesterday": true, "tomorrow": true,
	"q1": true, "q2": true, "q3": true, "q4": true,
}

func isNonWorkspaceNoun(s string) bool {
	return nonWorkspaceNouns[s]
}

func isStopWord(s string) bool {
	return stopWords[s]
}

// stripPunct removes leading and trailing punctuation from a token.
func stripPunct(s string) string {
	return strings.TrimFunc(s, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}
