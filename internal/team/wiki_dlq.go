package team

// wiki_dlq.go is the Dead-Letter Queue primitive for extraction failures.
//
// Design contract (WIKI-SCHEMA.md §11.13):
//   - Files are append-only on disk. Never rewritten.
//   - Successful replays write a {"artifact_sha":"...","resolved_at":"..."} tombstone.
//   - Promotions write {"artifact_sha":"...","promoted_at":"..."} in extractions.jsonl
//     and a full DLQEntry in permanent-failures.jsonl.
//   - ReadyForReplay scans the full file and skips tombstoned artifact_shas.
//     It uses the latest state per SHA (last-write-wins in file order).
//   - Backoff formula: min(10min × 2^retry_count, 6h).
//   - max_retries default: 5. error_category="validation" forces max_retries=1.
//
// This file is the primitive only. Cron scheduling is a separate concern
// (broker-level) and lives in a future commit.

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

// ── Constants ─────────────────────────────────────────────────────────────────

// DLQDefaultMaxRetries is the default retry ceiling before an entry is promoted
// to permanent-failures.jsonl.
const DLQDefaultMaxRetries = 5

// DLQValidationMaxRetries is the max retry ceiling for programming-error
// categories (validation): never retry past the first attempt.
const DLQValidationMaxRetries = 1

// dlqBaseBackoff is the unit backoff (10 min × 2^retry_count, capped at 6 h).
const dlqBaseBackoff = 10 * time.Minute

// dlqMaxBackoff is the ceiling of the exponential backoff window.
const dlqMaxBackoff = 6 * time.Hour

// ── Types ─────────────────────────────────────────────────────────────────────

// DLQErrorCategory describes the nature of the extraction failure.
// "validation" errors are never retried beyond the first attempt.
type DLQErrorCategory string

const (
	DLQCategoryParse           DLQErrorCategory = "parse"
	DLQCategoryProviderTimeout DLQErrorCategory = "provider_timeout"
	DLQCategoryValidation      DLQErrorCategory = "validation"
	// DLQCategoryFactLogPersist is the category for a fact-log JSONL append
	// that failed AFTER the extraction LLM call succeeded and SubmitFacts
	// applied the in-memory index mutation. These are local I/O / git /
	// queue-saturation failures — NOT provider timeouts — so they carry
	// their own category (different metrics bucket, same backoff curve)
	// and a dedicated replay path that re-tries the append without
	// re-running the LLM (which would skip reinforced rows and leave the
	// JSONL permanently missing). See §7.4 substrate guarantee.
	DLQCategoryFactLogPersist DLQErrorCategory = "fact_log_persist"
)

// FactLogAppendPayload carries the state needed to retry a failed fact-log
// JSONL append without re-running extraction. Populated on entries whose
// ErrorCategory is DLQCategoryFactLogPersist.
//
// The payload captures the exact JSONL lines the original append tried to
// write (one JSON-encoded TypedFact per line, newline-terminated) along with
// the target kind/slug and the originating artifact SHA — enough for the
// replay handler to reconstruct the EnqueueFactLogAppend call and
// deterministically dedupe by fact_id against the current on-disk file.
type FactLogAppendPayload struct {
	Kind        string `json:"kind"`
	Slug        string `json:"slug"`
	ArtifactSHA string `json:"artifact_sha"`
	// JSONLLines is the raw multi-line content the append was attempting to
	// write. Preserving the bytes verbatim keeps the content hash stable
	// across retries.
	JSONLLines string `json:"jsonl_lines"`
}

// FactLogAppendSHA synthesises the DLQ row key for a fact-log append failure.
// One artifact extraction can produce append failures for multiple
// (kind, slug) groups; using the raw artifact SHA would collide across them
// in readLatestStateLocked. The synthesized form
// "factlog:{kind}:{slug}:{artifactSHA}" is unique per target file, never
// collides with an extraction-class entry, and keeps the rest of the DLQ
// contract unchanged (tombstones, retry bookkeeping, last-write-wins).
func FactLogAppendSHA(kind, slug, artifactSHA string) string {
	return "factlog:" + kind + ":" + slug + ":" + artifactSHA
}

