package team

// broker_lifecycle_transition.go is the single chokepoint for writes to the
// multi-agent harness lifecycle state machine on a teamTask. It owns:
//
//   - The LifecycleState typed string and its canonical values plus
//     the "unknown" migration fallback.
//   - The forward-map table that derives the legacy status / reviewState /
//     pipelineStage / blocked fields from a LifecycleState.
//   - The b.lifecycleIndex inverse-index map that gives the inbox an O(1)
//     lookup of "all tasks in state X".
//   - The migration shim that, on first broker boot post-deploy, fills
//     LifecycleState for tasks that came back from disk without one.
//   - The transitionLifecycleLocked helper that mutates a task's lifecycle
//     state in place AND keeps the four derived fields plus the index map
//     synchronized by construction.
//
// All callers must already hold b.mu before calling the locked helpers.
// The unlocked TransitionLifecycle wrapper is the public entry point for
// future Lane B / C / D event handlers that arrive on goroutines without
// the mutex held.
//
// Self-heal gating (build-time gate #1): when the new state is
// LifecycleStateBlocked the transition layer explicitly does NOT
// invoke requestCapabilitySelfHealingLocked. The blocked state
// is a typed legitimate condition, not an error needing self-heal. The
// unit test must observe the call site, not just the absence of side
// effects, so the gate is implemented as a hard branch in BlockTask itself
// (no scheduler is given a no-op closure).

import (
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
)

// ErrIssueNotApproved is returned by every dispatch entry point when the
// target task is not in an executable lifecycle state. Callers that surface
// this to the user should display a human-readable "issue not approved for
// dispatch" message. The gate is server-side and intentional — see spec
// "## Eng review decisions → Architecture → Approval gate".
var ErrIssueNotApproved = errors.New("issue not approved for dispatch")

// isExecutableTeamTaskStatus reports whether a LifecycleState permits dispatch
// of a turn to the owner. Running and Approved dispatch EXECUTION turns;
// Planning dispatches a PLAN-ONLY turn (the owner writes a plan, does not change
// the repo — enforced by the provider's native read-only/plan permission mode,
// see resolveTurnPosture, plus the plan-only work packet, see planModeDirective).
// All other states — Drafting, Intake, Review, ChangesRequested, etc. — must NOT
// trigger any owner turn.
//
// This is the single chokepoint: every dispatch entry point in the broker must
// call isExecutableTeamTaskStatus before enqueuing work. Comments are always
// allowed in any state. Callers that must distinguish "plan only" from "execute"
// check the state directly (Planning vs Running/Approved).
func isExecutableTeamTaskStatus(s LifecycleState) bool {
	return s == LifecycleStateRunning || s == LifecycleStateApproved || s == LifecycleStatePlanning
}

// isPreExecutionLifecycleState reports whether a task has not yet entered
// execution: no agent turn has legitimately run for it and no work product
// can exist. An "approve" on a task in one of these states means "start the
// work" (activation), NEVER "accept the delivered work" (terminal) — the
// ICP-eval v3 J2 failure was Approve & Start resolving to terminal
// `approved` on zero-work tasks ([19:04], shot 28).
func isPreExecutionLifecycleState(s LifecycleState) bool {
	switch s {
	case LifecycleStateDrafting, LifecycleStateIntake, LifecycleStateReady, LifecycleStateQueuedBehindOwner,
		LifecycleStatePlanning:
		// Planning is pre-execution: the owner is writing a plan read-only, no
		// work product exists yet. Approving a planning task is "start the work"
		// (Planning→Running), never a done-claim — so the verification/DoD gate
		// and the parked-complete gate treat it the same as other pre-execution
		// states.
		return true
	}
	return false
}

// lifecycleMigrationOnce ensures migrateLifecycleStatesLocked runs at most
// once per broker process even if multiple startup hooks call into it.
// Keyed by *Broker so multiple brokers (typically only in tests) each
// get their own *sync.Once. Stored as a package-level sync.Map rather
// than a field on Broker so brokers stay zero-value usable.
var lifecycleMigrationOnce sync.Map // *Broker -> *sync.Once

// LifecycleState is the typed source of truth for the multi-agent control
// loop. The canonical values plus LifecycleStateUnknown (migration
// fallback) form a closed enum; new states require updating both the
// forward-map (lifecycleDerivedFields) and the migration shim.
type LifecycleState string

