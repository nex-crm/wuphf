package workflowpress

import (
	"context"
	"errors"
	"testing"
)

// runner_test.go locks down the runtime kernel directly: the deterministic guard
// evaluator (the subtlest piece) and the Runner's error/edge behavior, separately
// from the end-to-end generate-then-run gate in generate_test.go.

// TestDefaultGuardEvaluator is a table test over the guard shapes the three
// ground-truth specs use, plus their boundary cases. It proves the evaluator
// resolves operands from the fixture, applies named-threshold defaults, handles
// the renewal date-arithmetic shape, and fails a guard (false, no error) when its
// data is absent — never advancing the machine on missing data.
func TestDefaultGuardEvaluator(t *testing.T) {
	t.Parallel()
	eval := DefaultGuardEvaluator{}
	tests := []struct {
		name   string
		expr   string
		fields map[string]string
		want   bool
	}{
		{"icp_at_threshold", "icp_score >= icp_threshold", map[string]string{"icp_score": "50"}, true},
		{"icp_above_threshold", "icp_score >= icp_threshold", map[string]string{"icp_score": "82"}, true},
		{"icp_below_threshold", "icp_score >= icp_threshold", map[string]string{"icp_score": "20"}, false},
		{"match_confident", "best_candidate.match_score >= match_threshold", map[string]string{"match_score": "0.96"}, true},
		{"match_weak", "best_candidate.match_score >= match_threshold", map[string]string{"match_score": "0.10"}, false},
		{"no_match_inverse_true", "best_candidate.match_score < match_threshold", map[string]string{"match_score": "0.10"}, true},
		{"no_match_inverse_false", "best_candidate.match_score < match_threshold", map[string]string{"match_score": "0.96"}, false},
		{"usage_drop_over_20", "usage_trend.delta_pct < -0.20", map[string]string{"delta_pct": "-0.35"}, true},
		{"usage_drop_at_20_not_over", "usage_trend.delta_pct < -0.20", map[string]string{"delta_pct": "-0.20"}, false},
		{"usage_drop_small", "usage_trend.delta_pct < -0.20", map[string]string{"delta_pct": "-0.05"}, false},
		{"renewal_within_window", "renewal_date - now <= 60d", map[string]string{"renewal_in_days": "30"}, true},
		{"renewal_at_window", "renewal_date - now <= 60d", map[string]string{"renewal_in_days": "60"}, true},
		{"renewal_past_window", "renewal_date - now <= 60d", map[string]string{"renewal_in_days": "120"}, false},
		{"absent_lhs_is_false", "icp_score >= icp_threshold", map[string]string{}, false},
		{"absent_renewal_is_false", "renewal_date - now <= 60d", map[string]string{}, false},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := eval.Eval(Guard{Name: tc.name, Expr: tc.expr}, tc.fields)
			if err != nil {
				t.Fatalf("Eval(%q): unexpected error %v", tc.expr, err)
			}
			if got != tc.want {
				t.Errorf("Eval(%q, %v) = %v, want %v", tc.expr, tc.fields, got, tc.want)
			}
		})
	}
}

// TestGuardEvaluatorRejectsUnparseableExpr proves a guard with no comparison
// operator errors (a malformed contract is a loud failure, not a silent false).
func TestGuardEvaluatorRejectsUnparseableExpr(t *testing.T) {
	t.Parallel()
	_, err := DefaultGuardEvaluator{}.Eval(Guard{Name: "bad", Expr: "always_true"}, nil)
	if err == nil {
		t.Fatal("expected an error for an expression with no comparison operator")
	}
}