// DLQEntry is one row in wiki/.dlq/extractions.jsonl.
// All time fields are RFC3339 UTC strings on the wire.
type DLQEntry struct {
	ArtifactSHA        string           `json:"artifact_sha"`
	ArtifactPath       string           `json:"artifact_path"`
	Kind               string           `json:"kind"`
	LastError          string           `json:"last_error"`
	ErrorCategory      DLQErrorCategory `json:"error_category"`
	RetryCount         int              `json:"retry_count"`
	MaxRetries         int              `json:"max_retries"`
	FirstFailedAt      time.Time        `json:"first_failed_at"`
	LastAttemptedAt    time.Time        `json:"last_attempted_at"`
	NextRetryNotBefore time.Time        `json:"next_retry_not_before"`
	// FactLogAppend is populated only when ErrorCategory is
	// DLQCategoryFactLogPersist. Carries the state needed for the
	// append-specific replay path; unused/nil for extraction-class entries.
	FactLogAppend *FactLogAppendPayload `json:"fact_log_append,omitempty"`
}

// dlqTombstone is the append-only tombstone written when an entry is resolved
// or promoted. It carries exactly one of resolved_at or promoted_at.
type dlqTombstone struct {
	ArtifactSHA string    `json:"artifact_sha"`
	ResolvedAt  time.Time `json:"resolved_at,omitempty"`
	PromotedAt  time.Time `json:"promoted_at,omitempty"`
}

// DLQ owns all read/write access to wiki/.dlq/. It is safe for concurrent use.
//
// mu is an RWMutex: writers (Enqueue, RecordAttempt, MarkResolved) take the
// write lock; read-only callers (Inspect, ReadyForReplay) take the read
// lock. ReadyForReplay is semantically read-only — it does not modify any
// file — so it can safely hold RLock alongside concurrent Inspect calls
// from an operator dashboard.
type DLQ struct {
	root               string       // absolute path to wiki root; DLQ files live in <root>/.dlq/
	mu                 sync.RWMutex // guards all file mutations
	corruptLineCount   atomic.Uint64
	permCorruptLineCnt atomic.Uint64
}

// ── Constructor ───────────────────────────────────────────────────────────────

// NewDLQ constructs a DLQ rooted at the given wiki root. The .dlq/ sub-
// directory is created lazily on first write.
func NewDLQ(root string) *DLQ {
	return &DLQ{root: root}
}

// ── File paths ────────────────────────────────────────────────────────────────

func (d *DLQ) extractionsPath() string {
	return filepath.Join(d.root, ".dlq", "extractions.jsonl")
}

func (d *DLQ) permanentPath() string {
	return filepath.Join(d.root, ".dlq", "permanent-failures.jsonl")
}

func (d *DLQ) ensureDir() error {
	return os.MkdirAll(filepath.Dir(d.extractionsPath()), 0o755)
}

// ── Public API ────────────────────────────────────────────────────────────────

// Enqueue appends a new DLQEntry to extractions.jsonl. The entry's max_retries
// is coerced to DLQValidationMaxRetries when ErrorCategory is "validation".
// Callers should set FirstFailedAt and NextRetryNotBefore; if zero they are
// defaulted to now and now+base_backoff respectively.
func (d *DLQ) Enqueue(_ context.Context, e DLQEntry) error {
	now := time.Now().UTC()
	e = coerceDLQEntry(e, now)

	d.mu.Lock()
	defer d.mu.Unlock()

	if err := d.ensureDir(); err != nil {
		return fmt.Errorf("dlq: ensure dir: %w", err)
	}
	return appendLine(d.extractionsPath(), e)
}

