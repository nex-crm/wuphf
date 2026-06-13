package scanner

// egress_redaction_test.go proves the fail-closed egress contract: clean and
// single-token content is allowed (the latter scrubbed), while poison-class
// secrets and over-cap entropy content are DENIED with no content emitted.

import (
	"encoding/base64"
	"math/rand"
	"strings"
	"testing"
)

func TestRedactForEgress_CleanProseAllowedUnchanged(t *testing.T) {
	in := "The warehouse bot should reconcile the open invoices and report totals back in the thread."
	got := RedactForEgress(in)
	if !got.Allowed {
		t.Fatalf("clean prose denied: reason=%q", got.DenyReason)
	}
	if got.Redactions != 0 {
		t.Fatalf("clean prose had %d redactions, want 0", got.Redactions)
	}
	if got.Content != in {
		t.Fatalf("clean prose content mutated:\n got %q\nwant %q", got.Content, in)
	}
}

func TestRedactForEgress_KnownTokenRedactedButAllowed(t *testing.T) {
	// A single non-poison pattern hit (openai key) is scrubbed, and the item is
	// still safe to emit because pattern redaction is exhaustive.
	secret := "sk-proj-ABCDEFGHIJKLMNOPQRSTUVWXYZ012345"
	in := "Use key " + secret + " for the call."
	got := RedactForEgress(in)
	if !got.Allowed {
		t.Fatalf("single-token content denied: reason=%q", got.DenyReason)
	}
	if got.Redactions < 1 {
		t.Fatalf("expected >=1 redaction, got %d", got.Redactions)
	}
	if strings.Contains(got.Content, secret) {
		t.Fatalf("secret survived into emitted content: %q", got.Content)
	}
	if !strings.Contains(got.Content, "[REDACTED]") {
		t.Fatalf("expected [REDACTED] marker, got %q", got.Content)
	}
}

func TestRedactForEgress_PoisonDeniesAndWithholdsContent(t *testing.T) {
	in := "Deploy key:\n-----BEGIN OPENSSH PRIVATE KEY-----\nb3BlbnNzaC1rZXkt\n-----END OPENSSH PRIVATE KEY-----\n"
	got := RedactForEgress(in)
	if got.Allowed {
		t.Fatalf("poison content was allowed")
	}
	if got.DenyReason != "poisoned" {
		t.Fatalf("deny reason = %q, want poisoned", got.DenyReason)
	}
	if got.Content != "" {
		t.Fatalf("denied content must be empty, got %q", got.Content)
	}
}

func TestRedactForEgress_EntropyCapDeniesAndWithholdsContent(t *testing.T) {
	// More than maxEntropyHitsPerFile high-entropy tokens: the entropy pass
	// stops early, so the remainder is unproven and the whole item is withheld.
	r := rand.New(rand.NewSource(7))
	var b strings.Builder
	b.WriteString("batch of opaque handles: ")
	for i := 0; i < maxEntropyHitsPerFile+2; i++ {
		b.WriteString(highEntropyEgressToken(r))
		b.WriteByte(' ')
	}
	got := RedactForEgress(b.String())
	if got.Allowed {
		t.Fatalf("over-cap entropy content was allowed (redactions=%d)", got.Redactions)
	}
	if got.DenyReason != "entropy-cap" {
		t.Fatalf("deny reason = %q, want entropy-cap", got.DenyReason)
	}
	if got.Content != "" {
		t.Fatalf("denied content must be empty, got %q", got.Content)
	}
}

// highEntropyEgressToken returns a 44-char base64 token (32 random bytes)
// guaranteed to mix letters and digits, so it reliably trips the entropy
// heuristic. hasLetterAndDigit is shared from scanner_entropy_test.go.
func highEntropyEgressToken(r *rand.Rand) string {
	buf := make([]byte, 32)
	for {
		for i := range buf {
			buf[i] = byte(r.Intn(256))
		}
		tok := base64.RawStdEncoding.EncodeToString(buf)
		if hasLetterAndDigit(tok) {
			return tok
		}
	}
}
