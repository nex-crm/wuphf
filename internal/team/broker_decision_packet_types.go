package team

// broker_decision_packet_types.go owns the wire-shape Go structs for the
// task-level Decision Packet — the single human-facing artifact the
// multi-agent control loop produces per task. The packet aggregates input
// from three writer roles:
//
//   - intake driver (Lane B) → Spec
//   - owner agent (later lane) → SessionReport + ChangedFiles
//   - reviewer agents (Lane D) → ReviewerGrades
//
// Aggregation is deterministic field rollup (per design doc P3 revision):
// agents do not synthesise the packet; they populate fields. The human
// reads the result.
//
// Coordination notes (Lane C is the canonical home for these shapes):
//
//   - Lane D shipped a stub `Severity` + `ReviewerGrade` in
//     `broker_reviewer_routing_types.go` so its convergence rule could
//     compile in parallel. Lane D's commit message states the stub will
//     be dropped when Lane C ships and Lane C's definition becomes the
//     source of truth.
//   - Lane E shipped a stub `DecisionPacket` + `Spec` + `SessionReport`
//     + `ReviewerGrade` + `Severity` + `Dependencies` in
//     `broker_inbox_packet_types.go` so its REST handlers could
//     compile + ship before Lane C landed. Lane E's commit message
//     states "When Lane C lands, it adds: mutators on b.decisionPackets
//     ..." — Lane C is expected to absorb the read-side and own the
//     definitions.
//
// At integration time, this file replaces the stubs in both
// broker_reviewer_routing_types.go (Severity + ReviewerGrade) and
// broker_inbox_packet_types.go (the rest). The integration agent
// must:
//
//  1. Drop `Severity` and `ReviewerGrade` from
//     broker_reviewer_routing_types.go and rely on this file's
//     definitions (they share the same constant values, so Lane D's
//     timeout filler keeps working unchanged).
//  2. Drop the type definitions from broker_inbox_packet_types.go
//     (DecisionPacket, Spec, SessionReport, Win, DeadEnd, DiffSummary,
//     ReviewerGrade, Severity, ACItem, FeedbackItem, Dependencies)
//     keeping only `findDecisionPacketLocked` (which depends on
//     b.decisionPackets — Lane C wraps it in a state struct, see
//     below).
//  3. Reconcile `b.decisionPackets` shape: Lane E declared
//     `decisionPackets map[string]*DecisionPacket`. Lane C upgrades it
//     to `decisionPackets *decisionPacketState` (carries the store
//     seam + per-task running-flush timers). The change is local to
//     Lane C; Lane E's `findDecisionPacketLocked` is rewritten on top
//     of the state struct.
//
// JSON tags are camelCase across the harness wire — the Lane G web UI
// already consumes camelCase, and standardising the backend on the same
// casing avoids a translation layer at the REST handler. The on-disk
// decision_packet.json file uses the same shape; v1 has no production
// users yet, so flipping casing is safe.
//
// Compile-time discipline: Severity is a typed string, not a free-form
// string field, so reviewer typos ("majr") fail to build the same way
// non-canonical LifecycleStates do. ReviewerGrade.SubmittedAt is
// time.Time (not Lane D's stub-string) because the on-disk JSON
// contract is the design doc DecisionPacket; Lane D's `string` field
// is a pre-Lane-C compromise that integration removes.

import "time"

// Severity is the typed string constant used by reviewer agent grades.
// Mirrors the CodeRabbit five-tier convention so v1 reviewer routing
// can fan grades to the same UI without per-tier translation logic.
type Severity string

const (
	// SeverityCritical is a blocking finding the reviewer believes
	// prevents merge without a fix. UI surfaces these in red at the top
	// of the grades section.
	SeverityCritical Severity = "critical"
	// SeverityMajor is a substantial finding that should be addressed
	// before merge but does not block by itself. UI orange.
	SeverityMajor Severity = "major"
	// SeverityMinor is a cleanup / style suggestion the reviewer would
	// accept either way. UI yellow (AA-compliant pair, see design doc).
	SeverityMinor Severity = "minor"
	// SeverityNitpick is the lowest-confidence suggestion tier. UI blue.
	SeverityNitpick Severity = "nitpick"
	// SeveritySkipped is the reviewer-timeout / reviewer-process-exit
	// placeholder so the convergence rule can fire with a complete
	// grade list. The UI greys these out and surfaces a banner.
	SeveritySkipped Severity = "skipped"
)

// canonicalSeverities is the closed set of valid Severity values — used
// by IsCanonical below. Appending here is intentional: a new severity
// requires updating the typed constants too, which forces the compiler
// gate.
func canonicalSeverities() []Severity {
	return []Severity{
		SeverityCritical,
		SeverityMajor,
		SeverityMinor,
		SeverityNitpick,
		SeveritySkipped,
	}
}

// IsCanonical reports whether s is one of the five typed Severity
// values. Returns false for the zero value and for any free-form
// string cast.
func (s Severity) IsCanonical() bool {
	for _, want := range canonicalSeverities() {
		if s == want {
			return true
		}
	}
	return false
}

// Spec / ACItem / FeedbackItem live in broker_intake_types.go (Lane B).
// They are the canonical definitions for the harness; Lane C consumes
// them as-is rather than re-defining a parallel shape.