const (
	// LifecycleStateUnknown is the migration fallback for tasks whose
	// derived-field tuple does not appear in lifecycleMigrationMap. The
	// broker logs a warning and surfaces the task as an explicit operator
	// decision instead of silently picking a state.
	LifecycleStateUnknown LifecycleState = "unknown"

	LifecycleStateIntake   LifecycleState = "intake"
	LifecycleStateReady    LifecycleState = "ready"
	LifecycleStateRunning  LifecycleState = "running"
	LifecycleStateReview   LifecycleState = "review"
	LifecycleStateDecision LifecycleState = "decision"
	// LifecycleStateBlocked is the generic "this task is paused" state: it is
	// recoverable and waiting on something — an upstream dependency to finish,
	// or an owner agent that stopped (timed out / errored) before durable
	// progress. The unblock cascade (unblockDependentsLocked) moves it back to
	// review when its blocker resolves. The legacy value was
	// "blocked_on_pr_merge", a holdover from the retired PR-style review/merge
	// model; WUPHF has no PRs, so the name was a misnomer. Persisted snapshots
	// carrying the old value migrate via normalizeLegacyLifecycleStateName.
	LifecycleStateBlocked           LifecycleState = "blocked"
	LifecycleStateQueuedBehindOwner LifecycleState = "queued_behind_owner"
	LifecycleStateChangesRequested  LifecycleState = "changes_requested"
	LifecycleStateApproved          LifecycleState = "approved"
	// LifecycleStateRejected marks work that a reviewer rejected
	// outright as un-landable. Distinct from Blocked (recoverable,
	// waiting on upstream) and from ChangesRequested (non-terminal,
	// owner revises). Dependent tasks STAY blocked because the work
	// did not land.
	LifecycleStateRejected LifecycleState = "rejected"

	// LifecycleStateDrafting marks a task the human EXPLICITLY parked
	// (the composer's Backlog/park path, or a legacy persisted draft).
	// Nothing lands here by default — creation IS the authorization, so
	// new tasks land Running (owner set) or Ready (ownerless). Agents can
	// post comments on a parked task; they CANNOT dispatch tool calls or
	// execution work — isExecutableTeamTaskStatus is the dispatch gate
	// that refuses execution turns in this state (tested in
	// broker_lifecycle_dispatch_test.go). The state value also round-trips
	// cleanly through JSON and the lifecycle index.
	//
	// PipelineStage choice: "draft" (matches the spec's `draft` phase name
	// in the CEO state machine and is shorter/clearer than "drafting" at
	// the data layer; the presentation layer uses "Drafting" for the UI
	// label via STATE_PILL_TOKENS on the frontend).
	LifecycleStateDrafting LifecycleState = "drafting"

	// LifecycleStatePlanning is the autonomous-planning phase of "Plan mode".
	// A plan-first task enters Planning so the owner is DISPATCHED to write a
	// plan (into its own notebook) before any execution — the turn runs in the
	// provider's NATIVE read-only/plan permission mode (Claude --permission-mode
	// plan, Codex -s read-only; see resolveTurnPosture) and the work packet is
	// plan-only (explore, ask the human any genuine questions via
	// human_interview, write the plan, then stop). It is executable (the owner
	// must be woken to plan), Status="in_progress" so it shows as actively
	// worked, and maps to the in_progress display stage. Approving the plan
	// (via the plan-approval human_interview, or a human task approve) transitions
	// Planning→Running, after which the owner executes against its own plan and
	// may create the sub-tasks the plan called for. A task that skips Plan mode
	// goes straight to Running.
	LifecycleStatePlanning LifecycleState = "planning"

	// LifecycleStateArchived marks work that has been intentionally moved
	// off the active board. Archived tasks are terminal and are excluded
	// from default active listings (same gate as done/approved), but are
	// returned when the caller passes include_done=true so the board's
	// Archive column can fetch them. Distinct from Rejected (reviewer
	// rejected the work as un-landable) and Approved (work landed). An
	// archived task can be reopened via the reopen action which resets
	// it to Drafting.
	LifecycleStateArchived LifecycleState = "archived"
)

// normalizeLegacyLifecycleStateName maps legacy lifecycle state string
// values to their canonical equivalents:
//
//   - "merged" -> "approved" (per the artifact-and-approve vocabulary
//     alignment from /plan-design-review + /plan-eng-review on 2026-05-11).
//   - "blocked_on_pr_merge" -> "blocked" (the PR-style review/merge model is
//     retired; the state was always the generic recoverable-blocked state, so
//     it keeps its behavior under the honest name. Existing prod snapshots
//     carry the old value and must load cleanly).
//
// Pass-through for every other input so this stays a targeted shim, not a
// general migration table.
//
// Called from teamTask.UnmarshalJSON so an older broker-state.json loads
// cleanly without manual migration. The next save writes the canonical
// name; the shim has no second turn on disk.
func normalizeLegacyLifecycleStateName(s LifecycleState) LifecycleState {
	switch strings.ToLower(strings.TrimSpace(string(s))) {
	case "merged":
		return LifecycleStateApproved
	case "blocked_on_pr_merge":
		return LifecycleStateBlocked
	}
	return s
}

