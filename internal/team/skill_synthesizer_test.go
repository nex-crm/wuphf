package team

// skill_synthesizer_test.go covers the Stage B synthesis pass: candidate
// scanning, dedup, guard, coalesce, budget capping, and not-a-skill rejection.
// LLM round-trips and aggregator scans are stubbed via the small
// stageBCandidateSource + stageBLLMProvider interfaces so the tests stay
// hermetic — wiki worker is nil (no markdown backend), so writeSkillProposalLocked
// exercises its in-memory path.

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// stubCandidateSource is a hermetic stageBCandidateSource that returns a
// pre-baked candidate slice + optional error. The synthesizer accepts the
// interface, so this satisfies it without dragging in the notebook or
// self-heal scanners.
type stubCandidateSource struct {
	candidates []SkillCandidate
	err        error
	calls      atomic.Int64
}

func (s *stubCandidateSource) Scan(_ context.Context, maxTotal int) ([]SkillCandidate, error) {
	s.calls.Add(1)
	if s.err != nil {
		return nil, s.err
	}
	if maxTotal > 0 && len(s.candidates) > maxTotal {
		return s.candidates[:maxTotal], nil
	}
	return s.candidates, nil
}

// stubLLMProvider returns a programmable response per call. queue is drained
// in order; once empty, returns the not-a-skill error.
type stubLLMProvider struct {
	mu              sync.Mutex
	queue           []stubLLMResponse
	respondNotSkill bool
	calls           atomic.Int64
}

type stubLLMResponse struct {
	fm       SkillFrontmatter
	body     string
	enhance  string
	renameTo string
	err      error
}

func (p *stubLLMProvider) SynthesizeSkill(_ context.Context, _ SkillCandidate, _ string) (StageBSynthDecision, error) {
	p.calls.Add(1)
	if p.respondNotSkill {
		return StageBSynthDecision{}, errors.New("synth: candidate rejected by LLM as not-a-skill")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.queue) == 0 {
		return StageBSynthDecision{}, errors.New("synth: candidate rejected by LLM as not-a-skill")
	}
	r := p.queue[0]
	p.queue = p.queue[1:]
	return StageBSynthDecision{
		Frontmatter: r.fm,
		Body:        r.body,
		Enhance:     r.enhance,
		RenameTo:    r.renameTo,
	}, r.err
}

// newSynthWithCandidates wires a synthesizer with a stub candidate source +
// the supplied provider, returning both for assertion access.
func newSynthWithCandidates(_ *testing.T, b *Broker, prov stageBLLMProvider, cands []SkillCandidate) *SkillSynthesizer {
	src := &stubCandidateSource{candidates: cands}
	synth := NewSkillSynthesizer(b, src)
	synth.provider = prov
	return synth
}

func TestSynthesizeOnce_BasicWritesProposal(t *testing.T) {
	b := newTestBroker(t)
	prov := &stubLLMProvider{
		queue: []stubLLMResponse{{
			fm: SkillFrontmatter{
				Name:        "deploy-runbook",
				Description: "Deploy a service from staging to prod.",
			},
			body: "## Steps\n1. Tag the release.\n2. Watch dashboards.\n",
		}},
	}
	cand := SkillCandidate{
		Source:               SourceNotebookCluster,
		SuggestedName:        "deploy-runbook",
		SuggestedDescription: "Deploy a service from staging to prod.",
		SignalCount:          3,
		Excerpts: []SkillCandidateExcerpt{
			{Path: "team/agents/eng/notebook/deploy.md", Snippet: "we deploy weekly", Author: "eng"},
			{Path: "team/agents/ops/notebook/release.md", Snippet: "release flow", Author: "ops"},
		},
	}
	synth := newSynthWithCandidates(t, b, prov, []SkillCandidate{cand})

	res, err := synth.SynthesizeOnce(context.Background(), "manual")
	if err != nil {
		t.Fatalf("SynthesizeOnce: %v", err)
	}
	if res.Synthesized != 1 {
		t.Fatalf("Synthesized: got %d, want 1 (errors: %+v)", res.Synthesized, res.Errors)
	}
	if res.CandidatesScanned != 1 {
		t.Fatalf("CandidatesScanned: got %d, want 1", res.CandidatesScanned)
	}
	if res.Deduped != 0 || res.RejectedByGuard != 0 {
		t.Fatalf("unexpected counts: deduped=%d rejected=%d", res.Deduped, res.RejectedByGuard)
	}
	if prov.calls.Load() != 1 {
		t.Fatalf("provider calls: got %d, want 1", prov.calls.Load())
	}

	// The proposal should now be in b.skills.
	b.mu.Lock()
	existing := b.findSkillByNameLocked("deploy-runbook")
	b.mu.Unlock()
	if existing == nil {
		t.Fatalf("expected deploy-runbook in b.skills after synth")
	}
	if !strings.Contains(existing.Content, "## Signals") {
		t.Fatalf("expected Signals footer in body, got %q", existing.Content)
	}
}

