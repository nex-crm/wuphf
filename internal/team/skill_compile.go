package team

// skill_compile.go is the Stage A orchestration layer. It coordinates the
// SkillScanner with cooldown / coalesce semantics so manual clicks and the
// background cron never step on each other, and it owns the cron + (future)
// event-driven entry points.

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"
)

// Sentinel errors callers can branch on.
var (
	// ErrCompileCoalesced indicates a compile request collapsed into the
	// in-flight pass. The pending request was queued; the in-flight pass
	// will run one extra cycle before exiting.
	ErrCompileCoalesced = errors.New("compile coalesced into in-flight run")

	// ErrCompileCooldown indicates the cron tick was suppressed because a
	// recent compile pass finished within the cooldown window. Manual
	// triggers are not subject to cooldown.
	ErrCompileCooldown = errors.New("compile skipped: within cooldown window")
)

// Default cron interval and cooldown. Both are overridable via env vars.
const (
	defaultSkillCompileInterval = 30 * time.Minute
	defaultSkillCompileCooldown = 25 * time.Minute
)

// SkillCompileMetrics captures cumulative + last-run telemetry for the Stage
// A compile loop. All fields are updated and read atomically so callers need
// not hold broker.mu.
type SkillCompileMetrics struct {
	ManualClicksTotal             int64
	CronTicksTotal                int64
	ProposalsCreatedTotal         int64
	ProposalsApprovedTotal        int64
	ProposalsRejectedByGuardTotal int64
	LastTickDurationMs            int64
	// LastSkillCompilePassAtNano stores unix nanoseconds of the last successful
	// compile pass (0 = never). Updated and read via atomic.StoreInt64 /
	// atomic.LoadInt64 so reads are safe without broker.mu.
	LastSkillCompilePassAtNano int64
	// StageBProposalsTotal counts proposals written by the Stage B
	// synthesizer (LLM-synth from candidate signals). Incremented atomically
	// once the unified write helper accepts the proposal.
	StageBProposalsTotal int64
	// CounterNudgesFiredTotal counts skill_review_nudge tasks fired by the
	// Hermes-style per-agent counter (Stage B'). Incremented atomically by
	// the tool-event hot path each time a nudge task is appended.
	CounterNudgesFiredTotal int64
	// SelfHealCandidatesScanned counts candidates with Source ==
	// SourceSelfHealResolved that the synthesizer attempted to LLM-synthesize.
	SelfHealCandidatesScanned int64
	// SelfHealSkillsSynthesized counts self-heal candidates that the LLM
	// accepted AND that successfully wrote through the unified funnel.
	SelfHealSkillsSynthesized int64
	// SelfHealLLMRejections counts self-heal candidates rejected by the LLM
	// or by the post-LLM sanity checks (parse failures, name regex, body
	// heading missing, length checks, etc.).
	SelfHealLLMRejections int64
	// EmbeddingCallsTotal is incremented every time the notebook scanner
	// computes a fresh embedding (cache miss path). Cache hits do NOT
	// bump this counter — see EmbeddingCacheHitsTotal.
	EmbeddingCallsTotal int64
	// EmbeddingCacheHitsTotal counts on-disk cache hits across all
	// embedding paths. A high hit ratio is the goal — we never want to
	// re-embed the same entry once it has stabilised in the cache.
	EmbeddingCacheHitsTotal int64
	// EmbeddingCacheMissesTotal counts cache misses (live API calls).
	// EmbeddingCallsTotal == EmbeddingCacheMissesTotal in steady state;
	// the two diverge when a single batched API call fans out to N
	// per-text Set events.
	EmbeddingCacheMissesTotal int64
	// EmbeddingCostUsdBits stores a float64 USD cost using
	// math.Float64bits. Updated via addFloatBits / loadFloatBits in
	// notebook_signal_scanner_embeddings.go so reads + writes are
	// lock-free.
	EmbeddingCostUsdBits uint64
	// SemanticDedupHitsTotal counts proposals that matched an existing
	// skill via the semantic dedup gate (Jaro-Winkler or embedding cosine).
	SemanticDedupHitsTotal int64
	// SkillEnhancementsTotal counts proposals that enhanced an existing
	// skill instead of being discarded or created as new.
	SkillEnhancementsTotal int64
}

