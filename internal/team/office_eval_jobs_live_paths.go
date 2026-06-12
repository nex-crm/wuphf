package team

// office_eval_jobs_live_paths.go — the `live-paths` eval job.
//
// Grader round-2 lesson (v3, 6/10): "all three shipped families were green
// in the eval suite and two of three failed live. The suite asserts the
// mechanisms; the product's lifecycle has multiple paths that never enter
// them." This job replicates the v3 FAILURES end-to-end at the HTTP/API
// layer the FE actually uses — not against the intended path.
//
// ── PHASE A: live control-flow paths enumerated from the v3 run ────────────
//
// (1) "Approve & Start" (FE ApproveAndStartButton → postDecision) fires
//     POST /tasks/{id}/decision {"action":"approve","created_by":"human"}.
//     recordTaskDecisionInternal resolves the target from the BROKER task's
//     LifecycleState. J1's button worked because the task was still typed
//     Drafting. J2's identical button closed OFFICE-246/229 terminally
//     because legacy mutations after create (assign/claim/submit in
//     MutateTask) update status/reviewState and re-derive LifecycleState via
//     reindexTaskLifecycleFromLegacyLocked — which never re-stamped the
//     Decision Packet. GET /tasks/{id} serves the PACKET's lifecycleState,
//     so the page still said "drafting" while the broker said Running →
//     approve mapped to terminal Approved with zero work (server.log
//     18:44:12: "wiki promotion for task OFFICE-246" fired at the click).
//
// (2) The task "Approve" button in decision/changes-requested state (FE
//     TaskActionToolbar) fires POST /tasks {"action":"approve",...}. The
//     mutation LANDED (200, status→done) but the page kept rendering the
//     stale packet state ("decision") — a dead-LOOKING approve. Same root
//     cause as (1): packet never re-stamped by legacy mutation paths.
//
// (3) "Reopen task" (FE ReopenTaskButton) fires POST /tasks
//     {"action":"reopen","id","channel","created_by":"human"}. Backend bug
//     found: reopen PRE-SET task.LifecycleState to the target before
//     applyLifecycleStateLocked, so the inverse lifecycle index never
//     removed the task from its terminal bucket — reopened tasks stayed
//     "approved" on index-backed surfaces. (The live "no POST fires" could
//     not be reproduced from the component code; the click→POST contract is
//     pinned by a component test instead.)
//
// (4) Wiki review verbs (FE updateReviewState) fire POST
//     /review/{id}/request-changes and /review/{id}/approve with
//     {"actor_slug":"","rationale":"..."}. request-changes 400s when
//     rationale is empty — the FE never offered a rationale input and
//     swallowed the error (optimistic update + silent rollback). approve
//     500/409s bubble from the atomic wiki commit (git wedge, duplicate
//     target) — also swallowed. The queue looked frozen ([17:43], [18:14]).
//
// (5) Agents executed and "completed" tasks still in `drafting`: the
//     dispatch gate (isExecutableTeamTaskStatus in sendTaskUpdate) refuses
//     execution turns, but a CEO chat-turn can still call team_task
//     action=complete — MutateTask had no pre-start gate, so J3's
//     OFFICE-337 was "done" by narration from drafting with neither the
//     DoD gate nor the human gate ever passed ([19:52–19:57]).
//
// (6) Dependency release: OnDecisionRecorded runs unblockDependentsLocked
//     after EVERY decision — including Drafting→Running activations — and
//     terminal zero-work approvals (1) made upstreams "terminal" at the
//     click, so OFFICE-253 was released against a one-pager that never
//     existed ([18:45:40]). Release must require upstream COMPLETION (with
//     artifact for defined tasks), never an approval click.
//
// (7) Board (GET /tasks list: task.lifecycle_state) and task page (GET
//     /tasks/{id}: packet.lifecycleState) read DIFFERENT fields → board
//     "6/6 approved" vs pages "drafting/decision" ([20:08]). Fixed by
//     re-stamping the packet on every typed-state write AND serving the
//     live task state on the detail read.
//
// ── PHASE C checks (this job) ──────────────────────────────────────────────
//  (a) create with an owner lands RUNNING and the owner dispatches —
//      creation is the authorization; there is no start-approval ceremony
//  (b) approve with zero work on a defined running task → structured error
//      (the review-path truthful-approve gate)
//  (c) reopen via the exact FE payload works (and leaves no stale index)
//  (d) wiki review request-changes + approve via the FE payloads succeed;
//      the empty-rationale contract errors loudly
//  (e) a PARKED task (composer Backlog/park — the one deliberate way into
//      drafting): agent complete is refused, no turn dispatches, and the
//      human's start affordance un-parks it into running
//  (f) dependent stays blocked until upstream completion-with-artifact
//  (g) board list and task detail report the same state for the same task

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"
)

