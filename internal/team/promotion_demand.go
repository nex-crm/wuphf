package team

// promotion_demand.go is PR 3 of the notebook-wiki-promise design
// (~/.gstack/projects/nex-crm-wuphf/najmuzzaman-main-design-20260505-131620-notebook-wiki-promise.md).
//
// It defines the cross-agent demand signal that drives auto-promotion of
// notebook entries to the team wiki. When agent A searches agent B's notebook
// shelf and gets a hit, the broker emits a PromotionDemandEvent. The
// NotebookDemandIndex aggregates these events on a 7-day sliding window with
// per-(entry, searcher, day) deduplication. Entries that breach the threshold
// (default 3.0) are auto-escalated as pending ReviewLog promotions.
//
// Contract for PRs 4 and 5:
//   - PR 4 (CEO ranking) records DemandSignalCEOReviewFlag (weight +1.5).
//   - PR 5 (channel context-ask) records DemandSignalChannelContextAsk (+2.0).
// Both call idx.Record(evt) with the appropriate Signal field. Weights live
// in signalWeight() — never hardcoded inside Record.
//
// Lock discipline:
//   - NotebookDemandIndex has its own idx.mu. Methods NEVER call b.mu.
//   - The broker hook (recordNotebookDemandAsync) calls Record outside b.mu.
//   - AutoEscalateDemandCandidates acquires idx.mu for reads, then calls
//     ReviewLog.Submit (which has its own mutex). Never holds idx.mu while
//     calling external code that might re-acquire idx.mu.

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// PromotionDemandSignal enumerates the wire-format demand signal kinds. New
// signal types added by future PRs MUST extend this enum and the corresponding
// signalWeight() mapping; do not invent ad-hoc weights at call sites.
type PromotionDemandSignal int

const (
	// DemandSignalCrossAgentSearch fires when one agent searches another's
	// notebook shelf and gets a hit. PR 3 wiring.
	DemandSignalCrossAgentSearch PromotionDemandSignal = iota
	// DemandSignalChannelContextAsk fires when a channel context-ask
	// classifier matches and a notebook search returns hits. PR 5 wiring.
	DemandSignalChannelContextAsk
	// DemandSignalCEOReviewFlag fires when the CEO explicitly flags an
	// entry via team_notebook_review. PR 4 wiring.
	DemandSignalCEOReviewFlag
	// DemandSignalRejectionCooldown applies a negative weight when a prior
	// promotion attempt was rejected, suppressing re-escalation for the
	// 7-day window default.
	DemandSignalRejectionCooldown
)

// PromotionDemandEvent is the JSONL record persisted to
// <wiki_root>/.promotion-demand/events.jsonl. Field tags are stable wire
// format; renaming any field is a breaking change.
type PromotionDemandEvent struct {
	EntryPath    string                `json:"entry_path"`
	OwnerSlug    string                `json:"owner_slug"`
	SearcherSlug string                `json:"searcher_slug"`
	Signal       PromotionDemandSignal `json:"signal"`
	RecordedAt   time.Time             `json:"recorded_at"`
}

// DemandCandidate is the aggregated view of one entry's current rolling score
// returned by TopCandidates.
type DemandCandidate struct {
	EntryPath string                `json:"entry_path"`
	OwnerSlug string                `json:"owner_slug"`
	Score     float64               `json:"score"`
	TopSignal PromotionDemandSignal `json:"top_signal"`
}

// promotionDemandReader is the slice of WikiWorker (or fake in tests) that
// AutoEscalateDemandCandidates needs to validate that a candidate path still
// exists on disk before submitting it as a promotion. This avoids escalating
// stale or hallucinated paths.
type promotionDemandReader interface {
	NotebookRead(path string) ([]byte, error)
}

// ErrPromotionDemandInvalid is returned by Record when the event is malformed.
var ErrPromotionDemandInvalid = errors.New("promotion_demand: invalid event")