// CanonicalLifecycleStates returns the valid lifecycle states (excluding
// the unknown migration fallback) in stable order. Used by tests sweeping
// the forward map.
func CanonicalLifecycleStates() []LifecycleState {
	return []LifecycleState{
		LifecycleStateDrafting,
		LifecycleStateIntake,
		LifecycleStateReady,
		LifecycleStatePlanning,
		LifecycleStateRunning,
		LifecycleStateReview,
		LifecycleStateDecision,
		LifecycleStateBlocked,
		LifecycleStateQueuedBehindOwner,
		LifecycleStateChangesRequested,
		LifecycleStateApproved,
		LifecycleStateRejected,
		LifecycleStateArchived,
	}
}

// lifecycleDerivedFieldsRow captures the tuple of legacy fields that the
// forward-map table assigns when a task enters a LifecycleState. The
// Blocked column is only true for states that intentionally pause execution.
type lifecycleDerivedFieldsRow struct {
	PipelineStage string
	ReviewState   string
	Status        string
	Blocked       bool
}

// lifecycleDerivedFields is the forward-map from LifecycleState to the
// legacy (pipelineStage, reviewState, status, blocked) tuple. The table
// lives in source so test build-time gate #3 (forward map) can walk it
// directly.
//
// Deviation from design doc: the doc-prescribed row for the blocked state
// is {pipelineStage:"review", reviewState:"ready_for_review",
// status:"in_progress", blocked:true}. Pre-Lane-A code paths in the broker
// (notifier_targets.go, headless_codex_queue.go, broker_requests_interviews.go,
// broker_tasks_plan.go, broker_defaults.go, broker_tasks_lifecycle.go,
// broker_tasks_mutation_service.go) all check status == "blocked" as the
// load-bearing signal that a task is paused. Flipping that contract in
// Lane A would either regress dozens of legacy code paths or require a
// matching Lane A sweep of every reader, which is out of scope here. We
// keep status="blocked" for the blocked state and let Lane F's CLI
// rewrite or v1.1 cleanup migrate the readers off the legacy contract
// once the rest of the harness is in place. The lifecycle index and the
// LifecycleState field still source-of-truth correctly.
var lifecycleDerivedFields = map[LifecycleState]lifecycleDerivedFieldsRow{
	// Drafting: explicitly parked — agents comment but cannot dispatch.
	// PipelineStage="draft" matches the spec's draft phase name. Status="open"
	// keeps the task visible in the open-tasks view; Blocked=false so it is
	// not confused with a waiting-on-upstream state. isExecutableTeamTaskStatus
	// (above) is the dispatch guard that refuses execution turns for tasks in
	// this state.
	LifecycleStateDrafting: {PipelineStage: "draft", ReviewState: "pending_review", Status: "open", Blocked: false},
	// Planning: owner is dispatched to write a plan. Status="in_progress" +
	// executable so the planning turn fires; PipelineStage="plan".
	LifecycleStatePlanning:          {PipelineStage: "plan", ReviewState: "pending_review", Status: "in_progress", Blocked: false},
	LifecycleStateIntake:            {PipelineStage: "triage", ReviewState: "pending_review", Status: "open", Blocked: false},
	LifecycleStateReady:             {PipelineStage: "triage", ReviewState: "pending_review", Status: "open", Blocked: false},
	LifecycleStateRunning:           {PipelineStage: "implement", ReviewState: "pending_review", Status: "in_progress", Blocked: false},
	LifecycleStateReview:            {PipelineStage: "review", ReviewState: "ready_for_review", Status: "in_progress", Blocked: false},
	LifecycleStateDecision:          {PipelineStage: "review", ReviewState: "ready_for_review", Status: "in_progress", Blocked: false},
	LifecycleStateBlocked:           {PipelineStage: "review", ReviewState: "ready_for_review", Status: "blocked", Blocked: true},
	LifecycleStateQueuedBehindOwner: {PipelineStage: "triage", ReviewState: "pending_review", Status: "open", Blocked: true},
	LifecycleStateChangesRequested:  {PipelineStage: "implement", ReviewState: "pending_review", Status: "in_progress", Blocked: false},
	LifecycleStateApproved:          {PipelineStage: "ship", ReviewState: "approved", Status: "done", Blocked: false},
	// Rejected keeps Blocked: true so the unblock cascade in
	// unblockDependentsLocked treats the upstream as unresolved and
	// downstream tasks STAY blocked permanently. Status="rejected" is
	// NOT in isTerminalTeamTaskStatus, which is what we want.
	LifecycleStateRejected: {PipelineStage: "review", ReviewState: "rejected", Status: "rejected", Blocked: true},
	// Archived: terminal, off-board. Status="archived" is added to
	// isTerminalTeamTaskStatus so archived tasks are treated as closed
	// (not re-dispatched). Blocked=false because the task is not waiting
	// on anything; it is simply off the active board. ReviewState="approved"
	// mirrors the approved path (clean terminal, not a failure).
	LifecycleStateArchived: {PipelineStage: "archived", ReviewState: "approved", Status: "archived", Blocked: false},
}

