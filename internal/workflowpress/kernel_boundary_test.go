package workflowpress

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// kernel_boundary_test.go is the Phase-C regression: it proves per-workflow domain
// knowledge lives OUTSIDE the protected kernel, injected through the
// BlueprintRegistry seam and carried on the spec's GuardConfig, so the kernel
// stops growing per workflow.
//
// Two halves:
//
//  1. A brand-new workflow, defined ENTIRELY in this test (its state machine and
//     its own guard threshold the kernel has never seen), synthesises, freezes,
//     generates and RUNS end-to-end through the kernel — with no edit to any kernel
//     file. The new threshold resolves because it rides on the contract's
//     GuardConfig, not the runner runtime.
//  2. A source check: the per-workflow constants that USED to be hardcoded in the
//     kernel (the workflow ids, the named thresholds, the fixture alias) appear in
//     NO kernel-boundary .go file. They live only in the outside-kernel resident
//     (revops_workflows.go) and the fixtures. This is what "requires no kernel-file
//     edit" means, made mechanical.

// kernelFiles are the protected-kernel source files (doc.go's boundary): the
// contract schema, validation, generator, shipcheck, runner runtime, overlay
// machinery, synthesis machinery, discovery, freeze, executor, adapter. The
// outside-kernel resident revops_workflows.go and all *_test.go files are
// deliberately EXCLUDED — they are where per-workflow data is allowed to live.
var kernelFiles = []string{
	"adapter.go",
	"contract.go",
	"discovery.go",
	"doc.go",
	"executor.go",
	"freeze.go",
	"generate.go",
	"generate_templates.go",
	"improvement.go",
	"infer.go",
	"kernel.go",
	"redact.go",
	"runner.go",
	"schema.go",
	"schema_enums.go",
	"synthesize.go",
	"validate.go",
}

// reviveBlueprint is a FOURTH workflow that exists only here. It is a minimal but
// complete state machine guarded by a threshold (dormant_days_threshold) the kernel
// has never seen, carried on the blueprint's GuardConfig. Registering it through
// the registry must be enough to make the whole kernel pipeline run it.
func reviveBlueprint() SynthesisBlueprint {
	return SynthesisBlueprint{
		Goal:        "Revive a dormant account once it has been inactive past the dormancy threshold by drafting a win-back.",
		Operator:    defaultOperator,
		EntityOrder: []string{"Account"},
		EntityDescriptions: map[string]string{
			"Account": "A customer account with a days-since-last-activity counter.",
		},
		// dormant_days_threshold is THIS workflow's rubric constant. It is supplied by
		// the registry, never by the kernel runner — that is the whole point.
		GuardConfig: GuardConfig{
			Thresholds: map[string]float64{
				"dormant_days_threshold": 90,
			},
		},
		States: []State{
			{Name: "active", Description: "The account is active.", Initial: true, Provenance: stated(1.0)},
			{Name: "revived", Description: "A win-back was drafted for the dormant account.", Terminal: true, Provenance: stated(1.0)},
		},
		Events: []Event{
			{Name: "inactivity_checked", Trigger: TriggerScheduled, Schedule: "0 9 * * MON", From: "active", To: "revived", Guard: "is_dormant", Provenance: stated(1.0)},
		},
		Guards: []Guard{
			{Name: "is_dormant", Expr: "days_inactive >= dormant_days_threshold", Provenance: stated(1.0)},
		},
		Actions: []Action{
			// An external write inferred from the operator's by-hand outreach; inferred
			// => approval forced by synthActions, exactly like the ground-truth specs.
			{Name: "draft_winback", Kind: ActionExternalWrite, On: "inactivity_checked", Target: "email-draft", Provenance: inferred(0.65)},
		},
		VerificationScenarios: []VerificationScenario{
			{
				Name:              "dormant_account_is_revived",
				Given:             map[string]string{"days_inactive": "120"},
				When:              "inactivity_checked",
				ExpectTransitions: []Transition{{From: "active", To: "revived"}},
				ExpectApproval:    true,
			},
			{
				Name:              "active_account_is_not_revived",
				Given:             map[string]string{"days_inactive": "10"},
				When:              "inactivity_checked",
				ExpectTransitions: nil,
			},
		},
	}
}

