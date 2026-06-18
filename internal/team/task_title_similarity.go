package team

import (
	"strings"
	"unicode"
)

// task_title_similarity.go provides normalized, fuzzy title matching used to
// catch duplicate and shallow-restating tasks that exact-title dedup misses
// ("Ship the MVP" vs "Build the first MVP slice" vs "ship mvp!"). The exact
// EqualFold check the reuse path used only collapses byte-identical titles, so
// the model spawned near-duplicates freely; this widens the net deterministically.

// titleStopwords are low-signal tokens dropped before comparing titles, so that
// filler and politeness words do not make two restatements of the same work
// look different. Kept deliberately small — removing real nouns/verbs would
// over-merge distinct work.
var titleStopwords = map[string]struct{}{
	"a": {}, "an": {}, "the": {}, "to": {}, "for": {}, "of": {}, "and": {},
	"or": {}, "in": {}, "on": {}, "with": {}, "our": {}, "my": {}, "your": {},
	"this": {}, "that": {}, "please": {}, "can": {}, "you": {}, "we": {},
	"i": {}, "us": {}, "it": {}, "is": {}, "be": {}, "into": {}, "up": {},
	"first": {}, "new": {}, "task": {}, "issue": {},
}

// normalizeTitleTokens lowercases the title, strips punctuation, splits on
// whitespace, and drops stopwords. Returns the surviving content tokens in
// order. Empty input (or input that is all stopwords/punctuation) yields nil.
func normalizeTitleTokens(title string) []string {
	lowered := strings.ToLower(strings.TrimSpace(title))
	if lowered == "" {
		return nil
	}
	fields := strings.FieldsFunc(lowered, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		if _, stop := titleStopwords[f]; stop {
			continue
		}
		out = append(out, stemTitleToken(f))
	}
	return out
}

// stemTitleToken applies a deliberately tiny stem so trivial singular/plural
// variants collapse ("webhooks" == "webhook", "emails" == "email"). It strips a
// trailing "s" from tokens longer than three letters; anything fancier risks
// over-merging distinct words, and this is only a dedup heuristic.
func stemTitleToken(tok string) string {
	if len(tok) > 3 && strings.HasSuffix(tok, "s") && !strings.HasSuffix(tok, "ss") {
		return tok[:len(tok)-1]
	}
	return tok
}

// normalizeTitleKey joins the normalized content tokens into a stable, sorted-
// free key. Two titles with the same key (after stopword/punctuation removal,
// same token order) are treated as exact restatements.
func normalizeTitleKey(title string) string {
	return strings.Join(normalizeTitleTokens(title), " ")
}

// titleTokenSimilarity returns the Jaccard similarity (0..1) of the two titles'
// normalized content-token SETS. Order-insensitive so "MVP onboarding flow" and
// "onboarding flow for the MVP" score high. Returns 0 when either side has no
// content tokens.
func titleTokenSimilarity(a, b string) float64 {
	ta := normalizeTitleTokens(a)
	tb := normalizeTitleTokens(b)
	if len(ta) == 0 || len(tb) == 0 {
		return 0
	}
	setA := make(map[string]struct{}, len(ta))
	for _, t := range ta {
		setA[t] = struct{}{}
	}
	setB := make(map[string]struct{}, len(tb))
	for _, t := range tb {
		setB[t] = struct{}{}
	}
	inter := 0
	for t := range setA {
		if _, ok := setB[t]; ok {
			inter++
		}
	}
	union := len(setA) + len(setB) - inter
	if union == 0 {
		return 0
	}
	return float64(inter) / float64(union)
}

// titleSimilarityThreshold is the Jaccard cutoff at/above which two titles are
// treated as the same intent. Set conservatively high: dedup with title+owner
// only already risks over-merging (see findReusableTaskLocked), so the fuzzy
// widening must stay tight enough that genuinely-distinct work is not collapsed.
const titleSimilarityThreshold = 0.8

// titlesAreSimilar reports whether two titles name the same work for dedup /
// shallow-subtask purposes. True when their normalized keys match exactly OR
// their token-set Jaccard similarity meets titleSimilarityThreshold.
func titlesAreSimilar(a, b string) bool {
	keyA := normalizeTitleKey(a)
	keyB := normalizeTitleKey(b)
	if keyA == "" || keyB == "" {
		return false
	}
	if keyA == keyB {
		return true
	}
	return titleTokenSimilarity(a, b) >= titleSimilarityThreshold
}