// derivedFieldsFor returns the forward-map row for a state, plus a flag
// reporting whether the state is canonical. Unknown states return a
// zero-value row and ok=false so callers can decide whether to surface a
// warning.
func derivedFieldsFor(state LifecycleState) (lifecycleDerivedFieldsRow, bool) {
	row, ok := lifecycleDerivedFields[state]
	return row, ok
}

// LifecycleStage is the 7-value display grouping that collapses the 12
// execution substrate LifecycleState values into the user-facing board
// columns. The stage concept lives only in Go-side canonical use and
// tests; the web frontend derives its own grouping from the lifecycle_state
// wire field and does NOT receive a stage field over the wire.
type LifecycleStage string

const (
	// StageScheduled is reserved for routines/scheduled work. No
	// LifecycleState maps to StageScheduled — it comes from a different
	// scheduling primitive. lifecycleStageFor never returns StageScheduled.
	StageScheduled  LifecycleStage = "scheduled"
	StageBacklog    LifecycleStage = "backlog"
	StageInProgress LifecycleStage = "in_progress"
	StageBlocked    LifecycleStage = "blocked"
	StageNeedsHuman LifecycleStage = "needs_human"
	StageDone       LifecycleStage = "done"
	StageArchive    LifecycleStage = "archive"
)

// lifecycleStageFor maps a LifecycleState to its display LifecycleStage.
// The mapping is:
//   - backlog     ← drafting, intake, ready, unknown
//   - in_progress ← planning, running, review, changes_requested
//   - blocked     ← blocked, queued_behind_owner
//   - needs_human ← decision
//   - done        ← approved
//   - archive     ← archived, rejected
//
// StageScheduled is never returned (it comes from a different scheduling
// primitive). Any unmapped state defaults to StageBacklog.
func lifecycleStageFor(s LifecycleState) LifecycleStage {
	switch s {
	case LifecycleStateDrafting, LifecycleStateIntake, LifecycleStateReady, LifecycleStateUnknown:
		return StageBacklog
	case LifecycleStatePlanning, LifecycleStateRunning, LifecycleStateReview, LifecycleStateChangesRequested:
		return StageInProgress
	case LifecycleStateBlocked, LifecycleStateQueuedBehindOwner:
		return StageBlocked
	case LifecycleStateDecision:
		return StageNeedsHuman
	case LifecycleStateApproved:
		return StageDone
	case LifecycleStateArchived, LifecycleStateRejected:
		return StageArchive
	default:
		return StageBacklog
	}
}

// lifecycleMigrationKey is the legacy (pipelineStage, reviewState, status,
// blocked) tuple the migration shim consults to derive a LifecycleState
// for tasks loaded from a pre-Lane-A broker-state.json snapshot. Keys
// cover every tuple actually produced by pre-Lane-A code paths plus a
// handful of obvious aliases.
type lifecycleMigrationKey struct {
	PipelineStage string
	ReviewState   string
	Status        string
	Blocked       bool
}