// TestNewWorkflowGoesThroughRegistryNoKernelEdit is the Phase-C regression: a new
// workflow added through the BlueprintRegistry synthesises, freezes and RUNS
// end-to-end through the kernel — its registry-supplied threshold resolving via the
// spec's GuardConfig — with no kernel-file edit.
func TestNewWorkflowGoesThroughRegistryNoKernelEdit(t *testing.T) {
	t.Parallel()

	const id = "dormant-account-revive"

	// Register the new workflow ALONGSIDE the RevOps ones, through the public
	// registry constructor. No kernel file is touched: the blueprint and its
	// threshold are pure data handed to NewBlueprintRegistry.
	registry := NewBlueprintRegistry(map[string]SynthesisBlueprint{
		"trial-to-ae-routing":       trialToAERoutingBlueprint(),
		"renewal-risk-sweep":        renewalRiskSweepBlueprint(),
		"inbound-lead-dedupe-merge": inboundLeadDedupeMergeBlueprint(),
		id:                          reviveBlueprint(),
	})

	// Synthesize the draft from minimal research (the new workflow's id + one
	// observed entity). Synthesis is the kernel machinery; it reads the blueprint
	// from the injected registry, not from any hardcoded table.
	research := WorkflowResearch{
		SchemaVersion: SchemaVersionWorkflowResearch,
		WorkflowID:    id,
		InferredSchemas: []InferredSchema{
			{Entity: "Account", SampleCount: 3, Fields: []InferredField{{Name: "id", PresentCount: 3}, {Name: "days_inactive", PresentCount: 3}}},
		},
	}
	draft, err := Synthesize(research, registry)
	if err != nil {
		t.Fatalf("Synthesize(%s): %v", id, err)
	}

	// The blueprint's threshold rode onto the contract's GuardConfig — proving the
	// per-workflow guard constant travels on the spec, not the kernel.
	if got := draft.GuardConfig.Thresholds["dormant_days_threshold"]; got != 90 {
		t.Fatalf("draft GuardConfig threshold = %v, want 90 (must be carried from the registry blueprint)", got)
	}

	// Freeze the draft through the human gate, then run it through the kernel
	// Runner. The frozen contract carries its own threshold; NewRunner wires it
	// into the default evaluator with no per-workflow kernel knowledge.
	frozen, err := Freeze(draft, FreezeRequest{
		WorkflowID: id,
		Version:    draft.Version,
		Decision:   DecisionApprove,
		Operator:   defaultOperator,
	})
	if err != nil {
		t.Fatalf("Freeze(%s): %v", id, err)
	}

	// Generate the local tool and prove the mechanical gate passes — the full
	// kernel pipeline (generate + shipcheck) accepts the new workflow unchanged.
	gen, err := Generate(&frozen.Spec)
	if err != nil {
		t.Fatalf("Generate(%s): %v", id, err)
	}
	report, err := RunShipcheck(&frozen.Spec, gen)
	if err != nil {
		t.Fatalf("RunShipcheck(%s): %v", id, err)
	}
	if !report.Passed {
		t.Fatalf("shipcheck failed for the new workflow: %+v", report.Findings)
	}

	// Run the dormant scenario: days_inactive 120 >= the registry-supplied
	// threshold 90, so the account is revived. A bare DefaultGuardEvaluator{} is
	// enriched by NewRunner from spec.GuardConfig, so the new threshold resolves
	// without the kernel knowing it.
	runner, err := NewRunner(&frozen.Spec, NewHostExecutor(), DefaultGuardEvaluator{})
	if err != nil {
		t.Fatalf("NewRunner(%s): %v", id, err)
	}
	res, err := runner.Run(context.Background(), RunInput{
		Event:  "inactivity_checked",
		Fields: map[string]string{"days_inactive": "120"},
	})
	if err != nil {
		t.Fatalf("Run(%s): %v", id, err)
	}
	if res.FinalState != "revived" {
		t.Fatalf("final state = %q, want revived (registry threshold must resolve)", res.FinalState)
	}
	if !res.ApprovalRequested {
		t.Error("expected the inferred external write to request approval")
	}

	// Below the threshold the machine must NOT advance — confirming the threshold
	// is actually consulted, not ignored.
	resActive, err := NewRunnerAndRun(t, &frozen.Spec, RunInput{
		Event:  "inactivity_checked",
		Fields: map[string]string{"days_inactive": "10"},
	})
	if err != nil {
		t.Fatalf("Run(active, %s): %v", id, err)
	}
	if resActive.FinalState != "active" || len(resActive.Transitions) != 0 {
		t.Fatalf("active account advanced: final %q, %d transitions", resActive.FinalState, len(resActive.Transitions))
	}
}

