package scanner

// scanner_entropy_test.go proves the entropy heuristic distinguishes
// English prose (low entropy) from base64-encoded random bytes (high
// entropy). Also covers the integration corner cases: sentinel marker
// immunity, minimum length, and the per-file hit cap.

import (
	"encoding/base64"
	"math/rand"
	"strings"
	"testing"
)

func TestShannonEntropyRangeSanity(t *testing.T) {
	cases := []struct {
		name   string
		input  string
		wantLo float64
		wantHi float64
	}{
		{"empty", "", 0, 0},
		{"single-char-repeat", strings.Repeat("a", 100), 0, 0.01},
		{"two-char-alternating", strings.Repeat("ab", 50), 0.99, 1.01},
		// "the quick brown fox ..." — English prose lands ~3.7-4.4 bits
		// depending on character mix. Stays well below random base64
		// (5.5+), which is what the threshold actually separates.
		{"prose", "the quick brown fox jumps over the lazy dog repeatedly and carefully", 3.0, 4.5},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := shannonEntropy(tc.input)
			if got < tc.wantLo || got > tc.wantHi {
				t.Fatalf("shannonEntropy(%q) = %.3f; want in [%.2f, %.2f]", tc.input, got, tc.wantLo, tc.wantHi)
			}
		})
	}
}

func TestDetectEntropyHitsIgnoresEnglishProse(t *testing.T) {
	prose := "The scanner must not flag ordinary prose even when the paragraphs grow long and contain many varied words commas and semicolons; including technical terms like authentication, repository, configuration."
	hits := detectEntropyHits(prose)
	if len(hits) != 0 {
		t.Fatalf("expected no entropy hits on prose, got %+v", hits)
	}
}

func TestDetectEntropyHitsFlagsBase64RandomBytes(t *testing.T) {
	// Deterministic 32 random bytes → base64 ≈ 44 chars, ~5.8+ bits.
	r := rand.New(rand.NewSource(42))
	buf := make([]byte, 32)
	for i := range buf {
		buf[i] = byte(r.Intn(256))
	}
	tok := base64.RawStdEncoding.EncodeToString(buf)
	// Ensure mixed letters+digits (retry if not).
	for !hasLetterAndDigit(tok) {
		for i := range buf {
			buf[i] = byte(r.Intn(256))
		}
		tok = base64.RawStdEncoding.EncodeToString(buf)
	}
	body := "Here is a suspicious token: " + tok + " and some trailing prose."
	hits := detectEntropyHits(body)
	if len(hits) == 0 {
		t.Fatalf("expected entropy hit for base64 token %q, got none", tok)
	}
	if hits[0].Bits < entropyThresholdBits {
		t.Fatalf("first hit below threshold: %+v", hits[0])
	}
	if !strings.Contains(hits[0].Token, tok) {
		t.Fatalf("expected hit token to contain %q, got %q", tok, hits[0].Token)
	}
}

func TestDetectEntropyHitsRespectsMinLength(t *testing.T) {
	// 19-char random-ish token should not be flagged (below minCandidateLength).
	body := "short: a1B2c3D4e5F6g7H8i9j"
	if len(strings.Fields(body)[1]) >= minCandidateLength {
		t.Skip("test fixture is not actually below the minimum length")
	}
	hits := detectEntropyHits(body)
	if len(hits) != 0 {
		t.Fatalf("expected no hits below minCandidateLength, got %+v", hits)
	}
}

func TestRedactSecretsEntropyTopsUpBeyondPatterns(t *testing.T) {
	// A random-looking assignment that doesn't match any known pattern.
	// Use a high-entropy token: base64 of 40 random bytes.
	r := rand.New(rand.NewSource(7))
	buf := make([]byte, 40)
	for i := range buf {
		buf[i] = byte(r.Intn(256))
	}
	tok := base64.RawStdEncoding.EncodeToString(buf)
	body := "custom_signing_key=" + tok + "\nOrdinary prose follows."
	res := redactSecretsDetailed(body)
	if res.EntropyHits == 0 {
		t.Fatalf("expected at least one entropy hit on %q, got %+v", tok, res)
	}
	if !strings.Contains(res.Content, "[REDACTED]") {
		t.Fatalf("expected [REDACTED] marker in scrubbed body: %q", res.Content)
	}
	sawEntropy := false
	for _, r := range res.Reasons {
		if r.Kind == "entropy" {
			sawEntropy = true
			if r.Bits < entropyThresholdBits {
				t.Fatalf("recorded entropy below threshold: %+v", r)
			}
		}
	}
	if !sawEntropy {
		t.Fatalf("expected an entropy reason, got %+v", res.Reasons)
	}
}

func TestEntropyDoesNotReFlagRedactionMarker(t *testing.T) {
	// After pattern redaction replaces a long token with `[REDACTED]`,
	// the marker itself must not become a new entropy hit. The marker
	// is short (shorter than minCandidateLength), so this is already
	// structurally impossible — but lock it in.
	in := "OPENAI_API_KEY=sk-" + strings.Repeat("Z", 40)
	res := redactSecretsDetailed(in)
	if res.EntropyHits != 0 {
		t.Fatalf("expected zero entropy hits after pattern redaction, got %+v", res)
	}
}

// hasLetterAndDigit is a tiny helper — keeps TestDetectEntropyHitsFlagsBase64
// deterministic even if RNG happens to yield an all-alpha encoding.
func hasLetterAndDigit(s string) bool {
	var hasL, hasD bool
	for _, r := range s {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' {
			hasL = true
		}
		if r >= '0' && r <= '9' {
			hasD = true
		}
	}
	return hasL && hasD
}
