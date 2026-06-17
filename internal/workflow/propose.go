package workflow

import (
	"fmt"
	"sort"
)

// propose.go closes the self-healing loop. It mines persisted run records for
// RECURRING exceptions (events that keep arriving with nowhere to go) and drafts
// additive overlays that catch them in an explicit exception state — so a real
// gap in the contract becomes a reviewable proposal instead of a silently
// dropped event. Each proposal is pre-validated to review clean, so the operator
// only ever sees shippable overlays; acceptance still goes through AcceptOverlay.

// ProposeOptions tunes the proposer.
type ProposeOptions struct {
	MinRecurrences int // how many times an exception must recur (default 2)
}

// ProposeOverlays returns overlays that handle recurring unhandled events found
// in the run records. Empty when nothing recurs past the threshold.
func ProposeOverlays(base *Spec, runs []RunRecord, opts ProposeOptions) []Overlay {
	if opts.MinRecurrences <= 0 {
		opts.MinRecurrences = 2
	}

	// Group recurring no_transition exceptions by (from-state, event); keep one
	// representative event sequence per group to build a capture scenario.
	type key struct{ from, event string }
	counts := map[key]int{}
	sample := map[key][]ScenarioEvent{}
	for _, r := range runs {
		for i, a := range r.Result.Audit {
			if a.Skipped != "no_transition" {
				continue
			}
			k := key{from: a.From, event: a.Event}
			counts[k]++
			if _, ok := sample[k]; !ok {
				sample[k] = r.Events[:minInt(i+1, len(r.Events))]
			}
		}
	}

	eventExists := func(id string) bool {
		for _, e := range base.Events {
			if e.ID == id {
				return true
			}
		}
		return false
	}

	var out []Overlay
	for k, n := range counts {
		if n < opts.MinRecurrences || base.hasTransition(k.from, k.event) {
			continue
		}
		caught := "unhandled_" + k.event
		ov := Overlay{
			ID:     "ov-auto-" + k.from + "-" + k.event,
			SpecID: base.ID,
			Source: "recurring_exception",
			Reason: fmt.Sprintf("event %q recurred %d times unhandled at state %q", k.event, n, k.from),
			AddStates: []State{
				{ID: caught, Label: "Unhandled " + k.event},
			},
			AddTransitions: []Transition{
				{From: k.from, To: caught, On: k.event},
			},
			AddTerminal: []string{caught},
		}
		if !eventExists(k.event) {
			ov.AddEvents = []Event{{ID: k.event, Label: k.event}}
		}

		// Capture the new behavior as a scenario so the catch is regression-proven.
		patched := Apply(base, ov)
		res := Run(&patched, sample[k], nil)
		ov.AddScenarios = []Scenario{{
			Name:          "auto_" + k.from + "_" + k.event,
			Events:        sample[k],
			ExpectStates:  res.StateSeq,
			ExpectActions: res.ActionsFired,
		}}

		// Only surface proposals that actually review clean.
		if _, review := ReviewOverlay(base, ov); review.Accepted {
			out = append(out, ov)
		}
	}

	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