// signalWeight maps signal kind → score weight. Single source of truth.
// Add new signals here; do NOT scatter constants at call sites.
func signalWeight(s PromotionDemandSignal) float64 {
	switch s {
	case DemandSignalCrossAgentSearch:
		return 1.0
	case DemandSignalChannelContextAsk:
		return 2.0
	case DemandSignalCEOReviewFlag:
		return 1.5
	case DemandSignalRejectionCooldown:
		return -2.0
	}
	return 0
}

// PromotionDemandSignalLabel is the exported alias of signalLabel. PR 4
// (teammcp.team_notebook_review) needs the rendered label string for the
// CEO-facing JSON, and the teammcp package can't see unexported helpers.
func PromotionDemandSignalLabel(s PromotionDemandSignal) string {
	return signalLabel(s)
}

// signalLabel renders a PromotionDemandSignal as a stable string for snapshot
// comparisons and rationale strings.
func signalLabel(s PromotionDemandSignal) string {
	switch s {
	case DemandSignalCrossAgentSearch:
		return "cross_agent_search"
	case DemandSignalChannelContextAsk:
		return "channel_context_ask"
	case DemandSignalCEOReviewFlag:
		return "ceo_review_flag"
	case DemandSignalRejectionCooldown:
		return "rejection_cooldown"
	}
	return "unknown"
}

// demandWindowDefaultDays is the rolling window over which events are summed.
const demandWindowDefaultDays = 7

// demandThresholdDefault is the minimum score that triggers auto-escalation.
const demandThresholdDefault = 3.0

// envDemandWindow names the env var that overrides the rolling-window size.
const envDemandWindow = "WUPHF_PROMOTION_DEMAND_WINDOW_DAYS"

// envDemandThreshold names the env var that overrides the auto-escalation
// threshold.
const envDemandThreshold = "WUPHF_PROMOTION_DEMAND_THRESHOLD"

// demandRecordKey is the dedupe identity for an in-memory event: same searcher
// hitting same entry on same UTC day collapses into one record.
type demandRecordKey struct {
	EntryPath    string
	SearcherSlug string
	Day          string // "YYYY-MM-DD" UTC
	Signal       PromotionDemandSignal
}

// demandRecord stores one deduped event with its owner slug for later
// score reconstruction (we need OwnerSlug when surfacing candidates).
type demandRecord struct {
	OwnerSlug  string
	Signal     PromotionDemandSignal
	RecordedAt time.Time
}

// NotebookDemandIndex aggregates PromotionDemandEvents into per-entry rolling
// scores backed by an append-only JSONL log. All methods are safe for
// concurrent callers.
type NotebookDemandIndex struct {
	logPath    string
	windowDays int
	threshold  float64

	mu sync.Mutex
	// events keyed by entry_path → dedupe-key → record. Two-level map keeps
	// per-entry score computation O(events-for-entry) without scanning the
	// global set.
	events map[string]map[demandRecordKey]demandRecord
	// escalated tracks entry paths that have already been submitted to the
	// review log. Prevents AutoEscalateDemandCandidates from re-submitting
	// the same path on every tick.
	escalated map[string]struct{}
	clock     func() time.Time

	// progressCond signals tests that an event was recorded so they can
	// wait deterministically without sleep loops. Same pattern as
	// AutoNotebookWriter.
	progressMu   sync.Mutex
	progressCond *sync.Cond
}

// NewNotebookDemandIndex opens (or creates) the JSONL log at logPath, replays
// existing events into the in-memory map, and returns a ready index. Window
// and threshold honour env overrides; invalid env values fall back to the
// defaults with a warn log.
func NewNotebookDemandIndex(logPath string) (*NotebookDemandIndex, error) {
	idx := &NotebookDemandIndex{
		logPath:    logPath,
		windowDays: resolveWindowDays(),
		threshold:  resolveThreshold(),
		events:     make(map[string]map[demandRecordKey]demandRecord),
		escalated:  make(map[string]struct{}),
		clock:      time.Now,
	}
	idx.progressCond = sync.NewCond(&idx.progressMu)
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		return nil, fmt.Errorf("promotion_demand: mkdir: %w", err)
	}
	if err := idx.replay(); err != nil {
		return nil, err
	}
	return idx, nil
}