// snapshotSkillCompileMetrics returns a copy of m suitable for serialization.
// All fields are loaded atomically; no lock is required by the caller.
func snapshotSkillCompileMetrics(m *SkillCompileMetrics) SkillCompileMetrics {
	if m == nil {
		return SkillCompileMetrics{}
	}
	return SkillCompileMetrics{
		ManualClicksTotal:             atomic.LoadInt64(&m.ManualClicksTotal),
		CronTicksTotal:                atomic.LoadInt64(&m.CronTicksTotal),
		ProposalsCreatedTotal:         atomic.LoadInt64(&m.ProposalsCreatedTotal),
		ProposalsApprovedTotal:        atomic.LoadInt64(&m.ProposalsApprovedTotal),
		ProposalsRejectedByGuardTotal: atomic.LoadInt64(&m.ProposalsRejectedByGuardTotal),
		LastTickDurationMs:            atomic.LoadInt64(&m.LastTickDurationMs),
		LastSkillCompilePassAtNano:    atomic.LoadInt64(&m.LastSkillCompilePassAtNano),
		StageBProposalsTotal:          atomic.LoadInt64(&m.StageBProposalsTotal),
		CounterNudgesFiredTotal:       atomic.LoadInt64(&m.CounterNudgesFiredTotal),
		SelfHealCandidatesScanned:     atomic.LoadInt64(&m.SelfHealCandidatesScanned),
		SelfHealSkillsSynthesized:     atomic.LoadInt64(&m.SelfHealSkillsSynthesized),
		SelfHealLLMRejections:         atomic.LoadInt64(&m.SelfHealLLMRejections),
		EmbeddingCallsTotal:           atomic.LoadInt64(&m.EmbeddingCallsTotal),
		EmbeddingCacheHitsTotal:       atomic.LoadInt64(&m.EmbeddingCacheHitsTotal),
		EmbeddingCacheMissesTotal:     atomic.LoadInt64(&m.EmbeddingCacheMissesTotal),
		EmbeddingCostUsdBits:          atomic.LoadUint64(&m.EmbeddingCostUsdBits),
		SemanticDedupHitsTotal:        atomic.LoadInt64(&m.SemanticDedupHitsTotal),
		SkillEnhancementsTotal:        atomic.LoadInt64(&m.SkillEnhancementsTotal),
	}
}

// ensureSkillScanner lazily constructs the scanner. Called the first time
// compileWikiSkills runs so tests that never trigger compilation pay no cost.
func (b *Broker) ensureSkillScanner() *SkillScanner {
	b.mu.Lock()
	if b.skillScanner != nil {
		s := b.skillScanner
		b.mu.Unlock()
		return s
	}
	worker := b.wikiWorker
	b.mu.Unlock()

	var promptPath string
	if worker != nil {
		if repo := worker.Repo(); repo != nil {
			promptPath = filepath.Join(repo.Root(), "team", "skills", ".system", "skill-creator.md")
		}
	}
	provider := NewDefaultLLMProvider(promptPath)
	scanner := NewSkillScanner(b, provider, skillCompileBudgetFromEnv())

	b.mu.Lock()
	if b.skillScanner == nil {
		b.skillScanner = scanner
	}
	out := b.skillScanner
	b.mu.Unlock()
	return out
}

// SetSkillScanner replaces the broker's scanner — used by tests to inject a
// fake provider.
func (b *Broker) SetSkillScanner(s *SkillScanner) {
	b.mu.Lock()
	b.skillScanner = s
	b.mu.Unlock()
}

// SetSkillSynthesizer replaces the broker's Stage B synthesizer — used by
// tests to inject a fake aggregator + provider.
func (b *Broker) SetSkillSynthesizer(s *SkillSynthesizer) {
	b.mu.Lock()
	b.skillSynthesizer = s
	b.mu.Unlock()
}

