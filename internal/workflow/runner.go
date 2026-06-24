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
	Skipped string   `json:"skipped,omitempty"` // "duplicate" | "no_transition" | "guard_failed" | "action_failed" | "pending_approval"
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

	// runData accumulates action outputs across the WHOLE run, not just within a
	// single transition. A linear contract puts each step on its own transition
	// (start -> step_1 -> step_2 -> done), so a fetch on transition 1 and a
	// summarize on transition 2 are separate hops; resetting per transition left
	// the summarize step blind to the fetched data. Persisting it here is what
	// makes fetch -> summarize -> send actually pass data down the chain.
	runData := map[string]any{}

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

		// Overlay this transition's event data onto the accumulated run data
		// (prior action outputs are retained across transitions — see runData
		// declaration above).
		for k, v := range ev.Data {
			runData[k] = v
		}
		failed := false
		pending := false // halt awaiting human approval — a deliberate stop, not a failure
		var failedID, failErr string
		for _, aid := range t.Actions {
			out := exec(actionByID[aid], runData)
			if !out.OK {
				failed = true
				failedID = aid
				failErr = out.Err
				// A gated action (e.g. an external send the human must approve)
				// halts the chain with needs_approval set and NO error. That is
				// not a failure — distinguish it so the audit reads
				// "pending_approval", not "action_failed".
				if na, ok := out.Output["needs_approval"].(bool); ok && na {
					pending = true
				}
				// Capture the failed action's diagnostic output (gate decision,
				// request_id, provider error) into the run record so a failed send
				// is debuggable instead of silently swallowed.
				if res.Outputs == nil {
					res.Outputs = map[string]any{}
				}
				for k, v := range out.Output {
					res.Outputs[k] = v
				}
				if failErr != "" {
					res.Outputs[aid+"_error"] = failErr
				}
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
			reason := "action_failed"
			if pending {
				reason = "pending_approval"
			}
			entry := AuditEntry{Event: ev.Event, From: cur, Skipped: reason}
			if failedID != "" {
				entry.Actions = []string{failedID}
			}
			res.Audit = append(res.Audit, entry)
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
