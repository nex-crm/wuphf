package team

// stage_b_signals.go orchestrates the Stage B signal sources. It is the
// single seam the synthesizer (PR 2-B) reads from when it needs the
// SkillCandidate stream — Stage B reuses PR 1a-B's compile cron + manual
// button, so we do not register a separate scheduler here.

import (
	"context"
	"fmt"
	"log/slog"
)

// defaultStageBMaxTotal caps the number of candidates returned per
// aggregator pass when the caller passes maxTotal <= 0.
const defaultStageBMaxTotal = 15

// StageBSignalAggregator runs the notebook + self-heal signal scanners
// sequentially and returns their union, capped at maxTotal candidates.
type StageBSignalAggregator struct {
	broker *Broker

	notebookScanner *NotebookSignalScanner
	selfHealScanner *SelfHealSignalScanner
}

// NewStageBSignalAggregator wires the default scanners against the supplied
// broker. Tests may construct alternate scanners and assemble an aggregator
// directly via the exported fields.
func NewStageBSignalAggregator(b *Broker) *StageBSignalAggregator {
	return &StageBSignalAggregator{
		broker:          b,
		notebookScanner: NewNotebookSignalScanner(b),
		selfHealScanner: NewSelfHealSignalScanner(b),
	}
}

// Scan runs the notebook scanner first (clusters need a longer history to
// stabilise) then the self-heal scanner. Both errors are surfaced as a
// joined error so partial results are still usable. If maxTotal <= 0 we
// fall back to defaultStageBMaxTotal.
func (a *StageBSignalAggregator) Scan(ctx context.Context, maxTotal int) ([]SkillCandidate, error) {
	if a == nil {
		return nil, nil
	}
	if maxTotal <= 0 {
		maxTotal = defaultStageBMaxTotal
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("stage_b_signals: ctx cancelled: %w", err)
	}

	var (
		out  []SkillCandidate
		errs []error
	)

	if a.notebookScanner != nil {
		nbCands, err := a.notebookScanner.Scan(ctx)
		if err != nil {
			slog.Warn("stage_b_notebook_scan_failed", "err", err)
			errs = append(errs, fmt.Errorf("notebook scan: %w", err))
		}
		out = append(out, nbCands...)
	}

	if a.selfHealScanner != nil && len(out) < maxTotal {
		shCands, err := a.selfHealScanner.Scan(ctx)
		if err != nil {
			slog.Warn("stage_b_self_heal_scan_failed", "err", err)
			errs = append(errs, fmt.Errorf("self-heal scan: %w", err))
		}
		out = append(out, shCands...)
	}

	if len(out) > maxTotal {
		out = out[:maxTotal]
	}

	slog.Info("stage_b_aggregator_pass",
		"candidates_total", len(out),
		"max_total", maxTotal,
	)

	switch len(errs) {
	case 0:
		return out, nil
	case 1:
		return out, errs[0]
	default:
		return out, fmt.Errorf("stage_b_signals: %d sources failed: %v", len(errs), errs)
	}
}
