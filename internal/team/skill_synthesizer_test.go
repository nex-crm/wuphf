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
	fm   SkillFrontmatter
	body string
	err  error
}

func (p *stubLLMProvider) SynthesizeSkill(_ context.Context, _ SkillCandidate, _ string) (SkillFrontmatter, string, error) {
	p.calls.Add(1)
	if p.respondNotSkill {
		return SkillFrontmatter{}, "", errors.New("synth: candidate rejected by LLM as not-a-skill")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.queue) == 0 {
		return SkillFrontmatter{}, "", errors.New("synth: candidate rejected by LLM as not-a-skill")
	}
	r := p.queue[0]
	p.queue = p.queue[1:]
	return r.fm, r.body, r.err
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

// TestSynthesizeOnce_SimilarityDivertsToEnhance exercises the
// errors.As(writeErr, &errSkillSimilarToExisting) wiring in SynthesizeOnce:
// when writeSkillProposalLocked returns the sentinel, the synthesizer
// must (a) NOT write the candidate to b.skills, (b) NOT count it as an
// error or as Synthesized, (c) bump res.EnhancementCandidates, and
// (d) append an enhance_skill_proposal interview pointing at the
// existing skill's slug.
func TestSynthesizeOnce_SimilarityDivertsToEnhance(t *testing.T) {
	b := newTestBroker(t)

	// Seed an active skill the candidate will collide with.
	b.mu.Lock()
	addSkill(b, "send-invoice-reminder", "Send the AR follow-up at d7.",
		"candidate-text body that the embedder keys off.")
	b.mu.Unlock()

	// Stub embedder returns the same vector for both candidate and
	// existing → cosine = 1.0, well above the enhance threshold (0.85).
	v := l2norm([]float32{1, 0, 0})
	b.skillEmbedder = staticVecEmbedder(v, v)

	prov := &stubLLMProvider{
		queue: []stubLLMResponse{{
			fm: SkillFrontmatter{
				Name:        "invoice-d7-reminder",
				Description: "AR reminder for the d7 cohort.",
			},
			body: "## Steps\n1. Pull the AR list.\n2. Send the reminder.\n",
		}},
	}
	cand := SkillCandidate{
		Source:               SourceNotebookCluster,
		SuggestedName:        "invoice-d7-reminder",
		SuggestedDescription: "AR reminder for the d7 cohort.",
		SignalCount:          2,
		Excerpts: []SkillCandidateExcerpt{
			{Path: "team/agents/csm/notebook/ar.md", Snippet: "AR followup", Author: "csm"},
		},
	}
	synth := newSynthWithCandidates(t, b, prov, []SkillCandidate{cand})

	requestsBefore := len(b.requests)

	res, err := synth.SynthesizeOnce(context.Background(), "manual")
	if err != nil {
		t.Fatalf("SynthesizeOnce: %v", err)
	}
	if res.Synthesized != 0 {
		t.Errorf("Synthesized: got %d, want 0 (sentinel must not count as a synth)", res.Synthesized)
	}
	if res.EnhancementCandidates != 1 {
		t.Fatalf("EnhancementCandidates: got %d, want 1", res.EnhancementCandidates)
	}
	if len(res.Errors) != 0 {
		t.Errorf("Errors: got %+v, want empty (similarity divert is not an error)", res.Errors)
	}

	// Candidate must NOT have been written.
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.findSkillByNameLocked("invoice-d7-reminder") != nil {
		t.Error("similar candidate must not be written to b.skills on enhance verdict")
	}

	// An enhance_skill_proposal interview must have been appended pointing
	// at the existing skill.
	if got, want := len(b.requests), requestsBefore+1; got != want {
		t.Fatalf("requests len: got %d, want %d", got, want)
	}
	last := b.requests[len(b.requests)-1]
	if last.Kind != "enhance_skill_proposal" {
		t.Errorf("interview kind: got %q, want enhance_skill_proposal", last.Kind)
	}
	if last.ReplyTo != "invoice-d7-reminder" {
		t.Errorf("interview ReplyTo: got %q, want invoice-d7-reminder", last.ReplyTo)
	}
	if last.Channel == "" {
		t.Error("interview channel must default to general, got empty")
	}
	if !strings.Contains(last.Title, "send-invoice-reminder") {
		t.Errorf("interview title should reference the existing slug, got %q", last.Title)
	}
	if !strings.Contains(last.Question, "send-invoice-reminder") {
		t.Errorf("interview question should reference the existing slug, got %q", last.Question)
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
