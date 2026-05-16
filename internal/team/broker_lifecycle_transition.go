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
// LifecycleStateBlockedOnPRMerge the transition layer explicitly does NOT
// invoke requestCapabilitySelfHealingLocked. The blocked-on-PR-merge state
// is a typed legitimate condition, not an error needing self-heal. The
// unit test must observe the call site, not just the absence of side
// effects, so the gate is implemented as a hard branch in BlockTask itself
// (no scheduler is given a no-op closure).

import (
	"fmt"
	"log"
	"strings"
	"sync"
)

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

	LifecycleStateIntake            LifecycleState = "intake"
	LifecycleStateReady             LifecycleState = "ready"
	LifecycleStateRunning           LifecycleState = "running"
	LifecycleStateReview            LifecycleState = "review"
	LifecycleStateDecision          LifecycleState = "decision"
	LifecycleStateBlockedOnPRMerge  LifecycleState = "blocked_on_pr_merge"
	LifecycleStateQueuedBehindOwner LifecycleState = "queued_behind_owner"
	LifecycleStateChangesRequested  LifecycleState = "changes_requested"
	LifecycleStateApproved          LifecycleState = "approved"
)

// normalizeLegacyLifecycleStateName maps pre-Phase-1 lifecycle state
// string values to their post-Phase-1 canonical equivalents. The only
// rename in v1 is "merged" -> "approved" (per the artifact-and-approve
// vocabulary alignment from /plan-design-review + /plan-eng-review on
// 2026-05-11). Pass-through for every other input so this stays a
// targeted shim, not a general migration table.
//
// Called from teamTask.UnmarshalJSON so a pre-Phase-1
// broker-state.json loads cleanly without manual migration. The next
// save writes the canonical name; the shim has no second turn on disk.
func normalizeLegacyLifecycleStateName(s LifecycleState) LifecycleState {
	if strings.EqualFold(strings.TrimSpace(string(s)), "merged") {
		return LifecycleStateApproved
	}
	return s
}