// TestNewRunnerRejectsInvalidSpec proves NewRunner re-validates: it is an
// execution boundary a hand-built or JSON-loaded spec can reach without ever
// passing the freeze gate. A spec carrying an inferred external-write with
// RequiresApproval=false (the write-approval bypass) — or an unknown ActionKind
// that fails open past that rule — must be refused at construction, never reach
// Run. Regression for the NewRunner-does-not-validate bypass.
func TestNewRunnerRejectsInvalidSpec(t *testing.T) {
	t.Parallel()

	prov := Provenance{TrustTier: TrustOperatorStated, Confidence: 1.0}
	base := func() *WorkflowSpec {
		return &WorkflowSpec{
			ID:       "wf",
			Version:  1,
			Goal:     "do the thing",
			Operator: "revops",
			States: []State{
				{Name: "start", Initial: true, Provenance: prov},
				{Name: "done", Terminal: true, Provenance: prov},
			},
			Events: []Event{
				{Name: "go", Trigger: TriggerExternal, From: "start", To: "done", Provenance: prov},
			},
			VerificationScenarios: []VerificationScenario{
				{Name: "happy", When: "go", ExpectTransitions: []Transition{{From: "start", To: "done"}}},
			},
		}
	}

	tests := []struct {
		name   string
		action Action
	}{
		{
			name: "inferred external-write skipping approval",
			action: Action{
				Name: "post_it", Kind: ActionExternalWrite, On: "go",
				RequiresApproval: false,
				Provenance:       Provenance{TrustTier: TrustInferred, Confidence: 0.5},
			},
		},
		{
			name: "unknown action kind failing open past approval",
			action: Action{
				Name: "send_blast", Kind: ActionKind("send-email-blast"), On: "go",
				RequiresApproval: false,
				Provenance:       Provenance{TrustTier: TrustInferred, Confidence: 0.9},
			},
		},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			spec := base()
			spec.Actions = []Action{tc.action}
			r, err := NewRunner(spec, NewHostExecutor(), nil)
			if err == nil {
				t.Fatal("NewRunner accepted an invalid spec; the approval gate was bypassed")
			}
			if !errors.Is(err, ErrInvalidSpec) {
				t.Fatalf("NewRunner err = %v, want wrapping ErrInvalidSpec", err)
			}
			if r != nil {
				t.Error("NewRunner returned a non-nil runner alongside an error")
			}
		})
	}
}

// TestRunnerRejectsUnknownEvent proves Run fails loudly on an event the spec does
// not define, wrapping ErrRunner.
func TestRunnerRejectsUnknownEvent(t *testing.T) {
	t.Parallel()
	spec := loadExample(t, "trial-to-ae-routing")
	r, err := NewRunner(spec, NewHostExecutor(), nil)
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	_, err = r.Run(context.Background(), RunInput{Event: "no_such_event"})
	if !errors.Is(err, ErrRunner) {
		t.Fatalf("got %v, want ErrRunner", err)
	}
}

// TestRunnerNilGuardUsesDefault proves a nil GuardEvaluator falls back to the
// default fixture-driven evaluator (callers need not supply one).
func TestRunnerNilGuardUsesDefault(t *testing.T) {
	t.Parallel()
	spec := loadExample(t, "trial-to-ae-routing")
	r, err := NewRunner(spec, nil, nil)
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	res, err := r.Run(context.Background(), RunInput{
		Event:  "trial_signed_up",
		Fields: map[string]string{"icp_score": "82"},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.FinalState != "routed" {
		t.Errorf("final state = %q, want routed (default guard should pass)", res.FinalState)
	}
}

// TestGenerateRejectsInvalidSpec proves the generator refuses an invalid spec
// rather than emitting a broken tool — the structural half of the freeze gate is
// upstream, but Generate re-checks defensively.
func TestGenerateRejectsInvalidSpec(t *testing.T) {
	t.Parallel()
	// A spec with a write-action that skips approval is invalid (write-needs-
	// approval rule); Generate must refuse it.
	bad := &WorkflowSpec{
		ID:       "broken",
		Version:  1,
		Goal:     "g",
		Operator: "revops",
		States: []State{
			{Name: "a", Initial: true, Provenance: Provenance{TrustTier: TrustObserved, Confidence: 1}},
			{Name: "b", Terminal: true, Provenance: Provenance{TrustTier: TrustObserved, Confidence: 1}},
		},
		Events: []Event{
			{Name: "go", Trigger: TriggerExternal, From: "a", To: "b", Provenance: Provenance{TrustTier: TrustObserved, Confidence: 1}},
		},
		Actions: []Action{
			{Name: "w", Kind: ActionExternalWrite, On: "go", RequiresApproval: false, Provenance: Provenance{TrustTier: TrustInferred, Confidence: 0.5}},
		},
		VerificationScenarios: []VerificationScenario{
			{Name: "s", When: "go", ExpectTransitions: []Transition{{From: "a", To: "b"}}},
		},
	}
	if _, err := Generate(bad); !errors.Is(err, ErrInvalidSpec) {
		t.Fatalf("Generate(invalid) = %v, want ErrInvalidSpec", err)
	}
	if _, err := Generate(nil); !errors.Is(err, ErrInvalidSpec) {
		t.Fatalf("Generate(nil) = %v, want ErrInvalidSpec", err)
	}
}