// ReadyForReplay scans extractions.jsonl and returns every entry whose
// next_retry_not_before is ≤ now, has retry_count < max_retries, and has
// no tombstone (resolved_at or promoted_at).
//
// Only the latest state per artifact_sha is consulted (last-write-wins in
// append order), so successive RecordAttempt calls correctly reflect the
// updated backoff window rather than an old eligible row.
//
// Read-only: holds the read lock so concurrent Inspect calls do not block.
func (d *DLQ) ReadyForReplay(_ context.Context, now time.Time) ([]DLQEntry, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	latest, tombstones, err := d.readLatestStateLocked()
	if err != nil {
		return nil, err
	}

	var out []DLQEntry
	for _, e := range latest {
		if _, dead := tombstones[e.ArtifactSHA]; dead {
			continue
		}
		if e.RetryCount >= e.MaxRetries {
			continue
		}
		if now.Before(e.NextRetryNotBefore) {
			continue
		}
		out = append(out, e)
	}
	return out, nil
}

// RecordAttempt bumps retry_count, updates last_attempted_at and
// next_retry_not_before, and appends the updated state. If the bump crosses
// max_retries, the entry is promoted to permanent-failures. cat is the
// error category of the new attempt.
func (d *DLQ) RecordAttempt(_ context.Context, artifactSHA string, attemptErr error, cat string) error {
	now := time.Now().UTC()

	d.mu.Lock()
	defer d.mu.Unlock()

	latest, tombstones, err := d.readLatestStateLocked()
	if err != nil {
		return err
	}
	if _, dead := tombstones[artifactSHA]; dead {
		return fmt.Errorf("dlq: artifact %q is already tombstoned", artifactSHA)
	}

	current, found := latest[artifactSHA]
	if !found {
		return fmt.Errorf("dlq: artifact %q not found in extractions.jsonl", artifactSHA)
	}

	current.RetryCount++
	current.LastAttemptedAt = now
	if attemptErr != nil {
		current.LastError = attemptErr.Error()
	}
	if cat != "" {
		current.ErrorCategory = DLQErrorCategory(cat)
		if current.ErrorCategory == DLQCategoryValidation {
			current.MaxRetries = DLQValidationMaxRetries
		}
	}
	current.NextRetryNotBefore = now.Add(dlqBackoff(current.RetryCount))

	if current.RetryCount >= current.MaxRetries {
		// Promote to permanent failures.
		if err := d.ensureDir(); err != nil {
			return fmt.Errorf("dlq: ensure dir: %w", err)
		}
		if err := appendLine(d.permanentPath(), current); err != nil {
			return fmt.Errorf("dlq: write permanent failure: %w", err)
		}
		tombstone := dlqTombstone{ArtifactSHA: artifactSHA, PromotedAt: now}
		return appendLine(d.extractionsPath(), tombstone)
	}

	// Still within retry budget — append updated state.
	return appendLine(d.extractionsPath(), current)
}

// Snapshot is the read-only view of the DLQ returned by Inspect. It is
// safe to serialise to JSON for operator surfaces.
type Snapshot struct {
	// Pending are extraction entries that have not been tombstoned. They
	// may or may not be past their NextRetryNotBefore — callers that
	// need "ready now" should filter by NextRetryNotBefore ≤ now.
	Pending []DLQEntry `json:"pending"`
	// PermanentFailures are entries that crossed their max_retries and
	// were promoted to permanent-failures.jsonl. Append-only; callers
	// should treat the order as oldest-first (file order).
	PermanentFailures []DLQEntry `json:"permanent_failures"`
}