// livePathsClient wraps an httptest server with the broker token the FE
// would hold, so every check speaks the exact wire the web client speaks.
type livePathsClient struct {
	srv   *httptest.Server
	token string
}

func (c *livePathsClient) do(method, path string, body map[string]any) (int, string, error) {
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return 0, "", err
		}
		reader = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(context.Background(), method, c.srv.URL+path, reader)
	if err != nil {
		return 0, "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		return res.StatusCode, "", err
	}
	return res.StatusCode, string(raw), nil
}

func (c *livePathsClient) postJSON(path string, body map[string]any) (int, string, error) {
	return c.do(http.MethodPost, path, body)
}

func (c *livePathsClient) getJSON(path string) (int, string, error) {
	return c.do(http.MethodGet, path, nil)
}

// taskDetailStates extracts (packet lifecycleState, task.lifecycle_state,
// task.status) from a GET /tasks/{id} response body.
func taskDetailStates(body string) (packetState, taskState, taskStatus string) {
	var parsed struct {
		LifecycleState string `json:"lifecycleState"`
		Task           struct {
			LifecycleState string `json:"lifecycle_state"`
			Status         string `json:"status"`
		} `json:"task"`
	}
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		return "", "", ""
	}
	return parsed.LifecycleState, parsed.Task.LifecycleState, parsed.Task.Status
}

// listTaskState finds taskID in a GET /tasks list response and returns its
// (lifecycle_state, status, blocked).
func listTaskState(body, taskID string) (string, string, bool, bool) {
	var parsed struct {
		Tasks []struct {
			ID             string `json:"id"`
			LifecycleState string `json:"lifecycle_state"`
			Status         string `json:"status"`
			Blocked        bool   `json:"blocked"`
		} `json:"tasks"`
	}
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		return "", "", false, false
	}
	for _, t := range parsed.Tasks {
		if t.ID == taskID {
			return t.LifecycleState, t.Status, t.Blocked, true
		}
	}
	return "", "", false, false
}

