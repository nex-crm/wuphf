package team

// scanner_detector.go hosts the pluggable ChangeDetector interface plus
// the mtime-based implementation (and its secret-redaction helpers). The
// hash-based detector from nex-cli is NOT ported — v1.1 only needs mtime
// semantics for the HTTP endpoints and hook-driven re-scans. Re-adding
// hash detection later is a matter of wiring a second struct here.

import (
	"fmt"
	"io/fs"
	"os"
	"regexp"
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

// secretPatterns sweep obvious token shapes before ingestion. These are
// the same patterns documented in the v1.1 eng review. Keep the regexes
// conservative — false positives are cheap (a [REDACTED] marker in prose)
// but false negatives can leak credentials into the wiki forever.
var secretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`sk-[A-Za-z0-9_-]{20,}`),        // OpenAI-style
	regexp.MustCompile(`AKIA[0-9A-Z]{16}`),             // AWS access keys
	regexp.MustCompile(`Bearer [A-Za-z0-9_.=\-]{10,}`), // Bearer tokens
	regexp.MustCompile(`[A-Z_]+_API_KEY=\S+`),          // env-style keys
	regexp.MustCompile(`ghp_[A-Za-z0-9]{36}`),          // GitHub PATs
}

// redactSecrets returns (redacted, matches). Callers must discard the
// file entirely when matches > maxRedactionsPerFile.
func redactSecrets(content string) (string, int) {
	total := 0
	out := content
	for _, re := range secretPatterns {
		out = re.ReplaceAllStringFunc(out, func(_ string) string {
			total++
			return "[REDACTED]"
		})
	}
	return out, total
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
