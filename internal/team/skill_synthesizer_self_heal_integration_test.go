package team

// skill_synthesizer_self_heal_integration_test.go wires the SelfHeal
// scanner → StageBSignalAggregator → SkillSynthesizer chain end-to-end
// against an in-memory broker. The LLM provider is stubbed via
// stageBLLMProvider so the test stays hermetic, but the candidate is
// produced by the real SelfHealSignalScanner from a seeded incident task.

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nex-crm/wuphf/internal/agent"
)

// TestSynthesizer_SelfHealEndToEnd_WritesProposedSkill is the flagship
// integration test for the wiring: a resolved self-heal incident lands on
// the broker, the aggregator scans it, the synthesizer asks the (stub) LLM,
// the response is accepted, and the skill ends up in b.skills with the
// expected provenance markers.
func TestSynthesizer_SelfHealEndToEnd_WritesProposedSkill(t *testing.T) {
	b := newTestBroker(t)

	now := time.Now().UTC()
	seedSelfHealTask(t, b, teamTask{
		ID:         "task-101",
		Title:      selfHealingTaskTitle("deploy-bot", "task-7"),
		Details:    selfHealingTaskDetails("deploy-bot", "task-7", agent.EscalationCapabilityGap, "missing deploy specialist"),
		Owner:      "deploy-bot",
		Status:     "done",
		PipelineID: "incident",
		TaskType:   "incident",
		CreatedAt:  now.Add(-30 * time.Minute).Format(time.RFC3339),
		UpdatedAt:  now.Format(time.RFC3339),
	})

	// Wire the real aggregator (which contains the SelfHealSignalScanner)
	// against a stub LLM provider that returns a valid handle- skill.
	agg := NewStageBSignalAggregator(b)
	prov := &stubLLMProvider{
		queue: []stubLLMResponse{{
			fm: SkillFrontmatter{
				Name:        "handle-capability-gap",
				Description: "when blocked because a deploy specialist is missing, run discovery and add the relay.",
			},
			body: "## When this fires\nThe agent reports a capability_gap.\n\n## Steps\n1. Discover available relays.\n2. Add the relay.\n\n## Source incident\ntask-101\n",
		}},
	}
	synth := NewSkillSynthesizer(b, agg)
	synth.provider = prov

	res, err := synth.SynthesizeOnce(context.Background(), "manual")
	if err != nil {
		t.Fatalf("SynthesizeOnce: %v", err)
	}
	if res.Synthesized != 1 {
		t.Fatalf("Synthesized: got %d want 1 (errors: %+v)", res.Synthesized, res.Errors)
	}
	if res.CandidatesScanned != 1 {
		t.Errorf("CandidatesScanned: got %d want 1", res.CandidatesScanned)
	}
	if prov.calls.Load() != 1 {
		t.Errorf("LLM calls: got %d want 1", prov.calls.Load())
	}

	// The skill should now be in b.skills.
	b.mu.Lock()
	skill := b.findSkillByNameLocked("handle-capability-gap")
	b.mu.Unlock()
	if skill == nil {
		t.Fatal("expected handle-capability-gap in b.skills")
	}
	if !strings.HasPrefix(skill.Name, "handle-") {
		t.Errorf("name should start with handle-, got %q", skill.Name)
	}
	if skill.Status != "proposed" {
		t.Errorf("status: got %q want proposed", skill.Status)
	}
	if !strings.Contains(skill.Content, "## Source incident") {
		t.Errorf("expected ## Source incident citation in body, got:\n%s", skill.Content)
	}
	if !strings.Contains(skill.Content, "task-101") {
		t.Errorf("expected task-101 citation in body or signals footer, got:\n%s", skill.Content)
	}
	if !strings.Contains(skill.Content, "## Signals") {
		t.Errorf("expected Signals footer in body, got:\n%s", skill.Content)
	}
	// The Signals footer must include the incident task ID as the source
	// path so source_signals provenance is preserved.
	if !strings.Contains(skill.Content, "`task-101`") {
		t.Errorf("expected backtick-wrapped task-101 in Signals footer, got:\n%s", skill.Content)
	}
}

