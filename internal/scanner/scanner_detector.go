package scanner

// scanner_detector.go hosts the pluggable ChangeDetector interface plus
// the mtime-based implementation (and its secret-redaction helpers). The
// hash-based detector from nex-cli is NOT ported — v1.1 only needs mtime
// semantics for the HTTP endpoints and hook-driven re-scans. Re-adding
// hash detection later is a matter of wiring a second struct here.

import (
	"fmt"
	"io/fs"
	"os"
	"strings"
)

// ChangeDetector decides whether a file needs re-ingestion and records
// success after the fact. Mirrors the TS ChangeDetector interface.
type ChangeDetector interface {
	IsChanged(absolutePath string, info fs.FileInfo) bool
	MarkIngested(absolutePath string, info fs.FileInfo, context string)
	Save() error
}

// MtimeChangeDetector is the mtime+size detector used by the HTTP scan API.
// Cheaper than hashing and good enough for the idempotency contract.
type MtimeChangeDetector struct {
	manifest *ScanManifest
}

// NewMtimeChangeDetector loads the on-disk manifest and returns a detector.
func NewMtimeChangeDetector() (*MtimeChangeDetector, error) {
	m, err := ReadScanManifest()
	if err != nil {
		return nil, fmt.Errorf("scanner: read manifest: %w", err)
	}
	return &MtimeChangeDetector{manifest: m}, nil
}

func (d *MtimeChangeDetector) IsChanged(absolutePath string, info fs.FileInfo) bool {
	return d.manifest.IsChanged(absolutePath, info)
}

func (d *MtimeChangeDetector) MarkIngested(absolutePath string, info fs.FileInfo, ctx string) {
	d.manifest.MarkIngested(absolutePath, info, ctx)
}

func (d *MtimeChangeDetector) Save() error {
	return WriteScanManifest(d.manifest)
}

// Roots returns every scan root recorded in the manifest.
func (d *MtimeChangeDetector) Roots() []string {
	return d.manifest.Roots()
}

// HasRoot reports whether root has been scanned before (any manifest entry
// lives under that root). Gates the human-confirmation flow.
func (d *MtimeChangeDetector) HasRoot(root string) bool {
	return d.manifest.HasRoot(root)
}

// --- Secret redaction ---

// Redaction thresholds. A file with more than maxRedactionsPerFile matches
// is treated as a probable secret file and skipped wholesale.
const maxRedactionsPerFile = 3

// RedactionReason records why a particular redaction fired. The scanner
// surfaces these through the skip log so humans can trace a quarantine
// back to the exact rule (`pattern: aws-access-key`) or heuristic
// (`entropy: ~5.43 bits`).
type RedactionReason struct {
	// Kind is either "pattern" or "entropy".
	Kind string
	// Name is the pattern Name for Kind=="pattern", empty for entropy.
	Name string
	// Bits is the measured entropy for Kind=="entropy", 0 for patterns.
	Bits float64
}

// RedactionResult bundles the scrubbed body with everything the caller
// needs to decide whether to ingest, skip, or log the file.
type RedactionResult struct {
	Content string
	// PatternHits is the number of known-pattern matches redacted.
	PatternHits int
	// EntropyHits is the number of entropy-heuristic redactions.
	EntropyHits int
	// Poisoned is true when at least one match came from a pattern with
	// Poison=true — the caller MUST skip the file regardless of count.
	Poisoned bool
	// Reasons is ordered by first appearance and deduplicated by Name
	// (or by a synthetic "entropy" key). Useful for human-readable logs.
	Reasons []RedactionReason
}

// Matches is the total of pattern + entropy hits. Kept for symmetry with
// the old `redactSecrets` return, which callers use to gate the file-
// level skip.
func (r RedactionResult) Matches() int {
	return r.PatternHits + r.EntropyHits
}

// redactSecrets scrubs known-format tokens first, then sweeps the
// remainder with the Shannon-entropy heuristic. Returns the redacted
// content, the total hit count, and a breakdown the caller can log.
//
// The two-pass ordering matters: pattern redaction replaces known tokens
// with `[REDACTED]` markers BEFORE the entropy pass runs, so entropy can
// never double-count a pattern we already caught. The markers themselves
// are short and low-entropy so they don't trip the heuristic.
func redactSecrets(content string) (string, int) {
	res := redactSecretsDetailed(content)
	return res.Content, res.Matches()
}

