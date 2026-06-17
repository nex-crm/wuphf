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
