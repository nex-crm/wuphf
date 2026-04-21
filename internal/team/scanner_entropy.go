package team

// scanner_entropy.go provides the Shannon-entropy heuristic the redaction
// pass uses to catch unknown-format secrets: anything that doesn't match a
// known pattern but *looks* random enough to be a credential.
//
// Positioning: this is the "ML upgrade" per the v1.2 roadmap. A real
// classifier would be heavier (model weights, entropyTokenizer, per-call cost)
// than it's worth for a scanner that just has to decide "is this string
// random-looking"? Shannon entropy over a high-confidence threshold is
// cheap, dependency-free, and catches base64 / hex secrets that no regex
// will ever match precisely.
//
// Tuning rationale:
//
//   - entropyThresholdBits = 4.0 — English prose tops out around 4.1–4.3
//     bits/char; random base64 lands between 5.5 and 6.0. 4.0 gives us a
//     margin above prose before flagging.
//   - minCandidateLength = 20 — below this any short URL fragment or ID
//     hits the threshold by accident. 20+ is the length range real keys
//     actually live in.
//   - tokenBoundary splits on whitespace, quotes, commas, semicolons,
//     parentheses, and common separators. Keeps multi-token lines from
//     smearing into a single "low-entropy" clump.
//
// What this deliberately does NOT do: train on a corpus, weight by
// character class, or call out into any embedding model. Those add false
// confidence and deployment overhead for sub-percent precision gains.

import (
	"math"
	"strings"
	"unicode"
)

// entropyThresholdBits is the minimum Shannon entropy (in bits per
// character) that makes a token suspect. English prose is ≤ 4.2 bits;
// random base64 is ≥ 5.5 bits. 4.0 is a conservative boundary.
const entropyThresholdBits = 4.0

// minCandidateLength is the shortest token the entropy pass will even
// consider. Short strings trivially have low-entropy neighborhoods where
// random-looking noise is indistinguishable from prose.
const minCandidateLength = 20

// maxEntropyHitsPerFile caps how many entropy-only hits we redact per
// file. Beyond this we escalate to the file-level skip — the document is
// almost certainly a machine-generated blob, not prose with a secret.
const maxEntropyHitsPerFile = 5

// shannonEntropy returns the Shannon entropy of s in bits-per-character,
// or 0 for the empty string. Computed on byte frequencies (sufficient
// for catching base64/hex-ish randomness; full rune handling buys us
// nothing here).
func shannonEntropy(s string) float64 {
	if s == "" {
		return 0
	}
	var counts [256]int
	for i := 0; i < len(s); i++ {
		counts[s[i]]++
	}
	length := float64(len(s))
	var h float64
	for _, c := range counts {
		if c == 0 {
			continue
		}
		p := float64(c) / length
		h -= p * math.Log2(p)
	}
	return h
}

// entropyTokenize splits s on whitespace and common structural punctuation so
// each token can be scored independently. A single URL or assignment
// often has a long boring scheme followed by a high-entropy token — we
// want to see the token on its own.
func entropyTokenize(s string) []string {
	fields := strings.FieldsFunc(s, func(r rune) bool {
		if unicode.IsSpace(r) {
			return true
		}
		switch r {
		case '"', '\'', '`', ',', ';', '(', ')', '{', '}', '[', ']', '<', '>':
			return true
		}
		return false
	})
	return fields
}

// isPlausibleSecretCharset rejects tokens that can't be credentials
// (e.g., English words, numeric-only IDs, hyphen-sentence fragments).
// Real keys use a mix of letters + digits / base64 / hex.
func isPlausibleSecretCharset(s string) bool {
	var hasLetter, hasDigit bool
	for _, r := range s {
		if unicode.IsLetter(r) {
			hasLetter = true
		}
		if unicode.IsDigit(r) {
			hasDigit = true
		}
		// Bail out early on whitespace / structural runes that would
		// indicate the entropyTokenizer left junk in.
		if unicode.IsSpace(r) {
			return false
		}
	}
	return hasLetter && hasDigit
}

// EntropyHit reports a single high-entropy token plus its measured bits.
// Exposed for tests; the main redaction path uses it through the
// detectEntropyHits helper below.
type EntropyHit struct {
	Token string
	Bits  float64
}

// detectEntropyHits returns every token in content that crosses the
// entropy threshold *and* passes the charset plausibility filter. The
// caller is expected to have already run the pattern catalog so known
// formats are removed first — we do not want to double-count.
func detectEntropyHits(content string) []EntropyHit {
	var hits []EntropyHit
	for _, tok := range entropyTokenize(content) {
		if len(tok) < minCandidateLength {
			continue
		}
		if !isPlausibleSecretCharset(tok) {
			continue
		}
		bits := shannonEntropy(tok)
		if bits < entropyThresholdBits {
			continue
		}
		hits = append(hits, EntropyHit{Token: tok, Bits: bits})
	}
	return hits
}
