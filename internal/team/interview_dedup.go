package team

// interview_dedup.go — cross-agent semantic dedupe for HUMAN-directed
// interviews (kind=interview only).
//
// Live smoke run: FIVE agents asked the human the same "which CRM?"
// question in five separate blocking interviews; answering one did not
// ground the rest. handlePostRequest now checks every new interview
// question against:
//
//   - ACTIVE pending interviews: a semantically-similar question does NOT
//     create a second card — the asking agent is attached as a subscriber
//     (humanInterview.AlsoAsking) on the existing request and polls the
//     SAME request id, so the one human answer fans out to every asker
//     through the existing answer-delivery path.
//   - RECENTLY-ANSWERED interviews (within recentInterviewAnswerReuseWindow):
//     the existing answer is returned to the asking agent immediately
//     ("already answered by the human") and no card is raised.
//
// Scope guard: ONLY kind=interview requests dedupe semantically. Action
// approval gates (approval/connect/fallback/confirm/choice) carry distinct
// payloads — their exact DedupeKey already collapses retries, and two
// approvals for different actions must never merge. Secret requests never
// dedupe either (their content is private to one ask).
//
// Similarity reuses the repo's cheap text machinery (normalized text +
// jaro_winkler.go, the same tiers as policy/skill dedupe) and is
// deliberately CONSERVATIVE: a missed dedupe costs one extra card; a wrong
// merge feeds agent A the human's answer to agent B's different question.

import (
	"strings"
	"time"
	"unicode"
)

// recentInterviewAnswerReuseWindow bounds how long an already-answered
// interview keeps grounding new similar asks without re-prompting the
// human. ~2h: long enough to cover a whole working session of agents
// rediscovering the same gap, short enough that stale decisions get
// re-confirmed the next session.
const recentInterviewAnswerReuseWindow = 2 * time.Hour

// interviewDedupeJaroThreshold gates the whole-question Jaro-Winkler tier.
// Set very high on purpose: full-sentence Jaro-Winkler scores ~0.96 for
// "send the renewal email to Acme" vs "... to Corti" (different asks!), so
// this tier only catches typo-level rephrasings; paraphrases are handled
// by the content-token tier below.
const interviewDedupeJaroThreshold = 0.97

// interviewDedupeTokenThreshold gates the content-token Jaccard tier
// (stopwords removed). 0.75 keeps entity-swapped questions apart
// ({send,renewal,email,acme} vs {send,renewal,email,corti} = 0.6) while
// merging honest paraphrases of the same ask.
const interviewDedupeTokenThreshold = 0.75

// interviewQuestionStopwords are function words dropped before the token
// comparison so phrasing ("should we…" vs "do you want the team to…")
// does not mask that two questions ask the same thing.
var interviewQuestionStopwords = map[string]struct{}{
	"a": {}, "an": {}, "the": {}, "and": {}, "or": {}, "but": {}, "if": {},
	"then": {}, "than": {}, "so": {}, "as": {}, "of": {}, "in": {}, "on": {},
	"at": {}, "by": {}, "with": {}, "to": {}, "for": {}, "from": {},
	"into": {}, "about": {}, "over": {}, "under": {}, "up": {}, "down": {},
	"out": {}, "off": {},
	"is": {}, "are": {}, "was": {}, "were": {}, "be": {}, "been": {},
	"being": {}, "am": {}, "do": {}, "does": {}, "did": {}, "done": {},
	"have": {}, "has": {}, "had": {},
	"i": {}, "me": {}, "my": {}, "we": {}, "us": {}, "our": {}, "you": {},
	"your": {}, "they": {}, "them": {}, "their": {}, "it": {}, "its": {},
	"this": {}, "that": {}, "these": {}, "those": {}, "there": {}, "here": {},
	"what": {}, "which": {}, "who": {}, "whom": {}, "whose": {}, "when": {},
	"where": {}, "how": {}, "why": {},
	"should": {}, "would": {}, "could": {}, "can": {}, "will": {},
	"shall": {}, "may": {}, "might": {}, "must": {},
	"want": {}, "wants": {}, "need": {}, "needs": {}, "prefer": {},
	"please": {}, "team": {}, "just": {}, "also": {}, "any": {}, "all": {},
	"some": {}, "more": {}, "no": {}, "not": {}, "yes": {},
}

