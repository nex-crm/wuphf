package workflowpress

// contract.go defines the two artifacts at the head of the press pipeline:
//
//   - WorkflowResearch — raw, evidence-rich discovery. Append-only, kept messy,
//     persisted OUTSIDE the kernel. Never the source of truth for generation.
//   - WorkflowSpec — the canonical, operator-reviewed contract. A workflow STATE
//     MACHINE (not just an API spec) that everything downstream is generated and
//     verified against. INSIDE the kernel's schema.
//
// Each inferable element of the spec carries Provenance (trust tier +
// confidence) so an inferred write degrades to a human-approved one. The freeze
// step is the human gate: the operator reviews and accepts the contract.

// Wire-format (schema) versions for the two contract artifacts. These are the
// version of the SERIALIZED SHAPE of the artifact — distinct from the per-spec
// content Version counter (which bumps when an overlay is accepted). The schema
// version bumps only on a BREAKING change to the wire shape (a removed or renamed
// field, a changed type, or a tightened invariant); additive changes within a
// major version do not bump it. See docs/specs/workflow-press.md for the compat
// policy.
//
// Validate fails CLOSED on any schema_version it does not recognise: an unknown
// or newer version means the producer speaks a wire format this kernel cannot be
// sure it understands, so it is rejected rather than decoded best-effort (which
// would risk silently zero-valuing a guard or a RequiresApproval flag).
const (
	// SchemaVersionWorkflowSpec is the current supported wire-format version of a
	// WorkflowSpec. Start at 1; bump on a breaking change to the spec wire shape.
	SchemaVersionWorkflowSpec = 1
	// SchemaVersionWorkflowResearch is the current supported wire-format version of
	// a WorkflowResearch. Start at 1; bump on a breaking change to the research wire
	// shape.
	SchemaVersionWorkflowResearch = 1
)

// TrustTier is the load-bearing provenance dimension borrowed from
// cli-printing-press. It drives caution: an inferred or merely observed
// write-action requires human approval, while an operator-stated one may be
// looser.
type TrustTier string

const (
	// TrustObserved means the element was seen directly in captured evidence
	// (CDP traces, sample records) but not confirmed by the operator.
	TrustObserved TrustTier = "observed"
	// TrustOperatorStated means the operator explicitly told us this element.
	// It is the only tier that may relax write-approval.
	TrustOperatorStated TrustTier = "operator-stated"
	// TrustInferred means the element was synthesised by the model from
	// evidence. The lowest trust; inferred writes always require approval.
	TrustInferred TrustTier = "inferred"
)

// Valid reports whether the trust tier is one of the three known values.
func (t TrustTier) Valid() bool {
	switch t {
	case TrustObserved, TrustOperatorStated, TrustInferred:
		return true
	default:
		return false
	}
}

// Provenance attaches evidence-trust to an inferable spec element. Confidence is
// a 0..1 score; TrustTier degrades writes to human approval unless the element
// was operator-stated. Evidence optionally points back into the research store
// (e.g. a trace id or sample-record key) so a reviewer can audit the inference.
type Provenance struct {
	TrustTier  TrustTier `json:"trust_tier"`
	Confidence float64   `json:"confidence"`
	Evidence   []string  `json:"evidence,omitempty"`
}

// IsOperatorStated reports whether the element was explicitly stated by the
// operator, which is the only condition under which a write-action's approval
// requirement may be relaxed.
func (p Provenance) IsOperatorStated() bool {
	return p.TrustTier == TrustOperatorStated
}

// --- WorkflowResearch: raw discovery (outside the kernel) ---

