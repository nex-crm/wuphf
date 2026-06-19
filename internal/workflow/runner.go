package workflow

// runner.go executes a Spec deterministically: a small state machine over an
// ordered event stream. Same spec + same events + same ActionExec => identical
// RunResult. Non-deterministic work (LLM drafting, external sends) is confined
// to the ActionExec hook, so the orchestration itself is provable.

// ActionOutcome is what an ActionExec returns for one action.
type ActionOutcome struct {
	OK     bool           `json:"ok"`
	Output map[string]any `json:"output,omitempty"`
	Err    string         `json:"err,omitempty"`
}

// ActionExec runs one action against the current event data. The runner stays
// deterministic regardless of what the hook does; for shipcheck the hook is a
// pure recorder. In the broker, deterministic actions run inline and llm/
// external actions route through the agent / approval gate.
type ActionExec func(action Action, data map[string]any) ActionOutcome

// AuditEntry is one processed event: either a taken transition, or a skip with
// a reason. Audit completeness (every event accounted for) is a shipcheck.
type AuditEntry struct {
	Event   string   `json:"event"`
	From    string   `json:"from"`
	To      string   `json:"to,omitempty"`
	Actions []string `json:"actions,omitempty"`
	Skipped string   `json:"skipped,omitempty"` // "duplicate" | "no_transition" | "guard_failed" | "action_failed"
}

// RunResult is the deterministic output of executing a Spec over events.
type RunResult struct {
	StateSeq     []string     `json:"state_seq"` // initial state, then each entered state
	ActionsFired []string     `json:"actions_fired"`
	Audit        []AuditEntry `json:"audit"`
	FinalState   string       `json:"final_state"`
	Deduped      int          `json:"deduped"`
	// Outputs are the artifacts actions produced (e.g. the composed digest),
	// merged in action order. Empty for the pure-recorder shipcheck path, so
	// the orchestration stays deterministic; populated only on real runs.
	Outputs map[string]any `json:"outputs,omitempty"`
}

// recordingExec is the default hook: it records nothing extra and reports OK.
// Deterministic, dependency-free — exactly what shipcheck needs.
func recordingExec(Action, map[string]any) ActionOutcome { return ActionOutcome{OK: true} }

// Run executes the spec over the event stream. A nil exec defaults to a pure
// OK recorder (the shipcheck path).
func Run(s *Spec, events []ScenarioEvent, exec ActionExec) RunResult {
	if exec == nil {
		exec = recordingExec
	}
	actionByID := make(map[string]Action, len(s.Actions))
	for _, a := range s.Actions {
		actionByID[a.ID] = a
	}

	res := RunResult{StateSeq: []string{s.Initial}, FinalState: s.Initial}
	cur := s.Initial
	seen := map[string]bool{}

	for _, ev := range events {
		// Idempotency: a repeated event (same dedup key) is a no-op.
		if ev.DedupKey != "" {
			if seen[ev.DedupKey] {
				res.Deduped++
				res.Audit = append(res.Audit, AuditEntry{Event: ev.Event, From: cur, Skipped: "duplicate"})
				continue
			}
			seen[ev.DedupKey] = true
		}

		t := s.matchTransition(cur, ev.Event, ev.Data)
		if t == nil {
			// Distinguish "no transition exists" from "guard failed" for audit.
			reason := "no_transition"
			if s.hasTransition(cur, ev.Event) {
				reason = "guard_failed"
			}
			res.Audit = append(res.Audit, AuditEntry{Event: ev.Event, From: cur, Skipped: reason})
			continue
		}

		// Thread action outputs forward so a later action sees what an earlier
		// one produced (fetched emails -> composed digest), and capture them
		// into the run record. Seeded from the event data each transition.
		runData := make(map[string]any, len(ev.Data))
		for k, v := range ev.Data {
			runData[k] = v
		}
		failed := false
		for _, aid := range t.Actions {
			out := exec(actionByID[aid], runData)
			if !out.OK {
				failed = true
				break
			}
			res.ActionsFired = append(res.ActionsFired, aid)
			for k, v := range out.Output {
				runData[k] = v
				if res.Outputs == nil {
					res.Outputs = map[string]any{}
				}
				res.Outputs[k] = v
			}
		}
		if failed {
			res.Audit = append(res.Audit, AuditEntry{Event: ev.Event, From: cur, Skipped: "action_failed"})
			continue
		}

		res.Audit = append(res.Audit, AuditEntry{Event: ev.Event, From: cur, To: t.To, Actions: t.Actions})
		cur = t.To
		res.StateSeq = append(res.StateSeq, cur)
		res.FinalState = cur
	}
	return res
}

// matchTransition returns the first transition (spec order) from `from` on
// `event` whose guard passes against data, or nil.
func (s *Spec) matchTransition(from, event string, data map[string]any) *Transition {
	for i := range s.Transitions {
		t := &s.Transitions[i]
		if t.From == from && t.On == event && evalGuard(t.Guard, data) {
			return t
		}
	}
	return nil
}

// hasTransition reports whether any transition exists from `from` on `event`
// (ignoring guards) — lets the runner label a skip as guard_failed vs no_transition.
func (s *Spec) hasTransition(from, event string) bool {
	for i := range s.Transitions {
		if s.Transitions[i].From == from && s.Transitions[i].On == event {
			return true
		}
	}
	return false
}
