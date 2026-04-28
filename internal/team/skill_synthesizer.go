package team

// skill_synthesizer.go is the Stage B orchestrator. It consumes
// SkillCandidate events from the StageBSignalAggregator (PR 2-A), asks the
// LLM to synthesize a SKILL.md from each candidate, deduplicates against the
// in-memory skill index, runs the safety guard at agent_created trust, and
// writes proposals through writeSkillProposalLocked.
//
// Concurrency contract mirrors the Stage A compile loop: at most one synth
// pass runs at a time; concurrent triggers coalesce into the in-flight pass
// and recurse exactly once after it completes.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"
)

// ErrSynthCoalesced indicates a synth request collapsed into an in-flight
// pass. Callers can branch on this to avoid surfacing a false error.
var ErrSynthCoalesced = errors.New("synth coalesced into in-flight run")

// stageBWikiContextCap is the per-pass byte cap on concatenated wiki context
// fed to the LLM. 30KB matches the design's guard against runaway prompt
// size and keeps the pass deterministic across tokenisers.
const stageBWikiContextCap = 30 * 1024

// stageBDefaultBudget is the per-pass synth budget when the env var is unset
// or invalid. Matches the design default of 10 candidates per pass.
const stageBDefaultBudget = 10

// SynthError records a single per-candidate failure during a synth pass.
type SynthError struct {
	CandidateName string `json:"candidate_name"`
	Reason        string `json:"reason"`
}

// StageBSynthResult is the JSON-serializable summary of a single synth pass.
// Counts mirror ScanResult so callers can fold them into Stage A telemetry.
type StageBSynthResult struct {
	CandidatesScanned int          `json:"candidates_scanned"`
	Synthesized       int          `json:"synthesized"`
	Deduped           int          `json:"deduped"`
	RejectedByGuard   int          `json:"rejected_by_guard"`
	Errors            []SynthError `json:"errors,omitempty"`
	DurationMs        int64        `json:"duration_ms"`
	Trigger           string       `json:"trigger"`
}

// stageBCandidateSource is the small interface SkillSynthesizer reads
// candidates from. It exists so tests can inject a fake without standing up
// a full *StageBSignalAggregator (which itself depends on the notebook +
// self-heal scanners). The production wiring uses *StageBSignalAggregator,
// which already satisfies this interface.
type stageBCandidateSource interface {
	Scan(ctx context.Context, maxTotal int) ([]SkillCandidate, error)
}

// SkillSynthesizer aggregates Stage B signals, asks the LLM to synthesize a
// skill body for each candidate, and writes proposals through the broker's
// unified funnel.
type SkillSynthesizer struct {
	broker        *Broker
	aggregator    stageBCandidateSource
	provider      stageBLLMProvider
	budgetPerPass int
}

// NewSkillSynthesizer constructs a synthesizer bound to broker b. The
// aggregator is required (the synthesizer has nothing to do without
// candidates); the provider is set separately by the caller so tests can
// inject fakes.
func NewSkillSynthesizer(b *Broker, agg stageBCandidateSource) *SkillSynthesizer {
	return &SkillSynthesizer{
		broker:        b,
		aggregator:    agg,
		budgetPerPass: stageBSynthBudgetFromEnv(),
	}
}

// SynthesizeOnce runs one synth pass: aggregate candidates → LLM synthesize →
// dedup → safety guard → write proposal. trigger is one of "manual", "cron",
// or "event" for telemetry.
//
// Concurrency:
//  1. Acquire b.mu, check / set b.skillSynthInflight.
//  2. If a pass is already in flight, set b.skillSynthCoalesced and return
//     ErrSynthCoalesced.
//  3. Release b.mu, run the pass.
//  4. Re-acquire b.mu, clear the inflight flag, and recurse once if a
//     coalesced request arrived during the pass.
func (s *SkillSynthesizer) SynthesizeOnce(ctx context.Context, trigger string) (StageBSynthResult, error) {
	start := time.Now()
	res := StageBSynthResult{Trigger: trigger}

	if s.broker == nil {
		return res, errors.New("skill_synthesizer: broker is nil")
	}
	if s.aggregator == nil {
		return res, errors.New("skill_synthesizer: aggregator is nil")
	}
	if s.provider == nil {
		return res, errors.New("skill_synthesizer: provider is nil")
	}

	// --- Coalesce gate (mirrors compileWikiSkills) ---
	s.broker.mu.Lock()
	if s.broker.skillSynthInflight {
		s.broker.skillSynthCoalesced = true
		s.broker.mu.Unlock()
		return res, ErrSynthCoalesced
	}
	s.broker.skillSynthInflight = true
	s.broker.mu.Unlock()

	// --- Run the pass ---
	res = s.runPass(ctx, trigger, start)

	// --- Clear the inflight flag and recurse once if coalesced ---
	s.broker.mu.Lock()
	s.broker.skillSynthInflight = false
	coalesced := s.broker.skillSynthCoalesced
	s.broker.skillSynthCoalesced = false
	s.broker.mu.Unlock()

	if coalesced {
		// One extra pass to drain any signals that arrived during this run.
		// The coalesce flag inside runPass uses the same gate, so further
		// concurrent arrivals just set the flag again without recursing.
		_, _ = s.SynthesizeOnce(ctx, trigger)
	}

	slog.Info("stage_b_synth_pass",
		"trigger", res.Trigger,
		"candidates_scanned", res.CandidatesScanned,
		"synthesized", res.Synthesized,
		"deduped", res.Deduped,
		"rejected_by_guard", res.RejectedByGuard,
		"errors", len(res.Errors),
		"duration_ms", res.DurationMs,
	)

	return res, nil
}