// TestSynthesizer_SelfHealEndToEnd_LLMRejectsCandidate exercises the
// rejection path: the aggregator surfaces the candidate, the LLM says no,
// nothing is written to b.skills, and the rejection is captured in the
// result errors.
func TestSynthesizer_SelfHealEndToEnd_LLMRejectsCandidate(t *testing.T) {
	b := newTestBroker(t)

	now := time.Now().UTC()
	seedSelfHealTask(t, b, teamTask{
		ID:         "task-202",
		Title:      selfHealingTaskTitle("deploy-bot", "task-8"),
		Details:    selfHealingTaskDetails("deploy-bot", "task-8", agent.EscalationCapabilityGap, "one-off env quirk"),
		Owner:      "deploy-bot",
		Status:     "done",
		PipelineID: "incident",
		TaskType:   "incident",
		CreatedAt:  now.Add(-time.Hour).Format(time.RFC3339),
		UpdatedAt:  now.Format(time.RFC3339),
	})

	agg := NewStageBSignalAggregator(b)
	prov := &stubLLMProvider{respondNotSkill: true}
	synth := NewSkillSynthesizer(b, agg)
	synth.provider = prov

	res, err := synth.SynthesizeOnce(context.Background(), "manual")
	if err != nil {
		t.Fatalf("SynthesizeOnce: %v", err)
	}
	if res.Synthesized != 0 {
		t.Errorf("Synthesized: got %d want 0", res.Synthesized)
	}
	if len(res.Errors) == 0 {
		t.Errorf("expected rejection captured in errors slice")
	}

	b.mu.Lock()
	skillCount := len(b.skills)
	b.mu.Unlock()
	if skillCount != 0 {
		t.Errorf("expected no skills, got %d", skillCount)
	}
}

// TestSynthesizer_SelfHealEndToEnd_StageBProposalsTotalIncrements exercises
// the compileWikiSkills wrapper: a self-heal candidate accepted by the LLM
// must show up in StageBProposalsTotal so the /skills/compile/stats endpoint
// surfaces the lift.
func TestSynthesizer_SelfHealEndToEnd_StageBProposalsTotalIncrements(t *testing.T) {
	b := newTestBroker(t)

	now := time.Now().UTC()
	seedSelfHealTask(t, b, teamTask{
		ID:         "task-303",
		Title:      selfHealingTaskTitle("deploy-bot", "task-9"),
		Details:    selfHealingTaskDetails("deploy-bot", "task-9", agent.EscalationCapabilityGap, "missing relay"),
		Owner:      "deploy-bot",
		Status:     "done",
		PipelineID: "incident",
		TaskType:   "incident",
		CreatedAt:  now.Add(-time.Hour).Format(time.RFC3339),
		UpdatedAt:  now.Format(time.RFC3339),
	})

	// Wire the synthesizer with a stub LLM that approves the candidate.
	agg := NewStageBSignalAggregator(b)
	prov := &stubLLMProvider{
		queue: []stubLLMResponse{{
			fm: SkillFrontmatter{
				Name:        "handle-capability-gap-relay",
				Description: "when blocked because the relay is missing, discover and add it.",
			},
			body: "## When this fires\nA capability_gap blocks deploy.\n\n## Steps\n1. Discover.\n2. Add.\n",
		}},
	}
	synth := NewSkillSynthesizer(b, agg)
	synth.provider = prov
	b.SetSkillSynthesizer(synth)
	// Inject a stub Stage A scanner so compileWikiSkills doesn't touch the
	// real wiki tree.
	b.SetSkillScanner(NewSkillScanner(b, &instantProvider{}, 100))

	if _, err := b.compileWikiSkills(context.Background(), "", false, "manual"); err != nil {
		t.Fatalf("compileWikiSkills: %v", err)
	}
	if got := atomic.LoadInt64(&b.skillCompileMetrics.StageBProposalsTotal); got != 1 {
		t.Errorf("StageBProposalsTotal: got %d want 1", got)
	}
}
