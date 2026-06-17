package workflow

import (
	"path/filepath"
	"testing"
)

func TestReferralSpecShipchecks(t *testing.T) {
	s := mustLoad(t)
	rep := Shipcheck(s)
	t.Logf("\n%s", rep.String())
	if !rep.Passed {
		t.Fatalf("referral spec failed shipcheck:\n%s", rep.String())
	}
	want := []string{
		"structure", "audit_completeness", "transition_coverage",
		"terminal_reachable", "determinism", "idempotency",
	}
	got := map[string]bool{}
	for _, c := range rep.Checks {
		got[c.Name] = c.Pass
	}
	for _, name := range want {
		if !got[name] {
			t.Errorf("expected passing check %q, missing or failed", name)
		}
	}
}

func TestRunnerHappyPath(t *testing.T) {
	s := mustLoad(t)
	sc := scenarioByName(t, s, "happy_path")
	res := Run(s, sc.Events, nil)
	if res.FinalState != "referred" {
		t.Fatalf("final state %q, want referred", res.FinalState)
	}
	if !equalStrings(res.StateSeq, sc.ExpectStates) {
		t.Fatalf("states %v, want %v", res.StateSeq, sc.ExpectStates)
	}
	if !equalStrings(res.ActionsFired, sc.ExpectActions) {
		t.Fatalf("actions %v, want %v", res.ActionsFired, sc.ExpectActions)
	}
}

func TestRunnerGuardBlocksMissingOwner(t *testing.T) {
	s := mustLoad(t)
	sc := scenarioByName(t, s, "missing_owner_guard")
	res := Run(s, sc.Events, nil)
	if res.FinalState != "identified" {
		t.Fatalf("missing owner should not advance: final %q", res.FinalState)
	}
	if len(res.ActionsFired) != 0 {
		t.Fatalf("no actions on a blocked guard, got %v", res.ActionsFired)
	}
	if len(res.Audit) != 1 || res.Audit[0].Skipped != "guard_failed" {
		t.Fatalf("audit should record guard_failed: %+v", res.Audit)
	}
}

func TestRunnerDedup(t *testing.T) {
	s := mustLoad(t)
	sc := scenarioByName(t, s, "duplicate_send")
	res := Run(s, sc.Events, nil)
	if res.Deduped != 1 {
		t.Fatalf("deduped %d, want 1", res.Deduped)
	}
	if res.FinalState != "sent" {
		t.Fatalf("final %q, want sent (duplicate must not re-send)", res.FinalState)
	}
}

func TestGuardEval(t *testing.T) {
	cases := []struct {
		expr string
		data map[string]any
		want bool
	}{
		{"", nil, true},
		{"owner exists", map[string]any{"owner": "x"}, true},
		{"owner exists", map[string]any{"owner": ""}, false},
		{"owner exists", map[string]any{}, false},
		{"priority == high", map[string]any{"priority": "high"}, true},
		{"priority == high", map[string]any{"priority": "low"}, false},
		{"priority != high", map[string]any{"priority": "low"}, true},
	}
	for _, c := range cases {
		if got := evalGuard(c.expr, c.data); got != c.want {
			t.Errorf("evalGuard(%q,%v)=%v want %v", c.expr, c.data, got, c.want)
		}
	}
}

func TestBrokenSpecFailsShipcheck(t *testing.T) {
	// Transition references an undefined action -> Validate fails -> structure fails.
	s := &Spec{
		ID: "broken", Initial: "a",
		States:      []State{{ID: "a"}, {ID: "b"}},
		Events:      []Event{{ID: "go"}},
		Actions:     []Action{{ID: "x", Kind: ActionDeterministic}},
		Transitions: []Transition{{From: "a", To: "b", On: "go", Actions: []string{"nope"}}},
		Scenarios:   []Scenario{{Name: "s", Events: []ScenarioEvent{{Event: "go"}}, ExpectStates: []string{"a", "b"}}},
	}
	rep := Shipcheck(s)
	if rep.Passed {
		t.Fatal("broken spec must fail shipcheck")
	}
	if len(rep.Checks) == 0 || rep.Checks[0].Name != "structure" || rep.Checks[0].Pass {
		t.Fatalf("structure check should fail first: %+v", rep.Checks)
	}
}

func TestUnreachableStateFailsCoverage(t *testing.T) {
	s := &Spec{
		ID: "orphan", Initial: "a",
		States:      []State{{ID: "a"}, {ID: "b"}, {ID: "orphan"}},
		Events:      []Event{{ID: "go"}},
		Transitions: []Transition{{From: "a", To: "b", On: "go"}},
		Scenarios:   []Scenario{{Name: "s", Events: []ScenarioEvent{{Event: "go"}}, ExpectStates: []string{"a", "b"}}},
	}
	rep := Shipcheck(s)
	if rep.Passed {
		t.Fatal("unreachable state must fail coverage")
	}
	var cov *Check
	for i := range rep.Checks {
		if rep.Checks[i].Name == "transition_coverage" {
			cov = &rep.Checks[i]
		}
	}
	if cov == nil || cov.Pass {
		t.Fatalf("transition_coverage should fail: %+v", rep.Checks)
	}
}

func mustLoad(t *testing.T) *Spec {
	t.Helper()
	s, err := LoadSpec(filepath.Join("testdata", "referral.workflow-spec.json"))
	if err != nil {
		t.Fatalf("load referral spec: %v", err)
	}
	return s
}

func scenarioByName(t *testing.T, s *Spec, name string) Scenario {
	t.Helper()
	for _, sc := range s.Scenarios {
		if sc.Name == name {
			return sc
		}
	}
	t.Fatalf("scenario %q not found", name)
	return Scenario{}
}
