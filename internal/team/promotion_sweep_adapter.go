package team

// promotion_sweep_adapter.go wires PR 3's NotebookDemandIndex and PR 1's
// AutoNotebookWriter into the PromotionSweep contract. Kept in a
// separate file so promotion_sweep.go has zero broker imports — the
// sweep itself is purely an orchestrator and depends on the
// promotionEscalator and notebookCounter interfaces only.
//
// Lock invariants: the adapter methods acquire ONLY the locks of the
// primitives they delegate to. No b.mu is acquired here.

import (
	"context"
	"time"
)

// demandIndexEscalator adapts NotebookDemandIndex + ReviewLog +
// promotionDemandReader (typically the WikiWorker) to the
// promotionEscalator interface consumed by PromotionSweep.
//
// The review log is resolved through a callback rather than a captured
// pointer because broker_wiki_lifecycle.go wires the demand index
// (and starts the sweep) before the review log is constructed. The
// callback closes over Broker.ReviewLog so the first tick that fires
// after ensureReviewLog completes picks up the live review log
// without restarting the sweep.
type demandIndexEscalator struct {
	idx         *NotebookDemandIndex
	reviewLogFn func() *ReviewLog
	reader      promotionDemandReader
}

// newDemandIndexEscalator binds the three primitives the sweep depends
// on. The reviewLogFn callback is called once per AutoEscalate so a
// review log that comes online mid-run is picked up on the next tick.
// Any of the inputs may be nil — the escalator gracefully no-ops.
func newDemandIndexEscalator(idx *NotebookDemandIndex, reviewLogFn func() *ReviewLog, reader promotionDemandReader) *demandIndexEscalator {
	return &demandIndexEscalator{idx: idx, reviewLogFn: reviewLogFn, reader: reader}
}

func (e *demandIndexEscalator) AutoEscalate(ctx context.Context) error {
	if e == nil || e.idx == nil {
		return nil
	}
	var rl *ReviewLog
	if e.reviewLogFn != nil {
		rl = e.reviewLogFn()
	}
	return e.idx.AutoEscalateDemandCandidates(ctx, rl, e.reader)
}

func (e *demandIndexEscalator) CandidateCount() int {
	if e == nil || e.idx == nil {
		return 0
	}
	// A generous cap is fine here — TopCandidates is O(events) and the
	// office-scale demand index never holds more than a few hundred
	// entries within the rolling window.
	return len(e.idx.TopCandidates(1024))
}

// NearThresholdCount counts entries whose rolling score is at or above
// 80% of the auto-escalation threshold. These are the entries one or
// two demand events away from tripping promotion; their density is
// what drives demand-pressure cadence escalation.
func (e *demandIndexEscalator) NearThresholdCount() int {
	if e == nil || e.idx == nil {
		return 0
	}
	threshold := e.idx.Threshold()
	if threshold <= 0 {
		return 0
	}
	floor := threshold * 0.8
	count := 0
	for _, c := range e.idx.TopCandidates(1024) {
		if c.Score >= floor && c.Score < threshold {
			count++
		}
	}
	return count
}

// autoWriterNotebookCounter adapts AutoNotebookWriter to the
// notebookCounter interface. The "written" counter is monotonic and
// the most recent notebook entry's modified time provides the
// last-commit timestamp via the writer's progress signalling.
//
// We approximate "last commit time" as wall-clock-of-last-write by
// recording a per-call timestamp on the writer's written counter
// transition; rather than touching the writer, we just read the
// counter value and stash the current clock at sweep time. The sweep
// only needs a "did anything change since last tick?" signal — the
// counter delta alone is sufficient. The timestamp field is preserved
// in the interface for future use (e.g. age-based gates).
type autoWriterNotebookCounter struct {
	writer *AutoNotebookWriter
}

func newAutoWriterNotebookCounter(w *AutoNotebookWriter) *autoWriterNotebookCounter {
	return &autoWriterNotebookCounter{writer: w}
}

func (a *autoWriterNotebookCounter) NotebookCommitCount() int {
	if a == nil || a.writer == nil {
		return 0
	}
	return int(a.writer.Counters().Written)
}

// NotebookLastCommitTime is provided for parity with the
// notebookCounter interface but the real implementation relies on the
// Written counter delta as the dominant change signal. Returning the
// zero time keeps the gate's Equal() comparison stable across ticks
// when no writes have happened. Future PRs can plumb a real
// last-commit timestamp from the wiki repo if age-based logic is
// added.
func (a *autoWriterNotebookCounter) NotebookLastCommitTime() time.Time {
	return time.Time{}
}