// SetClockForTest overrides the clock used for window-expiry checks. Test-only.
func (idx *NotebookDemandIndex) SetClockForTest(clock func() time.Time) {
	if idx == nil || clock == nil {
		return
	}
	idx.mu.Lock()
	defer idx.mu.Unlock()
	idx.clock = clock
}

// Threshold returns the configured auto-escalation threshold. Useful for
// observability surfaces.
func (idx *NotebookDemandIndex) Threshold() float64 {
	if idx == nil {
		return demandThresholdDefault
	}
	idx.mu.Lock()
	defer idx.mu.Unlock()
	return idx.threshold
}

// WindowDays returns the configured sliding-window length in days.
func (idx *NotebookDemandIndex) WindowDays() int {
	if idx == nil {
		return demandWindowDefaultDays
	}
	idx.mu.Lock()
	defer idx.mu.Unlock()
	return idx.windowDays
}

func resolveWindowDays() int {
	raw := strings.TrimSpace(os.Getenv(envDemandWindow))
	if raw == "" {
		return demandWindowDefaultDays
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v <= 0 {
		log.Printf("promotion_demand: invalid %s=%q; using default %d", envDemandWindow, raw, demandWindowDefaultDays)
		return demandWindowDefaultDays
	}
	return v
}

func resolveThreshold() float64 {
	raw := strings.TrimSpace(os.Getenv(envDemandThreshold))
	if raw == "" {
		return demandThresholdDefault
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil || v <= 0 {
		log.Printf("promotion_demand: invalid %s=%q; using default %g", envDemandThreshold, raw, demandThresholdDefault)
		return demandThresholdDefault
	}
	return v
}

// truncateToDay returns the YYYY-MM-DD UTC string for the dedupe day bucket.
func truncateToDay(t time.Time) string {
	return t.UTC().Format("2006-01-02")
}

// Record persists a demand event (append to JSONL) and updates the in-memory
// score map. Same-day duplicates from the same searcher on the same entry
// collapse into one record.
func (idx *NotebookDemandIndex) Record(evt PromotionDemandEvent) error {
	if idx == nil {
		return ErrPromotionDemandInvalid
	}
	if strings.TrimSpace(evt.EntryPath) == "" {
		return fmt.Errorf("%w: entry_path is required", ErrPromotionDemandInvalid)
	}
	if strings.TrimSpace(evt.OwnerSlug) == "" {
		return fmt.Errorf("%w: owner_slug is required", ErrPromotionDemandInvalid)
	}
	if evt.RecordedAt.IsZero() {
		evt.RecordedAt = time.Now().UTC()
	} else {
		evt.RecordedAt = evt.RecordedAt.UTC()
	}
	key := demandRecordKey{
		EntryPath:    evt.EntryPath,
		SearcherSlug: evt.SearcherSlug,
		Day:          truncateToDay(evt.RecordedAt),
		Signal:       evt.Signal,
	}

	idx.mu.Lock()
	bucket, ok := idx.events[evt.EntryPath]
	if !ok {
		bucket = make(map[demandRecordKey]demandRecord)
		idx.events[evt.EntryPath] = bucket
	}
	if _, dup := bucket[key]; dup {
		idx.mu.Unlock()
		// Same searcher, same entry, same day, same signal → collapse.
		// Signal progress so tests waiting on Record completion (even on a
		// dup) make forward progress.
		idx.signalProgress()
		return nil
	}
	bucket[key] = demandRecord{
		OwnerSlug:  evt.OwnerSlug,
		Signal:     evt.Signal,
		RecordedAt: evt.RecordedAt,
	}
	err := idx.appendLocked(evt)
	idx.mu.Unlock()
	idx.signalProgress()
	return err
}

// signalProgress wakes any goroutine parked in WaitForCondition. Cheap.
func (idx *NotebookDemandIndex) signalProgress() {
	idx.progressMu.Lock()
	idx.progressCond.Broadcast()
	idx.progressMu.Unlock()
}

// WaitForCondition blocks until predicate returns true or ctx is cancelled.
// Test-only helper modelled on AutoNotebookWriter.WaitForCondition.
func (idx *NotebookDemandIndex) WaitForCondition(ctx context.Context, predicate func() bool) error {
	if idx == nil {
		return nil
	}
	if predicate() {
		return nil
	}
	cancelWatcher := make(chan struct{})
	defer close(cancelWatcher)
	go func() {
		select {
		case <-ctx.Done():
			idx.progressMu.Lock()
			idx.progressCond.Broadcast()
			idx.progressMu.Unlock()
		case <-cancelWatcher:
		}
	}()
	idx.progressMu.Lock()
	defer idx.progressMu.Unlock()
	for !predicate() {
		if err := ctx.Err(); err != nil {
			return err
		}
		idx.progressCond.Wait()
	}
	return nil
}

// Score returns the current rolling-window score for entryPath. Events older
// than windowDays are excluded.
func (idx *NotebookDemandIndex) Score(entryPath string) float64 {
	if idx == nil {
		return 0
	}
	idx.mu.Lock()
	defer idx.mu.Unlock()
	return idx.scoreLocked(entryPath)
}

func (idx *NotebookDemandIndex) scoreLocked(entryPath string) float64 {
	bucket, ok := idx.events[entryPath]
	if !ok {
		return 0
	}
	cutoff := idx.clock().UTC().Add(-time.Duration(idx.windowDays) * 24 * time.Hour)
	var total float64
	for _, rec := range bucket {
		if rec.RecordedAt.Before(cutoff) {
			continue
		}
		total += signalWeight(rec.Signal)
	}
	return total
}

// TopCandidates returns up to n candidates sorted by descending score. Entries
// with zero or negative score are excluded.
func (idx *NotebookDemandIndex) TopCandidates(n int) []DemandCandidate {
	if idx == nil || n <= 0 {
		return nil
	}
	idx.mu.Lock()
	defer idx.mu.Unlock()
	out := make([]DemandCandidate, 0, len(idx.events))
	cutoff := idx.clock().UTC().Add(-time.Duration(idx.windowDays) * 24 * time.Hour)
	for path, bucket := range idx.events {
		var score float64
		var topSignal PromotionDemandSignal
		var topWeight float64
		var owner string
		for _, rec := range bucket {
			if rec.RecordedAt.Before(cutoff) {
				continue
			}
			w := signalWeight(rec.Signal)
			score += w
			if w > topWeight {
				topWeight = w
				topSignal = rec.Signal
			}
			if owner == "" {
				owner = rec.OwnerSlug
			}
		}
		if score <= 0 {
			continue
		}
		out = append(out, DemandCandidate{
			EntryPath: path,
			OwnerSlug: owner,
			Score:     score,
			TopSignal: topSignal,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].EntryPath < out[j].EntryPath
	})
	if len(out) > n {
		out = out[:n]
	}
	return out
}

// AutoEscalateDemandCandidates submits any entry whose rolling score has
// breached the threshold to the review log as a new pending promotion.
// Idempotent: an entry already escalated (in-memory tracker) is not
// re-submitted, and entries whose path no longer exists on disk are skipped.
func (idx *NotebookDemandIndex) AutoEscalateDemandCandidates(
	ctx context.Context,
	reviewLog *ReviewLog,
	reader promotionDemandReader,
) error {
	if idx == nil || reviewLog == nil {
		return nil
	}
	_ = ctx // reserved for future cancellation; current path is fast.

	// Collect candidates above threshold under idx.mu, then release before
	// calling out to ReviewLog (which acquires its own mutex). Never hold
	// idx.mu across an external call.
	idx.mu.Lock()
	candidates := make([]DemandCandidate, 0, len(idx.events))
	cutoff := idx.clock().UTC().Add(-time.Duration(idx.windowDays) * 24 * time.Hour)
	for path, bucket := range idx.events {
		if _, already := idx.escalated[path]; already {
			continue
		}
		var score float64
		var topSignal PromotionDemandSignal
		var topWeight float64
		var owner string
		for _, rec := range bucket {
			if rec.RecordedAt.Before(cutoff) {
				continue
			}
			w := signalWeight(rec.Signal)
			score += w
			if w > topWeight {
				topWeight = w
				topSignal = rec.Signal
			}
			if owner == "" {
				owner = rec.OwnerSlug
			}
		}
		if score >= idx.threshold && owner != "" {
			candidates = append(candidates, DemandCandidate{
				EntryPath: path,
				OwnerSlug: owner,
				Score:     score,
				TopSignal: topSignal,
			})
		}
	}
	threshold := idx.threshold
	idx.mu.Unlock()

	// Pre-check ReviewLog for any prior pending/in-review/changes-requested
	// promotion on this source_path so we don't stack duplicates across
	// process restarts (in-memory escalated map is empty after replay).
	existing := pendingSourcePathSet(reviewLog)

	for _, c := range candidates {
		if _, dup := existing[c.EntryPath]; dup {
			// Mark as escalated locally so we skip it next time without
			// re-listing the review log.
			idx.markEscalated(c.EntryPath)
			continue
		}
		// Validate the entry path is still on disk before promoting. Stale
		// or hallucinated paths get skipped, not submitted.
		if reader != nil {
			if _, err := reader.NotebookRead(c.EntryPath); err != nil {
				log.Printf("promotion_demand: skipping missing entry %q: %v", c.EntryPath, err)
				idx.markEscalated(c.EntryPath)
				continue
			}
		}
		targetPath := autoPromoteTargetPath(c.EntryPath)
		rationale := fmt.Sprintf("auto-escalated by demand index: score=%.2f top_signal=%s threshold=%.2f",
			c.Score, signalLabel(c.TopSignal), threshold)
		_, err := reviewLog.SubmitPromotion(SubmitPromotionRequest{
			SourceSlug: c.OwnerSlug,
			SourcePath: c.EntryPath,
			TargetPath: targetPath,
			Rationale:  rationale,
		})
		if err != nil {
			log.Printf("promotion_demand: SubmitPromotion failed for %q: %v", c.EntryPath, err)
			continue
		}
		idx.markEscalated(c.EntryPath)
	}
	return nil
}

func (idx *NotebookDemandIndex) markEscalated(path string) {
	idx.mu.Lock()
	idx.escalated[path] = struct{}{}
	idx.mu.Unlock()
}

// pendingSourcePathSet returns the set of source_paths currently held by the
// ReviewLog in non-terminal state. Used to dedupe escalation across process
// restarts where the in-memory escalated map is empty.
func pendingSourcePathSet(rl *ReviewLog) map[string]struct{} {
	out := map[string]struct{}{}
	if rl == nil {
		return out
	}
	for _, p := range rl.List("all") {
		if p == nil {
			continue
		}
		switch p.State {
		case PromotionPending, PromotionInReview, PromotionChangesRequested, PromotionApproved:
			out[p.SourcePath] = struct{}{}
		}
	}
	return out
}

// autoPromoteTargetPath maps a notebook entry path to the proposed wiki team
// path. Format: team/{date-stem}-from-{slug}.md where date-stem is the
// notebook filename without extension. The reviewer can rename during approve;
// this is the proposed default.
func autoPromoteTargetPath(notebookPath string) string {
	base := filepath.Base(notebookPath)
	stem := strings.TrimSuffix(base, filepath.Ext(base))
	parts := strings.SplitN(notebookPath, "/", 4)
	slug := "agent"
	if len(parts) >= 2 && parts[0] == "agents" {
		slug = parts[1]
	}
	return fmt.Sprintf("team/%s-from-%s.md", stem, slug)
}

// appendLocked persists one event to the JSONL log. Caller holds idx.mu.
// The file is opened in append mode for each call — the broker is single-process
// so per-call open/close is acceptable for the expected ≤50 events/day rate.
func (idx *NotebookDemandIndex) appendLocked(evt PromotionDemandEvent) error {
	line, err := json.Marshal(evt)
	if err != nil {
		return fmt.Errorf("promotion_demand: marshal: %w", err)
	}
	f, err := os.OpenFile(idx.logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("promotion_demand: open: %w", err)
	}
	defer func() { _ = f.Close() }()
	if _, err := f.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("promotion_demand: write: %w", err)
	}
	return nil
}

// replay rebuilds the in-memory score map from the JSONL log on startup.
// Malformed lines are SKIPPED with a warn log so a single corrupted record
// never costs the whole history. Events older than the window are dropped
// at replay time — they will not contribute to scores anyway, and keeping
// them in memory wastes bytes on a long-lived process.
func (idx *NotebookDemandIndex) replay() error {
	f, err := os.Open(idx.logPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil // empty log; nothing to replay.
		}
		return fmt.Errorf("promotion_demand: open for replay: %w", err)
	}
	defer func() { _ = f.Close() }()

	cutoff := idx.clock().UTC().Add(-time.Duration(idx.windowDays) * 24 * time.Hour)
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 4096), 1<<20)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var evt PromotionDemandEvent
		if err := json.Unmarshal(line, &evt); err != nil {
			log.Printf("promotion_demand: skipping malformed line: %v", err)
			continue
		}
		evt.RecordedAt = evt.RecordedAt.UTC()
		if evt.RecordedAt.Before(cutoff) {
			continue
		}
		key := demandRecordKey{
			EntryPath:    evt.EntryPath,
			SearcherSlug: evt.SearcherSlug,
			Day:          truncateToDay(evt.RecordedAt),
			Signal:       evt.Signal,
		}
		bucket, ok := idx.events[evt.EntryPath]
		if !ok {
			bucket = make(map[demandRecordKey]demandRecord)
			idx.events[evt.EntryPath] = bucket
		}
		bucket[key] = demandRecord{
			OwnerSlug:  evt.OwnerSlug,
			Signal:     evt.Signal,
			RecordedAt: evt.RecordedAt,
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("promotion_demand: replay scan: %w", err)
	}
	return nil
}