// Inspect returns a Snapshot of the current DLQ state. Read-only: does not
// append, tombstone, or mutate any file. Safe to call from an HTTP handler
// while the worker continues to enqueue and retry.
//
// Uses the read lock so multiple operator dashboards polling GET /wiki/dlq
// do not serialise on each other.
func (d *DLQ) Inspect(_ context.Context) (Snapshot, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	latest, tombstones, err := d.readLatestStateLocked()
	if err != nil {
		return Snapshot{}, fmt.Errorf("dlq: read latest: %w", err)
	}

	pending := make([]DLQEntry, 0, len(latest))
	for _, e := range latest {
		if _, dead := tombstones[e.ArtifactSHA]; dead {
			continue
		}
		pending = append(pending, e)
	}
	// Stable order by FirstFailedAt ascending so UI paginates predictably.
	sortEntriesByFirstFailedAt(pending)

	permanent, err := d.readPermanentLocked()
	if err != nil {
		return Snapshot{}, fmt.Errorf("dlq: read permanent: %w", err)
	}

	return Snapshot{
		Pending:           pending,
		PermanentFailures: permanent,
	}, nil
}

// readPermanentLocked scans permanent-failures.jsonl and returns every
// DLQEntry row in file order. Caller must hold d.mu (read or write).
//
// Lines that fail to unmarshal are logged and counted on
// d.permCorruptLineCnt so operators can distinguish "file is empty" from
// "file has corrupted rows". We do NOT abort the scan on a single bad row
// — the file is append-only and a single half-written line from a crash
// must not make the entire DLQ surface unreadable.
func (d *DLQ) readPermanentLocked() ([]DLQEntry, error) {
	f, err := os.Open(d.permanentPath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("open permanent-failures: %w", err)
	}
	defer func() { _ = f.Close() }()

	var out []DLQEntry
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 512*1024)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var e DLQEntry
		if err := json.Unmarshal(line, &e); err != nil {
			d.permCorruptLineCnt.Add(1)
			log.Printf("wiki_dlq: skipping corrupt permanent-failures.jsonl line %d: %v", lineNo, err)
			continue
		}
		if e.ArtifactSHA == "" {
			d.permCorruptLineCnt.Add(1)
			log.Printf("wiki_dlq: skipping permanent-failures.jsonl line %d: missing artifact_sha", lineNo)
			continue
		}
		out = append(out, e)
	}
	return out, scanner.Err()
}

// CorruptLineCounts returns the running tally of JSONL rows the DLQ has
// skipped because they failed to decode or were missing required fields.
// Operators poll this to distinguish "queue is empty" from "queue file is
// corrupted and silently losing entries". Counts are process-local and
// reset on restart — persistent counters are a Slice 3 concern.
func (d *DLQ) CorruptLineCounts() (extractions, permanent uint64) {
	return d.corruptLineCount.Load(), d.permCorruptLineCnt.Load()
}

// sortEntriesByFirstFailedAt orders in ascending FirstFailedAt so older
// failures list first. Stable against entries with identical timestamps
// (tie-break by ArtifactSHA).
func sortEntriesByFirstFailedAt(entries []DLQEntry) {
	// Small slice (bounded by active DLQ depth); insertion sort is fine.
	for i := 1; i < len(entries); i++ {
		for j := i; j > 0; j-- {
			a, b := entries[j-1], entries[j]
			if a.FirstFailedAt.After(b.FirstFailedAt) ||
				(a.FirstFailedAt.Equal(b.FirstFailedAt) && a.ArtifactSHA > b.ArtifactSHA) {
				entries[j-1], entries[j] = entries[j], entries[j-1]
			} else {
				break
			}
		}
	}
}

// MarkResolved appends a resolved_at tombstone. ReadyForReplay will skip this
// artifact_sha from now on.
func (d *DLQ) MarkResolved(_ context.Context, artifactSHA string) error {
	now := time.Now().UTC()
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.ensureDir(); err != nil {
		return fmt.Errorf("dlq: ensure dir: %w", err)
	}
	tombstone := dlqTombstone{ArtifactSHA: artifactSHA, ResolvedAt: now}
	return appendLine(d.extractionsPath(), tombstone)
}

// ── Internal helpers ──────────────────────────────────────────────────────────