// WorkflowResearch is the raw, evidence-rich discovery artifact
// (workflow-research.json). It is append-only and kept messy on purpose; it is
// the outside-the-kernel evidence store, never the source of truth for
// generation. Distinct from WorkflowSpec, which is the frozen contract.
type WorkflowResearch struct {
	// SchemaVersion is the wire-format version of this research artifact (NOT a
	// content counter). It must equal SchemaVersionWorkflowResearch; a strict
	// decoder asserts it so a removed/renamed field or a version mismatch fails
	// loudly instead of silently zero-valuing evidence.
	SchemaVersion int `json:"schema_version"`
	// WorkflowID ties research to the spec it informs (a stable slug like
	// "trial-to-ae-routing"). Multiple research records may share one id as
	// evidence accumulates.
	WorkflowID string `json:"workflow_id"`
	// SessionContext is free-form context captured from the discovery session
	// (what the operator was doing, which systems were open).
	SessionContext string `json:"session_context,omitempty"`
	// OperatorNotes are the operator's own words about the workflow.
	OperatorNotes []string `json:"operator_notes,omitempty"`
	// SampleRecords are raw example domain records observed during discovery.
	// Stored as opaque JSON so the evidence stays faithful to what was seen.
	SampleRecords []SampleRecord `json:"sample_records,omitempty"`
	// ObservedExceptions are edge/failure cases seen in the wild.
	ObservedExceptions []ObservedException `json:"observed_exceptions,omitempty"`
	// OperatorEdits are corrections the operator made to a synthesised spec;
	// these are a primary improvement signal.
	OperatorEdits []OperatorEdit `json:"operator_edits,omitempty"`
	// ToolTraces are captured tool/CDP invocations (the recorder WUPHF has and
	// cli-printing-press lacks).
	ToolTraces []ToolTrace `json:"tool_traces,omitempty"`
	// InferredEndpoints are the templated API endpoints Discover distilled from
	// the raw HTTP traces (concrete paths collapsed to /accounts/{id}). A derived
	// signal, not a contract — the freeze step decides what becomes an Action.
	InferredEndpoints []InferredEndpoint `json:"inferred_endpoints,omitempty"`
	// InferredSchemas are the count-based-nullability entity schemas Discover
	// inferred over the sample records (a field seen in every record is required,
	// one seen in only some is nullable). Also a derived signal.
	InferredSchemas []InferredSchema `json:"inferred_schemas,omitempty"`
}

// SampleRecord is one raw example domain record observed during discovery,
// retained as opaque JSON to keep the evidence faithful.
type SampleRecord struct {
	Entity string            `json:"entity"`
	Fields map[string]string `json:"fields,omitempty"`
	Source string            `json:"source,omitempty"`
}

// ObservedException is an edge or failure case seen during discovery, evidence
// for a future WorkflowSpec.Exception.
type ObservedException struct {
	Description string `json:"description"`
	Frequency   int    `json:"frequency,omitempty"`
	HandledAs   string `json:"handled_as,omitempty"`
}

// OperatorEdit records a correction the operator made to a synthesised spec.
// Edits are a primary improvement signal feeding proposed overlays.
type OperatorEdit struct {
	Path   string `json:"path"`
	Before string `json:"before,omitempty"`
	After  string `json:"after"`
	Reason string `json:"reason,omitempty"`
}

// ToolTrace is a captured tool/CDP invocation. The raw envelope is kept for
// inference; secrets must be redacted before persistence (see the security note
// in doc.go).
type ToolTrace struct {
	Tool    string `json:"tool"`
	Action  string `json:"action,omitempty"`
	Request string `json:"request,omitempty"`
	Result  string `json:"result,omitempty"`
}

// --- WorkflowSpec: the canonical contract (inside the kernel) ---

// WorkflowSpec is the frozen, operator-reviewed contract (workflow-spec.json)
// that everything downstream is generated and verified against. It is a workflow
// STATE MACHINE: states, events, guards and actions, plus exceptions, SLAs,
// verification scenarios and improvement signals.
type WorkflowSpec struct {
	// SchemaVersion is the wire-format version of this spec artifact. It is
	// DISTINCT from Version: SchemaVersion versions the serialized SHAPE of the
	// contract (it bumps only on a breaking wire-shape change), while Version is the
	// per-spec content counter that bumps when an overlay is accepted. It must equal
	// SchemaVersionWorkflowSpec; Validate and the generated tool's strict loader
	// assert it so a removed/renamed field or a version mismatch fails loudly rather
	// than silently zero-valuing a guard or a RequiresApproval flag.
	SchemaVersion int `json:"schema_version"`
	// ID is the stable workflow slug (e.g. "trial-to-ae-routing").
	ID string `json:"id"`
	// Version is the spec version, bumped when an overlay is accepted.
	Version int `json:"version"`
	// Goal is what the workflow accomplishes.
	Goal string `json:"goal"`
	// Operator is whose work this is — the RevOps human in the loop.
	Operator string `json:"operator"`
	// Entities are the domain objects the workflow moves.
	Entities []Entity `json:"entities"`
	// States are the nodes of the state machine. The first state is the initial
	// state.
	States []State `json:"states"`
	// Events are the named triggers that drive transitions (including scheduled
	// triggers).
	Events []Event `json:"events"`
	// Guards are the named conditions that gate transitions.
	Guards []Guard `json:"guards,omitempty"`
	// Actions are the side-effecting steps. Write-actions carry
	// RequiresApproval.
	Actions []Action `json:"actions"`
	// Exceptions are the known failure/edge cases and how they are handled.
	Exceptions []Exception `json:"exceptions,omitempty"`
	// SLAs are timing/freshness expectations.
	SLAs []SLA `json:"slas,omitempty"`
	// VerificationScenarios are the fixtures + expected transitions shipcheck
	// replays — the contract carries its own tests.
	VerificationScenarios []VerificationScenario `json:"verification_scenarios"`
	// ImprovementSignals are what to watch in live usage that should propose an
	// overlay.
	ImprovementSignals []ImprovementSignal `json:"improvement_signals,omitempty"`
	// GuardConfig carries the per-workflow guard-evaluation constants the spec's
	// guards reference but a fixture does not always carry — the named thresholds
	// (e.g. icp_threshold) and the fixture-key aliases (e.g. renewal_date ->
	// renewal_in_days). It is OPTIONAL (omitempty) and additive: a spec without it
	// evaluates guards against fixture data alone. Carrying these on the CONTRACT,
	// not in the kernel runner, is what lets a new workflow's rubric constants ship
	// with its spec instead of forcing an edit to the protected runtime — the
	// contract is self-contained and the kernel stops growing per workflow.
	GuardConfig GuardConfig `json:"guard_config,omitempty"`
}

