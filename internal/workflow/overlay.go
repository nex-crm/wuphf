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
	// AddAllowedReads extends the integration-read allow-list so an overlay that
	// adds an integration-read action can also authorize it. Without this an
	// added read would fail Validate (read not allowed) and the overlay would be
	// rejected. The human still reviews the overlay at accept time.
	AddAllowedReads []ActionRef `json:"add_allowed_reads,omitempty"`
}

// OverlayReview is the verdict on a proposed overlay.
type OverlayReview struct {
	OverlayID string          `json:"overlay_id"`
	Accepted  bool            `json:"accepted"`
	Shipcheck ShipcheckReport `json:"shipcheck"`
	Regressed []string        `json:"regressed_scenarios,omitempty"`
	// NoChange is true when the overlay would not alter the contract at all
	// (e.g. an attempted edit whose content already matched, or an empty
	// overlay). The runtime rejects it so the agent gets an honest "nothing
	// changed" instead of a phantom success with a bumped version.
	NoChange bool `json:"no_change,omitempty"`
}

// Apply returns a NEW spec with the overlay merged, version bumped. The base
// spec is never mutated.
//
// Merge is REPLACE-on-match (keyed by id / name / (from,on)): an overlay item
// whose key already exists OVERWRITES it; a new key is appended. This lets an
// overlay EDIT an existing element (e.g. change an action's params) instead of
// the old keep-first behaviour, which silently dropped the edit and let the
// runtime report a phantom success. Behavioural safety is preserved downstream:
// ReviewOverlay replays every ORIGINAL scenario against the patched spec, so a
// replacement that breaks declared behaviour is rejected as a regression, and a
// no-op overlay (changes nothing) is rejected too — an edit either lands or
// fails loud.
func Apply(base *Spec, o Overlay) Spec {
	out := cloneSpec(base)
	if strings.TrimSpace(o.SetGoal) != "" {
		out.Goal = o.SetGoal
	}
	out.States = mergeStates(out.States, o.AddStates)
	out.Events = mergeEvents(out.Events, o.AddEvents)
	out.Actions = mergeActions(out.Actions, o.AddActions)
	out.Transitions = mergeTransitions(out.Transitions, o.AddTransitions)
	out.Scenarios = mergeScenarios(out.Scenarios, o.AddScenarios)
	out.Terminal = mergeStrings(out.Terminal, o.AddTerminal)
	out.AllowedReads = mergeAllowedReads(out.AllowedReads, o.AddAllowedReads)
	out.Version = bumpVersion(out.Version)
	return out
}

// mergeAllowedReads appends new (platform, action_id) read grants, deduped.
func mergeAllowedReads(base, add []ActionRef) []ActionRef {
	seen := map[string]bool{}
	for _, r := range base {
		seen[allowKey(r.Platform, r.ActionID)] = true
	}
	out := append([]ActionRef(nil), base...)
	for _, r := range add {
		k := allowKey(r.Platform, r.ActionID)
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, r)
	}
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

	// Fail loud on a no-op: if the only difference from base is the bumped
	// version, the overlay changed nothing real (an edit that matched existing
	// content, or an empty/duplicate overlay). Reject so no phantom success is
	// reported.
	noChange := specEqualIgnoringVersion(base, &patched)

	return patched, OverlayReview{
		OverlayID: o.ID,
		Accepted:  rep.Passed && len(regressed) == 0 && !noChange,
		Shipcheck: rep,
		Regressed: regressed,
		NoChange:  noChange,
	}
}

// specEqualIgnoringVersion reports whether two specs are identical except for
// their Version field. Used to detect a no-op overlay.
func specEqualIgnoringVersion(a, b *Spec) bool {
	ac, bc := cloneSpec(a), cloneSpec(b)
	ac.Version, bc.Version = "", ""
	x, _ := json.Marshal(ac)
	y, _ := json.Marshal(bc)
	return string(x) == string(y)
}

// AcceptOverlay reviews and, if accepted, returns the patched (version-bumped)
// spec — same ID, so the caller persists it over the existing contract
// (update-over-create). A rejected overlay returns an error and the review.
func AcceptOverlay(base *Spec, o Overlay) (*Spec, OverlayReview, error) {
	patched, review := ReviewOverlay(base, o)
	if !review.Accepted {
		if review.NoChange {
			return nil, review, fmt.Errorf(
				"overlay %q rejected: it changes nothing — to edit an existing element, include it in add_* with the SAME id and the NEW content (the merge replaces on id match)",
				o.ID)
		}
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

// The merge* helpers REPLACE an existing element on a key match (in place,
// preserving order) and append a genuinely new one.

func mergeStates(base, add []State) []State {
	for _, x := range add {
		if i := indexOfState(base, x.ID); i >= 0 {
			base[i] = x
		} else {
			base = append(base, x)
		}
	}
	return base
}

func indexOfState(s []State, id string) int {
	for i := range s {
		if s[i].ID == id {
			return i
		}
	}
	return -1
}

func mergeEvents(base, add []Event) []Event {
	for _, x := range add {
		replaced := false
		for i := range base {
			if base[i].ID == x.ID {
				base[i] = x
				replaced = true
				break
			}
		}
		if !replaced {
			base = append(base, x)
		}
	}
	return base
}

func mergeActions(base, add []Action) []Action {
	for _, x := range add {
		replaced := false
		for i := range base {
			if base[i].ID == x.ID {
				base[i] = x
				replaced = true
				break
			}
		}
		if !replaced {
			base = append(base, x)
		}
	}
	return base
}

// mergeTransitions replaces a transition matched on its (From, On) key — the
// edge identity — and appends a new edge. Replacing (not appending a duplicate)
// means an edit to an existing edge's target/guard/actions actually takes
// effect instead of being shadowed by the original (the runner picks the first
// matching transition).
func mergeTransitions(base, add []Transition) []Transition {
	for _, x := range add {
		replaced := false
		for i := range base {
			if base[i].From == x.From && base[i].On == x.On {
				base[i] = x
				replaced = true
				break
			}
		}
		if !replaced {
			base = append(base, x)
		}
	}
	return base
}

func mergeScenarios(base, add []Scenario) []Scenario {
	for _, x := range add {
		replaced := false
		for i := range base {
			if base[i].Name == x.Name {
				base[i] = x
				replaced = true
				break
			}
		}
		if !replaced {
			base = append(base, x)
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