// lifecycleMigrationMap mirrors the forward-map but accepts the broader
// set of legacy tuples that pre-Lane-A code emitted. Tuples not present
// here fall through to LifecycleStateUnknown with a logged warning.
//
// Keys are normalised to lower-case before lookup; callers must pass
// normalised values via deriveLifecycleStateFromLegacy.
var lifecycleMigrationMap = map[lifecycleMigrationKey]LifecycleState{
	// Canonical tuples first — direct inverse of lifecycleDerivedFields.
	{PipelineStage: "draft", ReviewState: "pending_review", Status: "open", Blocked: false}:            LifecycleStateDrafting,
	{PipelineStage: "triage", ReviewState: "pending_review", Status: "open", Blocked: false}:           LifecycleStateReady,
	{PipelineStage: "implement", ReviewState: "pending_review", Status: "in_progress", Blocked: false}: LifecycleStateRunning,
	{PipelineStage: "review", ReviewState: "ready_for_review", Status: "in_progress", Blocked: false}:  LifecycleStateReview,
	{PipelineStage: "review", ReviewState: "ready_for_review", Status: "blocked", Blocked: true}:       LifecycleStateBlocked,
	{PipelineStage: "triage", ReviewState: "pending_review", Status: "open", Blocked: true}:            LifecycleStateQueuedBehindOwner,
	{PipelineStage: "ship", ReviewState: "approved", Status: "done", Blocked: false}:                   LifecycleStateApproved,
	{PipelineStage: "review", ReviewState: "rejected", Status: "rejected", Blocked: true}:              LifecycleStateRejected,
	// Plan mode writes {plan, pending_review, in_progress} while a task's
	// owner is planning. Back-derive to Planning so persisted planning tasks
	// resume in the planning phase (read-only, awaiting plan approval).
	{PipelineStage: "plan", ReviewState: "pending_review", Status: "in_progress", Blocked: false}: LifecycleStatePlanning,

	// changes_requested back-derivation. Same legacy tuple as Running
	// EXCEPT for the reviewState marker, so the inverse map distinguishes
	// "this task is iterating because the reviewer asked for changes"
	// from "this task is just running for the first time."
	{PipelineStage: "implement", ReviewState: "changes_requested", Status: "in_progress", Blocked: false}: LifecycleStateChangesRequested,

	// Pre-Lane-A code wrote status="blocked" instead of relying on the
	// blocked bool. Map every reasonable variant to LifecycleStateBlocked so
	// real production data has a deterministic landing pad.
	{PipelineStage: "", ReviewState: "", Status: "blocked", Blocked: true}:                         LifecycleStateBlocked,
	{PipelineStage: "", ReviewState: "", Status: "blocked", Blocked: false}:                        LifecycleStateBlocked,
	{PipelineStage: "implement", ReviewState: "pending_review", Status: "blocked", Blocked: true}:  LifecycleStateBlocked,
	{PipelineStage: "implement", ReviewState: "pending_review", Status: "blocked", Blocked: false}: LifecycleStateBlocked,
	{PipelineStage: "review", ReviewState: "ready_for_review", Status: "blocked", Blocked: false}:  LifecycleStateBlocked,

	// Bare statuses without pipeline metadata cover ad-hoc tasks that
	// never moved through the formal pipeline.
	{PipelineStage: "", ReviewState: "", Status: "open", Blocked: false}:        LifecycleStateReady,
	{PipelineStage: "", ReviewState: "", Status: "open", Blocked: true}:         LifecycleStateQueuedBehindOwner,
	{PipelineStage: "", ReviewState: "", Status: "in_progress", Blocked: false}: LifecycleStateRunning,
	{PipelineStage: "", ReviewState: "", Status: "review", Blocked: false}:      LifecycleStateReview,
	{PipelineStage: "", ReviewState: "", Status: "done", Blocked: false}:        LifecycleStateApproved,
	{PipelineStage: "", ReviewState: "", Status: "completed", Blocked: false}:   LifecycleStateApproved,

	// Cancelled/canceled — terminal but not "approved". Still mapped to
	// approved for v1 to avoid an unbounded lifecycle; v1.1 may introduce a
	// dedicated cancelled state.
	{PipelineStage: "", ReviewState: "", Status: "canceled", Blocked: false}:  LifecycleStateApproved,
	{PipelineStage: "", ReviewState: "", Status: "cancelled", Blocked: false}: LifecycleStateApproved,

	// Archived — tasks stored on disk with status="archived" (bare or
	// canonical tuple) resolve to LifecycleStateArchived so pre-existing
	// broker-state.json files load cleanly after the archive action ships.
	{PipelineStage: "", ReviewState: "", Status: "archived", Blocked: false}:                 LifecycleStateArchived,
	{PipelineStage: "archived", ReviewState: "approved", Status: "archived", Blocked: false}: LifecycleStateArchived,
}

// deriveLifecycleStateFromLegacy looks the legacy tuple up in the
// migration map. Inputs are case-normalised so capitalised values from
// human-edited state files still resolve. Returns LifecycleStateUnknown
// when no canonical mapping exists.
func deriveLifecycleStateFromLegacy(pipelineStage, reviewState, status string, blocked bool) LifecycleState {
	key := lifecycleMigrationKey{
		PipelineStage: strings.ToLower(strings.TrimSpace(pipelineStage)),
		ReviewState:   strings.ToLower(strings.TrimSpace(reviewState)),
		Status:        strings.ToLower(strings.TrimSpace(status)),
		Blocked:       blocked,
	}
	if state, ok := lifecycleMigrationMap[key]; ok {
		return state
	}
	return LifecycleStateUnknown
}