// runPass executes one budget-bounded scan + synthesize loop without any
// concurrency bookkeeping. Split out so the coalesce machinery can wrap it
// cleanly.
func (s *SkillSynthesizer) runPass(ctx context.Context, trigger string, start time.Time) StageBSynthResult {
	res := StageBSynthResult{Trigger: trigger}

	candidates, scanErr := s.aggregator.Scan(ctx, s.budgetPerPass)
	if scanErr != nil {
		res.Errors = append(res.Errors, SynthError{
			CandidateName: "",
			Reason:        "aggregator: " + scanErr.Error(),
		})
		res.DurationMs = time.Since(start).Milliseconds()
		return res
	}

	wikiRoot := s.resolveWikiRoot()

	for i, cand := range candidates {
		if ctx.Err() != nil {
			res.Errors = append(res.Errors, SynthError{
				CandidateName: cand.SuggestedName,
				Reason:        "context: " + ctx.Err().Error(),
			})
			break
		}
		if i >= s.budgetPerPass {
			break
		}
		res.CandidatesScanned++

		// Build the wiki context once per candidate. Capped at
		// stageBWikiContextCap bytes to keep prompt size bounded.
		wikiContext := buildStageBWikiContext(wikiRoot, cand.RelatedWikiPaths, stageBWikiContextCap)

		fm, body, synthErr := s.provider.SynthesizeSkill(ctx, cand, wikiContext)
		if synthErr != nil {
			res.Errors = append(res.Errors, SynthError{
				CandidateName: cand.SuggestedName,
				Reason:        "synth: " + synthErr.Error(),
			})
			continue
		}
		if strings.TrimSpace(fm.Name) == "" {
			res.Errors = append(res.Errors, SynthError{
				CandidateName: cand.SuggestedName,
				Reason:        "synth: empty name from llm",
			})
			continue
		}

		// --- Pre-write dedup ---
		s.broker.mu.Lock()
		existing := s.broker.findSkillByNameLocked(fm.Name)
		s.broker.mu.Unlock()
		if existing != nil {
			res.Deduped++
			continue
		}

		// --- Safety guard at agent_created trust ---
		// Stricter than community: caution is also rejected because Stage B
		// auto-synthesizes at scale.
		scan := ScanSkill(fm, body, TrustAgentCreated)
		if scan.Verdict != VerdictSafe {
			res.RejectedByGuard++
			res.Errors = append(res.Errors, SynthError{
				CandidateName: fm.Name,
				Reason:        "guard: " + scan.Summary,
			})
			continue
		}

		// --- Build the teamSkill spec + write through the unified funnel ---
		spec := stageBCandToSpec(fm, body, cand)
		s.broker.mu.Lock()
		written, writeErr := s.broker.writeSkillProposalLocked(spec)
		s.broker.mu.Unlock()
		if writeErr != nil {
			// Fall through: the unified helper rejects guard verdicts
			// stricter than the local check would, so a guard rejection at
			// this layer is the same severity as the local one.
			if isStageBGuardError(writeErr) {
				res.RejectedByGuard++
			}
			res.Errors = append(res.Errors, SynthError{
				CandidateName: fm.Name,
				Reason:        "write: " + writeErr.Error(),
			})
			continue
		}
		if written != nil && written.UpdatedAt != "" && written.CreatedAt != written.UpdatedAt {
			// Helper returned an existing skill (dedup race): count it.
			res.Deduped++
			continue
		}
		res.Synthesized++
	}

	res.DurationMs = time.Since(start).Milliseconds()
	return res
}

// resolveWikiRoot returns the on-disk wiki root or "" when the markdown
// backend is not initialised. Callers MUST tolerate "".
func (s *SkillSynthesizer) resolveWikiRoot() string {
	s.broker.mu.Lock()
	worker := s.broker.wikiWorker
	s.broker.mu.Unlock()
	if worker == nil {
		return ""
	}
	repo := worker.Repo()
	if repo == nil {
		return ""
	}
	return repo.Root()
}

