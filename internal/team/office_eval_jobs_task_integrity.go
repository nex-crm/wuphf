package team

// office_eval_jobs_task_integrity.go — the `task-integrity` eval job
// (ten-out-of-ten Wave A). Five deterministic checks replicating the v3
// task-model failures at the broker mutation/dispatch layer:
//
//	(a) self-heal for a stalled parent creates a CHILD (not a sibling dup),
//	    deduped against open lanes covering the same work, and its
//	    completion attaches the artifact to the PARENT through the
//	    legitimate path (V3-N8: deliverables shipped from OFFICE-295 while
//	    the primaries sat empty).
//	(b) a pack-style auto-seeded task lands in Drafting and does NOT
//	    dispatch until the human activates it (V3-N9: pack lanes
//	    self-started despite "queued… whenever you want").
//	(c) a long-title task feeds NO clipped title into the agent-facing
//	    details/packet of its repair sub-task (v3 [17:41:35]: one agent
//	    pass consumed a truncated source and shipped a conflicting brief).
//	(d) a double terminal-transition attempt produces exactly ONE
//	    task_delivered post + ONE inbox notice (v3 6× done-messages), and
//	    byte-identical consecutive wiki writes to the same path fold into
//	    one commit (v3 triple-identical commits).
//	(e) decision/approval cards name the task owner from the TASK RECORD,
//	    not the packet's last actor (v3 [18:44:21]: approve card named the
//	    wrong agent).

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/nex-crm/wuphf/internal/agent"
	"github.com/nex-crm/wuphf/internal/operations"
)

