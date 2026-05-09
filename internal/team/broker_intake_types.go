package team

// broker_intake_types.go owns the structured-output schema the synthetic
// intake agent must emit. The shapes mirror the design doc's "Intake agent /
// Output schema" section verbatim (see
// /Users/najmuzzaman/.gstack/projects/.../multi-agent-control-loop.md). They
// live in their own file so the intake driver, the parser, and the future
// Decision Packet aggregator (Lane C) all reference the same canonical
// definition without dragging the broader broker types in.
//
// Wire compatibility is load-bearing: Lane C's Decision Packet persistence
// (~/.wuphf/tasks/<id>/decision_packet.json) and the v1.1 native structured
// output path (response_format / json_schema) both rely on these JSON tags
// matching the prompt-engineered output the LLM emits. Keep the snake_case
// JSON names in lockstep with the system prompt's example block in
// broker_intake.go.

import "time"

// Spec is the intake agent's structured output. All fields are optional on
// the wire (omitempty) so a partial response can still parse for inspection,
// but the validator enforces the design doc's gate: Problem != "",
// len(AcceptanceCriteria) >= 1, Assignment != "".
//
// AutoAssign is the optional pre-declared owner agent slug. When non-empty
// the CLI runs the 3-second auto-assign countdown described in the design
// doc; Lane B exposes the cancellable countdown API and leaves the terminal
// UX to Lane F.
//
// Feedback is appended on changes_requested re-entry (Lane D wires that
// path); v1 intake never populates it on first parse.
type Spec struct {
	Problem            string         `json:"problem,omitempty"`
	TargetOutcome      string         `json:"target_outcome,omitempty"`
	AcceptanceCriteria []ACItem       `json:"acceptance_criteria,omitempty"`
	Assignment         string         `json:"assignment,omitempty"`
	Constraints        []string       `json:"constraints,omitempty"`
	AutoAssign         string         `json:"auto_assign,omitempty"`
	Feedback           []FeedbackItem `json:"feedback,omitempty"`
}

// ACItem is one acceptance-criterion checklist row. Done is always false
// when emitted by the intake agent; the owner agent flips it when the
// session report commits.
type ACItem struct {
	Statement string `json:"statement,omitempty"`
	Done      bool   `json:"done,omitempty"`
}

// FeedbackItem is one entry in the appendable feedback log Lane D writes
// when a reviewer asks for changes. v1 intake never produces these; the
// shape is here so Lane C can deserialize a Decision Packet that already
// carries feedback from a previous changes_requested cycle.
type FeedbackItem struct {
	AppendedAt time.Time `json:"appended_at,omitempty"`
	Author     string    `json:"author,omitempty"`
	Body       string    `json:"body,omitempty"`
}