// buildStageBWikiContext concatenates the requested wiki paths with
// `--- {path} ---` separators, truncating the total at cap bytes. Missing or
// unreadable files are skipped silently; the LLM call should still succeed
// with a degraded grounding window.
func buildStageBWikiContext(wikiRoot string, paths []string, cap int) string {
	if wikiRoot == "" || len(paths) == 0 || cap <= 0 {
		return ""
	}
	var b strings.Builder
	for _, p := range paths {
		clean := filepath.Clean(strings.TrimPrefix(strings.TrimSpace(p), "/"))
		if clean == "." || strings.HasPrefix(clean, "..") {
			continue
		}
		full := filepath.Join(wikiRoot, clean)
		raw, err := os.ReadFile(full)
		if err != nil {
			continue
		}
		section := fmt.Sprintf("--- %s ---\n%s\n", clean, string(raw))
		if b.Len()+len(section) > cap {
			// Truncate the last section so the final string stays under cap.
			remaining := cap - b.Len()
			if remaining > 0 {
				if remaining > len(section) {
					remaining = len(section)
				}
				b.WriteString(section[:remaining])
			}
			break
		}
		b.WriteString(section)
	}
	return b.String()
}

// stageBCandToSpec folds the LLM-synthesized frontmatter + candidate
// provenance into a teamSkill spec ready for writeSkillProposalLocked. The
// helper deliberately stamps a "Signals" footer onto the body so source
// provenance survives even though teamSkill itself doesn't carry a
// source_signals field.
func stageBCandToSpec(fm SkillFrontmatter, body string, cand SkillCandidate) teamSkill {
	tags := append([]string(nil), fm.Metadata.Wuphf.Tags...)
	tags = appendUnique(tags, fmt.Sprintf("signal:source-%s", cand.Source))
	tags = appendUnique(tags, fmt.Sprintf("signal:agents-%d", distinctAuthors(cand.Excerpts)))

	bodyWithSignals := appendStageBSignalsFooter(body, cand)

	title := strings.TrimSpace(fm.Metadata.Wuphf.Title)
	if title == "" {
		title = strings.TrimSpace(cand.SuggestedName)
	}
	if title == "" {
		title = strings.TrimSpace(fm.Name)
	}

	return teamSkill{
		Name:        fm.Name,
		Title:       title,
		Description: fm.Description,
		Content:     bodyWithSignals,
		CreatedBy:   "scanner", // system-author whitelist
		Channel:     "general",
		Tags:        tags,
		Trigger:     fm.Metadata.Wuphf.Trigger,
		Status:      "proposed",
	}
}

// appendStageBSignalsFooter renders a "## Signals" section onto body that
// summarises the candidate's provenance. Lives in the body so the
// rendered SKILL.md surfaces the source signals even without a
// metadata.wuphf.source_signals field on teamSkill.
func appendStageBSignalsFooter(body string, cand SkillCandidate) string {
	var b strings.Builder
	b.WriteString(strings.TrimRight(body, "\n"))
	b.WriteString("\n\n---\n\n## Signals\n\n")
	b.WriteString(fmt.Sprintf("Synthesized from %d signals across %d agents:\n\n",
		cand.SignalCount, distinctAuthors(cand.Excerpts)))
	for _, ex := range cand.Excerpts {
		author := strings.TrimSpace(ex.Author)
		if author == "" {
			author = "unknown"
		}
		path := strings.TrimSpace(ex.Path)
		if path == "" {
			path = "(no path)"
		}
		b.WriteString(fmt.Sprintf("- `%s` — %s\n", path, author))
	}
	b.WriteString("\n")
	return b.String()
}

// distinctAuthors lives in notebook_signal_scanner.go.

// isStageBGuardError reports whether err originated from the safety guard.
// writeSkillProposalLocked wraps guard rejections with "skill_guard: ".
func isStageBGuardError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "skill_guard:")
}

// stageBSynthBudgetFromEnv returns the per-pass synth budget. Defaults to
// stageBDefaultBudget when the env var is unset; falls back to the default
// for any non-positive integer to avoid runaway / disabled passes.
func stageBSynthBudgetFromEnv() int {
	raw := strings.TrimSpace(os.Getenv("WUPHF_STAGE_B_SYNTH_TICK_BUDGET"))
	if raw == "" {
		return stageBDefaultBudget
	}
	n := 0
	for _, c := range raw {
		if c < '0' || c > '9' {
			n = 0
			break
		}
		n = n*10 + int(c-'0')
	}
	if n <= 0 {
		return stageBDefaultBudget
	}
	return n
}

// stageBProposalsTotalLoad is a small accessor for tests + telemetry that
// avoids exposing the raw counter.
func (b *Broker) stageBProposalsTotalLoad() int64 {
	return atomic.LoadInt64(&b.skillCompileMetrics.StageBProposalsTotal)
}