func TestSynthesizeOnce_EmptyProviderResponseSkipsCandidate(t *testing.T) {
	b := newTestBroker(t)
	prov := &stubLLMProvider{
		queue: []stubLLMResponse{{}},
	}
	cand := SkillCandidate{
		Source:               SourceSelfHealResolved,
		SuggestedName:        "handle-capability-gap",
		SuggestedDescription: "How to resolve a capability gap.",
		SignalCount:          1,
	}
	synth := newSynthWithCandidates(t, b, prov, []SkillCandidate{cand})

	res, err := synth.SynthesizeOnce(context.Background(), "manual")
	if err != nil {
		t.Fatalf("SynthesizeOnce: %v", err)
	}
	if res.CandidatesScanned != 1 {
		t.Fatalf("CandidatesScanned: got %d, want 1", res.CandidatesScanned)
	}
	if res.Synthesized != 0 || res.Deduped != 0 || res.RejectedByGuard != 0 || len(res.Errors) != 0 {
		t.Fatalf("empty provider response should be a quiet skip, got %+v", res)
	}
}

func TestSynthesizeOnce_DedupAgainstExisting(t *testing.T) {
	b := newTestBroker(t)
	// Pre-seed an existing skill with the same slug.
	b.mu.Lock()
	b.skills = append(b.skills, teamSkill{
		Name:        "deploy-workflow",
		Title:       "Deploy Workflow",
		Description: "Existing.",
		Content:     "Existing body.",
		Status:      "active",
		CreatedBy:   "scanner",
	})
	b.mu.Unlock()

	prov := &stubLLMProvider{
		queue: []stubLLMResponse{{
			fm: SkillFrontmatter{
				Name:        "deploy-workflow",
				Description: "Same slug, fresh body.",
			},
			body: "## Steps\nDeploy steps.",
		}},
	}
	cand := SkillCandidate{
		Source:        SourceNotebookCluster,
		SuggestedName: "deploy-workflow",
		SignalCount:   2,
	}
	synth := newSynthWithCandidates(t, b, prov, []SkillCandidate{cand})

	res, err := synth.SynthesizeOnce(context.Background(), "manual")
	if err != nil {
		t.Fatalf("SynthesizeOnce: %v", err)
	}
	if res.Deduped != 1 {
		t.Fatalf("Deduped: got %d, want 1", res.Deduped)
	}
	if res.Synthesized != 0 {
		t.Fatalf("Synthesized: got %d, want 0", res.Synthesized)
	}
}

