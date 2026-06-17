package workflow

import "testing"

// A real self-heal: owners often never acknowledge, so add a stale path. The
// overlay must review clean (patched spec shipchecks, originals still replay)
// and accept with a bumped version.
func TestOverlayAcceptedAddsStalePath(t *testing.T) {
	base := mustLoad(t)
	overlay := Overlay{
		ID: "ov-stale", SpecID: base.ID, Source: "recurring_exception",
		Reason:    "owners frequently never acknowledge; add an SLA-stale path",
		AddStates: []State{{ID: "stale", Label: "Stale"}},
		AddEvents: []Event{{ID: "timeout", Label: "SLA timeout"}},
		AddTransitions: []Transition{
			{From: "sent", To: "stale", On: "timeout", Actions: []string{"track"}},
		},
		AddScenarios: []Scenario{{
			Name: "stale_path",
			Events: []ScenarioEvent{
				{Event: "process", Data: map[string]any{"company": "Acme", "owner": "y"}, DedupKey: "opp-s"},
				{Event: "timeout", DedupKey: "to-s"},
			},
			ExpectStates:  []string{"identified", "sent", "stale"},
			ExpectActions: []string{"draft_message", "route_owner", "slack_send", "track", "track"},
		}},
	}

	patched, review, err := AcceptOverlay(base, overlay)
	if err != nil {
		t.Fatalf("overlay should be accepted: %v\n%s", err, review.Shipcheck.String())
	}
	if !review.Accepted || len(review.Regressed) != 0 {
		t.Fatalf("review: accepted=%v regressed=%v", review.Accepted, review.Regressed)
	}
	if patched.Version != "2" {
		t.Fatalf("update-over-create should bump version, got %q", patched.Version)
	}
	if patched.ID != base.ID {
		t.Fatalf("patched id %q must equal base id %q (update, not create)", patched.ID, base.ID)
	}
	// The new path actually works on the patched spec.
	res := Run(patched, overlay.AddScenarios[0].Events, nil)
	if res.FinalState != "stale" {
		t.Fatalf("stale path final state %q, want stale", res.FinalState)
	}
	// Base is untouched.
	if base.Version != "1" {
		t.Fatalf("base mutated: version %q", base.Version)
	}
}

func TestOverlayRejectedUnreachableState(t *testing.T) {
	base := mustLoad(t)
	overlay := Overlay{ID: "ov-orphan", AddStates: []State{{ID: "orphan"}}}
	_, review := ReviewOverlay(base, overlay)
	if review.Accepted {
		t.Fatal("an unreachable new state must fail shipcheck coverage and be rejected")
	}
	if review.Shipcheck.Passed {
		t.Fatal("patched spec should not pass shipcheck")
	}
}

func TestOverlayRejectedBadScenario(t *testing.T) {
	base := mustLoad(t)
	overlay := Overlay{
		ID: "ov-badscen",
		AddScenarios: []Scenario{{
			Name: "wrong_expectation",
			Events: []ScenarioEvent{
				{Event: "process", Data: map[string]any{"owner": "z"}, DedupKey: "w1"},
			},
			ExpectStates: []string{"identified", "referred"}, // wrong: process -> sent
		}},
	}
	_, review := ReviewOverlay(base, overlay)
	if review.Accepted {
		t.Fatal("a scenario with a wrong expected path must fail replay and be rejected")
	}
}

func TestApplyIsImmutableAndDedups(t *testing.T) {
	base := mustLoad(t)
	nStates := len(base.States)
	overlay := Overlay{
		ID:        "ov-dup",
		SetGoal:   "Reworded goal",
		AddStates: []State{{ID: "sent"}}, // already exists -> dedup
	}
	patched := Apply(base, overlay)

	if len(base.States) != nStates || base.Goal == "Reworded goal" || base.Version != "1" {
		t.Fatal("Apply mutated the base spec")
	}
	if len(patched.States) != nStates {
		t.Fatalf("duplicate state should not be added: %d != %d", len(patched.States), nStates)
	}
	if patched.Goal != "Reworded goal" {
		t.Fatalf("SetGoal not applied: %q", patched.Goal)
	}
	if patched.Version != "2" {
		t.Fatalf("version not bumped: %q", patched.Version)
	}
}