// recordNotebookDemandAsync is the broker-side helper invoked from
// handleNotebookSearch. Non-blocking: dispatches one Record call per (path,
// owner) pair on a small goroutine so the HTTP response is never delayed by
// JSONL writes. Safe to call with a nil index — no-op.
//
// Lock invariant: caller MUST NOT hold b.mu. The goroutine never re-enters
// b.mu and the index uses its own mutex.
func (b *Broker) recordNotebookDemandAsync(ownerSlug string, hitPaths []string, searcherSlug string) {
	if b == nil {
		return
	}
	idx := b.demandIndex
	if idx == nil || len(hitPaths) == 0 {
		return
	}
	ownerSlug = strings.TrimSpace(ownerSlug)
	searcherSlug = strings.TrimSpace(searcherSlug)
	if ownerSlug == "" || searcherSlug == "" || ownerSlug == searcherSlug {
		return
	}
	// Snapshot inputs so the caller's slice can be mutated without affecting
	// the goroutine.
	paths := make([]string, 0, len(hitPaths))
	seen := map[string]struct{}{}
	for _, p := range hitPaths {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if _, dup := seen[p]; dup {
			continue
		}
		seen[p] = struct{}{}
		paths = append(paths, p)
	}
	if len(paths) == 0 {
		return
	}
	now := time.Now().UTC()
	go func() {
		for _, path := range paths {
			evt := PromotionDemandEvent{
				EntryPath:    path,
				OwnerSlug:    ownerSlug,
				SearcherSlug: searcherSlug,
				Signal:       DemandSignalCrossAgentSearch,
				RecordedAt:   now,
			}
			if err := idx.Record(evt); err != nil {
				log.Printf("promotion_demand: Record failed path=%s: %v", path, err)
			}
		}
	}()
}