// ensureSkillSynthesizer lazily constructs the Stage B synthesizer. Called the
// first time compileWikiSkills runs so tests that never trigger a Stage B
// pass pay no cost.
func (b *Broker) ensureSkillSynthesizer() *SkillSynthesizer {
	b.mu.Lock()
	if b.skillSynthesizer != nil {
		s := b.skillSynthesizer
		b.mu.Unlock()
		return s
	}
	b.mu.Unlock()

	agg := NewStageBSignalAggregator(b)
	provider := NewDefaultStageBLLMProvider(b)
	synth := NewSkillSynthesizer(b, agg)
	synth.provider = provider

	b.mu.Lock()
	if b.skillSynthesizer == nil {
		b.skillSynthesizer = synth
	}
	out := b.skillSynthesizer
	b.mu.Unlock()
	return out
}

// compileWikiSkills runs a single Stage A scan + write pass. trigger is one
// of "manual", "cron", "event"; tests may pass anything for traceability.
//
// Concurrency contract:
//  1. Acquire b.mu, check / set skillCompileInflight.
//  2. If a pass is already in flight, set skillCompileCoalesced and return
//     ErrCompileCoalesced.
//  3. If trigger=="cron" and we are inside the cooldown window, return
//     ErrCompileCooldown without running.
//  4. Release b.mu, run the scan.
//  5. Re-acquire b.mu, update metrics + cooldown timestamp + inflight flag.
//  6. If a coalesced request arrived during the run, recurse once.
func (b *Broker) compileWikiSkills(ctx context.Context, scopePath string, dryRun bool, trigger string) (ScanResult, error) {
	b.mu.Lock()
	if b.skillCompileInflight {
		b.skillCompileCoalesced = true
		b.mu.Unlock()
		return ScanResult{Trigger: trigger}, ErrCompileCoalesced
	}
	if trigger == "cron" {
		cooldown := skillCompileCooldownFromEnv()
		lastNano := atomic.LoadInt64(&b.skillCompileMetrics.LastSkillCompilePassAtNano)
		if lastNano != 0 && time.Since(time.Unix(0, lastNano)) < cooldown {
			b.mu.Unlock()
			return ScanResult{Trigger: trigger}, ErrCompileCooldown
		}
	}
	b.skillCompileInflight = true
	b.mu.Unlock()

	scanner := b.ensureSkillScanner()
	res, scanErr := scanner.Scan(ctx, scopePath, dryRun, trigger)

	// Stage B: LLM synthesizer pass over candidate signals from PR 2-A. Only
	// run when not in dry-run; the synthesizer writes proposals through the
	// same unified funnel so dry-run would produce false positives. Counts
	// fold into the returned ScanResult so callers see one combined number.
	if !dryRun {
		synth := b.ensureSkillSynthesizer()
		bRes, bErr := synth.SynthesizeOnce(ctx, trigger)
		if bErr != nil && !errors.Is(bErr, ErrSynthCoalesced) {
			res.Errors = append(res.Errors, ScanError{
				Slug:   "stage-b",
				Reason: "synth: " + bErr.Error(),
			})
		}
		res.Proposed += bRes.Synthesized
		res.Deduped += bRes.Deduped
		res.RejectedByGuard += bRes.RejectedByGuard
		atomic.AddInt64(&b.skillCompileMetrics.StageBProposalsTotal, int64(bRes.Synthesized))
		for _, e := range bRes.Errors {
			res.Errors = append(res.Errors, ScanError{
				Slug:   "stage-b:" + e.CandidateName,
				Reason: e.Reason,
			})
		}
	}

	atomic.StoreInt64(&b.skillCompileMetrics.LastSkillCompilePassAtNano, time.Now().UTC().UnixNano())
	atomic.StoreInt64(&b.skillCompileMetrics.LastTickDurationMs, res.DurationMs)
	b.mu.Lock()
	switch trigger {
	case "manual":
		atomic.AddInt64(&b.skillCompileMetrics.ManualClicksTotal, 1)
	case "cron":
		atomic.AddInt64(&b.skillCompileMetrics.CronTicksTotal, 1)
	}
	if !dryRun {
		atomic.AddInt64(&b.skillCompileMetrics.ProposalsCreatedTotal, int64(res.Proposed))
	}
	atomic.AddInt64(&b.skillCompileMetrics.ProposalsRejectedByGuardTotal, int64(res.RejectedByGuard))
	b.skillCompileInflight = false
	coalesced := b.skillCompileCoalesced
	b.skillCompileCoalesced = false
	b.mu.Unlock()

	// One extra pass if a request coalesced during the run. Use the same
	// trigger label so telemetry stays consistent.
	if coalesced && scanErr == nil {
		// Prevent unbounded recursion: the coalesced pass uses the same
		// concurrency check, and any new arrivals during it just set the
		// flag again without recursing.
		_, _ = b.compileWikiSkills(ctx, scopePath, dryRun, trigger)
	}

	return res, scanErr
}

