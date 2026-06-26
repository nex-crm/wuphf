package packer

import "github.com/nex-crm/wuphf/internal/scanner"

// SecretScanner is the redaction seam at the egress boundary. The default
// implementation wraps the fail-closed scanner.RedactForEgress; cloud can swap a
// tenant-specific scanner. A scan that is not OK means the content could not be
// proven clean and MUST NOT be emitted — the caller drops the item or holds the
// whole delegation, never sends the partially-scrubbed text.
type SecretScanner interface {
	Scan(content string) ScanResult
}

// ScanResult is the outcome of an egress scan.
type ScanResult struct {
	Content    string   // safe to emit ONLY when OK
	OK         bool     // false => withhold; could not be proven clean
	Redactions int      // number of secrets/PII tokens scrubbed
	Reason     string   // "" when OK; else "poisoned" | "entropy-cap"
	Reasons    []string // human/audit-facing redaction labels
}

// EgressScanner is the default SecretScanner, backed by the fail-closed
// scanner.RedactForEgress. Empty input is trivially clean.
type EgressScanner struct{}

// Scan runs the fail-closed egress redactor and adapts its result.
func (EgressScanner) Scan(content string) ScanResult {
	if content == "" {
		return ScanResult{OK: true}
	}
	r := scanner.RedactForEgress(content)
	return ScanResult{
		Content:    r.Content,
		OK:         r.Allowed,
		Redactions: r.Redactions,
		Reason:     r.DenyReason,
		Reasons:    r.Reasons,
	}
}