// NewRunnerAndRun is a tiny helper that builds a runner over spec and runs one
// input, so the regression test can assert both the dormant and active paths
// without repeating the construction.
func NewRunnerAndRun(t *testing.T, spec *WorkflowSpec, in RunInput) (*RunResult, error) {
	t.Helper()
	r, err := NewRunner(spec, NewHostExecutor(), DefaultGuardEvaluator{})
	if err != nil {
		return nil, err
	}
	return r.Run(context.Background(), in)
}

// TestKernelFilesHoldNoPerWorkflowData is the source-level half of the regression:
// the kernel-resident CONSTRUCTS that used to hold per-workflow data — the
// blueprints registry literal (synthesize.go) and the guard threshold/alias maps
// (runner.go) — must be gone from every kernel-boundary file. The check targets
// the data-bearing declarations, not illustrative comments: a kernel file may name
// a workflow as an example in a doc comment, but it must not DECLARE a
// workflow-keyed blueprint table or a hardcoded threshold/alias map.
//
// This makes "adding a new workflow requires no kernel-file edit" mechanical: those
// declarations are exactly what a new workflow would have forced an edit to, and
// they no longer exist in the kernel — the data lives in the registry/spec.
func TestKernelFilesHoldNoPerWorkflowData(t *testing.T) {
	t.Parallel()

	// Code fragments that, if present, mean per-workflow data is being DECLARED
	// inside the kernel. These are the precise constructs Phase C relocated.
	forbidden := []struct {
		fragment string
		why      string
	}{
		{"defaultThresholds", "named-threshold map was relocated to the spec's GuardConfig / the outside-kernel registry"},
		{"defaultFixtureAliases", "fixture-alias map was relocated to the spec's GuardConfig / the outside-kernel registry"},
		{"blueprints = map[string]SynthesisBlueprint", "the workflow-keyed blueprint registry was relocated to the outside-kernel RevOpsRegistry"},
		{"trialToAERoutingBlueprint()", "blueprint constructors live in the outside-kernel resident, not the kernel"},
		{"renewalRiskSweepBlueprint()", "blueprint constructors live in the outside-kernel resident, not the kernel"},
		{"inboundLeadDedupeMergeBlueprint()", "blueprint constructors live in the outside-kernel resident, not the kernel"},
	}

	for _, file := range kernelFiles {
		file := file
		t.Run(file, func(t *testing.T) {
			t.Parallel()
			raw, err := os.ReadFile(filepath.Clean(file))
			if err != nil {
				t.Fatalf("reading kernel file %q: %v", file, err)
			}
			src := string(raw)
			for _, f := range forbidden {
				if strings.Contains(src, f.fragment) {
					t.Errorf("kernel file %s declares per-workflow data %q — %s", file, f.fragment, f.why)
				}
			}
		})
	}
}

// TestPerWorkflowDataLivesOutsideTheKernel is the positive half: the per-workflow
// data the kernel no longer holds must actually exist in the outside-kernel
// resident. This stops a regression where the data is deleted from the kernel but
// never relocated (which would silently break synthesis). It also guards that the
// resident is NOT in the kernel-file list, so the boundary stays honest.
func TestPerWorkflowDataLivesOutsideTheKernel(t *testing.T) {
	t.Parallel()

	const resident = "revops_workflows.go"
	for _, kf := range kernelFiles {
		if kf == resident {
			t.Fatalf("%s is listed as a kernel file, but it is the outside-kernel resident", resident)
		}
	}

	raw, err := os.ReadFile(filepath.Clean(resident))
	if err != nil {
		t.Fatalf("reading resident %q: %v", resident, err)
	}
	src := string(raw)
	// The blueprint constructors and the per-workflow guard constants must live
	// here, outside the kernel.
	for _, want := range []string{
		"trialToAERoutingBlueprint",
		"renewalRiskSweepBlueprint",
		"inboundLeadDedupeMergeBlueprint",
		"icp_threshold",
		"match_threshold",
		"renewal_date", // the fixture-alias key
	} {
		if !strings.Contains(src, want) {
			t.Errorf("outside-kernel resident %s is missing per-workflow datum %q", resident, want)
		}
	}
}

// TestRevOpsRegistryRegistersAllThreeGroundTruthWorkflows guards that the
// outside-kernel registry still carries the three ground-truth workflows, so the
// relocation did not drop one.
func TestRevOpsRegistryRegistersAllThreeGroundTruthWorkflows(t *testing.T) {
	t.Parallel()
	reg := RevOpsRegistry()
	for _, name := range exampleNames {
		if _, ok := reg.Blueprint(name); !ok {
			t.Errorf("RevOpsRegistry missing blueprint for %q", name)
		}
	}
}