// GuardConfig is the per-workflow guard-evaluation knowledge a generic evaluator
// cannot read out of the fixture alone: the named thresholds a guard compares
// against, and the aliases mapping a guard operand's wording to the fixture key
// that actually carries its value. It is per-WORKFLOW domain knowledge, so it
// lives on the spec (or the outside-kernel registry that seeds the spec), NOT
// hardcoded in the kernel's DefaultGuardEvaluator. The zero value is empty: a
// guard whose operand is neither in the fixture nor in Thresholds simply does not
// hold (the evaluator fails the guard rather than the run).
type GuardConfig struct {
	// Thresholds supplies a deterministic value for a named threshold operand the
	// fixture does not carry (e.g. {"icp_threshold": 50}). These are the rubric
	// constants a real ICP / match / renewal model holds, kept explicit and stable
	// so generation stays deterministic and a scenario is self-contained.
	Thresholds map[string]float64 `json:"thresholds,omitempty"`
	// FixtureAliases maps a guard operand's last path segment to the fixture key
	// that actually carries its value, when the contract's domain wording and the
	// fixture's wording differ (e.g. the guard speaks of "renewal_date - now" while
	// the fixture carries the already-computed "renewal_in_days").
	FixtureAliases map[string]string `json:"fixture_aliases,omitempty"`
}

// isEmpty reports whether the config carries no thresholds and no aliases. A bare
// DefaultGuardEvaluator with an empty config is enriched by NewRunner from the
// running spec's GuardConfig; a non-empty one the caller supplied is respected.
func (c GuardConfig) isEmpty() bool {
	return len(c.Thresholds) == 0 && len(c.FixtureAliases) == 0
}

// Entity is a domain object the workflow moves (e.g. a trial signup, an
// account, an AE).
type Entity struct {
	Name        string     `json:"name"`
	Description string     `json:"description,omitempty"`
	Fields      []string   `json:"fields,omitempty"`
	Provenance  Provenance `json:"provenance"`
}

// State is a node in the workflow state machine. Initial marks the entry state;
// Terminal marks an end state.
type State struct {
	Name        string     `json:"name"`
	Description string     `json:"description,omitempty"`
	Initial     bool       `json:"initial,omitempty"`
	Terminal    bool       `json:"terminal,omitempty"`
	Provenance  Provenance `json:"provenance"`
}

// EventTrigger distinguishes how an event fires. A scheduled trigger is what
// makes a workflow like renewal-risk-sweep periodic.
type EventTrigger string

const (
	// TriggerExternal fires from an outside system (a webhook, a new record).
	TriggerExternal EventTrigger = "external"
	// TriggerScheduled fires on a timer/cron (e.g. weekly).
	TriggerScheduled EventTrigger = "scheduled"
	// TriggerInternal fires from a prior action's completion within the
	// workflow.
	TriggerInternal EventTrigger = "internal"
)

// Valid reports whether the event trigger is one of the three known values. An
// unknown trigger must be rejected by Validate so it cannot silently change how
// an event is scheduled or fired.
func (t EventTrigger) Valid() bool {
	switch t {
	case TriggerExternal, TriggerScheduled, TriggerInternal:
		return true
	default:
		return false
	}
}

// Event is a named trigger that drives transitions between states. Schedule is
// set (cron-ish) only when Trigger is scheduled.
type Event struct {
	Name       string       `json:"name"`
	Trigger    EventTrigger `json:"trigger"`
	Schedule   string       `json:"schedule,omitempty"`
	From       string       `json:"from"`
	To         string       `json:"to"`
	Guard      string       `json:"guard,omitempty"`
	Provenance Provenance   `json:"provenance"`
}