func TestSynthesizeOnce_GuardRejectsNonSafe(t *testing.T) {
	b := newTestBroker(t)
	// Body has shell metas inside a non-bash code block → caution at
	// agent_created trust → rejected by Stage B guard (stricter than community).
	cautionBody := "## Steps\n```python\nrun(\"a; b | c\")\n```\n"
	prov := &stubLLMProvider{
		queue: []stubLLMResponse{{
			fm: SkillFrontmatter{
				Name:        "shell-fence-skill",
				Description: "A description.",
			},
			body: cautionBody,
		}},
	}
	cand := SkillCandidate{
		Source:        SourceNotebookCluster,
		SuggestedName: "shell-fence-skill",
		SignalCount:   1,
	}
	synth := newSynthWithCandidates(t, b, prov, []SkillCandidate{cand})

	res, err := synth.SynthesizeOnce(context.Background(), "manual")
	if err != nil {
		t.Fatalf("SynthesizeOnce: %v", err)
	}
	if res.RejectedByGuard != 1 {
		t.Fatalf("RejectedByGuard: got %d, want 1 (errors: %+v)", res.RejectedByGuard, res.Errors)
	}
	if res.Synthesized != 0 {
		t.Fatalf("Synthesized: got %d, want 0", res.Synthesized)
	}
}

func TestSynthesizeOnce_LLMSaysNotSkill(t *testing.T) {
	b := newTestBroker(t)
	prov := &stubLLMProvider{respondNotSkill: true}
	cand := SkillCandidate{
		Source:        SourceNotebookCluster,
		SuggestedName: "unclear-candidate",
	}
	synth := newSynthWithCandidates(t, b, prov, []SkillCandidate{cand})

	res, err := synth.SynthesizeOnce(context.Background(), "manual")
	if err != nil {
		t.Fatalf("SynthesizeOnce: %v", err)
	}
	if res.Synthesized != 0 {
		t.Fatalf("Synthesized: got %d, want 0", res.Synthesized)
	}
	if len(res.Errors) == 0 {
		t.Fatalf("expected errors slice to capture the rejection, got empty")
	}
	if !strings.Contains(res.Errors[0].Reason, "not-a-skill") {
		t.Fatalf("expected not-a-skill reason, got %q", res.Errors[0].Reason)
	}
}

func TestSynthesizeOnce_BudgetCapAcrossCandidates(t *testing.T) {
	t.Setenv("WUPHF_STAGE_B_SYNTH_TICK_BUDGET", "3")

	b := newTestBroker(t)
	prov := &stubLLMProvider{respondNotSkill: true}

	// 8 candidates; budget=3 → only first 3 should be consumed by the
	// aggregator (which honours maxTotal in the stub) and by the synthesizer.
	var cands []SkillCandidate
	for i := 0; i < 8; i++ {
		cands = append(cands, SkillCandidate{
			Source:        SourceNotebookCluster,
			SuggestedName: "candidate-" + string(rune('a'+i)),
		})
	}
	synth := newSynthWithCandidates(t, b, prov, cands)

	res, err := synth.SynthesizeOnce(context.Background(), "manual")
	if err != nil {
		t.Fatalf("SynthesizeOnce: %v", err)
	}
	if res.CandidatesScanned > 3 {
		t.Fatalf("CandidatesScanned: got %d, want <= 3", res.CandidatesScanned)
	}
	if prov.calls.Load() > 3 {
		t.Fatalf("provider calls: got %d, want <= 3", prov.calls.Load())
	}
}

func TestSynthesizeOnce_CoalescesConcurrentTriggers(t *testing.T) {
	b := newTestBroker(t)
	// Manually flip the inflight flag so the second call coalesces. This
	// mirrors the Stage A pattern (TestCompileWikiSkills_CoalescesConcurrentRequests).
	b.mu.Lock()
	b.skillSynthInflight = true
	b.mu.Unlock()

	synth := newSynthWithCandidates(t, b, &stubLLMProvider{respondNotSkill: true}, nil)

	_, err := synth.SynthesizeOnce(context.Background(), "manual")
	if !errors.Is(err, ErrSynthCoalesced) {
		t.Fatalf("expected ErrSynthCoalesced, got %v", err)
	}

	b.mu.Lock()
	coalesced := b.skillSynthCoalesced
	b.mu.Unlock()
	if !coalesced {
		t.Fatalf("expected skillSynthCoalesced=true after coalesce hit")
	}

	// Cleanup so we don't leak goroutines or pollute follow-on tests.
	b.mu.Lock()
	b.skillSynthInflight = false
	b.skillSynthCoalesced = false
	b.mu.Unlock()
}

