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
	CandidatesScanned    int          `json:"candidates_scanned"`
	Synthesized          int          `json:"synthesized"`
	Deduped              int          `json:"deduped"`
	RejectedByGuard      int          `json:"rejected_by_guard"`
	RejectedByValidation int          `json:"rejected_by_validation"`
	Errors               []SynthError `json:"errors,omitempty"`
	DurationMs           int64        `json:"duration_ms"`
	Trigger              string       `json:"trigger"`
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
	gate          skillValidationGate // nil when WUPHF_SKILL_VALIDATION_GATE_DISABLED=true
	budgetPerPass int
}

// NewSkillSynthesizer constructs a synthesizer bound to broker b. The
// aggregator is required (the synthesizer has nothing to do without
// candidates); the provider is set separately by the caller so tests can
// inject fakes.
func NewSkillSynthesizer(b *Broker, agg stageBCandidateSource) *SkillSynthesizer {
	var gate skillValidationGate
	if strings.TrimSpace(os.Getenv("WUPHF_SKILL_VALIDATION_GATE_DISABLED")) != "true" {
		gate = newDefaultSkillValidationGate()
	}
	return &SkillSynthesizer{
		broker:        b,
		aggregator:    agg,
		gate:          gate,
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
		"rejected_by_validation", res.RejectedByValidation,
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

		decision, synthErr := s.provider.SynthesizeSkill(ctx, cand, wikiContext)
		if synthErr != nil {
			res.Errors = append(res.Errors, SynthError{
				CandidateName: cand.SuggestedName,
				Reason:        "synth: " + synthErr.Error(),
			})
			continue
		}
		fm := decision.Frontmatter
		body := decision.Body
		if strings.TrimSpace(fm.Name) == "" {
			if strings.TrimSpace(body) == "" {
				continue
			}
			res.Errors = append(res.Errors, SynthError{
				CandidateName: cand.SuggestedName,
				Reason:        "synth: empty name from llm",
			})
			continue
		}

		// --- Validation gate ---
		// LLM-as-judge: candidate must strictly improve on held-out task
		// fixtures versus the baseline (empty for new skills, existing body
		// for enhance). Skipped when no fixture file exists for the slug
		// (graceful degradation — common before teams author fixture sets).
		if s.gate != nil {
			gateSlug := fm.Name
			baselineBody := ""
			if decision.Enhance != "" {
				gateSlug = decision.Enhance
				s.broker.mu.Lock()
				if ex := s.broker.findSkillByNameLocked(decision.Enhance); ex != nil {
					baselineBody = ex.Content
				}
				s.broker.mu.Unlock()
			}
			hasFixtures, gateErr := s.gate.Validate(ctx, gateSlug, body, baselineBody, wikiRoot)
			if !hasFixtures {
				atomic.AddInt64(&s.broker.skillCompileMetrics.ValidationGateNoFixtures, 1)
			}
			if gateErr != nil {
				res.RejectedByValidation++
				atomic.AddInt64(&s.broker.skillCompileMetrics.ValidationGateRejections, 1)
				slog.Info("stage_b_synth_validation_gate_rejected",
					"source", string(cand.Source),
					"slug", gateSlug,
					"reason", gateErr.Error(),
				)
				res.Errors = append(res.Errors, SynthError{
					CandidateName: fm.Name,
					Reason:        "validation_gate: " + gateErr.Error(),
				})
				continue
			}
		}

		// --- Enhance / rename path ---
		// When the LLM chose to enhance an existing skill rather than mint
		// a new one, route the response through the existing enhance funnel
		// (and rename helper, when applicable) instead of the new-skill
		// write path. This is the load-bearing piece of "prefer enhance
		// over new": the LLM votes for it directly rather than relying on
		// post-hoc semantic dedup catching near-duplicates after the fact.
		if decision.Enhance != "" {
			applied, enhErr := s.routeEnhanceDecision(decision, cand)
			if enhErr != nil {
				res.Errors = append(res.Errors, SynthError{
					CandidateName: cand.SuggestedName,
					Reason:        "enhance: " + enhErr.Error(),
				})
				continue
			}
			if applied {
				res.Deduped++
				if cand.Source == SourceSelfHealResolved {
					atomic.AddInt64(&s.broker.skillCompileMetrics.SelfHealSkillsSynthesized, 1)
				}
				continue
			}
			// routeEnhanceDecision returning (false, nil) means the
			// referenced skill vanished — fall through and treat as a
			// fresh proposal under the LLM-supplied name.
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
			slog.Warn("stage_b_synth_guard_rejected",
				"source", string(cand.Source),
				"name", fm.Name,
				"verdict", string(scan.Verdict),
				"summary", scan.Summary,
			)
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
				slog.Warn("stage_b_synth_guard_rejected",
					"source", string(cand.Source),
					"name", fm.Name,
					"verdict", string(scan.Verdict),
					"summary", scan.Summary,
					"phase", "write",
				)
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
		if cand.Source == SourceSelfHealResolved {
			atomic.AddInt64(&s.broker.skillCompileMetrics.SelfHealSkillsSynthesized, 1)
		}
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

// routeEnhanceDecision applies an LLM-emitted enhance (or enhance + rename)
// decision to the existing skill catalog. It returns:
//
//   - (true, nil) when the existing skill was updated in place (enhance
//     or enhance + rename succeeded).
//   - (false, nil) when the referenced "enhance" skill does not exist (or
//     was archived). Caller treats this as a miss and falls through to the
//     new-skill write path under the LLM-supplied name.
//   - (false, err) on a hard write failure that should surface as an error
//     for the candidate.
//
// All wiki + broker mutation goes through the existing
// enhanceSkillLocked / renameAndEnhanceSkillLocked helpers so the
// deadlock-safe unlock/relock pattern is preserved.
func (s *SkillSynthesizer) routeEnhanceDecision(decision StageBSynthDecision, cand SkillCandidate) (bool, error) {
	enhance := strings.TrimSpace(decision.Enhance)
	if enhance == "" {
		return false, nil
	}
	renameTo := strings.TrimSpace(decision.RenameTo)
	enhancementBody := strings.TrimSpace(decision.Body)
	if enhancementBody == "" {
		return false, errors.New("empty enhancement body")
	}
	description := strings.TrimSpace(decision.Frontmatter.Description)

	s.broker.mu.Lock()
	defer s.broker.mu.Unlock()

	existing := s.broker.findSkillByNameLocked(enhance)
	if existing == nil {
		slog.Info("stage_b_synth_enhance_target_missing",
			"source", string(cand.Source),
			"enhance", enhance,
			"rename_to", renameTo,
		)
		return false, nil
	}

	// Rename + enhance: the LLM signals the existing skill's scope has
	// broadened (e.g. pitch-deck-saas → pitch-deck-creation). Route to
	// the rename helper, which updates the record in place and leaves a
	// redirect stub at the old path.
	if renameTo != "" && renameTo != enhance {
		if conflict := s.broker.findSkillByNameLocked(renameTo); conflict != nil {
			// The broader slug is already taken — fall back to a plain
			// enhance of the existing skill rather than clobbering a
			// distinct sibling.
			slog.Warn("stage_b_synth_rename_target_taken",
				"source", string(cand.Source),
				"enhance", enhance,
				"rename_to", renameTo,
				"fallback", "enhance-only",
			)
			renameTo = ""
		}
	}

	if renameTo != "" && renameTo != enhance {
		_, err := s.broker.renameAndEnhanceSkillLocked(enhance, renameTo, enhancementBody, description)
		if err != nil {
			return false, fmt.Errorf("rename+enhance %q → %q: %w", enhance, renameTo, err)
		}
		atomic.AddInt64(&s.broker.skillCompileMetrics.SkillEnhancementsTotal, 1)
		slog.Info("stage_b_synth_renamed_and_enhanced",
			"source", string(cand.Source),
			"from", enhance,
			"to", renameTo,
		)
		return true, nil
	}

	// Plain enhance. Use the existing helper, which performs the
	// deadlock-safe wiki write, mergeSkillContent, and broker record
	// update. We pass the candidate slug so the existing skill's
	// related_skills list gains a backlink for provenance.
	candSlug := skillSlug(strings.TrimSpace(cand.SuggestedName))
	if candSlug == "" {
		candSlug = skillSlug(decision.Frontmatter.Name)
	}
	_, err := s.broker.enhanceSkillLocked(existing.Name, enhancementBody, description, candSlug)
	if err != nil {
		return false, fmt.Errorf("enhance %q: %w", existing.Name, err)
	}
	atomic.AddInt64(&s.broker.skillCompileMetrics.SkillEnhancementsTotal, 1)
	slog.Info("stage_b_synth_enhanced",
		"source", string(cand.Source),
		"target", existing.Name,
	)
	return true, nil
}
