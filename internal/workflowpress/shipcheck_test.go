package workflowpress

import (
	"context"
	"testing"
)

// shipcheck_test.go locks down the mechanical-proof gate:
//
//   - the positive path: shipcheck PASSES over all three generated ground-truth
//     workflows, and every one of the seven checks individually passes;
//   - the negative path: a DEFECT seeded in the dedupe-merge runner (a broken
//     idempotency that double-applies the merge on re-run) is CAUGHT by shipcheck;
//   - the gate's guards: a spec/gen mismatch, a nil input, and an invalid spec are
//     refused before any check runs.
//
// The negative test injects the defect through the ShipcheckOptions runner-factory
// seam — it never touches the kernel Runner, mirroring the architecture rule that
// improvements (and, here, fault injection) live OUTSIDE the protected kernel.

// generateForCheck Generates the workflow for a loaded example, failing the test
// on error. The generated artifact is the second half of the (spec, gen) pair the
// gate proves.
func generateForCheck(t *testing.T, name string) (*WorkflowSpec, *GeneratedWorkflow) {
	t.Helper()
	spec := loadExample(t, name)
	gen, err := Generate(spec)
	if err != nil {
		t.Fatalf("Generate(%s): %v", name, err)
	}
	return spec, gen
}

// findingByName returns the finding for the named check, or fails the test if the
// report does not carry it (every report must carry all seven checks).
func findingByName(t *testing.T, report *ShipcheckReport, check string) ShipcheckFinding {
	t.Helper()
	for _, f := range report.Findings {
		if f.Check == check {
			return f
		}
	}
	t.Fatalf("report carries no finding for check %q", check)
	return ShipcheckFinding{}
}

// TestShipcheckPassesEveryGeneratedWorkflow is the core positive gate: shipcheck
// over all three generated ground-truth workflows passes, and each of the seven
// mechanical-proof checks passes individually with a deterministic, ordered
// findings list.
func TestShipcheckPassesEveryGeneratedWorkflow(t *testing.T) {
	t.Parallel()
	wantChecks := []string{
		checkFixtureReplay,
		checkTransitionCoverage,
		checkIdempotency,
		checkDuplicateHandling,
		checkStaleHandling,
		checkAuditCompleteness,
		checkAdapterParity,
	}
	for _, name := range exampleNames {
		name := name
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			spec, gen := generateForCheck(t, name)

			report, err := RunShipcheck(spec, gen)
			if err != nil {
				t.Fatalf("RunShipcheck(%s): %v", name, err)
			}
			if !report.Passed {
				for _, f := range report.Findings {
					t.Logf("  %s: passed=%v detail=%s", f.Check, f.Passed, f.Detail)
				}
				t.Fatalf("shipcheck failed for %s", name)
			}
			if report.WorkflowID != spec.ID || report.Version != spec.Version {
				t.Errorf("report id/version = %s/%d, want %s/%d", report.WorkflowID, report.Version, spec.ID, spec.Version)
			}
			// All seven checks present, in the fixed order, each passing.
			if len(report.Findings) != len(wantChecks) {
				t.Fatalf("got %d findings, want %d", len(report.Findings), len(wantChecks))
			}
			for i, want := range wantChecks {
				if report.Findings[i].Check != want {
					t.Errorf("finding %d = %q, want %q (order must be stable)", i, report.Findings[i].Check, want)
				}
				if !report.Findings[i].Passed {
					t.Errorf("%s: check %q failed: %s", name, report.Findings[i].Check, report.Findings[i].Detail)
				}
			}
		})
	}
}

// TestShipcheckIsDeterministic proves the gate's output is reproducible: two runs
// over the same (spec, gen) yield identical findings in identical order.
func TestShipcheckIsDeterministic(t *testing.T) {
	t.Parallel()
	spec, gen := generateForCheck(t, "inbound-lead-dedupe-merge")
	a, err := RunShipcheck(spec, gen)
	if err != nil {
		t.Fatalf("first run: %v", err)
	}
	b, err := RunShipcheck(spec, gen)
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if a.Passed != b.Passed || len(a.Findings) != len(b.Findings) {
		t.Fatalf("non-deterministic report shape")
	}
	for i := range a.Findings {
		if a.Findings[i] != b.Findings[i] {
			t.Errorf("finding %d drifted: %+v vs %+v", i, a.Findings[i], b.Findings[i])
		}
	}
}

// --- the negative path: a seeded defect must be CAUGHT ---

// doubleApplyRunner is a DEFECTIVE runtime double used only by the negative test.
// It wraps the real kernel Runner and DOUBLE-APPLIES the named idempotent action
// on EVERY run by appending a duplicate ActionCall to the result. This simulates a
// dedupe-merge runner whose merge is not actually idempotent: the merge applies
// twice within a single processing. The defect is deliberately ALWAYS-ON (not
// re-run-only) so it is internally consistent across runs — a gate that only
// compared first-vs-second re-runs would MISS it; shipcheck's within-run check
// catches it. The defect lives entirely outside the kernel.
type doubleApplyRunner struct {
	inner  *Runner
	action string
}