// migrateLifecycleStatesLocked is invoked once on first broker boot post
// Lane-A deploy. Tasks that already carry a LifecycleState are left
// untouched (idempotent across restarts). Tasks without one have their
// state derived from the legacy tuple and the index re-built from
// scratch.
//
// TODO(lane-a-followup): production-fixture test, see TODOS.md #0 — needs
// real broker-state.json snapshots from dogfood + opt-in external users.
func (b *Broker) migrateLifecycleStatesLocked() {
	if b == nil {
		return
	}
	b.lifecycleIndex = map[LifecycleState][]string{}
	for i := range b.tasks {
		task := &b.tasks[i]
		if task.LifecycleState != "" {
			b.indexLifecycleLocked(task.ID, "", task.LifecycleState)
			continue
		}
		derived := deriveLifecycleStateFromLegacy(task.pipelineStage, task.reviewState, task.status, task.blocked)
		if derived == LifecycleStateUnknown {
			// The on-disk pipeline_stage/review_state may use a newer
			// pipeline-template scheme (e.g. ActiveStage="act") that postdates
			// this map, or normalizeTaskPlan may have filled them in before
			// migration runs. Fall back to the bare status signal, which the
			// map covers for ad-hoc tasks — this rescues clean in-flight / open
			// / done / archived tasks regardless of the template stage names,
			// while genuinely contradictory tuples (e.g. status=in_progress AND
			// blocked) stay Unknown and are logged for operator triage.
			if byStatus := deriveLifecycleStateFromLegacy("", "", task.status, task.blocked); byStatus != LifecycleStateUnknown {
				derived = byStatus
			} else {
				log.Printf("broker: lifecycle migration: unknown tuple for task %q (pipeline_stage=%q review_state=%q status=%q blocked=%v) — falling back to %s",
					task.ID, task.pipelineStage, task.reviewState, task.status, task.blocked, LifecycleStateUnknown)
			}
		}
		task.LifecycleState = derived
		b.indexLifecycleLocked(task.ID, "", derived)
	}
}

// MigrateLifecycleStatesOnce is the broker startup entry point. Safe to
// call from any number of init hooks; the underlying migration runs
// exactly once per Broker pointer. Acquires b.mu internally.
func (b *Broker) MigrateLifecycleStatesOnce() {
	if b == nil {
		return
	}
	val, _ := lifecycleMigrationOnce.LoadOrStore(b, &sync.Once{})
	once := val.(*sync.Once)
	once.Do(func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		b.migrateLifecycleStatesLocked()
	})
}

func (b *Broker) reindexTaskLifecycleFromLegacyLocked(task *teamTask) {
	if b == nil || task == nil {
		return
	}
	derived := deriveLifecycleStateFromLegacy(task.pipelineStage, task.reviewState, task.status, task.blocked)
	if derived == LifecycleStateUnknown {
		switch status := strings.ToLower(strings.TrimSpace(task.status)); {
		case task.blocked || status == "blocked":
			derived = LifecycleStateBlocked
		case isTerminalTeamTaskStatus(status):
			derived = LifecycleStateApproved
		case status == "review" || strings.EqualFold(strings.TrimSpace(task.reviewState), "ready_for_review"):
			derived = LifecycleStateReview
		case status == "in_progress" || strings.TrimSpace(task.Owner) != "":
			derived = LifecycleStateRunning
		default:
			derived = LifecycleStateReady
		}
	}
	// Plan mode: legacy fields can't distinguish Planning (status=in_progress,
	// stage=plan) from Running, so once a task is in Planning keep it there
	// across legacy reindexes while it is still pre-execution. Only the explicit
	// plan-approval transition (Planning→Running) or a submit/terminal action —
	// which change the legacy status away from in_progress/open — move it out of
	// Planning. A blocked task is NOT preserved as Planning: blocked means it is
	// waiting on a dependency and the block derivation above (LifecycleStateBlocked)
	// must win, or the block would be silently masked.
	if task.LifecycleState == LifecycleStatePlanning && !task.blocked {
		switch strings.ToLower(strings.TrimSpace(task.status)) {
		case "", "open", "in_progress":
			derived = LifecycleStatePlanning
		}
	}
	// This helper is used after legacy mutation paths have deliberately
	// written status/review fields that predate the LifecycleState table.
	// Preserve that legacy tuple as authoritative and only repair the
	// typed state plus index classification.
	prev := task.LifecycleState
	task.LifecycleState = derived
	b.indexLifecycleLocked(task.ID, prev, derived)
	// Re-stamp the persisted Decision Packet whenever a legacy mutation
	// moved the typed state. GET /tasks/{id} (the task-detail page) serves
	// the PACKET's lifecycleState while the board list serves the task's —
	// before this hook, every legacy mutation path (claim/assign/approve/
	// complete/submit_for_review in MutateTask) updated the task but left
	// the packet stale, so the page showed "drafting"/"decision" while the
	// board showed "approved" (ICP-eval v3 [18:12:57], [20:08:01]) and the
	// human's Approve & Start click was judged against a state the page
	// never showed (J2 [19:04]). No-op when no packet exists for the task.
	if prev != derived {
		b.onLifecycleTransitionLocked(task.ID, prev, derived)
	}
}