// readLatestStateLocked scans extractions.jsonl and returns:
//   - latest: map[artifactSHA]DLQEntry with the most-recent state per SHA
//     (last-write-wins in append order — successive RecordAttempt calls
//     overwrite old eligibility windows).
//   - tombstones: set of artifact_shas that have been resolved or promoted.
//
// Caller must hold d.mu.
func (d *DLQ) readLatestStateLocked() (latest map[string]DLQEntry, tombstones map[string]struct{}, err error) {
	latest = make(map[string]DLQEntry)
	tombstones = make(map[string]struct{})

	f, err := os.Open(d.extractionsPath())
	if err != nil {
		if os.IsNotExist(err) {
			return latest, tombstones, nil
		}
		return nil, nil, fmt.Errorf("dlq: open extractions: %w", err)
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 512*1024)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		// Probe for tombstone markers first.
		var probe struct {
			ArtifactSHA string    `json:"artifact_sha"`
			ResolvedAt  time.Time `json:"resolved_at,omitempty"`
			PromotedAt  time.Time `json:"promoted_at,omitempty"`
		}
		if err := json.Unmarshal(line, &probe); err != nil {
			d.corruptLineCount.Add(1)
			log.Printf("wiki_dlq: skipping corrupt extractions.jsonl line %d: %v", lineNo, err)
			continue
		}
		if !probe.ResolvedAt.IsZero() || !probe.PromotedAt.IsZero() {
			tombstones[probe.ArtifactSHA] = struct{}{}
			continue
		}

		var entry DLQEntry
		if err := json.Unmarshal(line, &entry); err != nil {
			d.corruptLineCount.Add(1)
			log.Printf("wiki_dlq: skipping corrupt extractions.jsonl line %d: %v", lineNo, err)
			continue
		}
		if entry.ArtifactSHA == "" {
			d.corruptLineCount.Add(1)
			log.Printf("wiki_dlq: skipping extractions.jsonl line %d: missing artifact_sha", lineNo)
			continue
		}
		// Last-write-wins: overwrite any earlier state for this SHA.
		latest[entry.ArtifactSHA] = entry
	}
	if err := scanner.Err(); err != nil {
		return nil, nil, fmt.Errorf("dlq: scan extractions line %d: %w", lineNo, err)
	}
	return latest, tombstones, nil
}

// appendLine JSON-encodes v and appends it as a newline-terminated row.
func appendLine(path string, v any) error {
	line, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("dlq: marshal: %w", err)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("dlq: open for append %s: %w", filepath.Base(path), err)
	}
	defer func() { _ = f.Close() }()
	line = append(line, '\n')
	_, werr := f.Write(line)
	return werr
}

// coerceDLQEntry normalises defaults and applies policy constraints.
func coerceDLQEntry(e DLQEntry, now time.Time) DLQEntry {
	if e.MaxRetries <= 0 {
		e.MaxRetries = DLQDefaultMaxRetries
	}
	if e.ErrorCategory == DLQCategoryValidation {
		e.MaxRetries = DLQValidationMaxRetries
	}
	if e.FirstFailedAt.IsZero() {
		e.FirstFailedAt = now
	}
	if e.LastAttemptedAt.IsZero() {
		e.LastAttemptedAt = now
	}
	if e.NextRetryNotBefore.IsZero() {
		e.NextRetryNotBefore = now.Add(dlqBaseBackoff)
	}
	return e
}

// dlqBackoff returns min(10min × 2^retryCount, 6h). Saturates at the ceiling
// for any large retryCount so float overflow never produces a negative
// duration.
func dlqBackoff(retryCount int) time.Duration {
	if retryCount < 0 {
		retryCount = 0
	}
	// 2^16 × 10min ≈ 11.4 days, well past dlqMaxBackoff (6h). Past that
	// the float would overflow on conversion to int64.
	if retryCount > 16 {
		return dlqMaxBackoff
	}
	multiplier := math.Pow(2, float64(retryCount))
	dur := time.Duration(float64(dlqBaseBackoff) * multiplier)
	if dur < 0 || dur > dlqMaxBackoff {
		return dlqMaxBackoff
	}
	return dur
}
