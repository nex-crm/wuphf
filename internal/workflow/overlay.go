package workflow

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// overlay.go is stage 5 of the press: self-healing. Improvements never mutate a
// running contract directly. They arrive as OVERLAYS — additive patches sourced
// from operator edits or recurring exceptions — which are REVIEWED (the patched
// spec must shipcheck AND every original scenario must still replay) and only
// then ACCEPTED (the spec is updated in place, version bumped: update-over-create).
// The kernel (Spec + runner + shipcheck) stays small and protected; overlays
// live outside it.

// Overlay is an additive patch to a spec. Additive-only by design: an overlay
// can extend the contract (new states, paths, scenarios) but never silently
// rewrite existing behavior, so review can never be fooled into a regression.
type Overlay struct {
	ID     string `json:"id"`
	SpecID string `json:"spec_id"`
	Source string `json:"source"` // "operator_edit" | "recurring_exception"
	Reason string `json:"reason,omitempty"`

	SetGoal        string       `json:"set_goal,omitempty"`
	AddStates      []State      `json:"add_states,omitempty"`
	AddEvents      []Event      `json:"add_events,omitempty"`
	AddActions     []Action     `json:"add_actions,omitempty"`
	AddTransitions []Transition `json:"add_transitions,omitempty"`
	AddScenarios   []Scenario   `json:"add_scenarios,omitempty"`
	AddTerminal    []string     `json:"add_terminal,omitempty"`
}

// OverlayReview is the verdict on a proposed overlay.
type OverlayReview struct {
	OverlayID string          `json:"overlay_id"`
	Accepted  bool            `json:"accepted"`
	Shipcheck ShipcheckReport `json:"shipcheck"`
	Regressed []string        `json:"regressed_scenarios,omitempty"`
}

// Apply returns a NEW spec with the overlay merged (additions deduped by id),
// version bumped. The base spec is never mutated.
func Apply(base *Spec, o Overlay) Spec {
	out := cloneSpec(base)
	if strings.TrimSpace(o.SetGoal) != "" {
		out.Goal = o.SetGoal
	}
	out.States = mergeStates(out.States, o.AddStates)
	out.Events = mergeEvents(out.Events, o.AddEvents)
	out.Actions = mergeActions(out.Actions, o.AddActions)
	out.Transitions = append(out.Transitions, o.AddTransitions...)
	out.Scenarios = mergeScenarios(out.Scenarios, o.AddScenarios)
	out.Terminal = mergeStrings(out.Terminal, o.AddTerminal)
	out.Version = bumpVersion(out.Version)
	return out
}

// ReviewOverlay applies the overlay to a copy and proves the result:
// the patched spec must shipcheck, and every ORIGINAL scenario must still
// produce its declared path on the patched spec (no regression).
func ReviewOverlay(base *Spec, o Overlay) (Spec, OverlayReview) {
	patched := Apply(base, o)
	rep := Shipcheck(&patched)

	var regressed []string
	for _, sc := range base.Scenarios {
		res := Run(&patched, sc.Events, nil)
		if !equalStrings(res.StateSeq, sc.ExpectStates) ||
			(sc.ExpectActions != nil && !equalStrings(res.ActionsFired, sc.ExpectActions)) {
			regressed = append(regressed, sc.Name)
		}
	}

	return patched, OverlayReview{
		OverlayID: o.ID,
		Accepted:  rep.Passed && len(regressed) == 0,
		Shipcheck: rep,
		Regressed: regressed,
	}
}

// AcceptOverlay reviews and, if accepted, returns the patched (version-bumped)
// spec — same ID, so the caller persists it over the existing contract
// (update-over-create). A rejected overlay returns an error and the review.
func AcceptOverlay(base *Spec, o Overlay) (*Spec, OverlayReview, error) {
	patched, review := ReviewOverlay(base, o)
	if !review.Accepted {
		return nil, review, fmt.Errorf(
			"overlay %q rejected: shipcheck_passed=%v regressed=%v",
			o.ID, review.Shipcheck.Passed, review.Regressed)
	}
	return &patched, review, nil
}

func cloneSpec(s *Spec) Spec {
	data, _ := json.Marshal(s)
	var out Spec
	_ = json.Unmarshal(data, &out)
	return out
}

func mergeStates(base, add []State) []State {
	seen := map[string]bool{}
	for _, x := range base {
		seen[x.ID] = true
	}
	for _, x := range add {
		if !seen[x.ID] {
			base = append(base, x)
			seen[x.ID] = true
		}
	}
	return base
}

func mergeEvents(base, add []Event) []Event {
	seen := map[string]bool{}
	for _, x := range base {
		seen[x.ID] = true
	}
	for _, x := range add {
		if !seen[x.ID] {
			base = append(base, x)
			seen[x.ID] = true
		}
	}
	return base
}

func mergeActions(base, add []Action) []Action {
	seen := map[string]bool{}
	for _, x := range base {
		seen[x.ID] = true
	}
	for _, x := range add {
		if !seen[x.ID] {
			base = append(base, x)
			seen[x.ID] = true
		}
	}
	return base
}

func mergeScenarios(base, add []Scenario) []Scenario {
	seen := map[string]bool{}
	for _, x := range base {
		seen[x.Name] = true
	}
	for _, x := range add {
		if !seen[x.Name] {
			base = append(base, x)
			seen[x.Name] = true
		}
	}
	return base
}

func mergeStrings(base, add []string) []string {
	seen := map[string]bool{}
	for _, x := range base {
		seen[x] = true
	}
	for _, x := range add {
		if !seen[x] {
			base = append(base, x)
			seen[x] = true
		}
	}
	return base
}

func bumpVersion(v string) string {
	if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
		return strconv.Itoa(n + 1)
	}
	return v + ".1"
}