// redactSecretsDetailed is the full-fidelity variant. See redactSecrets
// for the caller-visible summary.
func redactSecretsDetailed(content string) RedactionResult {
	out := content
	var reasons []RedactionReason
	seenPattern := map[string]struct{}{}
	patternHits := 0
	poisoned := false

	// Pass 1: known patterns. Replace each match with `[REDACTED]` and
	// record the first occurrence of each pattern name.
	for _, sp := range secretPatterns {
		p := sp
		out = p.Pattern.ReplaceAllStringFunc(out, func(_ string) string {
			patternHits++
			if p.Poison {
				poisoned = true
			}
			if _, seen := seenPattern[p.Name]; !seen {
				seenPattern[p.Name] = struct{}{}
				reasons = append(reasons, RedactionReason{Kind: "pattern", Name: p.Name})
			}
			return "[REDACTED]"
		})
	}

	// Pass 2: entropy. Only redact tokens that survived pass 1 — we do
	// not want to re-flag the `[REDACTED]` marker or anything already
	// caught. `[REDACTED]` is short enough to miss minCandidateLength
	// anyway, but we stay defensive by keying off surviving tokens.
	hits := detectEntropyHits(out)
	entropyHits := 0
	entropyReasonRecorded := false
	for _, h := range hits {
		// Skip anything that is literally the sentinel marker.
		if strings.Contains(h.Token, "[REDACTED]") {
			continue
		}
		out = strings.ReplaceAll(out, h.Token, "[REDACTED]")
		entropyHits++
		if !entropyReasonRecorded {
			reasons = append(reasons, RedactionReason{Kind: "entropy", Bits: h.Bits})
			entropyReasonRecorded = true
		}
		if entropyHits >= maxEntropyHitsPerFile {
			// Stop further entropy redactions; the file-level skip will
			// catch this once the caller sees Matches() over the cap.
			break
		}
	}

	return RedactionResult{
		Content:     out,
		PatternHits: patternHits,
		EntropyHits: entropyHits,
		Poisoned:    poisoned,
		Reasons:     reasons,
	}
}

// formatRedactionReasons renders Reasons as a stable, log-friendly
// string: `pattern: openai, pattern: aws-access-key, entropy: ~5.43 bits`.
// Empty input yields "unknown" so the log line always has a cause.
func formatRedactionReasons(reasons []RedactionReason) string {
	if len(reasons) == 0 {
		return "unknown"
	}
	parts := make([]string, 0, len(reasons))
	for _, r := range reasons {
		switch r.Kind {
		case "pattern":
			parts = append(parts, fmt.Sprintf("pattern: %s", r.Name))
		case "entropy":
			parts = append(parts, fmt.Sprintf("entropy: ~%.2f bits", r.Bits))
		}
	}
	return strings.Join(parts, ", ")
}

// --- Extension loader ---

// defaultExtensions is the v1.1 prose-only allowlist. Overridable via
// WUPHF_SCAN_EXTENSIONS (comma-separated, leading "." optional).
var defaultExtensions = []string{".md", ".txt", ".rst", ".org", ".adoc"}

// LoadScanExtensions returns the configured allowlist. Order: caller-
// provided > env var > defaults. Leading "." is optional in env-supplied
// values.
func LoadScanExtensions(override []string) map[string]struct{} {
	var list []string
	switch {
	case len(override) > 0:
		list = override
	default:
		if env := strings.TrimSpace(os.Getenv("WUPHF_SCAN_EXTENSIONS")); env != "" {
			for _, raw := range strings.Split(env, ",") {
				raw = strings.TrimSpace(raw)
				if raw == "" {
					continue
				}
				list = append(list, raw)
			}
		}
		if len(list) == 0 {
			list = defaultExtensions
		}
	}
	set := make(map[string]struct{}, len(list))
	for _, e := range list {
		e = strings.ToLower(strings.TrimSpace(e))
		if e == "" {
			continue
		}
		if !strings.HasPrefix(e, ".") {
			e = "." + e
		}
		set[e] = struct{}{}
	}
	return set
}