// startSkillCompileCron launches the background ticker. Called from the
// broker startup path. Returns immediately if the interval is "0" or
// otherwise disabled.
func (b *Broker) startSkillCompileCron(ctx context.Context) {
	interval := skillCompileIntervalFromEnv()
	if interval <= 0 {
		slog.Info("skill_compile cron: disabled (set WUPHF_SKILL_COMPILE_INTERVAL to enable)")
		return
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		slog.Info("skill_compile cron: started", "interval", interval.String())
		for {
			select {
			case <-ctx.Done():
				slog.Info("skill_compile cron: stopping (context cancelled)")
				return
			case <-b.stopCh:
				slog.Info("skill_compile cron: stopping (broker shutdown)")
				return
			case <-ticker.C:
				if _, err := b.compileWikiSkills(ctx, "", false, "cron"); err != nil {
					if errors.Is(err, ErrCompileCooldown) {
						slog.Debug("skill_compile cron: skipped within cooldown")
					} else if errors.Is(err, ErrCompileCoalesced) {
						slog.Debug("skill_compile cron: coalesced into in-flight pass")
					} else {
						slog.Warn("skill_compile cron: pass error", "err", err)
					}
				}
			}
		}
	}()
}

// startSkillCompileEventListener subscribes to wiki write events so a freshly
// committed article can be scanned without waiting for the next cron tick.
//
// TODO(skill-compile): the existing PublishWikiEvent fan-out is per-channel
// and expects a goroutine drain pattern; wire this in once the cron path is
// proven in soak. Cron alone is sufficient for the v1 demo.
func (b *Broker) startSkillCompileEventListener(_ context.Context) {
	slog.Debug("skill_compile event listener: not yet wired (cron-only path)")
}

// ── env helpers ───────────────────────────────────────────────────────────

func skillCompileIntervalFromEnv() time.Duration {
	raw := strings.TrimSpace(os.Getenv("WUPHF_SKILL_COMPILE_INTERVAL"))
	if raw == "" {
		return defaultSkillCompileInterval
	}
	if raw == "0" || raw == "disabled" {
		return 0
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		slog.Warn("skill_compile: invalid WUPHF_SKILL_COMPILE_INTERVAL, falling back to default",
			"value", raw, "err", err)
		return defaultSkillCompileInterval
	}
	return d
}

func skillCompileCooldownFromEnv() time.Duration {
	raw := strings.TrimSpace(os.Getenv("WUPHF_SKILL_COMPILE_COOLDOWN"))
	if raw == "" {
		return defaultSkillCompileCooldown
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		slog.Warn("skill_compile: invalid WUPHF_SKILL_COMPILE_COOLDOWN, falling back to default",
			"value", raw, "err", err)
		return defaultSkillCompileCooldown
	}
	return d
}

func skillCompileBudgetFromEnv() int {
	raw := strings.TrimSpace(os.Getenv("WUPHF_SKILL_COMPILE_LLM_BUDGET"))
	if raw == "" {
		return defaultSkillCompileBudget
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
		return defaultSkillCompileBudget
	}
	return n
}