// Guard is a named condition that gates a transition (e.g. "lead score >= ICP
// threshold").
type Guard struct {
	Name       string     `json:"name"`
	Expr       string     `json:"expr"`
	Provenance Provenance `json:"provenance"`
}

// ActionKind classifies an action's effect. Writes (external/internal mutations)
// default to RequiresApproval unless operator-stated; reads do not.
type ActionKind string

const (
	// ActionRead is a non-mutating read (enrichment lookups, usage pulls).
	ActionRead ActionKind = "read"
	// ActionInternalWrite mutates internal state (create a task, update a
	// record).
	ActionInternalWrite ActionKind = "internal-write"
	// ActionExternalWrite mutates an external system or sends a message (route,
	// post to a channel, send outreach). Always the most cautious tier.
	ActionExternalWrite ActionKind = "external-write"
)

// Valid reports whether the action kind is one of the three known values.
//
// This is security-load-bearing: IsWrite fails OPEN for an unknown kind (it
// returns false, classifying the action as a read), so an unknown kind would
// slip past the write-approval rule in Validate. Validate must therefore reject
// any kind that is not Valid before IsWrite is ever consulted, so a write
// smuggled in under an unrecognised kind string cannot bypass approval.
func (k ActionKind) Valid() bool {
	switch k {
	case ActionRead, ActionInternalWrite, ActionExternalWrite:
		return true
	default:
		return false
	}
}

// IsWrite reports whether the action mutates state (and therefore is subject to
// the approval rule). It fails OPEN for an unknown kind (returns false); callers
// must gate on Valid first — Validate does — so an unrecognised kind is rejected
// rather than silently treated as a non-mutating read.
func (k ActionKind) IsWrite() bool {
	return k == ActionInternalWrite || k == ActionExternalWrite
}

// Action is a side-effecting step in the workflow. Write-actions carry
// RequiresApproval (true unless operator-stated). Idempotent marks actions that
// must be safe to re-run (e.g. a merge) so shipcheck can prove no double-apply.
type Action struct {
	Name string     `json:"name"`
	Kind ActionKind `json:"kind"`
	// On names the event whose transition fires this action.
	On string `json:"on,omitempty"`
	// Target is the system/channel the action touches (e.g. "deal channel",
	// "salesforce").
	Target string `json:"target,omitempty"`
	// RequiresApproval routes the action through the ExternalActionApprovalCard
	// before it runs. True unless the action's provenance is operator-stated.
	RequiresApproval bool `json:"requires_approval"`
	// Idempotent marks an action that must be safe to re-run. Shipcheck proves
	// it does not double-apply.
	Idempotent bool       `json:"idempotent,omitempty"`
	Provenance Provenance `json:"provenance"`
}

// Exception is a known failure/edge case and how it is handled.
type Exception struct {
	Name       string     `json:"name"`
	When       string     `json:"when"`
	Handling   string     `json:"handling"`
	Provenance Provenance `json:"provenance"`
}

// SLA is a timing/freshness expectation (e.g. "route within 5 minutes",
// "usage data no older than 24h").
type SLA struct {
	Name       string     `json:"name"`
	Metric     string     `json:"metric"`
	Threshold  string     `json:"threshold"`
	Provenance Provenance `json:"provenance"`
}

// VerificationScenario is a fixture + expected transitions that shipcheck
// replays. Given seeds the world, When names the triggering event, and
// ExpectTransitions are the state hops that must occur.
type VerificationScenario struct {
	Name              string            `json:"name"`
	Given             map[string]string `json:"given,omitempty"`
	When              string            `json:"when"`
	ExpectTransitions []Transition      `json:"expect_transitions"`
	// ExpectApproval asserts the scenario triggers a human approval gate (used
	// for external-write coverage).
	ExpectApproval bool `json:"expect_approval,omitempty"`
}

// Transition is one expected state hop in a verification scenario.
type Transition struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// ImprovementSignalKind classifies what live-usage signal should propose an
// overlay.
type ImprovementSignalKind string

const (
	// SignalRecurringException fires when an exception recurs above a threshold.
	SignalRecurringException ImprovementSignalKind = "recurring-exception"
	// SignalOperatorEdit fires when the operator repeatedly edits the same path.
	SignalOperatorEdit ImprovementSignalKind = "operator-edit"
	// SignalSLAMiss fires when an SLA is missed.
	SignalSLAMiss ImprovementSignalKind = "sla-miss"
)

// ImprovementSignal is what to watch in live usage that should propose an
// overlay (recurring exceptions, operator edits, SLA misses).
type ImprovementSignal struct {
	Kind      ImprovementSignalKind `json:"kind"`
	Watch     string                `json:"watch"`
	Threshold string                `json:"threshold,omitempty"`
}