func (b *Broker) markTaskQueuedBehindActiveOwnerLocked(task *teamTask) {
	if b == nil || task == nil {
		return
	}
	if err := b.applyLifecycleStateLocked(task, LifecycleStateQueuedBehindOwner); err != nil {
		log.Printf("broker: queue task %q behind owner: %v", task.ID, err)
	}
}

// indexLifecycleLocked maintains the b.lifecycleIndex map. Pass an empty
// string for fromState when adding a brand-new task (no prior bucket to
// remove from). Caller must hold b.mu.
func (b *Broker) indexLifecycleLocked(taskID string, fromState, toState LifecycleState) {
	if b == nil {
		return
	}
	if b.lifecycleIndex == nil {
		b.lifecycleIndex = map[LifecycleState][]string{}
	}
	if fromState != "" {
		bucket := b.lifecycleIndex[fromState]
		for i, id := range bucket {
			if id == taskID {
				bucket = append(bucket[:i], bucket[i+1:]...)
				break
			}
		}
		if len(bucket) == 0 {
			delete(b.lifecycleIndex, fromState)
		} else {
			b.lifecycleIndex[fromState] = bucket
		}
	}
	if toState == "" {
		return
	}
	bucket := b.lifecycleIndex[toState]
	for _, id := range bucket {
		if id == taskID {
			return
		}
	}
	b.lifecycleIndex[toState] = append(bucket, taskID)
}

// LifecycleIndexSnapshot returns a copy of the indexed lookup map, useful
// for test assertions. Acquires b.mu.
func (b *Broker) LifecycleIndexSnapshot() map[LifecycleState][]string {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.lifecycleIndexSnapshotLocked()
}

func (b *Broker) lifecycleIndexSnapshotLocked() map[LifecycleState][]string {
	out := make(map[LifecycleState][]string, len(b.lifecycleIndex))
	for state, ids := range b.lifecycleIndex {
		copyIDs := make([]string, len(ids))
		copy(copyIDs, ids)
		out[state] = copyIDs
	}
	return out
}

// applyLifecycleStateLocked stamps the new state and the four derived
// fields onto a task in place, and updates the inverse index. The caller
// is expected to have already validated newState as canonical and to be
// holding b.mu. Returns an error only when newState has no forward-map
// row; callers that pass a canonical state can safely ignore the error.
func (b *Broker) applyLifecycleStateLocked(task *teamTask, newState LifecycleState) error {
	if task == nil {
		return fmt.Errorf("apply lifecycle: task required")
	}
	row, ok := derivedFieldsFor(newState)
	if !ok {
		return fmt.Errorf("apply lifecycle: no forward-map row for %q", newState)
	}
	prev := task.LifecycleState
	task.LifecycleState = newState
	task.pipelineStage = row.PipelineStage
	task.reviewState = row.ReviewState
	task.status = row.Status
	task.blocked = row.Blocked
	b.indexLifecycleLocked(task.ID, prev, newState)
	// Emit a chat card on every non-initial transition for task_type=issue.
	// postIssueLifecycleCardLocked already skips when from==to and when
	// task is not an issue. Skip empty/unknown previous states so the
	// initial create transition (handled by IssueCreatedCard) does not
	// duplicate.
	if prev != "" && prev != LifecycleStateUnknown {
		b.postIssueLifecycleCardLocked(task, prev, newState, "system")
	}
	// Keep the persisted Decision Packet's lifecycleState in lockstep with
	// the task on EVERY typed-state write — direct applyLifecycleStateLocked
	// callers (reopen / reject / archive in MutateTask) used to skip the
	// packet flush that transitionLifecycleLocked performed, leaving the
	// task-detail read (which serves the packet) stale against the board
	// (which serves the task). No-op when the task has no packet.
	b.onLifecycleTransitionLocked(task.ID, prev, newState)
	return nil
}