func TestSynthesizeOnce_AggregatorErrorPropagates(t *testing.T) {
	b := newTestBroker(t)
	prov := &stubLLMProvider{respondNotSkill: true}
	src := &stubCandidateSource{err: errors.New("boom")}
	synth := NewSkillSynthesizer(b, src)
	synth.provider = prov

	res, err := synth.SynthesizeOnce(context.Background(), "manual")
	if err != nil {
		// The orchestrator surfaces aggregator errors via the result, not as
		// a top-level error, because partial passes are still useful.
		t.Fatalf("unexpected top-level error: %v", err)
	}
	if len(res.Errors) == 0 {
		t.Fatalf("expected aggregator error in result.Errors, got none")
	}
	if !strings.Contains(res.Errors[0].Reason, "aggregator") {
		t.Fatalf("expected aggregator-tagged error, got %q", res.Errors[0].Reason)
	}
}

func TestStageBSynthBudgetFromEnv(t *testing.T) {
	t.Run("default", func(t *testing.T) {
		t.Setenv("WUPHF_STAGE_B_SYNTH_TICK_BUDGET", "")
		if got := stageBSynthBudgetFromEnv(); got != stageBDefaultBudget {
			t.Fatalf("default budget: got %d, want %d", got, stageBDefaultBudget)
		}
	})
	t.Run("custom", func(t *testing.T) {
		t.Setenv("WUPHF_STAGE_B_SYNTH_TICK_BUDGET", "7")
		if got := stageBSynthBudgetFromEnv(); got != 7 {
			t.Fatalf("custom budget: got %d, want 7", got)
		}
	})
	t.Run("invalid falls back", func(t *testing.T) {
		t.Setenv("WUPHF_STAGE_B_SYNTH_TICK_BUDGET", "abc")
		if got := stageBSynthBudgetFromEnv(); got != stageBDefaultBudget {
			t.Fatalf("invalid budget should fall back: got %d", got)
		}
	})
}

func TestStageBSignalsFooter_RendersCitations(t *testing.T) {
	cand := SkillCandidate{
		Source:      SourceNotebookCluster,
		SignalCount: 2,
		Excerpts: []SkillCandidateExcerpt{
			{Path: "team/agents/a/notebook/x.md", Author: "a"},
			{Path: "team/agents/b/notebook/x.md", Author: "b"},
		},
	}
	body := appendStageBSignalsFooter("Body content.", cand)
	if !strings.Contains(body, "## Signals") {
		t.Fatalf("expected Signals heading, got %q", body)
	}
	if !strings.Contains(body, "across 2 agents") {
		t.Fatalf("expected agent count in footer, got %q", body)
	}
	if !strings.Contains(body, "team/agents/a/notebook/x.md") {
		t.Fatalf("expected first path in footer, got %q", body)
	}
}