func evalJobTaskIntegrity(fx *officeEvalFixture, r *OfficeEvalReport) error {
	const job = "task-integrity"

	// ── (b) pack-style auto lanes obey the drafting→activation gate ────────
	// Runs FIRST because the blueprint seed replaces the fixture's roster +
	// task list wholesale; the remaining checks build on the seeded office.
	bp := operations.Blueprint{
		ID:   "probe-pack",
		Name: "Probe Pack",
		Kind: "general",
		Starter: operations.StarterPlan{
			LeadSlug:                  "ceo",
			GeneralChannelDescription: "Primary coordination channel.",
			Agents: []operations.StarterAgent{
				{Slug: "ceo", Name: "CEO", Role: "lead", Checked: true, BuiltIn: true},
				{Slug: "eng", Name: "Engineer", Role: "engineering", Checked: true},
			},
			Tasks: []operations.StarterTask{{
				Channel: "general", Owner: "eng",
				Title:   "Run the first CRM hygiene sweep",
				Details: "Probe pack lane seeded by the blueprint.",
			}},
		},
	}
	fx.broker.mu.Lock()
	seedErr := fx.broker.seedFromBlueprintLocked(bp, nil, "", true, false)
	fx.broker.mu.Unlock()
	if seedErr != nil {
		return fmt.Errorf("seed blueprint: %w", seedErr)
	}
	var packID string
	for _, t := range fx.broker.AllTasks() {
		if t.Title == "Run the first CRM hygiene sweep" {
			packID = t.ID
		}
	}
	pack := fx.broker.TaskByID(packID)
	r.add(job, "pack-seeded lane lands in drafting, not running",
		pack != nil && pack.LifecycleState == LifecycleStateDrafting &&
			strings.EqualFold(strings.TrimSpace(pack.TaskType), "issue"),
		fmt.Sprintf("id=%s state=%s type=%s", packID, lifecycleStateOf(pack), taskTypeOf(pack)), "")

	// Agent completion from the seeded pre-start state is refused.
	_, packCompleteErr := fx.broker.MutateTask(TaskPostRequest{
		Action: "complete", ID: packID, Channel: "general", CreatedBy: "ceo",
	})
	packAfterRefusal := fx.broker.TaskByID(packID)
	r.add(job, "agent complete on a pack lane is refused pre-activation",
		packCompleteErr != nil && packAfterRefusal != nil &&
			packAfterRefusal.LifecycleState == LifecycleStateDrafting,
		fmt.Sprintf("err=%v state=%s", packCompleteErr, lifecycleStateOf(packAfterRefusal)), "")

	// No execution turn dispatches for the seeded lane (the same gate
	// sendTaskUpdate enforces on the live notify path).
	woke := make(chan string, 1)
	stub := func(_ *Launcher, _ context.Context, slug, _ string, _ ...string) error {
		select {
		case woke <- slug:
		default:
		}
		return nil
	}
	prior := headlessCodexRunTurnOverride.Load()
	headlessCodexRunTurnOverride.Store(&stub)
	if snapshot := fx.broker.TaskByID(packID); snapshot != nil {
		fx.launcher.sendTaskUpdate(
			notificationTarget{Slug: "eng"},
			officeActionLog{Kind: "task_updated", Actor: "ceo", Channel: snapshot.Channel, RelatedID: packID},
			*snapshot,
			"Pack lane probe — must not dispatch pre-activation.",
		)
	}
	dispatched := ""
	select {
	case dispatched = <-woke:
	case <-time.After(750 * time.Millisecond):
	}
	fx.launcher.stopHeadlessWorkers()
	headlessCodexRunTurnOverride.Store(prior)
	r.add(job, "pack lane does not dispatch before human activation", dispatched == "",
		fmt.Sprintf("dispatched=%q", dispatched), "")

	// Human activation starts it through the normal gate.
	if _, err := fx.broker.MutateTask(TaskPostRequest{
		Action: "approve", ID: packID, Channel: "general", CreatedBy: "human",
	}); err != nil {
		return fmt.Errorf("activate pack lane: %w", err)
	}
	packActivated := fx.broker.TaskByID(packID)
	r.add(job, "human activation starts the pack lane",
		packActivated != nil && packActivated.LifecycleState == LifecycleStateRunning,
		fmt.Sprintf("state=%s", lifecycleStateOf(packActivated)), "")

	// ── (a) self-heal: child not sibling, deduped, completion → parent ─────
	parentA, err := fx.broker.MutateTask(TaskPostRequest{
		Action: "create", Channel: "general", Title: "Send the Q4 renewal emails",
		Details: "Draft and send the three Q4 renewal emails.", Owner: "eng", CreatedBy: "ceo",
	})
	if err != nil {
		return err
	}
	parentAID := parentA.Task.ID
	if err := fx.activateTask(parentAID); err != nil {
		return err
	}
	child, reused, err := fx.broker.RequestSelfHealing("eng", parentAID, agent.EscalationStuck, "Agent stuck: provider session went stale.")
	if err != nil {
		return err
	}
	r.add(job, "self-heal creates a sub-task of the stalled parent",
		!reused && strings.TrimSpace(child.ParentIssueID) == parentAID && isSelfHealingTask(&child),
		fmt.Sprintf("child=%s parent_issue_id=%q pipeline=%q", child.ID, child.ParentIssueID, child.PipelineID), "")

	// A second escalation for the SAME stalled work (different agent +
	// reason → different exact title) merges into the open lane instead of
	// spawning a sibling dup.
	dup, dupReused, err := fx.broker.RequestSelfHealing("ceo", parentAID, agent.EscalationMaxRetries, "Repeated provider errors on the same task.")
	if err != nil {
		return err
	}
	openLanes := 0
	for _, t := range fx.broker.AllTasks() {
		if isSelfHealingTask(&t) && strings.TrimSpace(t.ParentIssueID) == parentAID && !isTerminalTeamTaskStatus(t.Status()) {
			openLanes++
		}
	}
	r.add(job, "second self-heal for the same work dedupes into the open lane",
		dupReused && dup.ID == child.ID && openLanes == 1,
		fmt.Sprintf("dupID=%s childID=%s reused=%v openLanes=%d", dup.ID, child.ID, dupReused, openLanes), "")

	// Complete the repair lane WITH a deliverable: the artifact + completion
	// must attach to the PARENT (artifact recorded, parent advanced into
	// Review for the human's decision), and the lane closes as the child.
	const healArtifact = "team/accounts/q4-renewal-emails.md"
	if _, err := fx.broker.MutateTask(TaskPostRequest{
		Action: "complete", ID: child.ID, Channel: child.Channel, CreatedBy: child.Owner, ArtifactPath: healArtifact,
	}); err != nil {
		return err
	}
	if cur := fx.broker.TaskByID(child.ID); cur != nil && !strings.EqualFold(strings.TrimSpace(cur.Status()), "done") {
		if _, err := fx.broker.MutateTask(TaskPostRequest{
			Action: "approve", ID: child.ID, Channel: child.Channel, CreatedBy: "human",
		}); err != nil {
			return err
		}
	}
	childDone := fx.broker.TaskByID(child.ID)
	parentAfterHeal := fx.broker.TaskByID(parentAID)
	r.add(job, "self-heal completion attaches the artifact to the parent",
		childDone != nil && strings.EqualFold(strings.TrimSpace(childDone.Status()), "done") &&
			parentAfterHeal != nil && parentAfterHeal.Artifact == healArtifact &&
			parentAfterHeal.LifecycleState == LifecycleStateReview &&
			strings.Contains(parentAfterHeal.Details, childDone.ID),
		fmt.Sprintf("child=%s parentArtifact=%q parentState=%s", strings.TrimSpace(childDone.Status()), parentAfterHeal.Artifact, parentAfterHeal.LifecycleState), "")

	// ── (c) no clipped title feeds the repair lane's agent-facing context ──
	const titleTail = "ACV $61,000 END-OF-TITLE-MARKER"
	longTitle := "Corti Labs account brief covering the Q4 renewal motion, the champion-departure risk, the open support escalations, and the " + titleTail
	const detailsTail = "The contract value is exactly $61,000 — END-OF-DETAILS-MARKER."
	parentC, err := fx.broker.MutateTask(TaskPostRequest{
		Action: "create", Channel: "general", Title: longTitle,
		Details: "Write the Corti Labs brief. " + detailsTail, Owner: "eng", CreatedBy: "ceo",
	})
	if err != nil {
		return err
	}
	if err := fx.activateTask(parentC.Task.ID); err != nil {
		return err
	}
	healC, _, err := fx.broker.RequestSelfHealing("eng", parentC.Task.ID, agent.EscalationStuck, "Stuck on the brief.")
	if err != nil {
		return err
	}
	packet := fx.launcher.notifyCtx().BuildTaskExecutionPacket(healC.Owner, officeActionLog{Actor: "ceo"}, healC, "Repair lane packet.")
	r.add(job, "repair sub-task carries the full title + parent contract, never a clipped echo",
		strings.Contains(healC.Title, titleTail) &&
			strings.Contains(healC.Details, titleTail) &&
			strings.Contains(healC.Details, detailsTail) &&
			strings.Contains(packet, titleTail) && strings.Contains(packet, detailsTail),
		fmt.Sprintf("titleHasTail=%v detailsHaveTitleTail=%v detailsHaveContract=%v packet=%d chars",
			strings.Contains(healC.Title, titleTail), strings.Contains(healC.Details, titleTail),
			strings.Contains(healC.Details, detailsTail), len(packet)), "")

	// ── (d) exactly one done-post per terminal transition ──────────────────
	parentD, err := fx.broker.MutateTask(TaskPostRequest{
		Action: "create", Channel: "general", Title: "Publish the pipeline baseline",
		Details: "Publish the pipeline-truth baseline.", Owner: "eng", CreatedBy: "ceo",
	})
	if err != nil {
		return err
	}
	dID := parentD.Task.ID
	if err := fx.activateTask(dID); err != nil {
		return err
	}
	if _, err := fx.broker.MutateTask(TaskPostRequest{Action: "complete", ID: dID, Channel: "general", CreatedBy: "eng"}); err != nil {
		return err
	}
	if cur := fx.broker.TaskByID(dID); cur != nil && !strings.EqualFold(strings.TrimSpace(cur.Status()), "done") {
		if _, err := fx.broker.MutateTask(TaskPostRequest{Action: "approve", ID: dID, Channel: "general", CreatedBy: "human"}); err != nil {
			return err
		}
	}
	// Double-attempt #1: a second terminal verb on the already-done task.
	if _, err := fx.broker.MutateTask(TaskPostRequest{Action: "approve", ID: dID, Channel: "general", CreatedBy: "human"}); err != nil {
		return err
	}
	// Double-attempt #2: the v3 flap — a legacy path drifts status off done
	// WITHOUT clearing CompletedAt, then the terminal verb re-lands the SAME
	// delivery. The done-post stamp must absorb the replay.
	fx.broker.mu.Lock()
	if t := fx.broker.taskByIDLocked(dID); t != nil {
		t.status = "review"
	}
	fx.broker.mu.Unlock()
	if _, err := fx.broker.MutateTask(TaskPostRequest{Action: "approve", ID: dID, Channel: "general", CreatedBy: "human"}); err != nil {
		return err
	}
	deliveredPosts := 0
	deliveredNotices := 0
	fx.broker.mu.Lock()
	for _, msg := range fx.broker.messages {
		if msg.Kind == taskDeliveredMessageKind && msg.SourceTaskID == dID {
			deliveredPosts++
		}
	}
	for _, req := range fx.broker.requests {
		if req.Kind == "notice" && req.IssueID == dID && req.Title == fmt.Sprintf("%s delivered", dID) {
			deliveredNotices++
		}
	}
	fx.broker.mu.Unlock()
	r.add(job, "double terminal-transition attempt posts exactly one done-post + one notice",
		deliveredPosts == 1 && deliveredNotices == 1,
		fmt.Sprintf("posts=%d notices=%d", deliveredPosts, deliveredNotices), "")

	// Wiki fold: byte-identical consecutive writes to the same path produce
	// exactly one commit (v3 [20:15]: triple-identical "confirm contact").
	const foldPath = "team/accounts/fold-probe.md"
	const foldContent = "# Fold probe\n\nDana Whitfield — confirmed contact.\n"
	if _, _, err := fx.broker.wikiWorker.Enqueue(context.Background(), "eng", foldPath, foldContent, "create", "chore: confirm contact"); err != nil {
		return fmt.Errorf("wiki fold seed: %w", err)
	}
	for i := 0; i < 2; i++ {
		if _, _, err := fx.broker.wikiWorker.Enqueue(context.Background(), "eng", foldPath, foldContent, "replace", "chore: confirm contact"); err != nil {
			return fmt.Errorf("wiki fold replay %d: %w", i, err)
		}
	}
	commits, err := fx.broker.wikiWorker.Repo().Log(context.Background(), foldPath)
	if err != nil {
		return fmt.Errorf("wiki fold log: %w", err)
	}
	r.add(job, "byte-identical consecutive wiki writes fold into one commit",
		len(commits) == 1, fmt.Sprintf("commits=%d", len(commits)), "")

	// ── (e) decision/approval cards name the owner from the task record ────
	parentE, err := fx.broker.MutateTask(TaskPostRequest{
		Action: "create", Channel: "general", Title: "Draft the exec-sponsor email",
		Details: "Draft the exec-sponsor email for the QBR.", Owner: "eng", CreatedBy: "ceo",
	})
	if err != nil {
		return err
	}
	eID := parentE.Task.ID
	// Contaminate the packet: the LAST packet actor is the CEO, not the
	// owner. The cards must keep naming the owner from the task record.
	fx.broker.mu.Lock()
	fx.broker.AppendPacketFeedbackLocked(eID, "ceo", "Packet-side note from the CEO — must not become the card actor.")
	fx.broker.mu.Unlock()
	if err := fx.broker.RecordTaskDecisionWithComment(eID, "approve", "", "human"); err != nil {
		return fmt.Errorf("activate via decision path: %w", err)
	}
	// Read the activation card BEFORE the next transition: the 10s lifecycle
	// card coalescer replaces it once the in_review card lands.
	startedOwner, startedFound := latestLifecycleCardOwner(fx.broker, eID, string(IssueLifecycleTransitionStarted))
	if _, err := fx.broker.MutateTask(TaskPostRequest{
		Action: "submit_for_review", ID: eID, Channel: "general", CreatedBy: "ceo",
		Details: "Submitted on the owner's behalf by the CEO.",
	}); err != nil {
		return err
	}
	// The "Ready for your review — submitted by @x" card is emitted on the
	// review→decision convergence transition; the deciding actor there is
	// the reviewer (CEO), not the owner.
	if err := fx.broker.OnReviewerConvergence(eID, "probe convergence"); err != nil {
		return fmt.Errorf("reviewer convergence: %w", err)
	}
	reviewOwner, reviewFound := latestLifecycleCardOwner(fx.broker, eID, string(IssueLifecycleTransitionInReview))
	r.add(job, "decision/approval cards name the task owner, not the packet's last actor",
		startedFound && startedOwner == "eng" && reviewFound && reviewOwner == "eng",
		fmt.Sprintf("started=%q(found=%v) in_review=%q(found=%v)", startedOwner, startedFound, reviewOwner, reviewFound), "")
	return nil
}

// latestLifecycleCardOwner scans the message log newest-first for the latest
// issue_lifecycle card on taskID with the given transition kind and returns
// the `owner` field of its structured payload.
func latestLifecycleCardOwner(b *Broker, taskID, transition string) (string, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for i := len(b.messages) - 1; i >= 0; i-- {
		msg := b.messages[i]
		if msg.Kind != "issue_lifecycle" || msg.SourceTaskID != taskID {
			continue
		}
		payload := struct {
			Owner      string `json:"owner"`
			Transition string `json:"transition"`
		}{}
		if err := json.Unmarshal(msg.Payload, &payload); err != nil {
			continue
		}
		if payload.Transition != transition {
			continue
		}
		return payload.Owner, true
	}
	return "", false
}

func lifecycleStateOf(t *teamTask) LifecycleState {
	if t == nil {
		return ""
	}
	return t.LifecycleState
}

func taskTypeOf(t *teamTask) string {
	if t == nil {
		return ""
	}
	return t.TaskType
}