// transitionLifecycleLocked is the package-private chokepoint for all
// LifecycleState writes. Callers must already hold b.mu. The reason
// argument is currently informational (logged on unknown→canonical
// transitions and reserved for the manifest event payload Lane B / C
// will wire up); it is intentionally not silently swallowed.
//
// Returns an error when newState is not canonical so callers must handle
// the bad-state case explicitly rather than silently corrupting the
// inbox index.
func (b *Broker) transitionLifecycleLocked(taskID string, newState LifecycleState, reason string) (*teamTask, error) {
	if b == nil {
		return nil, fmt.Errorf("transition lifecycle: nil broker")
	}
	if _, ok := derivedFieldsFor(newState); !ok {
		return nil, fmt.Errorf("transition lifecycle: %q is not a canonical state", newState)
	}
	for i := range b.tasks {
		if b.tasks[i].ID != taskID {
			continue
		}
		task := &b.tasks[i]
		prev := task.LifecycleState
		if err := b.applyLifecycleStateLocked(task, newState); err != nil {
			return nil, err
		}
		if prev == LifecycleStateUnknown && reason != "" {
			log.Printf("broker: lifecycle transition for task %q recovered from %s -> %s (reason=%q)",
				taskID, LifecycleStateUnknown, newState, reason)
		}
		// Lane C: the Decision Packet flush + debounced running-state
		// durability now ride applyLifecycleStateLocked itself, so EVERY
		// typed-state write path (including the legacy reindex and the
		// direct apply callers) keeps the packet in lockstep — no second
		// call here.
		// Lane D wire (#9): when a task enters review, auto-resolve
		// the reviewer set from the watching configuration and stamp
		// it onto the task. Skip when the task already carries a
		// manually-assigned reviewer list (caller may have invoked
		// AssignReviewers explicitly before transitioning) so we do
		// not stomp explicit human/owner overrides.
		if newState == LifecycleStateReview && prev != LifecycleStateReview {
			if len(task.Reviewers) == 0 {
				// Already under b.mu; pass nil so resolveReviewersLocked
				// falls back to the in-lock diff path. The unlocked
				// fast path is only available to the top-level
				// ResolveReviewers entry point (which can release the
				// lock before running git).
				slugs, resolveErr := b.resolveReviewersLocked(taskID, nil)
				if resolveErr != nil {
					log.Printf("broker: lifecycle transition %q -> review: resolve reviewers failed: %v", taskID, resolveErr)
				} else if len(slugs) > 0 {
					if assignErr := b.assignReviewersLocked(taskID, slugs); assignErr != nil {
						log.Printf("broker: lifecycle transition %q -> review: assign reviewers failed: %v", taskID, assignErr)
					}
				}
			}
		}
		return task, nil
	}
	return nil, fmt.Errorf("transition lifecycle: task %q not found", taskID)
}

// onLifecycleTransitionLocked is the Lane C persistence hook fired by
// the transition layer on every state change. Persists the current in-
// memory packet (if any) and arms / cancels the 5-second running-flush
// timer. Caller holds b.mu.
//
// We skip persistence entirely when no packet has been seeded for the
// task — Lane A tests that never go through the Decision Packet path
// must not start touching the filesystem just because a lifecycle
// transition fired. The prev state is currently informational; reserved
// for the manifest-event payload Lane G will wire up.
func (b *Broker) onLifecycleTransitionLocked(taskID string, prev, newState LifecycleState) {
	_ = prev
	if b == nil || b.decisionPackets == nil {
		return
	}
	state := b.decisionPackets
	state.mu.Lock()
	packet, ok := state.packets[taskID]
	state.mu.Unlock()
	if !ok || packet == nil {
		return
	}
	b.stampLifecycleStateLocked(packet)
	b.persistDecisionPacketLocked(taskID, *packet)
	switch newState {
	case LifecycleStateRunning:
		b.scheduleRunningFlushLocked(taskID)
	default:
		b.cancelRunningFlushLocked(taskID)
	}
}

// TransitionLifecycle is the public entry point that acquires b.mu before
// delegating to transitionLifecycleLocked. Lane B / C / D event handlers
// call this once they have a verified taskID and target state.
func (b *Broker) TransitionLifecycle(taskID string, newState LifecycleState, reason string) error {
	if b == nil {
		return fmt.Errorf("transition lifecycle: nil broker")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	_, err := b.transitionLifecycleLocked(taskID, newState, reason)
	return err
}

// OnDecisionRecorded is the registered handler for the future
// decision.recorded manifest event (emitted by Lane C). The handler
// extends unblockDependentsLocked over the union of DependsOn and
// BlockedOn so tasks waiting on a blocker transition into review the
// instant the blocking decision lands. Acquires b.mu and persists; the
// auto-notebook publish runs after persistence to mirror the existing
// cascade pattern.
func (b *Broker) OnDecisionRecorded(completedTaskID string) {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	mutationSnapshot := snapshotBrokerTaskMutationLocked(b)
	pending := b.unblockDependentsLocked(strings.TrimSpace(completedTaskID))
	if err := b.saveLocked(); err != nil {
		mutationSnapshot.restore(b)
		log.Printf("broker: OnDecisionRecorded saveLocked failed for %q: %v", completedTaskID, err)
		return
	}
	b.flushPendingAutoNotebookTransitionsLocked(pending, "system")
}