// TestSynthesizeOnce_EnhanceRoutesToExistingSkill confirms that when the
// LLM emits an enhance hint, the synthesizer merges into the existing
// skill instead of creating a new one. This is the core of the
// "prefer enhance over new" gate added in the deliberate-skill-generation
// work — without it, a near-duplicate notebook cluster could mint a
// fresh skill and only get caught after the fact by semantic dedup.
func TestSynthesizeOnce_EnhanceRoutesToExistingSkill(t *testing.T) {
	b := newTestBroker(t)
	// Pre-seed the existing skill.
	b.mu.Lock()
	b.skills = append(b.skills, teamSkill{
		Name:        "deploy-runbook",
		Title:       "Deploy Runbook",
		Description: "Deploy a service from staging to prod.",
		Content:     "## Steps\n1. Tag.\n2. Watch.\n",
		Status:      "active",
		CreatedBy:   "scanner",
	})
	b.mu.Unlock()

	prov := &stubLLMProvider{
		queue: []stubLLMResponse{{
			fm: SkillFrontmatter{
				Name:        "deploy-runbook",
				Description: "Deploy a service from staging to prod (enhanced).",
			},
			body:    "## Steps\n3. Roll back if the soak window trips a health gate.\n",
			enhance: "deploy-runbook",
		}},
	}
	cand := SkillCandidate{
		Source:        SourceNotebookCluster,
		SuggestedName: "deploy-runbook-v2",
		SignalCount:   3,
	}
	synth := newSynthWithCandidates(t, b, prov, []SkillCandidate{cand})

	res, err := synth.SynthesizeOnce(context.Background(), "manual")
	if err != nil {
		t.Fatalf("SynthesizeOnce: %v", err)
	}
	if res.Synthesized != 0 {
		t.Fatalf("Synthesized: got %d want 0 (enhance is not a new proposal)", res.Synthesized)
	}
	if res.Deduped != 1 {
		t.Fatalf("Deduped: got %d want 1 (enhance counts as dedup)", res.Deduped)
	}

	b.mu.Lock()
	enhanced := b.findSkillByNameLocked("deploy-runbook")
	skillCount := len(b.skills)
	b.mu.Unlock()
	if enhanced == nil {
		t.Fatalf("expected existing skill to survive")
	}
	if skillCount != 1 {
		t.Fatalf("expected exactly 1 skill after enhance, got %d", skillCount)
	}
	if !strings.Contains(enhanced.Content, "Roll back if the soak window trips a health gate") {
		t.Fatalf("expected enhancement merged into body, got:\n%s", enhanced.Content)
	}
	if atomic.LoadInt64(&b.skillCompileMetrics.SkillEnhancementsTotal) != 1 {
		t.Fatalf("SkillEnhancementsTotal: got %d want 1",
			atomic.LoadInt64(&b.skillCompileMetrics.SkillEnhancementsTotal))
	}
}

// TestSynthesizeOnce_RenameAndEnhanceBroadensSlug confirms the rename
// path: when the LLM signals that an existing skill's scope has
// broadened (e.g. pitch-deck-saas → pitch-deck-creation), the existing
// record is renamed in place and a redirect stub remains under the old
// slug so cached references resolve.
func TestSynthesizeOnce_RenameAndEnhanceBroadensSlug(t *testing.T) {
	b := newTestBroker(t)
	b.mu.Lock()
	b.skills = append(b.skills, teamSkill{
		Name:        "pitch-deck-saas",
		Title:       "SaaS Pitch Deck",
		Description: "Draft a SaaS pitch deck.",
		Content:     "## Steps\n1. Title.\n2. Problem.\n3. Demo.\n",
		Status:      "active",
		CreatedBy:   "scanner",
	})
	b.mu.Unlock()

	prov := &stubLLMProvider{
		queue: []stubLLMResponse{{
			fm: SkillFrontmatter{
				Name:        "pitch-deck-creation",
				Description: "Draft a pitch deck for any go-to-market motion.",
			},
			body:     "## Steps\n4. Adapt the demo slide for marketplace / consumer / enterprise deals.\n",
			enhance:  "pitch-deck-saas",
			renameTo: "pitch-deck-creation",
		}},
	}
	cand := SkillCandidate{
		Source:        SourceNotebookCluster,
		SuggestedName: "pitch-deck-marketplace",
		SignalCount:   3,
	}
	synth := newSynthWithCandidates(t, b, prov, []SkillCandidate{cand})

	res, err := synth.SynthesizeOnce(context.Background(), "manual")
	if err != nil {
		t.Fatalf("SynthesizeOnce: %v", err)
	}
	if res.Deduped != 1 {
		t.Fatalf("Deduped: got %d want 1 (rename counts as dedup)", res.Deduped)
	}

	b.mu.Lock()
	renamed := b.findSkillByNameLocked("pitch-deck-creation")
	oldStill := b.findSkillByNameLocked("pitch-deck-saas")
	b.mu.Unlock()

	if renamed == nil {
		t.Fatalf("expected pitch-deck-creation to exist after rename")
	}
	if !strings.Contains(renamed.Content, "Adapt the demo slide") {
		t.Fatalf("expected enhancement merged into renamed body, got:\n%s", renamed.Content)
	}
	if !strings.Contains(renamed.Content, "Title.") {
		t.Fatalf("expected original content preserved under renamed slug, got:\n%s", renamed.Content)
	}
	if oldStill != nil {
		t.Fatalf("expected pitch-deck-saas record to be replaced in-place, still found one")
	}
}

