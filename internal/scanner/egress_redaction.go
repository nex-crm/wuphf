package scanner

// egress_redaction.go adds a FAIL-CLOSED redaction contract on top of the
// display redactor, for the Slack context-packer's egress boundary.
//
// The display path (RedactSecretsForDisplay) is best-effort: it returns scrubbed
// text even when the scanner hit a limit, because a chat message with a few
// surviving high-entropy tokens is a tolerable display risk. That is the WRONG
// default when the text is leaving the office brain to a foreign LLM we do not
// control. A result that is Poisoned, or that reached the entropy-hit cap and
// stopped scanning early, cannot be proven clean — so the content must be
// WITHHELD, not emitted partially scrubbed. See
// docs/specs/slack-context-packer.md (Egress boundary).

// EgressRedaction is the result of running content through the fail-closed
// egress redactor. Content is safe to emit ONLY when Allowed is true.
type EgressRedaction struct {
	// Content is the redacted text. It is the empty string whenever Allowed is
	// false, so a caller can never accidentally emit partially-scrubbed bytes.
	Content string
	// Allowed reports whether the content may leave the brain.
	Allowed bool
	// Redactions is the total number of secret/PII tokens scrubbed.
	Redactions int
	// DenyReason is "" when Allowed, else one of: "poisoned", "entropy-cap".
	DenyReason string
	// Reasons are the human/audit-facing redaction labels (pattern names plus
	// the generic "entropy" label). Populated on both allow and deny so the
	// caller can record why an item was withheld.
	Reasons []string
}

// RedactForEgress is the fail-closed variant of RedactSecretsForDisplay. Use it
// for any brain content the context-packer ships to a foreign bot. It denies
// (emits no content) when:
//
//   - the scanner caught a poison-class secret (private key, service-account
//     JSON, aws secret) — Poisoned is set on the first such hit; or
//   - the entropy pass reached maxEntropyHitsPerFile and stopped early, so the
//     remainder of the text cannot be proven free of high-entropy secrets.
//
// Pattern redaction always runs to completion — every known pattern is applied —
// so a high pattern-hit count alone is NOT a deny signal; only an unproven
// remainder (entropy cap) or a poison hit is. The caller treats !Allowed as
// "drop this item / hold this delegation", never as "send the scrubbed text".
func RedactForEgress(content string) EgressRedaction {
	res := redactSecretsDetailed(content)
	out := EgressRedaction{
		Content:    res.Content,
		Redactions: res.Matches(),
		Reasons:    res.ReasonLabels(),
	}
	switch {
	case res.Poisoned:
		out.DenyReason = "poisoned"
	case res.EntropyHits >= maxEntropyHitsPerFile:
		out.DenyReason = "entropy-cap"
	default:
		out.Allowed = true
	}
	if !out.Allowed {
		// Never expose partially-scrubbed content on a deny. The whole point of
		// the egress boundary is that an unprovable item does not leave at all.
		out.Content = ""
	}
	return out
}
