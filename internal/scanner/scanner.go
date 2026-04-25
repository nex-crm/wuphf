package scanner

// scanner.go is the Scan entrypoint — it composes the walker
// (scanner_walk.go), the mtime detector + redaction (scanner_detector.go),
// and the on-disk manifest (scanner_manifest.go) into a single API the
// HTTP handler wires up.
//
// Port of nex-cli's ts/src/lib/file-scanner.ts, adapted for WUPHF:
//
//   - Symlinks are NOT followed (eng-review v1.1 divergence).
//   - Extension allowlist defaults to prose formats, overridable via env.
//   - Secret-redaction pass with a 3-match-per-file fail-closed skip.
//   - Size caps (per-file 10MB, per-scan 100MB, 1000 files).
//   - Human-confirmation gate on first scan of a new root.
//   - Ingested files land at team/inbox/raw/{source-slug}/<flat-name>.md.
//   - One atomic git commit via the scanner-identity helper on *Repo.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/nex-crm/wuphf/internal/config"
)

// ScannerSlug is the reserved git identity for scanner-authored commits.
// Distinct from wuphf-bootstrap / wuphf-recovery / system so audit tools
// can filter scanner ingestion out of human-authored history.
const ScannerSlug = "scanner"

// Size caps. Per eng-review v1.1: exceeding any cap aborts the scan.
const (
	maxScanFiles      = 1000
	maxScanFileBytes  = 10 * 1024 * 1024  // 10 MB per file
	maxScanTotalBytes = 100 * 1024 * 1024 // 100 MB per scan
)

// --- Errors ---

// ErrScanFileCountExceeded fires when discovery produces > maxScanFiles
// candidates. The scan aborts before any ingestion.
var ErrScanFileCountExceeded = fmt.Errorf("scanner: file count exceeds cap of %d", maxScanFiles)

// ErrScanFileTooLarge fires when a single file exceeds maxScanFileBytes.
var ErrScanFileTooLarge = fmt.Errorf("scanner: file exceeds %d bytes", maxScanFileBytes)

// ErrScanTotalTooLarge fires when accumulated bytes exceed maxScanTotalBytes.
var ErrScanTotalTooLarge = fmt.Errorf("scanner: total bytes exceed %d", maxScanTotalBytes)

// ErrScanConfirmationRequired is returned by Scan for a new root (no
// existing manifest entry). Callers must re-call with Confirm=true.
var ErrScanConfirmationRequired = fmt.Errorf("scanner: human confirmation required for new root")

// --- Types ---

// PreviewFile is a single row in a PreviewResult.
type PreviewFile struct {
	Path  string `json:"path"`
	Bytes int64  `json:"bytes"`
}

// PreviewResult is returned on the first call for a new root. It describes
// what *would* be ingested so the caller can get explicit user confirmation
// before reading file contents.
type PreviewResult struct {
	Root        string        `json:"root"`
	Files       []PreviewFile `json:"files"`
	TotalBytes  int64         `json:"total_bytes"`
	NeedsConfig bool          `json:"needs_confirmation"`
}

// ScanResult is returned on the confirmed (or idempotent) path.
type ScanResult struct {
	Root         string   `json:"root"`
	Scanned      int      `json:"scanned"`
	Ingested     int      `json:"ingested"`
	Skipped      int      `json:"skipped"`
	Redacted     int      `json:"redacted"`
	Errors       int      `json:"errors"`
	CommitSHA    string   `json:"commit_sha,omitempty"`
	IngestedPath []string `json:"ingested_paths,omitempty"`
}

// ScanOptions drive a single Scan call.
type ScanOptions struct {
	Root       string
	Extensions []string
	MaxDepth   int
	Confirm    bool
	Clock      func() time.Time // injected for tests; defaults to time.Now
}

// --- Scan entrypoint ---

