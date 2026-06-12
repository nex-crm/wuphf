package team

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
)

type TaskMutationErrorKind string

const (
	TaskMutationInvalid        TaskMutationErrorKind = "invalid"
	TaskMutationForbidden      TaskMutationErrorKind = "forbidden"
	TaskMutationNotFound       TaskMutationErrorKind = "not_found"
	TaskMutationConflict       TaskMutationErrorKind = "conflict"
	TaskMutationWorktreeFailed TaskMutationErrorKind = "worktree_failed"
	TaskMutationPersistFailed  TaskMutationErrorKind = "persist_failed"
	// TaskMutationVerificationFailed marks a complete/approve blocked by a
	// failing definition-of-done check (task_verification.go, U1.1).
	TaskMutationVerificationFailed TaskMutationErrorKind = "verification_failed"
	// TaskMutationArtifactRequired marks a mutation that would land a task
	// with a Definition in done without a delivered artifact on record
	// (core-loop B1, task_completion_hook.go). Pass artifact_path on the
	// completing call to clear it.
	TaskMutationArtifactRequired TaskMutationErrorKind = "artifact_required"
)

type TaskMutationError struct {
	Kind    TaskMutationErrorKind
	Message string
	Cause   error
}

func (e *TaskMutationError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

func (e *TaskMutationError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

func taskMutationError(kind TaskMutationErrorKind, message string, cause error) error {
	return &TaskMutationError{Kind: kind, Message: message, Cause: cause}
}

// ErrHumanObjectionOpen marks an approve blocked by an open human
// request-changes objection on the decision-endpoint path
// (recordTaskDecisionInternal). The HTTP handler maps it to 409.
var ErrHumanObjectionOpen = errors.New("human objection open")

// taskObjectionActor normalizes the attribution slug stored on a
// TaskReviewObjection. An empty actor is the local operator (the same
// convention checkTaskActionAuthLocked uses), attributed as "human".
func taskObjectionActor(actor string) string {
	actor = strings.TrimSpace(actor)
	if actor == "" {
		return "human"
	}
	return actor
}

// isInternalTaskActor reports whether the actor slug is one of the broker's
// internal recovery identities (auto-resolve safety net, self-heal, intake
// driver, migrations). These are exempt from the pre-start gates: they own
// legacy fold-in paths that must be able to park state. The empty slug is
// the local operator convention (same as checkTaskActionAuthLocked).
func isInternalTaskActor(actor string) bool {
	switch strings.ToLower(strings.TrimSpace(actor)) {
	case "", "system", "broker", "nex":
		return true
	}
	return false
}

// humanObjectionOpenMessage names the open objection in the forbidden
// error so the blocked agent knows exactly whose "no" stands and how to
// proceed (revise + resubmit, then wait for the human).
func humanObjectionOpenMessage(taskID, action string, obj *TaskReviewObjection) string {
	excerpt := strings.TrimSpace(obj.Body)
	if len(excerpt) > 280 {
		excerpt = excerpt[:277] + "..."
	}
	msg := fmt.Sprintf("cannot %s %s: an open human objection stands — @%s requested changes at %s", action, taskID, obj.Actor, obj.At)
	if excerpt != "" {
		msg += fmt.Sprintf(": %q", excerpt)
	}
	msg += ". Only the human can approve or complete this task while their objection is open. Address the feedback, resubmit with team_task action=submit_for_review, and wait for the human's decision."
	return msg
}

// humanNoteHaltClipChars bounds the note body stored on the task (and
// therefore rendered at the top of the next packet).
const humanNoteHaltClipChars = 2000

// humanNoteLeadsWithHalt reports whether a human message opens with a stop
// token ("stop", "wait", "hold" — covers "hold on") as its leading word.
func humanNoteLeadsWithHalt(content string) bool {
	fields := strings.FieldsFunc(strings.ToLower(strings.TrimSpace(content)), func(r rune) bool {
		return !(r >= 'a' && r <= 'z')
	})
	if len(fields) == 0 {
		return false
	}
	switch fields[0] {
	case "stop", "wait", "hold":
		return true
	}
	return false
}

// taskFollowUpActionKind is the action kind appended when a human posts
// into a DELIVERED task's channel. notifyTaskActionsLoop forwards it past
// the done-skip so the owner is re-engaged through the same wake path
// reopen uses (B1) — the structural fix for the post-done dead zone
// (ICP-eval v2 [01:48]/[01:58]: "make the tagline punchier" on a delivered
// task died in a 22-minute void).
const taskFollowUpActionKind = "task_followup"

// taskInTerminalDoneState reports whether the task sits in a delivered
// terminal state (done/approved) — the states where a later human post is a
// follow-up on shipped work rather than mid-flight steering. Archived tasks
// are excluded: the legacy channel fold-in parks orphaned chat under
// archived owner tasks that must never wake on lobby traffic.
func taskInTerminalDoneState(task *teamTask) bool {
	if task == nil || task.LifecycleState == LifecycleStateArchived {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(task.status), "done") ||
		task.LifecycleState == LifecycleStateApproved
}

// taskAwaitsHumanFollowUpWake reports whether a task sits in a waiting
// state where a human post in its channel must WAKE the owner (the
// task_followup path) because no naturally scheduled agent turn will ever
// carry the note: review, decision, changes_requested, blocked, and the
// terminal done/approved states. Pre-start states (drafting/intake/ready/
// queued) are excluded — they have the Approve & Start waiting-hint flow —
// and archived tasks never wake on channel traffic (legacy fold-ins).
func taskAwaitsHumanFollowUpWake(task *teamTask) bool {
	if task == nil {
		return false
	}
	switch task.LifecycleState {
	case LifecycleStateReview, LifecycleStateDecision,
		LifecycleStateChangesRequested, LifecycleStateBlockedOnPRMerge:
		return true
	}
	if taskInTerminalDoneState(task) {
		return true
	}
	// Legacy tasks without a typed state: fall back to the bare status
	// signals for the same waiting states.
	if task.LifecycleState == "" || task.LifecycleState == LifecycleStateUnknown {
		switch strings.ToLower(strings.TrimSpace(task.status)) {
		case "review", "blocked":
			return true
		}
	}
	return false
}

// markHumanNoteOnChannelTasksLocked stamps HumanNotePending on every
// non-system task in the message's channel that is either RUNNING or in a
// terminal-done state. Called from the message-post paths for HUMAN senders
// only.
//
// Running tasks: the live failure this closes is ICP-eval v2 [00:50] — a
// typed "Stop — do not build a placeholder" was never seen by the mid-turn
// agent and the fabricated one-pager shipped anyway. Per-task channels make
// this 1:1 in practice; #general's archived system task is excluded by the
// status guard.
//
// Non-running tasks (done-integrity + utterance-routing fix families): a
// human post into a task channel whose task sits in ANY waiting state —
// review, decision, changes_requested, blocked, or terminal done/approved —
// is steering with no natural next agent turn to ride. The note is stamped
// the same way AND a task_followup action is appended so the notify loop
// re-engages the OWNER (ICP-eval v3 [17:51→18:02]: redlines posted into a
// decision-state task channel got 14 minutes of dead air; v2's 22-minute
// post-done void was the same failure on done tasks). Restricted to
// non-#general channels: #general is the office lobby holding every legacy
// done task, and waking all their owners on any lobby post would be a
// broadcast storm; per-task channels are where the live dead zone occurred.
// Drafting/pre-start tasks keep the waiting-hint flow (Approve & Start) and
// archived tasks never wake on lobby traffic.
//
// Pure in-memory writes under the already-held lock — the caller's
// saveLocked persists them. Caller must hold b.mu.
func (b *Broker) markHumanNoteOnChannelTasksLocked(msg channelMessage) {
	if !isHumanMessageSender(msg.From) || strings.TrimSpace(msg.Content) == "" {
		return
	}
	channel := normalizeChannelSlug(msg.Channel)
	if channel == "" {
		channel = "general"
	}
	now := strings.TrimSpace(msg.Timestamp)
	if now == "" {
		now = time.Now().UTC().Format(time.RFC3339)
	}
	for i := range b.tasks {
		task := &b.tasks[i]
		if task.System {
			continue
		}
		if normalizeChannelSlug(task.Channel) != channel {
			continue
		}
		// "Running" means an execution turn can naturally carry the note:
		// the typed Running state, or a legacy task whose only signal is
		// status=in_progress. Review/Decision/ChangesRequested ALSO carry
		// the legacy in_progress status (lifecycleDerivedFields) but have
		// NO natural next turn — they must take the wake path below, so
		// the typed state wins over the legacy status here.
		running := task.LifecycleState == LifecycleStateRunning ||
			(task.LifecycleState == "" && strings.EqualFold(strings.TrimSpace(task.status), "in_progress"))
		followUp := !running && channel != "general" &&
			taskAwaitsHumanFollowUpWake(task) && strings.TrimSpace(task.Owner) != ""
		if !running && !followUp {
			continue
		}
		// Fresh struct every time (rollback safety; see TaskHumanNote).
		task.HumanNotePending = &TaskHumanNote{
			From: strings.TrimSpace(msg.From),
			Body: truncate(strings.TrimSpace(msg.Content), humanNoteHaltClipChars),
			At:   now,
			Halt: humanNoteLeadsWithHalt(msg.Content),
		}
		if followUp {
			summary := "human follow-up on delivered task: "
			if !taskInTerminalDoneState(task) {
				summary = "human posted in waiting task's channel: "
			}
			b.appendActionLocked(taskFollowUpActionKind, "office", channel, strings.TrimSpace(msg.From),
				truncateSummary(summary+strings.TrimSpace(msg.Content), 140), task.ID)
		}
	}
}

// ConsumeTaskHumanNote clears the pending human note on a task. Called by
// the packet builder when the owner's next packet has rendered the note —
// consumption is "the packet carried it", which also releases the halt gate
// on submit_for_review/complete.
func (b *Broker) ConsumeTaskHumanNote(taskID string) {
	taskID = strings.TrimSpace(taskID)
	if b == nil || taskID == "" {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	task := b.findTaskByIDLocked(taskID)
	if task == nil || task.HumanNotePending == nil {
		return
	}
	task.HumanNotePending = nil
	if err := b.saveLocked(); err != nil {
		log.Printf("task %s: persist human-note consumption: %v", taskID, err)
	}
}

// humanNoteHaltMessage names the unread stop order in the forbidden error
// so the blocked agent knows exactly why the transition is refused.
func humanNoteHaltMessage(taskID, action string, note *TaskHumanNote) string {
	excerpt := strings.TrimSpace(note.Body)
	if len(excerpt) > 280 {
		excerpt = excerpt[:277] + "..."
	}
	return fmt.Sprintf(
		"cannot %s %s: the human posted a stop order in this task's channel at %s that you have not yet processed: %q. Read it, address it, and wait for your next work packet (which carries the note) before retrying.",
		action, taskID, note.At, excerpt,
	)
}

// checkTaskActionAuthLocked enforces the hybrid CEO-managed Issues
// model. Returns nil when the actor may perform the action on the
// (optional) target task, or a TaskMutationForbidden error with a
// human-readable steer when not.
//
// Actor classes:
//   - humanActors: "human", "you", "" (system) — the operator. Always allowed.
//   - "system" / "broker": internal recovery (auto-resolve safety net,
//     self-heal, intake driver) — always allowed.
//   - leadActor: whoever holds the lead/CEO slug. Always allowed.
//   - taskOwner: the current Owner of the target task. Allowed for
//     status-transition actions on their own task.
//   - everyone else: specialist — only `comment` is open.
//
// Caller holds b.mu.
func (b *Broker) checkTaskActionAuthLocked(action, actor, targetTaskID string) error {
	a := strings.ToLower(strings.TrimSpace(action))
	actorSlug := strings.ToLower(strings.TrimSpace(actor))

	// Comment is open to all — every agent should be able to leave a
	// note on any Issue they can see.
	if a == "comment" {
		return nil
	}

	// Human + internal recovery actors are unrestricted.
	switch actorSlug {
	case "", "human", "you", "system", "broker", "nex":
		return nil
	}

	leadSlug := strings.ToLower(strings.TrimSpace(officeLeadSlugFrom(b.members)))
	// Pre-onboarding / test fixtures have no members → no lead. In
	// that state the office isn't managed yet, so don't block — the
	// gate only kicks in once a CEO/lead is in place.
	if leadSlug == "" {
		return nil
	}
	if actorSlug == leadSlug {
		return nil
	}
	// The gate ONLY blocks slugs that are registered as specialist
	// agents in this office. Unregistered actors (test slugs, CLI
	// scripts, external callers that pass an arbitrary created_by)
	// fall through — we have no basis to treat them as a specialist
	// being managed by CEO. This keeps tests + ad-hoc tooling working
	// while still blocking actual specialist agents from scope-editing
	// Issues that should go through CEO.
	if b.findMemberLocked(actorSlug) == nil {
		return nil
	}

	// Owner-allowed actions: the task's current owner can move their
	// own work through status transitions without going through CEO.
	// Requires a target task id to check ownership.
	ownerAllowed := map[string]bool{
		"submit_for_review": true,
		"review":            true,
		"complete":          true,
		"resume":            true,
		"release":           true,
		"claim":             true,
		"assign":            true,
		// block is the owner saying "I can't move because of <reason>"
		// (sets blocked=true). Allowed for owner so they can pause
		// their own work without going through CEO.
		"block": true,
		// cancel kills a task. Owner can cancel their own work; CEO
		// can cancel anything via the lead path above. Specialists
		// can't cancel work they don't own.
		"cancel": true,
		// reopen lets the owner pick their own delivered task back up —
		// the post-done follow-up packet ("FOLLOW-UP ON DELIVERED TASK")
		// instructs the owner to reopen when the human's post is a
		// revision request, and an owner-scoped reopen on their own work
		// is a status transition, not a scope edit. Reopening someone
		// ELSE's task stays CEO/human-only.
		"reopen": true,
	}
	// Reviewer-allowed actions: an agent assigned as a reviewer on the
	// task can bounce work back with request_changes and (in PR-loop
	// usage) approve/reject the submission. These are not "scope" edits
	// — they're the reviewer fulfilling their assigned role.
	reviewerAllowed := map[string]bool{
		"request_changes": true,
		"approve":         true,
		"reject":          true,
	}
	if targetTaskID != "" {
		if task := b.findTaskByIDLocked(strings.TrimSpace(targetTaskID)); task != nil {
			if ownerAllowed[a] && strings.EqualFold(strings.TrimSpace(task.Owner), actorSlug) {
				return nil
			}
			if reviewerAllowed[a] {
				for _, r := range task.Reviewers {
					if strings.EqualFold(strings.TrimSpace(r), actorSlug) {
						return nil
					}
				}
			}
		}
	}

	// Everything else is CEO-only for specialists. Steer them at the
	// suggestion channel so they know how to escalate scope ideas
	// without being silently blocked.
	return taskMutationError(
		TaskMutationForbidden,
		fmt.Sprintf(
			"only @%s (or the human) can %s an Issue. To propose a change, post team_task action=comment with a [SUGGESTION] prefix on the parent Issue and @-mention %s.",
			leadSlug, a, leadSlug,
		),
		nil,
	)
}

// defaultTaskTypeForCreate is the broker safety net for RULE ZERO. When an
// agent creates a top-level task via team_task action=create, the Tasks
// board only renders rows with task_type="issue" (see web TasksList
// isTaskSpecTask). Pre-fix the team_task tool schema listed example
// values "research, feature, launch, follow_up, bugfix, incident" without
// mentioning "issue", so LLMs picked one of those for human-asked work —
// the task landed in broker state but never reached the user-visible
// Tasks board, defeating RULE ZERO. The prompt was updated to instruct
// task_type="issue", and the schema description rewritten, but we also
// override here so a regressed prompt cannot silently break the surface.
//
// Override scope: empty input and the bare "follow_up" default (the value
// LLMs reach for when the schema example lists it first) become "issue".
// Real pipeline values picked deliberately (feature / research / launch /
// bugfix / incident / custom) pass through — sub-tasks INSIDE an Issue
// are allowed to carry those typed values per the canonical workflow,
// and tests asserting pipeline-specific behaviour rely on explicit types.
// parent_issue_id now ships; callers that set it pass an explicit task_type,
// so the "follow_up" override rarely fires for sub-tasks in practice. The
// override stays as a safety net for bare top-level creates.
func defaultTaskTypeForCreate(in string) string {
	s := strings.ToLower(strings.TrimSpace(in))
	if s == "" {
		return "issue"
	}
	switch s {
	case "follow_up", "follow-up", "followup":
		return "issue"
	}
	return strings.TrimSpace(in)
}

func writeTaskMutationHTTPError(w http.ResponseWriter, err error) {
	var mutationErr *TaskMutationError
	if !errors.As(err, &mutationErr) {
		log.Printf("task mutation: unexpected error: %v", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	status := http.StatusInternalServerError
	switch mutationErr.Kind {
	case TaskMutationInvalid:
		status = http.StatusBadRequest
	case TaskMutationForbidden:
		status = http.StatusForbidden
	case TaskMutationNotFound:
		status = http.StatusNotFound
	case TaskMutationConflict, TaskMutationArtifactRequired, TaskMutationVerificationFailed:
		status = http.StatusConflict
	case TaskMutationWorktreeFailed, TaskMutationPersistFailed:
		status = http.StatusInternalServerError
	}
	http.Error(w, mutationErr.Message, status)
}

func trimTaskDependencies(deps []string) []string {
	if len(deps) == 0 {
		return nil
	}
	trimmedDeps := make([]string, 0, len(deps))
	for _, dep := range deps {
		if trimmed := strings.TrimSpace(dep); trimmed != "" {
			trimmedDeps = append(trimmedDeps, trimmed)
		}
	}
	return trimmedDeps
}

func markTaskDone(task *teamTask, timestamp string) {
	if task == nil {
		return
	}
	if !strings.EqualFold(strings.TrimSpace(task.status), "done") && strings.TrimSpace(task.CompletedAt) == "" {
		task.CompletedAt = timestamp
	}
	task.status = "done"
}

func reconcileTaskReviewState(task *teamTask, action string) {
	if task == nil {
		return
	}
	// PR-style review-loop actions and terminal actions write reviewState
	// directly via applyLifecycleStateLocked; the reconciler must not
	// overwrite their authoritative value with a status-derived guess.
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "request_changes", "submit_for_review", "comment", "reject", "archive", "define", "reopen":
		// define is a metadata-only mutation (R4 intake contract); it must
		// not nudge reviewState off whatever the lifecycle layer set.
		// reopen writes the full Drafting/Running tuple via
		// applyLifecycleStateLocked; reconciling it back to not_required
		// breaks the inverse migration map (Drafting → Ready drift).
		return
	}
	if !taskNeedsStructuredReview(task) {
		// For new create actions, leave task.reviewState empty so downstream logic
		// can detect an uninitialized state; other actions or pre-set values
		// normalize to not_required when taskNeedsStructuredReview is false.
		if strings.TrimSpace(task.reviewState) != "" || !strings.EqualFold(strings.TrimSpace(action), "create") {
			task.reviewState = "not_required"
		}
		return
	}

	switch strings.ToLower(strings.TrimSpace(task.status)) {
	case "review":
		task.reviewState = "ready_for_review"
	case "done":
		switch {
		case strings.EqualFold(strings.TrimSpace(action), "approve"),
			strings.EqualFold(strings.TrimSpace(action), "complete"),
			strings.EqualFold(strings.TrimSpace(task.reviewState), "approved"):
			task.reviewState = "approved"
		default:
			task.reviewState = "ready_for_review"
		}
	default:
		switch strings.TrimSpace(task.reviewState) {
		case "pending_review", "ready_for_review", "approved":
		default:
			task.reviewState = "pending_review"
		}
	}
}

func (b *Broker) MutateTask(body TaskPostRequest) (TaskResponse, error) {
	action := strings.TrimSpace(body.Action)
	actor := strings.TrimSpace(body.CreatedBy)
	// Live FE payload shim (v3 fix family #2, [17:47→17:50]): the task
	// toolbar's reason-bearing verbs send the typed text as override_reason
	// (web/src/api/tasks.ts updateTaskStatus), NOT as details. Every
	// feedback consumer below reads body.Details, so the human's
	// "What needs to change?" text was dropped on the live path three runs
	// in a row ("No written feedback came through"). Fold the reason into
	// Details for the verbs whose semantics are "feedback text", so the
	// objection stamp, the wake notification, and the packet all carry it.
	if strings.TrimSpace(body.Details) == "" {
		switch action {
		case "request_changes", "reject", "block", "cancel":
			if reason := strings.TrimSpace(body.OverrideReason); reason != "" {
				body.Details = reason
			}
		}
	}
	now := time.Now().UTC().Format(time.RFC3339)
	channel := normalizeChannelSlug(body.Channel)
	if channel == "" {
		channel = "general"
	}

	// Permission preflight must run before any gate with external side
	// effects. The locked auth check below still runs again after these
	// pre-phases, so a task whose owner/reviewer changes while a verification
	// command runs is rechecked before mutation.
	b.mu.Lock()
	if err := b.checkTaskActionAuthLocked(action, actor, body.ID); err != nil {
		b.mu.Unlock()
		return TaskResponse{}, err
	}
	b.mu.Unlock()

	// Resubmission artifact-delta gate (done-integrity): an agent re-landing
	// changes-requested work must have actually changed the delivered
	// artifact. Runs BEFORE the lock below because it reads artifact files
	// (lock discipline in task_verification.go), and before the verification
	// gate so a blocked resubmission never pays for a command check.
	if err := b.gateTaskResubmissionArtifactDelta(body); err != nil {
		return TaskResponse{}, err
	}

	// U1.1 verification gate: a complete/approve on a task with a required
	// definition-of-done check must pass that check first. Runs BEFORE the
	// lock below because checks execute external commands (lock discipline
	// in task_verification.go).
	if err := b.gateTaskCompletionVerification(body); err != nil {
		return TaskResponse{}, err
	}

	// Artifact-hash capture for request_changes (done-integrity): hash the
	// delivered artifact NOW, outside the lock, so the objection stamped
	// below can carry it. Empty when the task has no artifact or the file
	// is unreadable — the resubmission gate then degrades to an audit stamp.
	var requestChangesArtifact, requestChangesArtifactHash string
	if action == "request_changes" {
		requestChangesArtifact, requestChangesArtifactHash = b.computeTaskArtifactHash(body.ID)
	}

	// B5 done-artifact existence pre-phase: stat the artifact this mutation
	// would land done with OUTSIDE the lock (file I/O discipline), so the
	// reachedDone gate below can reject phantom paths without holding b.mu
	// across an os.Stat. The locked gate compares the checked reference
	// against the task's artifact at gate time to stay race-safe.
	var doneArtifactRef string
	doneArtifactExists := true
	switch action {
	case "complete", "approve":
		doneArtifactRef, doneArtifactExists = b.peekTaskDoneArtifact(body)
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	// CEO-managed Issues gate (hybrid model, Slice 7):
	//   - scope-shaping actions (create / reassign / approve / reject /
	//     reopen / block / cancel) are restricted to CEO + human. They
	//     change WHAT the Issue is or WHO owns it.
	//   - owner status-transition actions (submit_for_review / complete
	//     / request_changes / resume / release / claim / assign) are
	//     allowed for CEO + human OR the task's current owner. They
	//     report WHERE the owner's own work is.
	//   - comment is always open — every agent can leave a note.
	//
	// Specialists who try to scope-edit get a clear error pointing them
	// at the suggestion channel (team_task action=comment with
	// [SUGGESTION] prefix). The auto-resolve / broker-internal create
	// path passes actor="system" or the broker's own slug which we
	// allow-list below so safety-net Issue creation keeps working.
	if err := b.checkTaskActionAuthLocked(action, actor, body.ID); err != nil {
		return TaskResponse{}, err
	}

	if action == "create" {
		if b.findChannelLocked(channel) == nil {
			return TaskResponse{}, taskMutationError(TaskMutationNotFound, "channel not found", nil)
		}
		if strings.TrimSpace(body.Title) == "" || actor == "" {
			return TaskResponse{}, taskMutationError(TaskMutationInvalid, "title and created_by required", nil)
		}
		if !b.canAccessChannelLocked(actor, channel) {
			return TaskResponse{}, taskMutationError(TaskMutationForbidden, "channel access denied", nil)
		}

		mutationSnapshot := snapshotBrokerTaskMutationLocked(b)
		rollbackTask := func() {
			mutationSnapshot.restore(b)
		}
		if existing := b.findReusableTaskLocked(taskReuseMatch{
			Channel:          channel,
			Title:            strings.TrimSpace(body.Title),
			ThreadID:         strings.TrimSpace(body.ThreadID),
			Owner:            strings.TrimSpace(body.Owner),
			PipelineID:       strings.TrimSpace(body.PipelineID),
			SourceSignalID:   strings.TrimSpace(body.SourceSignalID),
			SourceDecisionID: strings.TrimSpace(body.SourceDecisionID),
		}); existing != nil {
			beforeStatus := existing.status
			if details := strings.TrimSpace(body.Details); details != "" {
				existing.Details = details
			}
			if owner := strings.TrimSpace(body.Owner); owner != "" {
				existing.Owner = owner
				existing.status = "in_progress"
			}
			if taskType := strings.TrimSpace(body.TaskType); taskType != "" {
				existing.TaskType = defaultTaskTypeForCreate(taskType)
			}
			if pipelineID := strings.TrimSpace(body.PipelineID); pipelineID != "" {
				existing.PipelineID = pipelineID
			}
			if executionMode := strings.TrimSpace(body.ExecutionMode); executionMode != "" {
				existing.ExecutionMode = executionMode
			}
			if reviewState := strings.TrimSpace(body.ReviewState); reviewState != "" {
				existing.reviewState = reviewState
			}
			if sourceSignalID := strings.TrimSpace(body.SourceSignalID); sourceSignalID != "" {
				existing.SourceSignalID = sourceSignalID
			}
			if sourceDecisionID := strings.TrimSpace(body.SourceDecisionID); sourceDecisionID != "" {
				existing.SourceDecisionID = sourceDecisionID
			}
			if worktreePath := strings.TrimSpace(body.WorktreePath); worktreePath != "" {
				existing.WorktreePath = worktreePath
			}
			if worktreeBranch := strings.TrimSpace(body.WorktreeBranch); worktreeBranch != "" {
				existing.WorktreeBranch = worktreeBranch
			}
			if existing.ThreadID == "" && strings.TrimSpace(body.ThreadID) != "" {
				existing.ThreadID = strings.TrimSpace(body.ThreadID)
			}
			reconcileTaskReviewState(existing, action)
			b.reindexTaskLifecycleFromLegacyLocked(existing)
			syncTaskMemoryWorkflow(existing, now)
			b.ensureTaskOwnerChannelMembershipLocked(channel, existing.Owner)
			existing.UpdatedAt = now
			if err := rejectTheaterTaskForLiveBusiness(existing); err != nil {
				rollbackTask()
				return TaskResponse{}, taskMutationError(TaskMutationConflict, err.Error(), err)
			}
			b.scheduleTaskLifecycleLocked(existing)
			if err := b.syncTaskWorktreeLocked(existing); err != nil {
				rollbackTask()
				return TaskResponse{}, taskMutationError(TaskMutationWorktreeFailed, "failed to manage task worktree", err)
			}
			b.appendActionLocked("task_updated", "office", channel, actor, truncateSummary(existing.Title+" ["+existing.status+"]", 140), existing.ID)
			if err := b.saveLocked(); err != nil {
				rollbackTask()
				return TaskResponse{}, taskMutationError(TaskMutationPersistFailed, "failed to persist broker state", err)
			}
			b.emitTaskTransitionAutoNotebook(existing, beforeStatus, actor)
			return TaskResponse{Task: *existing}, nil
		}
		// Allocate the task ID before choosing the channel so we can
		// name the per-task channel "task-<id>" deterministically.
		b.counter++
		taskID := b.allocateIssueIDLocked()
		// For new business-objective tasks that defaulted to "general",
		// mint a dedicated task-<id> channel so each goal runs in
		// isolation.  Non-business / system / sub-issue / incident tasks
		// stay in "general" (shouldMintPerTaskChannel guards all that).
		if shouldMintPerTaskChannel(channel, &teamTask{
			Title:         strings.TrimSpace(body.Title),
			Details:       strings.TrimSpace(body.Details),
			Owner:         strings.TrimSpace(body.Owner),
			TaskType:      defaultTaskTypeForCreate(body.TaskType),
			PipelineID:    strings.TrimSpace(body.PipelineID),
			ExecutionMode: strings.TrimSpace(body.ExecutionMode),
			ParentIssueID: strings.TrimSpace(body.ParentIssueID),
		}) {
			if ch := b.createPerTaskChannelLocked(taskID, strings.TrimSpace(body.Title), strings.TrimSpace(body.Owner), actor); ch != nil {
				channel = ch.Slug
			}
		}
		verification, verr := normalizeTaskVerification(body.VerificationKind, body.VerificationSpec, body.VerificationRequired)
		if verr != nil {
			rollbackTask()
			return TaskResponse{}, taskMutationError(TaskMutationInvalid, verr.Error(), nil)
		}
		// DoD→verification at intake (done-integrity, task_dod_derive.go):
		// when the creating text states an explicit machine-checkable
		// definition of done and no verification was passed, encode it now —
		// the human's check gates done from the first turn.
		dodDerived := false
		if verification == nil {
			if derived := deriveTaskVerificationFromDetails(body.Details); derived != nil {
				verification = derived
				dodDerived = true
			}
		}
		task := teamTask{
			ID:               taskID,
			Channel:          channel,
			Title:            strings.TrimSpace(body.Title),
			Details:          strings.TrimSpace(body.Details),
			Owner:            strings.TrimSpace(body.Owner),
			status:           "open",
			CreatedBy:        actor,
			ThreadID:         strings.TrimSpace(body.ThreadID),
			TaskType:         defaultTaskTypeForCreate(body.TaskType),
			PipelineID:       strings.TrimSpace(body.PipelineID),
			ExecutionMode:    strings.TrimSpace(body.ExecutionMode),
			reviewState:      strings.TrimSpace(body.ReviewState),
			SourceSignalID:   strings.TrimSpace(body.SourceSignalID),
			SourceDecisionID: strings.TrimSpace(body.SourceDecisionID),
			WorktreePath:     strings.TrimSpace(body.WorktreePath),
			WorktreeBranch:   strings.TrimSpace(body.WorktreeBranch),
			DependsOn:        trimTaskDependencies(body.DependsOn),
			ParentIssueID:    strings.TrimSpace(body.ParentIssueID),
			Verification:     verification,
			CreatedAt:        now,
			UpdatedAt:        now,
		}
		// Force task_type=issue when parent_issue_id is set so every
		// sub-issue is renderable on the Issue detail surface (same
		// lifecycle, same packet, same components). Agents may pass
		// task_type=research/feature on sub-issues; we override here
		// so the FE doesn't have to special-case those.
		if task.ParentIssueID != "" {
			task.TaskType = "issue"
			// Sub-issues nest one level deep only. If the proposed
			// parent is itself a sub-issue (has its own parent),
			// reject the create so we don't get sub-sub-sub-issue
			// cascades the UI can't render legibly. Agents can
			// always rescope a sub-issue under the top-level parent
			// instead. The check stays inside the locked section
			// since we're reading other tasks' state.
			if parent := b.findTaskByIDLocked(task.ParentIssueID); parent != nil {
				if strings.TrimSpace(parent.ParentIssueID) != "" {
					rollbackTask()
					return TaskResponse{}, taskMutationError(
						TaskMutationConflict,
						"sub-issues can only be one level deep; pick the top-level parent instead",
						nil,
					)
				}
			}
		}
		if len(task.DependsOn) > 0 && b.hasUnresolvedDepsLocked(&task) {
			task.blocked = true
		} else if task.Owner != "" {
			task.status = "in_progress"
		}
		reconcileTaskReviewState(&task, action)
		syncTaskMemoryWorkflow(&task, now)
		b.ensureTaskOwnerChannelMembershipLocked(channel, task.Owner)
		b.queueTaskBehindActiveOwnerLaneLocked(&task)
		if err := rejectTheaterTaskForLiveBusiness(&task); err != nil {
			rollbackTask()
			return TaskResponse{}, taskMutationError(TaskMutationConflict, err.Error(), err)
		}
		b.scheduleTaskLifecycleLocked(&task)
		if err := b.syncTaskWorktreeLocked(&task); err != nil {
			rollbackTask()
			return TaskResponse{}, taskMutationError(TaskMutationWorktreeFailed, "failed to manage task worktree", err)
		}
		b.reindexTaskLifecycleFromLegacyLocked(&task)
		b.tasks = append(b.tasks, task)
		// Issues start in `drafting` regardless of whether an owner is
		// set, so the human sees the new Issue in the Drafting column
		// with an Approve & Start button before the agent begins work.
		// Without this, owner-set Issues go straight to `in_progress`
		// / running and the agent ploughs ahead — defeating the
		// human-approval gate the user demanded for "any work getting
		// done has an Issue in place". applyLifecycleStateLocked sets
		// pipeline_stage=draft, review_state=pending_review, status=open,
		// blocked=false, and updates LifecycleState in-place. Errors
		// are ignored: the only failure mode is an unrecognized state,
		// and LifecycleStateDrafting is canonical.
		if strings.EqualFold(task.TaskType, "issue") {
			// Preserve the dependency-block flag across the drafting
			// transition. LifecycleStateDrafting's derived row has
			// blocked=false (drafting issues aren't in any dispatch
			// queue), but a freshly created issue with unresolved
			// deps was just marked blocked above (line ~281). Without
			// this restore, the block silently disappears when the
			// human clicks Approve & Start.
			depBlocked := b.tasks[len(b.tasks)-1].blocked
			// Blank the legacy-derived LifecycleState before applying
			// Drafting so the prev=="" guard in applyLifecycleStateLocked
			// suppresses the (otherwise) redundant issue_lifecycle chat
			// card on Issue creation — postIssueCreatedCardLocked below
			// is the one card we want for the create event.
			b.tasks[len(b.tasks)-1].LifecycleState = ""
			_ = b.applyLifecycleStateLocked(&b.tasks[len(b.tasks)-1], LifecycleStateDrafting)
			if depBlocked {
				b.tasks[len(b.tasks)-1].blocked = true
			}
			task = b.tasks[len(b.tasks)-1]
		}
		b.appendActionLocked("task_created", "office", channel, task.CreatedBy, truncateSummary(task.Title, 140), task.ID)
		if dodDerived {
			b.appendActionLocked("verification_derived", "office", channel, "system",
				truncateSummary("verification auto-derived from DoD: "+verification.Spec, 140), task.ID)
		}
		// Seed a Decision Packet for Issues so the /tasks/{id} read path
		// returns 200 immediately instead of 404 "decision packet not yet
		// available". Pre-fix: tasks created via team_task action=create
		// had no packet until Lane B's intake driver ran SetSpec, so the
		// Issue detail surface failed to load and the user saw "Could not
		// load issue" even though the row was on the board. The packet
		// starts empty; intake fills spec.goal / context / approach /
		// acceptance as CEO streams them. Only seed for task_type=issue
		// so internal types (skill_review_nudge, incident self-heal) keep
		// their legacy no-packet behaviour.
		if strings.EqualFold(task.TaskType, "issue") {
			packet := b.getOrInitPacketLocked(task.ID)
			if packet != nil {
				b.stampLifecycleStateLocked(packet)
				b.persistDecisionPacketLocked(task.ID, *packet)
			}
			// Post the issue card into the channel so the human (and
			// other agents) see the new Issue land in chat with a
			// one-click link to the detail view. Independent of any
			// chat reply the creating agent posts itself.
			b.postIssueCreatedCardLocked(actor, &task)
		}
		if err := b.saveLocked(); err != nil {
			rollbackTask()
			return TaskResponse{}, taskMutationError(TaskMutationPersistFailed, "failed to persist broker state", err)
		}
		// Treat creation as a transition from "" → task.status so the owner's
		// shelf records the moment a task lands in their lane.
		b.emitTaskTransitionAutoNotebook(&task, "", actor)
		return TaskResponse{Task: task}, nil
	}

	requestedID := strings.TrimSpace(body.ID)
	for i := range b.tasks {
		if b.tasks[i].ID != requestedID {
			continue
		}
		task := &b.tasks[i]
		mutationSnapshot := snapshotBrokerTaskMutationLocked(b)
		rollbackTask := func() {
			mutationSnapshot.restore(b)
		}
		taskChannel := normalizeChannelSlug(task.Channel)
		if taskChannel == "" {
			taskChannel = channel
		}
		if b.findChannelLocked(taskChannel) == nil {
			return TaskResponse{}, taskMutationError(TaskMutationNotFound, "channel not found", nil)
		}
		// Authorize against the task's actual channel, not caller-supplied body.Channel.
		if !b.canAccessChannelLocked(actor, taskChannel) {
			return TaskResponse{}, taskMutationError(TaskMutationForbidden, "channel access denied", nil)
		}
		appendDetails := false
		reassignPrevOwner := ""
		reassignTriggered := false
		cancelTriggered := false
		cancelPrevOwner := ""
		requestChangesTriggered := false
		rejectTriggered := false
		submitForReviewTriggered := false
		beforeStatus := task.status
		// Human-sovereignty gate (core-loop grader fix family #1): while a
		// human request-changes objection is open on this task, no agent —
		// including the CEO/lead and internal system actors — may land it.
		// Only a human actor can approve/complete, which also clears the
		// objection; a human request_changes below refreshes it. ICP-eval
		// v2 J2: the CEO approved its subordinate's blind revision one
		// message after the human's standing rejection.
		if action == "approve" || action == "complete" {
			if obj := task.HumanObjection; obj != nil {
				if !isHumanMessageSender(actor) {
					return TaskResponse{}, taskMutationError(
						TaskMutationForbidden,
						humanObjectionOpenMessage(task.ID, action, obj),
						nil,
					)
				}
				task.HumanObjection = nil
			}
			// Any actor that legitimately reaches approve/complete also
			// retires the latest request-changes stamp: the rework cycle
			// it described is over, so the next packet must not carry a
			// stale "CHANGES REQUESTED" banner. (Agent-reviewer verdicts
			// have no HumanObjection, so this is the only clear they get.)
			// Rollback safety: the pre-mutation snapshot restores both
			// pointers if a later gate in this mutation fails.
			task.ChangesRequested = nil
		}
		// Pre-start gate (ICP-eval v3 fix family #1, J3 [19:52–19:57]): a
		// Drafting task has not been started — the human's Approve & Start
		// is the only way into execution. Completion or review-submission
		// from Drafting is impossible for every non-internal actor: the v3
		// run had the CEO work and "complete" OFFICE-337 while the pill
		// still read drafting, with no human gate ever passed. Internal
		// recovery actors (system/broker/nex, empty) are exempt — they own
		// migration/fold-in paths that park legacy state.
		if action == "complete" || action == "submit_for_review" {
			if task.LifecycleState == LifecycleStateDrafting && !isInternalTaskActor(actor) {
				return TaskResponse{}, taskMutationError(
					TaskMutationConflict,
					fmt.Sprintf("task %s has not been started — it is in drafting, waiting on the human to press Approve & Start. Work cannot be %sd from a pre-start state.", task.ID, strings.ReplaceAll(action, "_", " ")),
					nil,
				)
			}
		}
		// Approve on a pre-start task means "start the work", never "accept
		// delivered work" — there is no work. A HUMAN approve activates the
		// task through the same Drafting→Running transition the Approve &
		// Start button uses; an agent approve is refused because the start
		// authorization belongs to the human (v3 J2 [19:04]: zero-work tasks
		// closed terminally at the click).
		if action == "approve" && task.LifecycleState == LifecycleStateDrafting {
			if !isHumanMessageSender(actor) && !isInternalTaskActor(actor) {
				return TaskResponse{}, taskMutationError(
					TaskMutationForbidden,
					fmt.Sprintf("task %s is in drafting — only the human can approve & start it. Wait for the human's Approve & Start.", task.ID),
					nil,
				)
			}
			if err := b.applyLifecycleStateLocked(task, LifecycleStateRunning); err != nil {
				return TaskResponse{}, taskMutationError(TaskMutationConflict, "could not start task", err)
			}
			task.UpdatedAt = now
			b.ensureTaskOwnerChannelMembershipLocked(taskChannel, task.Owner)
			b.queueTaskBehindActiveOwnerLaneLocked(task)
			b.scheduleTaskLifecycleLocked(task)
			if err := b.syncTaskWorktreeLocked(task); err != nil {
				rollbackTask()
				return TaskResponse{}, taskMutationError(TaskMutationWorktreeFailed, "failed to manage task worktree", err)
			}
			// Wake the owner through the same notify path the decision
			// endpoint's Drafting→Running activation uses.
			b.appendActionLocked("task_updated", "office", taskChannel, actor, truncateSummary(task.Title+" [approved]", 140), task.ID)
			if err := b.saveLocked(); err != nil {
				rollbackTask()
				return TaskResponse{}, taskMutationError(TaskMutationPersistFailed, "failed to persist broker state", err)
			}
			b.emitTaskTransitionAutoNotebook(task, beforeStatus, actor)
			return TaskResponse{Task: *task}, nil
		}
		// Stop-order backstop (anti-fabrication fix family #2, ICP-eval v2
		// [00:50]): a human message that led with stop/wait/hold in this
		// task's channel blocks submit_for_review and complete by agents
		// until a packet build has consumed the note — an agent cannot land
		// work past a stop order it never read. A human performing the
		// action clears the note (they know what they said). Non-halt notes
		// never block; they only ride the next packet's top.
		if action == "complete" || action == "submit_for_review" {
			if note := task.HumanNotePending; note != nil {
				if isHumanMessageSender(actor) {
					task.HumanNotePending = nil
				} else if note.Halt {
					return TaskResponse{}, taskMutationError(
						TaskMutationForbidden,
						humanNoteHaltMessage(task.ID, action, note),
						nil,
					)
				}
			}
		}
		switch action {
		case "claim", "assign":
			if strings.TrimSpace(body.Owner) == "" {
				return TaskResponse{}, taskMutationError(TaskMutationInvalid, "owner required", nil)
			}
			task.Owner = strings.TrimSpace(body.Owner)
			task.status = "in_progress"
			if taskNeedsStructuredReview(task) {
				task.reviewState = "pending_review"
			} else {
				task.reviewState = "not_required"
			}
		case "reassign":
			if strings.TrimSpace(body.Owner) == "" {
				return TaskResponse{}, taskMutationError(TaskMutationInvalid, "owner required", nil)
			}
			reassignPrevOwner = strings.TrimSpace(task.Owner)
			newOwner := strings.TrimSpace(body.Owner)
			task.Owner = newOwner
			status := strings.ToLower(strings.TrimSpace(task.status))
			if status != "done" && status != "review" {
				task.status = "in_progress"
			}
			if taskNeedsStructuredReview(task) && strings.TrimSpace(task.reviewState) == "" {
				task.reviewState = "pending_review"
			}
			reassignTriggered = reassignPrevOwner != newOwner
		case "complete":
			if strings.EqualFold(strings.TrimSpace(task.status), "done") {
				if taskNeedsStructuredReview(task) {
					task.reviewState = "approved"
				}
				task.blocked = false
			} else if strings.EqualFold(strings.TrimSpace(task.status), "review") ||
				strings.EqualFold(strings.TrimSpace(task.reviewState), "ready_for_review") {
				markTaskDone(task, now)
				if taskNeedsStructuredReview(task) {
					task.reviewState = "approved"
				}
				task.blocked = false
			} else if taskNeedsStructuredReview(task) {
				task.status = "review"
				task.reviewState = "ready_for_review"
			} else {
				markTaskDone(task, now)
				task.blocked = false
			}
		case "review":
			task.status = "review"
			task.reviewState = "ready_for_review"
		case "approve":
			markTaskDone(task, now)
			task.blocked = false
			if taskNeedsStructuredReview(task) {
				task.reviewState = "approved"
			}
		case "block":
			if err := rejectFalseLocalWorktreeBlock(task, body.Details); err != nil {
				return TaskResponse{}, taskMutationError(TaskMutationConflict, err.Error(), err)
			}
			task.status = "blocked"
			task.blocked = true
		case "resume":
			if task.blocked {
				task.blocked = false
			}
			if strings.EqualFold(strings.TrimSpace(task.status), "blocked") {
				if strings.TrimSpace(task.Owner) != "" {
					task.status = "in_progress"
				} else {
					task.status = "open"
				}
			}
			appendDetails = true
		case "reopen":
			// Reopen a closed (rejected/cancelled/approved) Issue. When the
			// task still has a real owner, reopen straight into Running so
			// the owner is RE-ENGAGED through the same wake path a fresh
			// assignment uses: the task_updated action appended below routes
			// through notifyTaskActionsLoop → deliverTaskNotification →
			// enqueueHeadlessCodexTurn, and sendTaskUpdate only dispatches
			// executable lifecycle states. Reopening into Drafting left
			// reopened tasks as conversational dead zones (core-loop B1 /
			// ICP-eval finding) — the human's reopen click IS the restart
			// authorization, and reopen is already CEO/human-scoped.
			// Ownerless (or auto-parked) tasks keep the Drafting landing so
			// the human can staff + approve as before.
			reopenTarget := LifecycleStateDrafting
			if owner := strings.TrimSpace(task.Owner); owner != "" && !isAutoOwner(owner) {
				reopenTarget = LifecycleStateRunning
			}
			task.CompletedAt = ""
			// Apply with the REAL previous state. The old pre-set
			// (task.LifecycleState = reopenTarget before apply) made
			// prev==new to suppress the lifecycle chat card, but it also
			// broke the inverse index: indexLifecycleLocked never removed
			// the task from its terminal (approved/rejected) bucket, so a
			// reopened task stayed listed as approved on every index-backed
			// surface while its page said running — one more board/page
			// state split (ICP-eval v3 fix family #1). The card a real
			// transition emits is honest signal: the human reopened work.
			if err := b.applyLifecycleStateLocked(task, reopenTarget); err != nil {
				return TaskResponse{}, taskMutationError(TaskMutationConflict, "could not reopen issue", err)
			}
			appendDetails = true
		case "release":
			task.Owner = ""
			task.status = "open"
			task.blocked = false
		case "cancel":
			cancelPrevOwner = strings.TrimSpace(task.Owner)
			task.status = "canceled"
			task.blocked = false
			task.FollowUpAt = ""
			task.ReminderAt = ""
			task.RecheckAt = ""
			cancelTriggered = true
		case "request_changes":
			// PR-like revision loop. Reviewer rejects the current
			// submission and bounces the task back to its existing
			// owner with feedback. Owner stays unchanged; status
			// resets so the owner picks up the rework.
			task.status = "in_progress"
			task.reviewState = "changes_requested"
			task.blocked = false
			appendDetails = true
			requestChangesTriggered = true
			// Stamp the feedback TEXT on the task itself so it renders in
			// the owner's next execution packet and wake notification —
			// the Decision Packet feedback log alone is invisible to the
			// reworking agent (ICP-eval v2 J2). A HUMAN reviewer's
			// request additionally arms (or refreshes) the sovereignty
			// gate above. Fresh struct each time: rollback safety.
			objection := &TaskReviewObjection{
				Actor: taskObjectionActor(actor),
				Body:  strings.TrimSpace(body.Details),
				At:    now,
			}
			// Pin the artifact's content hash (computed outside the lock
			// above) so the resubmission gate can require a real delta.
			// Only stamp when the artifact reference is still the one we
			// hashed — a concurrent mutation may have swapped it.
			if requestChangesArtifactHash != "" && strings.TrimSpace(task.Artifact) == requestChangesArtifact {
				objection.ArtifactHash = requestChangesArtifactHash
			}
			task.ChangesRequested = objection
			if isHumanMessageSender(actor) {
				task.HumanObjection = objection
			}
		case "submit_for_review":
			// Explicit "hand off to reviewer" action so executor
			// agents have a verb that matches PR-review intent
			// instead of overloading "complete". The Details field
			// (if present) carries the submitted artifact (code,
			// copy, plan) which we capture below as a FeedbackItem
			// so it shows up in the unified Inbox Discussion thread.
			// appendDetails preserves prior task details (planner
			// spec, earlier submission notes) instead of clobbering
			// them with each resubmit.
			task.status = "review"
			task.reviewState = "ready_for_review"
			appendDetails = true
			submitForReviewTriggered = true
		case "comment":
			// Append-only comment with no state change. Used by both
			// humans and agents to leave PR-style notes on a task
			// before anyone decides to approve / request changes /
			// reject. The actual append happens below via the
			// appendDetails branch.
			appendDetails = true
		case "define":
			// R4 intake contract: the CEO (or human) sets/updates the
			// structured Definition — goal, deliverables (+format),
			// success criteria, access needed — BEFORE the task is
			// staffed. Auth: define is not owner- or reviewer-allowed,
			// so checkTaskActionAuthLocked above already restricted it
			// to CEO + human (same class as the scope-shaping actions).
			// No status change: this is metadata the execution packet
			// renders as the contract the owner works against.
			def, derr := normalizeTaskDefinition(body.Definition, now)
			if derr != nil {
				return TaskResponse{}, taskMutationError(TaskMutationInvalid, derr.Error(), nil)
			}
			// Validate the optional verification BEFORE mutating the
			// task so an invalid spec cannot leave a half-applied define.
			var defVerification *TaskVerification
			if strings.TrimSpace(body.VerificationKind) != "" {
				v, verr := normalizeTaskVerification(body.VerificationKind, body.VerificationSpec, body.VerificationRequired)
				if verr != nil {
					return TaskResponse{}, taskMutationError(TaskMutationInvalid, verr.Error(), nil)
				}
				defVerification = v
			}
			task.Definition = def
			// Machine-checkable success criteria arrive WITH their check
			// in the same call. Only set when no check exists yet so a
			// re-define cannot silently replace an established gate. When
			// the CEO dropped a human-stated DoD anyway, the conservative
			// deriver (task_dod_derive.go) backstops it from the criteria
			// text and stamps the action log.
			if task.Verification == nil {
				if defVerification != nil {
					task.Verification = defVerification
				} else if derived := deriveTaskVerificationFromDefinition(def); derived != nil {
					task.Verification = derived
					b.appendActionLocked("verification_derived", "office", taskChannel, "system",
						truncateSummary("verification auto-derived from DoD: "+derived.Spec, 140), task.ID)
				}
			}
			appendDetails = true
		case "reject":
			// Terminal "this work cannot land" outcome. Distinct from
			// block (recoverable, waiting on upstream) and from
			// request_changes (revise + resubmit). LifecycleStateRejected
			// keeps Blocked=true so unblockDependentsLocked treats the
			// upstream as unresolved and downstream tasks STAY blocked.
			//
			// Reject must carry a reason — a terminal "this won't land"
			// without context is hostile to the agent that has to
			// pivot. Enforce that contract at the API boundary so the
			// "@human reviewer rejected without saying why" failure
			// mode can't happen.
			if strings.TrimSpace(body.Details) == "" {
				return TaskResponse{}, taskMutationError(TaskMutationInvalid, "reject reason required", nil)
			}
			if err := b.applyLifecycleStateLocked(task, LifecycleStateRejected); err != nil {
				return TaskResponse{}, taskMutationError(TaskMutationInvalid, err.Error(), err)
			}
			appendDetails = true
			rejectTriggered = true
		case "archive":
			// Move the task off the active board. Archived tasks are
			// terminal (excluded from default active listings, included
			// with include_done=true for the Archive board column). An
			// optional note is captured via appendDetails so the actor
			// can document why the work was archived. Unlike reject,
			// no reason is required — archiving is a housekeeping act,
			// not a quality judgement. The task can be reopened via
			// the reopen action which resets it to Drafting.
			if err := b.applyLifecycleStateLocked(task, LifecycleStateArchived); err != nil {
				return TaskResponse{}, taskMutationError(TaskMutationInvalid, err.Error(), err)
			}
			appendDetails = true
		default:
			return TaskResponse{}, taskMutationError(TaskMutationInvalid, "unknown action", nil)
		}
		if strings.TrimSpace(body.Details) != "" {
			if appendDetails {
				if err := appendTaskDetailLocked(task, body.Details); err != nil {
					rollbackTask()
					return TaskResponse{}, taskMutationError(TaskMutationInvalid, err.Error(), err)
				}
			} else {
				task.Details = strings.TrimSpace(body.Details)
			}
		}
		if taskType := strings.TrimSpace(body.TaskType); taskType != "" {
			task.TaskType = taskType
		}
		if pipelineID := strings.TrimSpace(body.PipelineID); pipelineID != "" {
			task.PipelineID = pipelineID
		}
		if executionMode := strings.TrimSpace(body.ExecutionMode); executionMode != "" {
			task.ExecutionMode = executionMode
		}
		if reviewState := strings.TrimSpace(body.ReviewState); reviewState != "" {
			task.reviewState = reviewState
		}
		if sourceSignalID := strings.TrimSpace(body.SourceSignalID); sourceSignalID != "" {
			task.SourceSignalID = sourceSignalID
		}
		if sourceDecisionID := strings.TrimSpace(body.SourceDecisionID); sourceDecisionID != "" {
			task.SourceDecisionID = sourceDecisionID
		}
		if worktreePath := strings.TrimSpace(body.WorktreePath); worktreePath != "" {
			task.WorktreePath = worktreePath
		}
		if worktreeBranch := strings.TrimSpace(body.WorktreeBranch); worktreeBranch != "" {
			task.WorktreeBranch = worktreeBranch
		}
		if artifactPath := strings.TrimSpace(body.ArtifactPath); artifactPath != "" {
			if err := validateTaskArtifactPath(artifactPath); err != nil {
				rollbackTask()
				return TaskResponse{}, taskMutationError(TaskMutationInvalid, err.Error(), err)
			}
			task.Artifact = artifactPath
		}
		if !strings.EqualFold(strings.TrimSpace(task.status), "done") {
			task.CompletedAt = ""
		}
		reconcileTaskReviewState(task, action)
		b.reindexTaskLifecycleFromLegacyLocked(task)
		syncTaskMemoryWorkflow(task, now)
		reachedDone := strings.EqualFold(strings.TrimSpace(task.status), "done") &&
			!strings.EqualFold(strings.TrimSpace(beforeStatus), "done")
		// Artifact gate (core-loop B1): a task with a Definition cannot land
		// in done without a delivered artifact on record. Tasks without a
		// Definition keep legacy behavior — additive rollout.
		if reachedDone && task.Definition != nil && strings.TrimSpace(task.Artifact) == "" {
			rollbackTask()
			return TaskResponse{}, taskMutationError(
				TaskMutationArtifactRequired,
				fmt.Sprintf(
					"task %s has a Definition, so it cannot reach done without a delivered artifact. Publish the deliverable to the wiki, then retry this %s with artifact_path set to the wiki-relative path (e.g. \"team/playbooks/launch.md\") or the visual-artifact id.",
					task.ID, action,
				),
				nil,
			)
		}
		// B5 knowledge-integrity: the artifact must EXIST, not merely be a
		// non-empty string — phantom paths are how the v3 run shipped
		// chat-only deliverables past the gate (V3-N10). Existence was
		// checked outside the lock in the pre-phase; binding here only when
		// the reference matches what was checked keeps the gate race-safe
		// against concurrent artifact rewrites.
		if reachedDone && task.Definition != nil &&
			strings.TrimSpace(task.Artifact) == doneArtifactRef && !doneArtifactExists {
			rollbackTask()
			return TaskResponse{}, taskMutationError(
				TaskMutationArtifactRequired,
				fmt.Sprintf(
					"task %s cannot reach done: artifact %q does not exist in the wiki (or the task worktree). Publish the deliverable first, then retry this %s with artifact_path pointing at the real file.",
					task.ID, doneArtifactRef, action,
				),
				nil,
			)
		}
		if strings.EqualFold(strings.TrimSpace(task.status), "done") {
			overrideActor := strings.TrimSpace(body.MemoryWorkflowOverrideActor)
			if overrideActor == "" {
				overrideActor = actor
			}
			overrideReason := strings.TrimSpace(body.MemoryWorkflowOverrideReason)
			if overrideReason == "" {
				overrideReason = strings.TrimSpace(body.OverrideReason)
			}
			if err := applyMemoryWorkflowCompletionGate(task, overrideActor, overrideReason, body.MemoryWorkflowOverride, now); err != nil {
				rollbackTask()
				return TaskResponse{}, taskMutationError(TaskMutationConflict, err.Error(), err)
			}
		}
		b.ensureTaskOwnerChannelMembershipLocked(taskChannel, task.Owner)
		b.queueTaskBehindActiveOwnerLaneLocked(task)
		task.UpdatedAt = now
		if err := rejectTheaterTaskForLiveBusiness(task); err != nil {
			rollbackTask()
			return TaskResponse{}, taskMutationError(TaskMutationConflict, err.Error(), err)
		}
		// Any terminal status releases waiting dependents. isTerminalTeamTaskStatus
		// matches hasUnresolvedDepsLocked so cancelled parents do not orphan dependents.
		var pendingCascade []pendingTaskTransition
		if isTerminalTeamTaskStatus(task.status) {
			pendingCascade = b.unblockDependentsLocked(task.ID)
		}
		b.scheduleTaskLifecycleLocked(task)
		if err := b.syncTaskWorktreeLocked(task); err != nil {
			rollbackTask()
			return TaskResponse{}, taskMutationError(TaskMutationWorktreeFailed, "failed to manage task worktree", err)
		}
		b.appendActionLocked("task_updated", "office", taskChannel, actor, truncateSummary(task.Title+" ["+task.status+"]", 140), task.ID)
		if action == "block" {
			b.requestCapabilitySelfHealingLocked(task, actor, body.Details)
		}
		if reassignTriggered {
			b.postTaskReassignNotificationsLocked(actor, task, reassignPrevOwner)
			// A draft that just gained its owner is now actionable — the
			// only way forward is the human's Approve & Start, so raise
			// the (deduped) "waiting on you" notice that creation skipped
			// while the task was ownerless (ICP eval N5).
			if task.LifecycleState == LifecycleStateDrafting {
				b.postTaskAwaitingStartNoticeLocked(task)
			}
		}
		if cancelTriggered {
			b.postTaskCancelNotificationsLocked(actor, task, cancelPrevOwner)
		}
		if requestChangesTriggered {
			feedback := strings.TrimSpace(body.Details)
			b.postTaskRequestChangesNotificationsLocked(actor, task, feedback)
			b.AppendPacketFeedbackLocked(task.ID, actor, feedback)
		}
		if action == "comment" {
			b.AppendPacketFeedbackLocked(task.ID, actor, strings.TrimSpace(body.Details))
		}
		if action == "define" {
			// E5 intake gate (ten-out-of-ten): a Definition that lands with
			// placeholder markers or access needs raises the batched human
			// interview deterministically — before any subtask dispatch can
			// write around the holes. Runs before saveLocked so the request
			// persists with the define mutation.
			b.raiseDefinitionGapInterviewLocked(task, actor)
		}
		if submitForReviewTriggered {
			// Capture the submitted artifact (code, copy, plan) into
			// the Decision Packet's feedback thread so reviewers see
			// the exact submission inline in the unified Inbox.
			artifact := strings.TrimSpace(body.Details)
			if artifact != "" {
				b.AppendPacketFeedbackLocked(task.ID, actor, "📤 Submitted for review:\n"+artifact)
			}
		}
		if rejectTriggered {
			feedback := strings.TrimSpace(body.Details)
			b.postTaskRejectedNotificationsLocked(actor, task, feedback)
			b.AppendPacketFeedbackLocked(task.ID, actor, feedback)
		}
		if reachedDone {
			// Self-heal parent attach (ten-out-of-ten A1): a repair lane that
			// lands done with a deliverable routes the artifact + completion
			// back onto the stalled PARENT through the legitimate path
			// (artifact recorded, parent into Review for the human decision).
			// Runs before the done-post so the announcement reflects the
			// final ownership. Same locked section — persisted together.
			b.attachSelfHealCompletionToParentLocked(task)
			// Deterministic done-post (core-loop B1): announce the delivery
			// in the task channel and raise a non-blocking Inbox notice.
			// Runs before saveLocked so the message + notice persist with
			// the completing mutation. No LLM, no I/O — string assembly only.
			b.postTaskDeliveredLocked(task)
		}
		if err := b.saveLocked(); err != nil {
			rollbackTask()
			return TaskResponse{}, taskMutationError(TaskMutationPersistFailed, "failed to persist broker state", err)
		}
		b.emitTaskTransitionAutoNotebook(task, beforeStatus, actor)
		b.flushPendingAutoNotebookTransitionsLocked(pendingCascade, "system")
		// U4.1 auto-distillation + B1 entity extraction: a task that just
		// reached done becomes a learning (when machine-verified) and its
		// entities/associations land in the team knowledge graph. Queued as
		// a goroutine so the learning-log + fact-log writes run after b.mu
		// releases (same hazard class that killed the old auto-notebook-writer).
		if reachedDone {
			b.queueTaskDistillation(task.ID)
		}
		return TaskResponse{Task: *task}, nil
	}

	return TaskResponse{}, taskMutationError(TaskMutationNotFound, "task not found", nil)
}