func (d *doubleApplyRunner) Run(ctx context.Context, in RunInput) (*RunResult, error) {
	res, err := d.inner.Run(ctx, in)
	if err != nil {
		return nil, err
	}
	for _, a := range res.Actions {
		if a.Name == d.action {
			res.Actions = append(res.Actions, a) // duplicate apply: the double-merge defect
			break
		}
	}
	return res, nil
}

// TestShipcheckCatchesBrokenIdempotency is the load-bearing negative test: seed a
// defect in the dedupe-merge runner that double-applies the merge on re-delivery,
// and assert shipcheck CATCHES it — the report fails and the idempotency check is
// the one that flags it. A gate that cannot catch a broken merge is not a gate.
func TestShipcheckCatchesBrokenIdempotency(t *testing.T) {
	t.Parallel()
	spec, gen := generateForCheck(t, "inbound-lead-dedupe-merge")

	// Inject the defect via the runner-factory seam. NewRunner builds the real
	// kernel Runner; doubleApplyRunner wraps it to double-merge on re-run.
	defective := newShipcheckWithOptions(ShipcheckOptions{
		RunnerFactory: func(s *WorkflowSpec) (runtime, error) {
			inner, err := NewRunner(s, NewHostExecutor(), DefaultGuardEvaluator{})
			if err != nil {
				return nil, err
			}
			return &doubleApplyRunner{inner: inner, action: "merge_into_existing"}, nil
		},
	})

	report, err := defective.Check(context.Background(), spec, gen)
	if err != nil {
		t.Fatalf("Check with defect: %v", err)
	}
	if report.Passed {
		t.Fatal("shipcheck PASSED a runner with broken idempotency; the gate failed to catch the defect")
	}
	// The idempotency check must be the one that flags the double-apply.
	idem := findingByName(t, report, checkIdempotency)
	if idem.Passed {
		t.Errorf("idempotency check passed despite the seeded double-apply; detail=%s", idem.Detail)
	}
}

// TestShipcheckUnbrokenRunnerPasses is the negative test's control: the SAME
// factory seam, but wrapping the runner WITHOUT the defect (an action name that is
// never fired), proves the seam itself does not spuriously fail — the failure in
// the test above is the defect, not the injection mechanism.
func TestShipcheckUnbrokenRunnerPasses(t *testing.T) {
	t.Parallel()
	spec, gen := generateForCheck(t, "inbound-lead-dedupe-merge")
	clean := newShipcheckWithOptions(ShipcheckOptions{
		RunnerFactory: func(s *WorkflowSpec) (runtime, error) {
			inner, err := NewRunner(s, NewHostExecutor(), DefaultGuardEvaluator{})
			if err != nil {
				return nil, err
			}
			// "no_such_action" is never fired, so doubleApplyRunner never duplicates.
			return &doubleApplyRunner{inner: inner, action: "no_such_action"}, nil
		},
	})
	report, err := clean.Check(context.Background(), spec, gen)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !report.Passed {
		for _, f := range report.Findings {
			t.Logf("  %s: passed=%v detail=%s", f.Check, f.Passed, f.Detail)
		}
		t.Fatal("control runner (no defect) failed shipcheck; the injection seam is not behavior-preserving")
	}
}

// --- the gate's input guards ---

// TestShipcheckRejectsSpecGenMismatch proves the gate refuses a generated workflow
// that was produced from a different spec id/version, rather than silently proving
// the wrong tool.
func TestShipcheckRejectsSpecGenMismatch(t *testing.T) {
	t.Parallel()
	spec, gen := generateForCheck(t, "trial-to-ae-routing")
	gen.Version = gen.Version + 1 // simulate a stale/mismatched generated artifact
	if _, err := RunShipcheck(spec, gen); err == nil {
		t.Fatal("expected an error for a spec/gen version mismatch")
	}

	gen2 := &GeneratedWorkflow{WorkflowID: "some-other-id", Version: spec.Version, Files: gen.Files}
	if _, err := RunShipcheck(spec, gen2); err == nil {
		t.Fatal("expected an error for a spec/gen id mismatch")
	}
}

// TestShipcheckRejectsNilInputs proves the gate fails loudly on a nil spec or a
// nil generated workflow.
func TestShipcheckRejectsNilInputs(t *testing.T) {
	t.Parallel()
	_, gen := generateForCheck(t, "trial-to-ae-routing")
	if _, err := RunShipcheck(nil, gen); err == nil {
		t.Error("expected an error for a nil spec")
	}
	spec := loadExample(t, "trial-to-ae-routing")
	if _, err := RunShipcheck(spec, nil); err == nil {
		t.Error("expected an error for a nil generated workflow")
	}
}

// TestShipcheckSeamHonoursCancelledContext proves the kernel Shipcheck seam
// short-circuits on a cancelled context before any replay.
func TestShipcheckSeamHonoursCancelledContext(t *testing.T) {
	t.Parallel()
	spec, gen := generateForCheck(t, "trial-to-ae-routing")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := NewShipcheck().Check(ctx, spec, gen); err == nil {
		t.Error("expected an error for a cancelled context")
	}
}