// Scan walks the root, enforces size caps, applies the human-confirmation
// gate, redacts secrets, writes surviving files to team/inbox/raw/, and
// invokes commitFn for a single atomic commit. A nil commitFn skips the
// commit step (useful for dry runs and tests).
//
// First-scan flow (no manifest entry for root, Confirm=false):
//
//	→ returns ErrScanConfirmationRequired with a populated PreviewResult.
//
// Second-scan flow (Confirm=true, or manifest already has root):
//
//	→ executes the scan, returns a populated ScanResult.
//
// Idempotency: the mtime manifest is the source of truth. A re-scan with
// no file changes returns ScanResult{Ingested: 0} and produces no commit.
func Scan(
	ctx context.Context,
	opts ScanOptions,
	detector ChangeDetector,
	wikiRoot string,
	commitFn func(ctx context.Context, author, message string) (string, error),
) (*ScanResult, *PreviewResult, error) {
	if opts.Clock == nil {
		opts.Clock = time.Now
	}
	absRoot, err := filepath.Abs(opts.Root)
	if err != nil {
		return nil, nil, fmt.Errorf("scanner: resolve root: %w", err)
	}
	info, err := os.Stat(absRoot)
	if err != nil {
		return nil, nil, fmt.Errorf("scanner: stat root: %w", err)
	}
	if !info.IsDir() {
		return nil, nil, fmt.Errorf("scanner: root %q is not a directory", absRoot)
	}

	extSet := LoadScanExtensions(opts.Extensions)
	depth := opts.MaxDepth
	if depth <= 0 {
		depth = maxScanDepth
	}

	discovered := WalkDir(absRoot, absRoot, WalkOptions{
		Extensions: extSet,
		MaxDepth:   depth,
		IgnoreDirs: SkipDirs,
	})

	// File-count cap — abort before reading any content.
	if len(discovered) > maxScanFiles {
		return nil, nil, fmt.Errorf("%w: got %d", ErrScanFileCountExceeded, len(discovered))
	}

	// Enforce per-file and total size caps on the full discovered list,
	// even files we'd skip as unchanged. A 50GB markdown file on disk is
	// suspicious regardless of scan state.
	var totalBytes int64
	for _, f := range discovered {
		size := f.Info.Size()
		if size > maxScanFileBytes {
			return nil, nil, fmt.Errorf("%w: %s is %d bytes", ErrScanFileTooLarge, f.AbsolutePath, size)
		}
		totalBytes += size
	}
	if totalBytes > maxScanTotalBytes {
		return nil, nil, fmt.Errorf("%w: %d bytes across %d files", ErrScanTotalTooLarge, totalBytes, len(discovered))
	}

	// Human-confirmation gate.
	newRoot := !rootAlreadyScanned(detector, absRoot)
	if newRoot && !opts.Confirm {
		preview := &PreviewResult{
			Root:        absRoot,
			TotalBytes:  totalBytes,
			NeedsConfig: true,
			Files:       make([]PreviewFile, 0, len(discovered)),
		}
		for _, f := range discovered {
			preview.Files = append(preview.Files, PreviewFile{
				Path:  f.AbsolutePath,
				Bytes: f.Info.Size(),
			})
		}
		sort.Slice(preview.Files, func(i, j int) bool {
			return preview.Files[i].Path < preview.Files[j].Path
		})
		return nil, preview, ErrScanConfirmationRequired
	}

	// Confirmed path — apply mtime filter, then write surviving files.
	result := &ScanResult{
		Root:    absRoot,
		Scanned: len(discovered),
	}
	sourceSlug := slugifyRoot(absRoot)
	targetDir := filepath.Join(wikiRoot, "team", "inbox", "raw", sourceSlug)

	var writeOps []scanWriteOp
	for _, f := range discovered {
		if !detector.IsChanged(f.AbsolutePath, f.Info) {
			result.Skipped++
			continue
		}
		raw, err := os.ReadFile(f.AbsolutePath)
		if err != nil {
			result.Errors++
			continue
		}
		if strings.TrimSpace(string(raw)) == "" {
			result.Skipped++
			continue
		}
		redaction := redactSecretsDetailed(string(raw))
		matches := redaction.Matches()
		if redaction.Poisoned || matches > maxRedactionsPerFile {
			fmt.Fprintf(
				os.Stderr,
				"scanner: %s skipped — probable secrets file (%d matches; %s)\n",
				f.AbsolutePath, matches, formatRedactionReasons(redaction.Reasons),
			)
			result.Skipped++
			continue
		}
		if matches > 0 {
			result.Redacted++
		}
		redacted := redaction.Content
		dest := filepath.Join(targetDir, safeFilename(f.RelativePath))
		writeOps = append(writeOps, scanWriteOp{
			absSource: f.AbsolutePath,
			destPath:  dest,
			info:      f.Info,
			content:   redacted,
		})
	}

	if len(writeOps) == 0 {
		// Nothing changed. Persist the manifest anyway — IsChanged is
		// idempotent so this is safe and cheap.
		if err := detector.Save(); err != nil {
			return result, nil, fmt.Errorf("scanner: save manifest: %w", err)
		}
		return result, nil, nil
	}

	// The git commit is the atomic unit. Filesystem writes ahead of the
	// commit are fine: a crash between write and commit leaves the tree
	// dirty, which RecoverDirtyTree will fold into a recovery commit.
	if err := os.MkdirAll(targetDir, 0o700); err != nil {
		return result, nil, fmt.Errorf("scanner: mkdir target: %w", err)
	}
	for _, op := range writeOps {
		if err := os.MkdirAll(filepath.Dir(op.destPath), 0o700); err != nil {
			return result, nil, fmt.Errorf("scanner: mkdir %s: %w", filepath.Dir(op.destPath), err)
		}
		if err := os.WriteFile(op.destPath, []byte(op.content), 0o600); err != nil {
			return result, nil, fmt.Errorf("scanner: write %s: %w", op.destPath, err)
		}
		detector.MarkIngested(op.absSource, op.info, fmt.Sprintf("scanner:%s", absRoot))
		result.Ingested++
		result.IngestedPath = append(result.IngestedPath, op.destPath)
	}

	// Save manifest BEFORE committing so a crash between save and commit
	// still reflects reality on next run.
	if err := detector.Save(); err != nil {
		return result, nil, fmt.Errorf("scanner: save manifest: %w", err)
	}

	if commitFn != nil {
		msg := fmt.Sprintf("scanner: ingested %d files from %s", result.Ingested, absRoot)
		sha, err := commitFn(ctx, ScannerSlug, msg)
		if err != nil {
			return result, nil, fmt.Errorf("scanner: commit: %w", err)
		}
		result.CommitSHA = sha
	}

	return result, nil, nil
}