// normalizeInterviewQuestion lowercases, strips punctuation, and collapses
// whitespace — the interview analogue of normalizePolicyRuleText, with
// punctuation removal added so "HubSpot or Salesforce?" == "HubSpot or
// Salesforce".
func normalizeInterviewQuestion(q string) string {
	var sb strings.Builder
	sb.Grow(len(q))
	for _, r := range strings.ToLower(q) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			sb.WriteRune(r)
		} else {
			sb.WriteByte(' ')
		}
	}
	return strings.Join(strings.Fields(sb.String()), " ")
}

// interviewContentTokens returns the deduped non-stopword tokens of an
// already-normalized question.
func interviewContentTokens(normalized string) map[string]struct{} {
	tokens := make(map[string]struct{})
	for _, tok := range strings.Fields(normalized) {
		if _, stop := interviewQuestionStopwords[tok]; stop {
			continue
		}
		tokens[tok] = struct{}{}
	}
	return tokens
}

// interviewQuestionsSimilar reports whether two interview questions ask
// the human the same thing. Tiers, cheapest first:
//
//  1. exact normalized-text match,
//  2. whole-question Jaro-Winkler at a near-identical threshold,
//  3. content-token Jaccard (stopwords removed): identical token sets
//     always match; otherwise >= interviewDedupeTokenThreshold with at
//     least two shared content tokens.
func interviewQuestionsSimilar(a, b string) bool {
	na, nb := normalizeInterviewQuestion(a), normalizeInterviewQuestion(b)
	if na == "" || nb == "" {
		return false
	}
	if na == nb {
		return true
	}
	if JaroWinkler(na, nb) >= interviewDedupeJaroThreshold {
		return true
	}
	ta, tb := interviewContentTokens(na), interviewContentTokens(nb)
	if len(ta) == 0 || len(tb) == 0 {
		return false
	}
	inter := 0
	for tok := range ta {
		if _, ok := tb[tok]; ok {
			inter++
		}
	}
	union := len(ta) + len(tb) - inter
	if union == 0 {
		return false
	}
	jaccard := float64(inter) / float64(union)
	if jaccard >= 1.0 {
		// Identical content-token sets ("Which CRM should we use?" vs
		// "Which CRM do you want the team to use?") — same ask.
		return true
	}
	return inter >= 2 && jaccard >= interviewDedupeTokenThreshold
}

// interviewAskerKnown reports whether asker already gets this interview's
// answer — as the original asker or as an attached subscriber.
func interviewAskerKnown(req humanInterview, asker string) bool {
	if strings.EqualFold(strings.TrimSpace(req.From), asker) {
		return true
	}
	for _, slug := range req.AlsoAsking {
		if strings.EqualFold(strings.TrimSpace(slug), asker) {
			return true
		}
	}
	return false
}

// findActiveSimilarInterviewLocked returns the first ACTIVE human-directed
// interview whose question is semantically similar, searching across ALL
// channels — the live failure was five agents asking from five different
// task channels. Caller must hold b.mu.
func (b *Broker) findActiveSimilarInterviewLocked(question string) *humanInterview {
	for i := range b.requests {
		req := &b.requests[i]
		if !requestIsHumanInterview(*req) || !requestIsActive(*req) || req.Secret {
			continue
		}
		if interviewQuestionsSimilar(question, req.Question) {
			return req
		}
	}
	return nil
}

// recentlyAnsweredSimilarInterviewLocked returns the most recently
// answered human-directed interview (within
// recentInterviewAnswerReuseWindow) whose question is semantically
// similar, plus its answer summary. Caller must hold b.mu.
func (b *Broker) recentlyAnsweredSimilarInterviewLocked(question string) (*humanInterview, string) {
	var best *humanInterview
	var bestAt time.Time
	for i := range b.requests {
		req := &b.requests[i]
		if !requestIsHumanInterview(*req) || req.Secret || req.Answered == nil {
			continue
		}
		answeredAt, err := time.Parse(time.RFC3339, req.Answered.AnsweredAt)
		if err != nil || time.Since(answeredAt) > recentInterviewAnswerReuseWindow {
			continue
		}
		if !interviewQuestionsSimilar(question, req.Question) {
			continue
		}
		if best == nil || answeredAt.After(bestAt) {
			best, bestAt = req, answeredAt
		}
	}
	if best == nil {
		return nil, ""
	}
	return best, reqAnswerSummary(best.Answered)
}