func evalJobLivePaths(fx *officeEvalFixture, r *OfficeEvalReport) error {
	const job = "live-paths"

	// Serve the exact routes the FE uses, behind the same auth middleware.
	fx.broker.SetReviewerResolver(func(string) string { return "ceo" })
	fx.broker.ensureReviewLog()
	mux := http.NewServeMux()
	mux.HandleFunc("/tasks", fx.broker.requireAuth(fx.broker.handleTasks))
	mux.HandleFunc("/tasks/", fx.broker.requireAuth(fx.broker.handleTaskByID))
	mux.HandleFunc("/task-plan", fx.broker.requireAuth(fx.broker.handleTaskPlan))
	mux.HandleFunc("/notebook/write", fx.broker.requireAuth(fx.broker.handleNotebookWrite))
	mux.HandleFunc("/notebook/promote", fx.broker.requireAuth(fx.broker.handleNotebookPromote))
	mux.HandleFunc("/review/", fx.broker.requireAuth(fx.broker.handleReviewSubpath))
	srv := httptest.NewServer(mux)
	defer srv.Close()
	client := &livePathsClient{srv: srv, token: fx.broker.Token()}

	createTask := func(title string, deps ...string) (string, error) {
		body := map[string]any{
			"action": "create", "channel": "general", "title": title,
			"details": "Live-path probe work.", "owner": "eng", "created_by": "ceo",
		}
		if len(deps) > 0 {
			body["depends_on"] = deps
		}
		status, raw, err := client.postJSON("/tasks", body)
		if err != nil {
			return "", fmt.Errorf("create %q: %w", title, err)
		}
		if status != http.StatusOK {
			return "", fmt.Errorf("create %q: status=%d body=%s", title, status, raw)
		}
		var parsed struct {
			Task struct {
				ID string `json:"id"`
			} `json:"task"`
		}
		if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
			return "", err
		}
		return parsed.Task.ID, nil
	}

	// ── (a) create with an owner lands RUNNING; the owner dispatches ──────
	// Creation IS the authorization (founder directive): no Approve & Start
	// ceremony stands between the create and the owner's first turn.
	aID, err := createTask("Draft the renewal emails (live-path a)")
	if err != nil {
		return err
	}
	_, detailBody, err := client.getJSON("/tasks/" + aID)
	if err != nil {
		return err
	}
	packetState, taskState, taskStatus := taskDetailStates(detailBody)
	r.add(job, "create with an owner lands running (no start-approval ceremony)",
		taskState == string(LifecycleStateRunning) &&
			packetState == string(LifecycleStateRunning) && !strings.EqualFold(taskStatus, "done"),
		fmt.Sprintf("packet=%s task=%s status=%s", packetState, taskState, taskStatus), "")

	// The owner DISPATCHES off the create: the task_created action routed
	// through the live sendTaskUpdate path must enqueue an execution turn.
	aWoke := make(chan string, 1)
	aStub := func(_ *Launcher, _ context.Context, slug, _ string, _ ...string) error {
		select {
		case aWoke <- slug:
		default:
		}
		return nil
	}
	aPrior := headlessCodexRunTurnOverride.Load()
	headlessCodexRunTurnOverride.Store(&aStub)
	aSnapshot := fx.broker.TaskByID(aID)
	fx.launcher.sendTaskUpdate(
		notificationTarget{Slug: "eng"},
		officeActionLog{Kind: "task_created", Actor: "ceo", Channel: aSnapshot.Channel, RelatedID: aID},
		*aSnapshot,
		"Work packet for a freshly created task — must dispatch.",
	)
	aDispatched := ""
	select {
	case aDispatched = <-aWoke:
	case <-time.After(8 * time.Second):
	}
	fx.launcher.stopHeadlessWorkers()
	headlessCodexRunTurnOverride.Store(aPrior)
	r.add(job, "owner dispatches off the create (task_created turn enqueued)",
		aDispatched == "eng", fmt.Sprintf("dispatched=%q", aDispatched), "")

	// ── (b) approve with zero work on a defined running task ──────────────
	// The review-path truthful-approve gate: a created task lands running
	// with ZERO work done. An approve click on it must hit a structured
	// error ("mid-execution; nothing submitted"), never a terminal approve
	// (v3 J2 [19:04]: zero-work tasks closed terminally at the click).
	bID, err := createTask("Build the QBR one-pager (live-path b)")
	if err != nil {
		return err
	}
	if _, err := fx.broker.MutateTask(TaskPostRequest{
		Action: "define", ID: bID, Channel: "general", CreatedBy: "ceo",
		Definition: &TaskDefinition{
			Goal:            "Ship the QBR one-pager",
			Deliverables:    []TaskDeliverable{{Name: "one-pager", Format: "markdown in the wiki"}},
			SuccessCriteria: []string{"One-pager published to the wiki"},
		},
	}); err != nil {
		return err
	}
	if _, _, err := client.postJSON("/tasks", map[string]any{
		"action": "assign", "id": bID, "channel": "general", "owner": "eng", "created_by": "ceo",
	}); err != nil {
		return err
	}
	drifted := fx.broker.TaskByID(bID)
	bDecisionStatus, bDecisionBody, err := client.postJSON("/tasks/"+bID+"/decision", map[string]any{
		"action": "approve", "created_by": "human",
	})
	if err != nil {
		return err
	}
	bAfter := fx.broker.TaskByID(bID)
	r.add(job, "approve with zero work on a running defined task returns a structured error, never terminal",
		drifted != nil && drifted.LifecycleState == LifecycleStateRunning &&
			bDecisionStatus == http.StatusConflict && strings.Contains(bDecisionBody, "error") &&
			bAfter != nil && bAfter.LifecycleState != LifecycleStateApproved &&
			!strings.EqualFold(strings.TrimSpace(bAfter.status), "done"),
		fmt.Sprintf("drifted=%s decision=%d body=%s after=%s", drifted.LifecycleState, bDecisionStatus, truncate(bDecisionBody, 160), bAfter.LifecycleState), "")

	// Terminal approve on a defined task with submitted-but-artifactless
	// work is rejected by the artifact gate ON THE DECISION PATH.
	if _, err := fx.broker.MutateTask(TaskPostRequest{
		Action: "submit_for_review", ID: bID, Channel: "general", CreatedBy: "eng",
		Details: "Draft submitted (no artifact yet).",
	}); err != nil {
		return err
	}
	bGateStatus, bGateBody, err := client.postJSON("/tasks/"+bID+"/decision", map[string]any{
		"action": "approve", "created_by": "human",
	})
	if err != nil {
		return err
	}
	bGated := fx.broker.TaskByID(bID)
	r.add(job, "terminal approve without an artifact on a defined task is rejected on the decision path",
		bGateStatus == http.StatusConflict && strings.Contains(bGateBody, "artifact") &&
			bGated != nil && !strings.EqualFold(strings.TrimSpace(bGated.status), "done"),
		fmt.Sprintf("status=%d body=%s", bGateStatus, truncate(bGateBody, 160)), "")

	// ── (c) Reopen via the exact FE payload shape ──────────────────────────
	cID, err := createTask("Ship the launch checklist (live-path c)")
	if err != nil {
		return err
	}
	if _, err := fx.broker.MutateTask(TaskPostRequest{Action: "complete", ID: cID, Channel: "general", CreatedBy: "eng"}); err != nil {
		return err
	}
	if _, err := fx.broker.MutateTask(TaskPostRequest{Action: "approve", ID: cID, Channel: "general", CreatedBy: "human"}); err != nil {
		return err
	}
	// Exact FE payload: web/src/api/tasks.ts reopenTask.
	reopenStatus, reopenBody, err := client.postJSON("/tasks", map[string]any{
		"action": "reopen", "id": cID, "channel": "general", "created_by": "human",
	})
	if err != nil {
		return err
	}
	reopened := fx.broker.TaskByID(cID)
	staleApproved := false
	for _, id := range fx.broker.LifecycleIndexSnapshot()[LifecycleStateApproved] {
		if id == cID {
			staleApproved = true
		}
	}
	r.add(job, "reopen via the FE payload works and leaves no stale terminal index entry",
		reopenStatus == http.StatusOK && reopened != nil &&
			reopened.LifecycleState == LifecycleStateRunning && reopened.CompletedAt == "" && !staleApproved,
		fmt.Sprintf("status=%d body=%s state=%s staleApproved=%v", reopenStatus, truncate(reopenBody, 120), reopened.LifecycleState, staleApproved), "")

	// ── (d) Wiki review verbs via the FE payloads ──────────────────────────
	if _, _, err := client.postJSON("/notebook/write", map[string]any{
		"slug": "eng", "path": "agents/eng/notebook/acme-brief.md",
		"content": "# Acme brief\n\nLive-path probe.\n", "mode": "create", "commit_message": "seed",
	}); err != nil {
		return err
	}
	promoteStatus, promoteBody, err := client.postJSON("/notebook/promote", map[string]any{
		"my_slug": "eng", "source_path": "agents/eng/notebook/acme-brief.md",
		"target_wiki_path": "team/accounts/acme-brief.md", "rationale": "Ready for team wiki review.",
	})
	if err != nil {
		return err
	}
	var promoted struct {
		PromotionID string `json:"promotion_id"`
	}
	_ = json.Unmarshal([]byte(promoteBody), &promoted)
	if promoteStatus != http.StatusOK || promoted.PromotionID == "" {
		return fmt.Errorf("promote: status=%d body=%s", promoteStatus, promoteBody)
	}
	rid := promoted.PromotionID

	// Empty rationale (the old FE payload) must fail LOUDLY with a JSON
	// error — this is the 400 the v3 run swallowed three times in a row.
	emptyStatus, emptyBody, err := client.postJSON("/review/"+rid+"/request-changes", map[string]any{
		"actor_slug": "", "rationale": "",
	})
	if err != nil {
		return err
	}
	r.add(job, "wiki review request-changes with empty rationale returns a structured 400",
		emptyStatus == http.StatusBadRequest && strings.Contains(emptyBody, "rationale"),
		fmt.Sprintf("status=%d body=%s", emptyStatus, truncate(emptyBody, 120)), "")

	rcStatus, rcBody, err := client.postJSON("/review/"+rid+"/request-changes", map[string]any{
		"actor_slug": "", "rationale": "Merge with the existing Acme brief instead of duplicating.",
	})
	if err != nil {
		return err
	}
	r.add(job, "wiki review request-changes with a rationale succeeds via the FE payload",
		rcStatus == http.StatusOK && strings.Contains(rcBody, "changes-requested"),
		fmt.Sprintf("status=%d body=%s", rcStatus, truncate(rcBody, 120)), "")

	if _, _, err := client.postJSON("/review/"+rid+"/resubmit", map[string]any{"actor_slug": "eng"}); err != nil {
		return err
	}
	approveStatus, approveBody, err := client.postJSON("/review/"+rid+"/approve", map[string]any{
		"actor_slug": "", "rationale": "",
	})
	if err != nil {
		return err
	}
	r.add(job, "wiki review approve succeeds via the FE payload and lands the article",
		approveStatus == http.StatusOK && strings.Contains(approveBody, "approved"),
		fmt.Sprintf("status=%d body=%s", approveStatus, truncate(approveBody, 160)), "")

	// ── (e) agent turn / completion for a PARKED task is refused ──────────
	// Drafting is now reachable only through the deliberate park path (the
	// composer's Backlog action → POST /task-plan park=true). Everything
	// the old pre-start ceremony enforced binds ONLY here.
	ePlanStatus, ePlanBody, err := client.postJSON("/task-plan", map[string]any{
		"channel": "general", "created_by": "human",
		"tasks": []map[string]any{{
			"title": "Ship the MeetingMind landing page (live-path e)", "assignee": "eng",
			"details": "Parked on purpose — backlog lane.", "task_type": "issue", "park": true,
		}},
	})
	if err != nil {
		return err
	}
	var ePlan struct {
		Tasks []struct {
			ID string `json:"id"`
		} `json:"tasks"`
	}
	if err := json.Unmarshal([]byte(ePlanBody), &ePlan); err != nil {
		return fmt.Errorf("park via task-plan: status=%d body=%s: %w", ePlanStatus, ePlanBody, err)
	}
	if ePlanStatus != http.StatusOK || len(ePlan.Tasks) == 0 {
		return fmt.Errorf("park via task-plan: status=%d body=%s", ePlanStatus, ePlanBody)
	}
	eID := ePlan.Tasks[0].ID
	eParked := fx.broker.TaskByID(eID)
	r.add(job, "composer park lands the task in drafting (the one deliberate parked state)",
		eParked != nil && eParked.LifecycleState == LifecycleStateDrafting,
		fmt.Sprintf("state=%s", eParked.LifecycleState), "")
	eStatus, eBody, err := client.postJSON("/tasks", map[string]any{
		"action": "complete", "id": eID, "channel": "general", "created_by": "ceo",
	})
	if err != nil {
		return err
	}
	eTask := fx.broker.TaskByID(eID)
	r.add(job, "agent complete on a parked task is refused with a structured error",
		eStatus == http.StatusConflict && strings.Contains(eBody, "parked") &&
			eTask != nil && eTask.LifecycleState == LifecycleStateDrafting,
		fmt.Sprintf("status=%d body=%s state=%s", eStatus, truncate(eBody, 160), eTask.LifecycleState), "")

	// No execution turn dispatches for a parked task: the same gate
	// sendTaskUpdate enforces (isExecutableTeamTaskStatus).
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
	eTaskSnapshot := fx.broker.TaskByID(eID)
	fx.launcher.sendTaskUpdate(
		notificationTarget{Slug: "eng"},
		officeActionLog{Kind: "task_updated", Actor: "ceo", Channel: eTaskSnapshot.Channel, RelatedID: eID},
		*eTaskSnapshot,
		"Work packet for a parked task — must not dispatch.",
	)
	dispatched := ""
	select {
	case dispatched = <-woke:
	case <-time.After(750 * time.Millisecond):
	}
	fx.launcher.stopHeadlessWorkers()
	headlessCodexRunTurnOverride.Store(prior)
	r.add(job, "no execution turn dispatches for a parked task", dispatched == "",
		fmt.Sprintf("dispatched=%q", dispatched), "")

	// The human's start affordance ("Parked — start", FE postDecision
	// approve) is the ONE remaining start button — it un-parks into running.
	eStartStatus, eStartBody, err := client.postJSON("/tasks/"+eID+"/decision", map[string]any{
		"action": "approve", "created_by": "human",
	})
	if err != nil {
		return err
	}
	eStarted := fx.broker.TaskByID(eID)
	r.add(job, "the human's start affordance un-parks the task into running",
		eStartStatus == http.StatusOK && eStarted != nil && eStarted.LifecycleState == LifecycleStateRunning,
		fmt.Sprintf("status=%d body=%s state=%s", eStartStatus, truncate(eStartBody, 120), eStarted.LifecycleState), "")

	// ── (f) dependency releases on completion-with-artifact, not approval ──
	upID, err := createTask("Research competitor pricing (live-path f)")
	if err != nil {
		return err
	}
	if _, err := fx.broker.MutateTask(TaskPostRequest{
		Action: "define", ID: upID, Channel: "general", CreatedBy: "ceo",
		Definition: &TaskDefinition{
			Goal:            "Publish the competitor pricing research",
			Deliverables:    []TaskDeliverable{{Name: "research brief", Format: "markdown in the wiki"}},
			SuccessCriteria: []string{"Brief published to the wiki"},
		},
	}); err != nil {
		return err
	}
	downID, err := createTask("Write the pricing page (live-path f downstream)", upID)
	if err != nil {
		return err
	}
	// The upstream landed RUNNING at create (no activation step exists).
	// The dependent must be born blocked and STAY blocked until the
	// upstream actually completes with its artifact — release requires
	// COMPLETION, never a click (v3 released it at an approval click,
	// [18:45:40]).
	_, listBody, err := client.getJSON("/tasks?viewer_slug=human&all_channels=true&include_done=true")
	if err != nil {
		return err
	}
	_, _, downBlocked, downFound := listTaskState(listBody, downID)
	r.add(job, "dependent is born blocked while the running upstream is incomplete",
		downFound && downBlocked, fmt.Sprintf("found=%v blocked=%v", downFound, downBlocked), "")

	// Completion WITH artifact releases it. The artifact must exist on disk
	// (B5 existence gate — a phantom path no longer passes).
	const upArtifact = "team/research/competitor-pricing.md"
	if err := fx.seedWikiFile(upArtifact, "# Competitor pricing research\n"); err != nil {
		return err
	}
	if _, err := fx.broker.MutateTask(TaskPostRequest{
		Action: "complete", ID: upID, Channel: "general", CreatedBy: "eng", ArtifactPath: upArtifact,
	}); err != nil {
		return err
	}
	if cur := fx.broker.TaskByID(upID); cur != nil && !strings.EqualFold(strings.TrimSpace(cur.status), "done") {
		if _, err := fx.broker.MutateTask(TaskPostRequest{
			Action: "approve", ID: upID, Channel: "general", CreatedBy: "human", ArtifactPath: upArtifact,
		}); err != nil {
			return err
		}
	}
	upDone := fx.broker.TaskByID(upID)
	downAfter := fx.broker.TaskByID(downID)
	r.add(job, "dependent unblocks on upstream completion-with-artifact",
		upDone != nil && strings.EqualFold(strings.TrimSpace(upDone.status), "done") && upDone.Artifact == upArtifact &&
			downAfter != nil && !downAfter.blocked,
		fmt.Sprintf("upstream=%q artifact=%q downstreamBlocked=%v", strings.TrimSpace(upDone.status), upDone.Artifact, downAfter.blocked), "")

	// ── (g) board list and task detail report the same state ──────────────
	// Use the drifted task from (b): legacy mutations moved its typed state;
	// both endpoints must agree on what it is now.
	_, gList, err := client.getJSON("/tasks?viewer_slug=human&all_channels=true&include_done=true")
	if err != nil {
		return err
	}
	gListState, _, _, gFound := listTaskState(gList, bID)
	_, gDetail, err := client.getJSON("/tasks/" + bID)
	if err != nil {
		return err
	}
	gPacketState, gTaskState, _ := taskDetailStates(gDetail)
	r.add(job, "board list and task detail report the same lifecycle state",
		gFound && gListState != "" && gListState == gPacketState && gListState == gTaskState,
		fmt.Sprintf("list=%s packet=%s detail-task=%s", gListState, gPacketState, gTaskState), "")
	return nil
}
