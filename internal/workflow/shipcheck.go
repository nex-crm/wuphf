package workflow

import (
	"fmt"
	"strings"
)

// shipcheck.go is the mechanical proof a spec must pass before it ships. It is
// contract-derived (everything comes from the spec's own scenarios + structure)
// and deterministic, so it runs unattended on every overlay. A spec that fails
// shipcheck never reaches the runtime binding.

// Check is one mechanical assertion result.
type Check struct {
	Name   string `json:"name"`
	Pass   bool   `json:"pass"`
	Detail string `json:"detail,omitempty"`
}

// ShipcheckReport is the full proof for one spec.
type ShipcheckReport struct {
	SpecID string  `json:"spec_id"`
	Passed bool    `json:"passed"`
	Checks []Check `json:"checks"`
}

// Shipcheck proves the spec mechanically:
//   - structure: the contract validates
//   - scenario replay: each fixture produces its declared state path + actions
//   - audit completeness: every event is accounted for
//   - transition coverage: every state is reachable from initial
//   - terminal reachability: a declared terminal state is reachable
//   - determinism: replaying a scenario twice is byte-identical
//   - idempotency / duplicate handling: a re-sent event (same dedup key) is a
//     no-op and never re-fires actions
func Shipcheck(s *Spec) ShipcheckReport {
	r := ShipcheckReport{SpecID: s.ID}
	add := func(name string, pass bool, detail string) {
		r.Checks = append(r.Checks, Check{Name: name, Pass: pass, Detail: detail})
	}

	// 1. Structure.
	if err := s.Validate(); err != nil {
		add("structure", false, err.Error())
		r.Passed = false
		return r // nothing else is meaningful on a broken contract
	}
	add("structure", true, fmt.Sprintf("%d states, %d transitions, %d actions", len(s.States), len(s.Transitions), len(s.Actions)))

	// 2. Scenario replay + audit completeness.
	replayPass, auditPass := true, true
	for _, sc := range s.Scenarios {
		res := Run(s, sc.Events, nil)
		if !equalStrings(res.StateSeq, sc.ExpectStates) {
			replayPass = false
			add("replay:"+sc.Name, false, fmt.Sprintf("states got %v, want %v", res.StateSeq, sc.ExpectStates))
		} else if sc.ExpectActions != nil && !equalStrings(res.ActionsFired, sc.ExpectActions) {
			replayPass = false
			add("replay:"+sc.Name, false, fmt.Sprintf("actions got %v, want %v", res.ActionsFired, sc.ExpectActions))
		} else {
			add("replay:"+sc.Name, true, fmt.Sprintf("%d events -> %s", len(sc.Events), res.FinalState))
		}
		if len(res.Audit) != len(sc.Events) {
			auditPass = false
		}
	}
	_ = replayPass
	add("audit_completeness", auditPass, "every input event produces exactly one audit entry")

	// 3. Transition coverage: every state reachable from initial.
	reach := s.reachableStates()
	var unreached []string
	for _, id := range s.stateIDs() {
		if !reach[id] {
			unreached = append(unreached, id)
		}
	}
	add("transition_coverage", len(unreached) == 0, coverageDetail(unreached))

	// 4. Terminal reachability.
	if len(s.Terminal) > 0 {
		anyTerm := false
		for _, t := range s.Terminal {
			if reach[t] {
				anyTerm = true
				break
			}
		}
		add("terminal_reachable", anyTerm, strings.Join(s.Terminal, ","))
	}

	// 5. Determinism: a scenario replayed twice is identical.
	detPass := true
	for _, sc := range s.Scenarios {
		a := Run(s, sc.Events, nil)
		b := Run(s, sc.Events, nil)
		if !equalStrings(a.StateSeq, b.StateSeq) || !equalStrings(a.ActionsFired, b.ActionsFired) {
			detPass = false
		}
	}
	add("determinism", detPass, "identical output across repeated runs")

	// 6. Idempotency / duplicate handling: re-sending a scenario's events (same
	// dedup keys) must not advance further or re-fire actions. Only meaningful
	// for scenarios whose events all carry dedup keys.
	idemPass, idemTested := true, 0
	for _, sc := range s.Scenarios {
		if !allHaveDedupKeys(sc.Events) {
			continue
		}
		idemTested++
		once := Run(s, sc.Events, nil)
		twice := Run(s, append(append([]ScenarioEvent{}, sc.Events...), sc.Events...), nil)
		// Re-sending the same events must not advance the state machine further
		// nor re-fire any action: final state and the action trace are unchanged.
		if once.FinalState != twice.FinalState || !equalStrings(once.ActionsFired, twice.ActionsFired) {
			idemPass = false
		}
	}
	if idemTested == 0 {
		add("idempotency", false, "no scenario carries dedup keys; cannot prove duplicate handling")
	} else {
		add("idempotency", idemPass, fmt.Sprintf("%d scenario(s): re-sent events are no-ops", idemTested))
	}

	r.Passed = true
	for _, c := range r.Checks {
		if !c.Pass {
			r.Passed = false
			break
		}
	}
	return r
}

// String renders a human-readable proof, e.g. for a CLI / PR comment.
func (r ShipcheckReport) String() string {
	var b strings.Builder
	verdict := "FAIL"
	if r.Passed {
		verdict = "PASS"
	}
	fmt.Fprintf(&b, "workflow-shipcheck %s [%s]\n", r.SpecID, verdict)
	for _, c := range r.Checks {
		mark := "x"
		if c.Pass {
			mark = "+"
		}
		fmt.Fprintf(&b, "  [%s] %-22s %s\n", mark, c.Name, c.Detail)
	}
	return b.String()
}

func coverageDetail(unreached []string) string {
	if len(unreached) == 0 {
		return "all states reachable from initial"
	}
	return "unreachable: " + strings.Join(unreached, ",")
}

func allHaveDedupKeys(events []ScenarioEvent) bool {
	if len(events) == 0 {
		return false
	}
	for _, e := range events {
		if strings.TrimSpace(e.DedupKey) == "" {
			return false
		}
	}
	return true
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