// Win is one entry on SessionReport.TopWins. Delta is a short labelled
// magnitude string the UI renders in the leftmost column ("+128 LOC",
// "x3.2 faster", "-10 deps") so a reader can scan deltas without
// parsing the description. Description is the prose. Both fields are
// agent-authored; the broker never synthesises them.
type Win struct {
	Delta       string `json:"delta"`
	Description string `json:"description"`
}

// DeadEnd is one tried-and-discarded approach. The session report
// surfaces these explicitly so future runs (and the human reading the
// packet) can see what was attempted, not just what shipped — Karpathy
// autoresearch pattern. Reason is the agent's recorded rationale for
// abandoning the path.
type DeadEnd struct {
	Tried  string `json:"tried"`
	Reason string `json:"reason"`
}

// DiffSummary is one row in ChangedFiles. Lane B/C and the owner agent
// hand the broker the populated structure; the broker does not run git
// itself in v1 to compute the diff.
//
// Lane E's stub omits Status / RenamedFrom (Lane E only needs path +
// per-file delta counts for the inbox row hint). Lane C's canonical
// shape includes Status + RenamedFrom because they ship in the design
// doc's structure and because the Decision Packet view (Lane G)
// renders renames distinctly. The extra fields are JSON-omitempty so
// existing Lane E payloads continue to decode cleanly.
type DiffSummary struct {
	Path        string `json:"path"`
	Status      string `json:"status,omitempty"`
	Additions   int    `json:"additions"`
	Deletions   int    `json:"deletions"`
	RenamedFrom string `json:"renamedFrom,omitempty"`
}

// SessionReport is the owner-agent-authored summary that appears in the
// Decision Packet center column. Highlights renders as the lead summary
// paragraph; TopWins as the labelled-delta list; DeadEnds as the
// tried-and-discarded section.
//
// Per design doc, FullLog []ExperimentRow is intentionally omitted from
// v1 — research-class tasks (Karpathy autoresearch shape) are not in
// scope for v1 demand evidence. Defer to v1.1 when a real research
// session needs an experiment-row table.
type SessionReport struct {
	Highlights string            `json:"highlights"`
	TopWins    []Win             `json:"topWins"`
	DeadEnds   []DeadEnd         `json:"deadEnds"`
	Metadata   map[string]string `json:"metadata"`
}

// ReviewerGrade is one reviewer agent's CodeRabbit-style structured
// grade. Severity is typed (compile-time) so a typo by the agent
// produces a build-time failure rather than a silently mis-tier'd
// grade. FilePath and Line are optional (for grades targeting overall
// behaviour rather than a specific code site). SubmittedAt is when the
// broker recorded the grade, not when the agent's process started.
//
// SubmittedAt is `time.Time` (matches the design doc and Lane E's
// stub). Lane D's pre-integration stub used `string` (RFC3339) for the
// same field; integration drops Lane D's stub in favour of this
// definition, and Lane D's timeout filler is rewritten to populate
// time.Time. The constant values for Severity match Lane D's stub
// exactly (critical/major/minor/nitpick/skipped) so the timeout fill
// behaviour is preserved without code changes inside Lane D.
type ReviewerGrade struct {
	ReviewerSlug string    `json:"reviewerSlug"`
	Severity     Severity  `json:"severity"`
	Suggestion   string    `json:"suggestion"`
	Reasoning    string    `json:"reasoning"`
	FilePath     string    `json:"filePath"`
	Line         int       `json:"line"`
	SubmittedAt  time.Time `json:"submittedAt"`
}

// Dependencies captures the v1 parent/child + flat-blocked-on shape. v1
// has no DAG-wide cycle detection (parent/child is acyclic by tree
// definition; flat blockers cannot self-reference). v1.1 may extend.
type Dependencies struct {
	// ParentTaskID is empty for root tasks. Sub-issues set this to
	// their parent's task ID.
	ParentTaskID string `json:"parentTaskId"`
	// BlockedOn is a flat list of task IDs or PR identifiers blocking
	// this task's progress. Mirrors teamTask.BlockedOn (added in
	// Lane A).
	BlockedOn []string `json:"blockedOn"`
}

// DecisionPacket is the full task-level artifact. One per task. Stored
// in broker memory keyed by task ID, persisted as JSON at
// ~/.wuphf/tasks/<id>/decision_packet.json (atomic-rename pattern),
// and surfaced over HTTP at /tasks/:id (Lane E + Lane G).
//
// The on-disk JSON shape is 1:1 with the wire shape — both produced by
// json.Marshal of the same struct value. PascalCase keys match Lane E's
// already-shipped read-side stub and the design doc on-disk
// specification.
//
// LifecycleState is `LifecycleState` (the typed Lane A constant)
// rather than `string` (Lane E's stub) so direct callers cannot stamp
// a non-canonical state into the packet. The stub typed it as `string`
// only because Lane E shipped before Lane A's typed constant was
// visible on the integration branch; integration converts the field
// type without changing the wire format (the underlying JSON value is
// the same string).
type DecisionPacket struct {
	TaskID         string          `json:"taskId"`
	LifecycleState LifecycleState  `json:"lifecycleState"`
	Spec           Spec            `json:"spec"`
	SessionReport  SessionReport   `json:"sessionReport"`
	ChangedFiles   []DiffSummary   `json:"changedFiles"`
	ReviewerGrades []ReviewerGrade `json:"reviewerGrades"`
	Dependencies   Dependencies    `json:"dependencies"`
	UpdatedAt      time.Time       `json:"updatedAt"`
}
