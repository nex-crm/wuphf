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

// editBaseSpec is a minimal valid contract with one integration-read action,
// used to test in-place overlay edits.
func editBaseSpec() *Spec {
	return &Spec{
		ID: "wf", Version: "1", Initial: "start",
		States:      []State{{ID: "start"}, {ID: "done"}},
		Events:      []Event{{ID: "run"}},
		Transitions: []Transition{{From: "start", To: "done", On: "run", Actions: []string{"fetch"}}},
		Actions: []Action{{ID: "fetch", Kind: ActionDeterministic, Platform: "gmail", ActionID: "GMAIL_FETCH_EMAILS",
			Params: map[string]any{"query": "is:unread newer_than:7d"}}},
		AllowedReads: []ActionRef{{Platform: "gmail", ActionID: "GMAIL_FETCH_EMAILS"}},
		Scenarios: []Scenario{{Name: "happy", Events: []ScenarioEvent{{Event: "run", DedupKey: "s1"}},
			ExpectStates: []string{"start", "done"}, ExpectActions: []string{"fetch"}}},
	}
}

// TestOverlayEditsExistingActionInPlace: an overlay that re-declares an existing
// action id with new params REPLACES it — the edit actually lands (the bug was
// it being silently dropped, with a phantom success).
func TestOverlayEditsExistingActionInPlace(t *testing.T) {
	base := editBaseSpec()
	o := Overlay{ID: "tighten", SpecID: "wf", Source: "operator_edit",
		AddActions: []Action{{ID: "fetch", Kind: ActionDeterministic, Platform: "gmail", ActionID: "GMAIL_FETCH_EMAILS",
			Params: map[string]any{"query": "is:unread newer_than:3d"}}}}
	patched, review, err := AcceptOverlay(base, o)
	if err != nil {
		t.Fatalf("in-place param edit should be accepted: %v (review=%+v)", err, review)
	}
	if review.NoChange {
		t.Fatal("a real param edit must not be flagged NoChange")
	}
	// Exactly one fetch action, with the NEW query.
	var fetch *Action
	n := 0
	for i := range patched.Actions {
		if patched.Actions[i].ID == "fetch" {
			fetch = &patched.Actions[i]
			n++
		}
	}
	if n != 1 {
		t.Fatalf("edit must replace, not duplicate: got %d fetch actions", n)
	}
	if fetch.Params["query"] != "is:unread newer_than:3d" {
		t.Fatalf("query was not updated: %v", fetch.Params["query"])
	}
}

// TestOverlayNoOpRejected: an overlay that changes nothing (re-declares an
// action with identical content) is rejected as NoChange — no phantom success.
func TestOverlayNoOpRejected(t *testing.T) {
	base := editBaseSpec()
	o := Overlay{ID: "noop", SpecID: "wf", Source: "operator_edit",
		AddActions: []Action{base.Actions[0]}} // identical content
	_, review, err := AcceptOverlay(base, o)
	if err == nil {
		t.Fatal("a no-op overlay must be rejected, not silently accepted")
	}
	if !review.NoChange {
		t.Fatalf("rejection should be flagged NoChange: %+v", review)
	}
}

// TestOverlayEmptyRejected: a completely empty overlay is a no-op too.
func TestOverlayEmptyRejected(t *testing.T) {
	_, review, err := AcceptOverlay(editBaseSpec(), Overlay{ID: "empty", SpecID: "wf"})
	if err == nil || !review.NoChange {
		t.Fatalf("empty overlay must be rejected as NoChange: err=%v review=%+v", err, review)
	}
}

// TestOverlayTransitionEditReplaces: editing an existing edge's actions replaces
// the edge instead of appending a shadowed duplicate.
func TestOverlayTransitionEditReplaces(t *testing.T) {
	base := editBaseSpec()
	o := Overlay{ID: "edge", SpecID: "wf", Source: "operator_edit",
		AddTransitions: []Transition{{From: "start", To: "done", On: "run", Actions: []string{"fetch", "fetch"}}}}
	patched := Apply(base, o)
	count := 0
	for _, tr := range patched.Transitions {
		if tr.From == "start" && tr.On == "run" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("edge edit must replace, got %d start/run transitions", count)
	}
}