// scanWriteOp is an internal batch item for the post-filter write loop.
type scanWriteOp struct {
	absSource string
	destPath  string
	info      os.FileInfo
	content   string
}

// --- helpers ---

// safeSlugReplacer strips everything outside [a-z0-9-_]. Used on the scan
// root to produce a filesystem-safe source-slug for team/inbox/raw/<slug>/.
var safeSlugReplacer = regexp.MustCompile(`[^a-z0-9-_]+`)

func slugifyRoot(root string) string {
	cleaned := strings.ToLower(filepath.Clean(root))
	cleaned = strings.TrimPrefix(cleaned, "/")
	cleaned = safeSlugReplacer.ReplaceAllString(cleaned, "-")
	cleaned = strings.Trim(cleaned, "-")
	if cleaned == "" {
		return "root"
	}
	if len(cleaned) > 100 {
		cleaned = cleaned[:100]
	}
	return cleaned
}

// safeFilename converts a relative path to a filesystem-safe filename with
// a .md suffix. We flatten with double underscores so the inbox stays one
// directory deep per source — simpler to delete, simpler to audit.
func safeFilename(rel string) string {
	flat := filepath.ToSlash(rel)
	flat = strings.ReplaceAll(flat, "/", "__")
	if strings.ToLower(filepath.Ext(flat)) != ".md" {
		flat += ".md"
	}
	return flat
}

// rootAlreadyScanned checks the detector's manifest for any entry under
// root. A new-root first-scan triggers the confirmation gate.
func rootAlreadyScanned(detector ChangeDetector, root string) bool {
	mt, ok := detector.(*MtimeChangeDetector)
	if !ok {
		// Unknown detector — assume caller wants the permissive path.
		// Tests that inject a mock detector opt out of the gate by default.
		return true
	}
	return mt.HasRoot(root)
}

// ScannerWikiRoot returns the wiki root honouring WUPHF_RUNTIME_HOME so
// dev runs stay isolated from the prod ~/.wuphf.
func ScannerWikiRoot() string {
	home := strings.TrimSpace(config.RuntimeHomeDir())
	if home == "" {
		return filepath.Join(".wuphf", "wiki")
	}
	return filepath.Join(home, ".wuphf", "wiki")
}