// CanonicalLifecycleStates returns the valid lifecycle states (excluding
// the unknown migration fallback) in stable order. Used by tests sweeping
// the forward map.
func CanonicalLifecycleStates() []LifecycleState {
	return []LifecycleState{
		LifecycleStateIntake,
		LifecycleStateReady,
		LifecycleStateRunning,
		LifecycleStateReview,
		LifecycleStateDecision,
		LifecycleStateBlockedOnPRMerge,
		LifecycleStateQueuedBehindOwner,
		LifecycleStateChangesRequested,
		LifecycleStateApproved,
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
// Deviation from design doc: the doc-prescribed row for blocked_on_pr_merge
// is {pipelineStage:"review", reviewState:"ready_for_review",
// status:"in_progress", blocked:true}. Pre-Lane-A code paths in the broker
// (notifier_targets.go, headless_codex_queue.go, broker_requests_interviews.go,
// broker_tasks_plan.go, broker_defaults.go, broker_tasks_lifecycle.go,
// broker_tasks_mutation_service.go) all check status == "blocked" as the
// load-bearing signal that a task is paused. Flipping that contract in
// Lane A would either regress dozens of legacy code paths or require a
// matching Lane A sweep of every reader, which is out of scope here. We
// keep status="blocked" for blocked_on_pr_merge and let Lane F's CLI
// rewrite or v1.1 cleanup migrate the readers off the legacy contract
// once the rest of the harness is in place. The lifecycle index and the
// LifecycleState field still source-of-truth correctly.
var lifecycleDerivedFields = map[LifecycleState]lifecycleDerivedFieldsRow{
	LifecycleStateIntake:            {PipelineStage: "triage", ReviewState: "pending_review", Status: "open", Blocked: false},
	LifecycleStateReady:             {PipelineStage: "triage", ReviewState: "pending_review", Status: "open", Blocked: false},
	LifecycleStateRunning:           {PipelineStage: "implement", ReviewState: "pending_review", Status: "in_progress", Blocked: false},
	LifecycleStateReview:            {PipelineStage: "review", ReviewState: "ready_for_review", Status: "in_progress", Blocked: false},
	LifecycleStateDecision:          {PipelineStage: "review", ReviewState: "ready_for_review", Status: "in_progress", Blocked: false},
	LifecycleStateBlockedOnPRMerge:  {PipelineStage: "review", ReviewState: "ready_for_review", Status: "blocked", Blocked: true},
	LifecycleStateQueuedBehindOwner: {PipelineStage: "triage", ReviewState: "pending_review", Status: "open", Blocked: true},
	LifecycleStateChangesRequested:  {PipelineStage: "implement", ReviewState: "pending_review", Status: "in_progress", Blocked: false},
	LifecycleStateApproved:          {PipelineStage: "ship", ReviewState: "approved", Status: "done", Blocked: false},
}

// derivedFieldsFor returns the forward-map row for a state, plus a flag
// reporting whether the state is canonical. Unknown states return a
// zero-value row and ok=false so callers can decide whether to surface a
// warning.
func derivedFieldsFor(state LifecycleState) (lifecycleDerivedFieldsRow, bool) {
	row, ok := lifecycleDerivedFields[state]
	return row, ok
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
	{PipelineStage: "triage", ReviewState: "pending_review", Status: "open", Blocked: false}:           LifecycleStateReady,
	{PipelineStage: "implement", ReviewState: "pending_review", Status: "in_progress", Blocked: false}: LifecycleStateRunning,
	{PipelineStage: "review", ReviewState: "ready_for_review", Status: "in_progress", Blocked: false}:  LifecycleStateReview,
	{PipelineStage: "review", ReviewState: "ready_for_review", Status: "blocked", Blocked: true}:       LifecycleStateBlockedOnPRMerge,
	{PipelineStage: "triage", ReviewState: "pending_review", Status: "open", Blocked: true}:            LifecycleStateQueuedBehindOwner,
	{PipelineStage: "ship", ReviewState: "approved", Status: "done", Blocked: false}:                   LifecycleStateApproved,

	// Pre-Lane-A code wrote status="blocked" instead of relying on the
	// blocked bool. Map every reasonable variant to blocked_on_pr_merge so
	// real production data has a deterministic landing pad.
	{PipelineStage: "", ReviewState: "", Status: "blocked", Blocked: true}:                         LifecycleStateBlockedOnPRMerge,
	{PipelineStage: "", ReviewState: "", Status: "blocked", Blocked: false}:                        LifecycleStateBlockedOnPRMerge,
	{PipelineStage: "implement", ReviewState: "pending_review", Status: "blocked", Blocked: true}:  LifecycleStateBlockedOnPRMerge,
	{PipelineStage: "implement", ReviewState: "pending_review", Status: "blocked", Blocked: false}: LifecycleStateBlockedOnPRMerge,
	{PipelineStage: "review", ReviewState: "ready_for_review", Status: "blocked", Blocked: false}:  LifecycleStateBlockedOnPRMerge,

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
			log.Printf("broker: lifecycle migration: unknown tuple for task %q (pipeline_stage=%q review_state=%q status=%q blocked=%v) — falling back to %s",
				task.ID, task.pipelineStage, task.reviewState, task.status, task.blocked, LifecycleStateUnknown)
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
			derived = LifecycleStateBlockedOnPRMerge
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
	// This helper is used after legacy mutation paths have deliberately
	// written status/review fields that predate the LifecycleState table.
	// Preserve that legacy tuple as authoritative and only repair the
	// typed state plus index classification.
	prev := task.LifecycleState
	task.LifecycleState = derived
	b.indexLifecycleLocked(task.ID, prev, derived)
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
		// Lane C: every transition triggers a Decision Packet flush
		// when the broker has a packet for the task, plus debounced
		// running-state durability. The hook is a no-op when no
		// packet has been seeded for the task.
		b.onLifecycleTransitionLocked(taskID, prev, newState)
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
// BlockedOn so tasks waiting on a PR merge transition into review the
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