// TestSynthesizeOnce_EnhanceFallsThroughWhenTargetMissing confirms the
// graceful fallback: if the LLM hallucinates an enhance target that
// doesn't exist, we don't error — we treat the response as a fresh
// new-skill proposal so the agent's work isn't lost.
func TestSynthesizeOnce_EnhanceFallsThroughWhenTargetMissing(t *testing.T) {
	b := newTestBroker(t)
	prov := &stubLLMProvider{
		queue: []stubLLMResponse{{
			fm: SkillFrontmatter{
				Name:        "deploy-runbook",
				Description: "Deploy a service.",
			},
			body:    "## Steps\n1. Tag.\n2. Watch.\n",
			enhance: "skill-that-does-not-exist",
		}},
	}
	cand := SkillCandidate{
		Source:        SourceNotebookCluster,
		SuggestedName: "deploy-runbook",
		SignalCount:   3,
		Excerpts: []SkillCandidateExcerpt{
			{Path: "team/agents/eng/notebook/deploy.md", Snippet: "we deploy", Author: "eng"},
			{Path: "team/agents/ops/notebook/release.md", Snippet: "we deploy", Author: "ops"},
		},
	}
	synth := newSynthWithCandidates(t, b, prov, []SkillCandidate{cand})

	res, err := synth.SynthesizeOnce(context.Background(), "manual")
	if err != nil {
		t.Fatalf("SynthesizeOnce: %v", err)
	}
	if res.Synthesized != 1 {
		t.Fatalf("Synthesized: got %d want 1 (fall-through to new-skill path)", res.Synthesized)
	}
}

func TestStageBProposalsTotalIncrementsOnSynth(t *testing.T) {
	b := newTestBroker(t)
	prov := &stubLLMProvider{
		queue: []stubLLMResponse{{
			fm: SkillFrontmatter{
				Name:        "fresh-skill",
				Description: "A fresh skill from signals.",
			},
			body: "## Steps\nDo the thing.\n",
		}},
	}
	cand := SkillCandidate{
		Source:        SourceNotebookCluster,
		SuggestedName: "fresh-skill",
		SignalCount:   1,
	}
	synth := newSynthWithCandidates(t, b, prov, []SkillCandidate{cand})
	b.SetSkillSynthesizer(synth)
	// Inject a stub Stage A scanner so compileWikiSkills doesn't touch the
	// real wiki tree (the scanner returns immediately when the wiki worker
	// is nil — but we set one anyway to belt-and-brace the fast path).
	b.SetSkillScanner(NewSkillScanner(b, &instantProvider{}, 100))

	res, err := b.compileWikiSkills(context.Background(), "", false, "manual")
	if err != nil {
		t.Fatalf("compileWikiSkills: %v", err)
	}
	if res.Proposed < 1 {
		t.Fatalf("expected res.Proposed >= 1, got %d (errors: %+v)", res.Proposed, res.Errors)
	}
	if got := atomic.LoadInt64(&b.skillCompileMetrics.StageBProposalsTotal); got != 1 {
		t.Fatalf("StageBProposalsTotal: got %d, want 1", got)
	}
}
