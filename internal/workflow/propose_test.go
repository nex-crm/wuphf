package workflow

import "testing"

func TestProposeOverlayFromRecurringException(t *testing.T) {
	s := mustLoad(t)
	// "refer" fired at the initial state has no transition -> no_transition.
	mkRun := func(key string) RunRecord {
		return Execute(s, "manual", []ScenarioEvent{{Event: "refer", DedupKey: key}}, nil)
	}
	runs := []RunRecord{mkRun("a"), mkRun("b")}

	props := ProposeOverlays(s, runs, ProposeOptions{MinRecurrences: 2})
	if len(props) != 1 {
		t.Fatalf("want 1 proposal, got %d: %+v", len(props), props)
	}
	ov := props[0]
	if ov.Source != "recurring_exception" || len(ov.AddTransitions) != 1 {
		t.Fatalf("unexpected proposal: %+v", ov)
	}

	// The proposed overlay reviews clean and actually catches the event.
	patched, review, err := AcceptOverlay(s, ov)
	if err != nil || !review.Accepted {
		t.Fatalf("proposed overlay should accept clean: %v\n%s", err, review.Shipcheck.String())
	}
	res := Run(patched, []ScenarioEvent{{Event: "refer", DedupKey: "c"}}, nil)
	if res.FinalState == "identified" {
		t.Fatalf("refer should now be caught, not dropped: %q", res.FinalState)
	}
}

func TestProposeBelowThresholdNoProposal(t *testing.T) {
	s := mustLoad(t)
	runs := []RunRecord{Execute(s, "manual", []ScenarioEvent{{Event: "refer"}}, nil)}
	if props := ProposeOverlays(s, runs, ProposeOptions{MinRecurrences: 2}); len(props) != 0 {
		t.Fatalf("a single occurrence should propose nothing, got %d", len(props))
	}
}

func TestProposeNothingForHealthyRuns(t *testing.T) {
	s := mustLoad(t)
	// Happy-path runs produce no exceptions -> no proposals.
	sc := scenarioByName(t, s, "happy_path")
	runs := []RunRecord{Execute(s, "manual", sc.Events, nil), Execute(s, "manual", sc.Events, nil)}
	if props := ProposeOverlays(s, runs, ProposeOptions{}); len(props) != 0 {
		t.Fatalf("healthy runs should propose nothing, got %d", len(props))
	}
}

// TestProposeOverlaysIgnoresPlatformMarkers locks RFC E2: a run whose outputs
// carry size-reducer markers and whose audit shows an action_failed (e.g. a
// ResultTooLargeError) must NOT generate overlay proposals — those are platform
// mechanics, not recurring workflow exceptions.
func TestProposeOverlaysIgnoresPlatformMarkers(t *testing.T) {
	base := &Spec{
		ID:          "wf",
		Initial:     "start",
		States:      []State{{ID: "start"}, {ID: "done"}},
		Events:      []Event{{ID: "run"}},
		Transitions: []Transition{{From: "start", To: "done", On: "run", Actions: []string{"fetch"}}},
		Actions:     []Action{{ID: "fetch", Kind: ActionDeterministic, Platform: "gmail", ActionID: "GMAIL_FETCH_EMAILS"}},
	}
	markerRun := RunRecord{
		SpecID: "wf",
		Events: []ScenarioEvent{{Event: "run"}},
		Result: RunResult{
			Audit:   []AuditEntry{{Event: "run", From: "start", Skipped: "action_failed"}},
			Outputs: map[string]any{"fetch_reduction": Reduction{Truncated: true}, "fetch_error": "result_too_large"},
		},
	}
	// Three identical marker runs — well over MinRecurrences — must still yield 0.
	got := ProposeOverlays(base, []RunRecord{markerRun, markerRun, markerRun}, ProposeOptions{MinRecurrences: 2})
	if len(got) != 0 {
		t.Fatalf("platform markers must not drive proposals, got %d: %+v", len(got), got)
	}
}
