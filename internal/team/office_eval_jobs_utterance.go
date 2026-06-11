package team

// office_eval_jobs_utterance.go — the `utterance-routing` eval job.
//
// Grader round-2 fix family #2 (v3, 6/10): "make every human utterance
// reach an agent, and make blocking asks loud." This job replicates the
// v3 failures at the HTTP layer with the exact payloads the FE fires:
//
//	(a) Task-toolbar "Request changes" ([17:47→17:50]): the FE sends the
//	    typed text as override_reason (web/src/api/tasks.ts), the broker
//	    read only details — three live runs dropped the text. The exact
//	    FE payload must land the text in the changes_requested stamp, in
//	    the channel wake, and in the owner's next packet.
//	(b) Thread reply to an interview ([19:24:53]): the Inbox card's only
//	    affordance posted a chat reply that reached no agent. A thread
//	    reply anchored to the interview must BE the answer the polling
//	    agent receives — and creating the interview must post a loud
//	    chat announcement in its channel (the thread anchor).
//	(c) Plain chat in a waiting (decision-state) task channel
//	    ([17:51→18:02]): 14 minutes of dead air. The post must stamp the
//	    note AND re-enqueue the owner with the note leading the packet.
//	(d) One agent's pending interview must not wedge the office
//	    ([19:23:59]): dispatch for OTHER agents keeps flowing; only the
//	    asking agent's new turns are parked until the answer lands, then
//	    its lane resumes. The blocking-request chat gate is channel-
//	    scoped: it parks chat in ITS channel only.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"
)

const utteranceWakeTimeout = 4 * time.Second

func evalJobUtteranceRouting(fx *officeEvalFixture, r *OfficeEvalReport) error {
	const job = "utterance-routing"

	// Serve the exact routes the FE and the MCP tools use, behind auth.
	mux := http.NewServeMux()
	mux.HandleFunc("/tasks", fx.broker.requireAuth(fx.broker.handleTasks))
	mux.HandleFunc("/tasks/", fx.broker.requireAuth(fx.broker.handleTaskByID))
	mux.HandleFunc("/messages", fx.broker.requireAuth(fx.broker.handleMessages))
	mux.HandleFunc("/requests", fx.broker.requireAuth(fx.broker.handleRequests))
	mux.HandleFunc("/requests/answer", fx.broker.requireAuth(fx.broker.handleRequestAnswer))
	mux.HandleFunc("/interview/answer", fx.broker.requireAuth(fx.broker.handleInterviewAnswer))
	srv := httptest.NewServer(mux)
	defer srv.Close()
	client := &livePathsClient{srv: srv, token: fx.broker.Token()}

	createTask := func(title, owner string) (*teamTask, error) {
		status, raw, err := client.postJSON("/tasks", map[string]any{
			"action": "create", "channel": "general", "title": title,
			"details": "Utterance-routing probe work.", "owner": owner, "created_by": "ceo",
		})
		if err != nil {
			return nil, fmt.Errorf("create %q: %w", title, err)
		}
		if status != http.StatusOK {
			return nil, fmt.Errorf("create %q: status=%d body=%s", title, status, raw)
		}
		var parsed struct {
			Task struct {
				ID string `json:"id"`
			} `json:"task"`
		}
		if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
			return nil, err
		}
		task := fx.broker.TaskByID(parsed.Task.ID)
		if task == nil {
			return nil, fmt.Errorf("create %q: task %s not found after create", title, parsed.Task.ID)
		}
		return task, nil
	}
	activate := func(taskID string) error {
		status, raw, err := client.postJSON("/tasks/"+taskID+"/decision", map[string]any{
			"action": "approve", "created_by": "human",
		})
		if err != nil {
			return err
		}
		if status != http.StatusOK {
			return fmt.Errorf("activate %s: status=%d body=%s", taskID, status, raw)
		}
		return nil
	}

	// ── (a) FE-payload request-changes → text reaches stamp + wake + packet ──
	const rcText = "Show the full email bodies inline; use Dana Whitfield as the Acme contact; Corti ARR is $61k — do not mark done until I read them."
	taskA, err := createTask("Draft tailored renewal emails (utterance a)", "eng")
	if err != nil {
		return err
	}
	if err := activate(taskA.ID); err != nil {
		return err
	}
	if _, _, err := client.postJSON("/tasks", map[string]any{
		"action": "submit_for_review", "id": taskA.ID, "channel": taskA.Channel,
		"created_by": "eng", "details": "Draft v1 submitted.",
	}); err != nil {
		return err
	}
	// Exact FE payload: web/src/api/tasks.ts updateTaskStatus with
	// overrideReason — the reason rides override_reason (and the FE also
	// mirrors it into memory_workflow_override_reason); details is ABSENT.
	rcStatus, rcBody, err := client.postJSON("/tasks", map[string]any{
		"action": "request_changes", "id": taskA.ID, "channel": "general",
		"created_by":                      "human",
		"memory_workflow_override_reason": rcText,
		"override_reason":                 rcText,
	})
	if err != nil {
		return err
	}
	stamped := fx.broker.TaskByID(taskA.ID)
	r.add(job, "FE request-changes payload lands the typed text in the changes_requested stamp",
		rcStatus == http.StatusOK && stamped != nil &&
			stamped.ChangesRequested != nil && strings.Contains(stamped.ChangesRequested.Body, "Dana Whitfield") &&
			stamped.HumanObjection != nil,
		fmt.Sprintf("status=%d body=%s stamp=%v", rcStatus, truncate(rcBody, 120), stamped.ChangesRequested), "")

	// The wake the owner actually receives is a channel message
	// (postTaskRequestChangesNotificationsLocked) — it must carry the text.
	_, msgsBody, err := client.getJSON("/messages?channel=" + stamped.Channel + "&viewer_slug=human&limit=50")
	if err != nil {
		return err
	}
	r.add(job, "request-changes wake message in the task channel carries the typed text",
		strings.Contains(msgsBody, "Dana Whitfield"),
		fmt.Sprintf("channel=%s", stamped.Channel), "")

	// And the owner's next packet — both the message-wake packet (the
	// live wake path for changes_requested, which is not an executable
	// state) and the next task-execution packet — must lead with it.
	var rcWake channelMessage
	for _, m := range fx.broker.ChannelMessages(stamped.Channel) {
		if m.Kind == "task_changes_requested" {
			rcWake = m
		}
	}
	msgPacket := fx.launcher.buildMessageWorkPacket(rcWake, "eng")
	execPacket := fx.launcher.notifyCtx().BuildTaskExecutionPacket("eng", officeActionLog{Actor: "human"}, *stamped, "Revise per feedback.")
	r.add(job, "owner's next packet carries the request-changes text",
		strings.Contains(msgPacket, "Dana Whitfield") && strings.Contains(msgPacket, "CHANGES REQUESTED") &&
			strings.Contains(execPacket, "Dana Whitfield"),
		fmt.Sprintf("msgPacket=%d chars execPacket=%d chars wakeKind=%q", len(msgPacket), len(execPacket), rcWake.Kind), "")

	// ── (b) interview is loud + a thread reply IS the answer ────────────────
	const interviewQ = "Two things before I queue the sends: 1. Your sender name 2. Acme meeting dates (Tue–Thu preferred)."
	// Exact MCP wire: teammcp handleHumanInterview → POST /requests.
	ivStatus, ivBody, err := client.postJSON("/requests", map[string]any{
		"kind": "interview", "channel": "general", "from": "eng",
		"title": "Human interview", "question": interviewQ, "context": "",
		"blocking": false, "required": false, "reply_to": "", "issue_id": "",
	})
	if err != nil {
		return err
	}
	var ivParsed struct {
		ID      string `json:"id"`
		Request struct {
			ReplyTo string `json:"reply_to"`
		} `json:"request"`
	}
	if err := json.Unmarshal([]byte(ivBody), &ivParsed); err != nil {
		return err
	}
	_, generalMsgs, err := client.getJSON("/messages?channel=general&viewer_slug=human&limit=50")
	if err != nil {
		return err
	}
	r.add(job, "interview creation posts a loud chat announcement that anchors the thread",
		ivStatus == http.StatusOK && ivParsed.ID != "" && ivParsed.Request.ReplyTo != "" &&
			strings.Contains(generalMsgs, "sender name"),
		fmt.Sprintf("status=%d id=%s anchor=%q", ivStatus, ivParsed.ID, ivParsed.Request.ReplyTo), "")

	// Exact FE thread-reply payload: web/src/api/client.ts postMessage via
	// ThreadPanel — from "you", reply_to = the thread anchor.
	const ivReply = "Sender name: Maya Reyes. Acme meeting dates: July 8 2pm ET / July 9 10am ET."
	replyStatus, replyBody, err := client.postJSON("/messages", map[string]any{
		"from": "you", "channel": "general", "content": ivReply,
		"reply_to": ivParsed.Request.ReplyTo,
	})
	if err != nil {
		return err
	}
	_, answerBody, err := client.getJSON("/interview/answer?id=" + ivParsed.ID)
	if err != nil {
		return err
	}
	var answerParsed struct {
		Answered *struct {
			ChoiceID   string `json:"choice_id"`
			CustomText string `json:"custom_text"`
		} `json:"answered"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal([]byte(answerBody), &answerParsed); err != nil {
		return err
	}
	r.add(job, "a human thread reply to the interview IS the answer the polling agent receives",
		replyStatus == http.StatusOK && answerParsed.Status == "answered" &&
			answerParsed.Answered != nil && answerParsed.Answered.CustomText == ivReply,
		fmt.Sprintf("reply=%d body=%s answer=%s", replyStatus, truncate(replyBody, 80), truncate(answerBody, 160)), "")

	// ── (c) plain chat in a decision-state task channel wakes the owner ─────
	taskC, err := createTask("Build the QBR one-pager (utterance c)", "eng")
	if err != nil {
		return err
	}
	if err := activate(taskC.ID); err != nil {
		return err
	}
	if _, _, err := client.postJSON("/tasks", map[string]any{
		"action": "submit_for_review", "id": taskC.ID, "channel": taskC.Channel,
		"created_by": "eng", "details": "One-pager draft submitted.",
	}); err != nil {
		return err
	}
	// Park the task on the human (the v3 dead-air state). Reviewer
	// convergence is not under test here; the typed chokepoint is.
	if err := fx.broker.TransitionLifecycle(taskC.ID, LifecycleStateDecision, "eval: parked on the human decision"); err != nil {
		return err
	}
	taskC = fx.broker.TaskByID(taskC.ID)
	const redlines = "Redlines: Dana Whitfield + dana.whitfield@acme.example for Acme, Corti resolution date July 15 2026, sender is Maya. Finalize and resubmit."
	// Exact FE payload: web/src/api/client.ts postMessage from the channel
	// composer — plain chat, no @-mention, no tags.
	noteStatus, _, err := client.postJSON("/messages", map[string]any{
		"from": "you", "channel": taskC.Channel, "content": redlines,
	})
	if err != nil {
		return err
	}
	noted := fx.broker.TaskByID(taskC.ID)
	var followUp *officeActionLog
	for _, action := range fx.broker.Actions() {
		if action.Kind == taskFollowUpActionKind && action.RelatedID == taskC.ID {
			a := action
			followUp = &a
		}
	}
	r.add(job, "human chat in a decision-state task channel stamps the note and appends the wake action",
		noteStatus == http.StatusOK && noted != nil && noted.LifecycleState == LifecycleStateDecision &&
			noted.HumanNotePending != nil && strings.Contains(noted.HumanNotePending.Body, "July 15") &&
			followUp != nil,
		fmt.Sprintf("status=%d state=%s note=%v followup=%v", noteStatus, noted.LifecycleState, noted.HumanNotePending != nil, followUp != nil), "")
	if followUp == nil {
		return nil
	}

	// The wake re-enqueues the OWNER with the note leading the packet.
	// deliverTaskNotification is exactly what notifyTaskActionsLoop calls
	// for taskFollowUpActionKind (its kind allowlist includes it).
	woke := make(chan [2]string, 4)
	stub := func(_ *Launcher, _ context.Context, slug, notification string, _ ...string) error {
		select {
		case woke <- [2]string{slug, notification}:
		default:
		}
		return nil
	}
	prior := headlessCodexRunTurnOverride.Load()
	headlessCodexRunTurnOverride.Store(&stub)
	defer headlessCodexRunTurnOverride.Store(prior)

	fx.launcher.deliverTaskNotification(*followUp, *noted)
	var ownerWake [2]string
	select {
	case ownerWake = <-woke:
	case <-time.After(utteranceWakeTimeout):
	}
	noteLeads := false
	if idx := strings.Index(ownerWake[1], "HUMAN POSTED ON YOUR WAITING TASK"); idx >= 0 {
		header := strings.Index(ownerWake[1], "[Task update")
		noteLeads = header < 0 || idx < header
	}
	r.add(job, "the wake re-enqueues the owner with the note leading the packet",
		ownerWake[0] == "eng" && strings.Contains(ownerWake[1], "July 15") && noteLeads,
		fmt.Sprintf("slug=%q packet=%d chars noteLeads=%v", ownerWake[0], len(ownerWake[1]), noteLeads), "")

	// ── (d) a pending interview parks only the asking agent's lane ──────────
	ivdStatus, ivdBody, err := client.postJSON("/requests", map[string]any{
		"kind": "interview", "channel": "general", "from": "eng",
		"title": "Human interview", "question": "Which CRM export format do you want?",
		"blocking": false, "required": false, "reply_to": "", "issue_id": "",
	})
	if err != nil {
		return err
	}
	var ivdParsed struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(ivdBody), &ivdParsed); err != nil {
		return err
	}
	if ivdStatus != http.StatusOK || ivdParsed.ID == "" {
		return fmt.Errorf("interview (d): status=%d body=%s", ivdStatus, ivdBody)
	}

	// Task B belongs to a DIFFERENT agent (the lead). Its dispatch must
	// flow while eng's interview is pending — the v3 office froze here.
	taskB, err := createTask("Ship the pipeline baseline (utterance d)", "ceo")
	if err != nil {
		return err
	}
	if err := activate(taskB.ID); err != nil {
		return err
	}
	bSnapshot := fx.broker.TaskByID(taskB.ID)
	fx.launcher.deliverTaskNotification(officeActionLog{
		Kind: "task_updated", Actor: "human", Channel: bSnapshot.Channel, RelatedID: taskB.ID,
	}, *bSnapshot)
	otherWake := [2]string{}
	select {
	case otherWake = <-woke:
	case <-time.After(utteranceWakeTimeout):
	}
	r.add(job, "a pending interview for agent A does not stop a turn for agent B",
		otherWake[0] == "ceo",
		fmt.Sprintf("dispatched=%q while interview %s pending", otherWake[0], ivdParsed.ID), "")

	// The ASKING agent's lane is parked while its interview is pending…
	fx.broker.mu.Lock()
	if t := fx.broker.taskByIDLocked(taskA.ID); t != nil {
		// request_changes left it changes_requested; force Running so the
		// only gate under test is the interview suppression.
		t.LifecycleState = LifecycleStateRunning
		t.status = "in_progress"
	}
	fx.broker.mu.Unlock()
	taskA2 := fx.broker.TaskByID(taskA.ID)
	fx.launcher.sendTaskUpdate(notificationTarget{Slug: "eng"}, officeActionLog{
		Kind: "task_updated", Actor: "human", Channel: taskA2.Channel, RelatedID: taskA2.ID,
	}, *taskA2, "Continue work.")
	parked := ""
	select {
	case wake := <-woke:
		parked = wake[0]
	case <-time.After(750 * time.Millisecond):
	}
	r.add(job, "the asking agent's own lane is parked while its interview is pending",
		parked == "", fmt.Sprintf("dispatched=%q", parked), "")

	// …and resumes once the human answers (exact FE payload:
	// web/src/api/client.ts answerRequest).
	ansStatus, ansBody, err := client.postJSON("/requests/answer", map[string]any{
		"id": ivdParsed.ID, "choice_id": "answer_directly", "custom_text": "CSV, one row per account.",
	})
	if err != nil {
		return err
	}
	fx.launcher.sendTaskUpdate(notificationTarget{Slug: "eng"}, officeActionLog{
		Kind: "task_updated", Actor: "human", Channel: taskA2.Channel, RelatedID: taskA2.ID,
	}, *taskA2, "Continue work.")
	resumed := ""
	select {
	case wake := <-woke:
		resumed = wake[0]
	case <-time.After(utteranceWakeTimeout):
	}
	fx.launcher.stopHeadlessWorkers()
	r.add(job, "answering the interview resumes the asking agent's lane",
		ansStatus == http.StatusOK && resumed == "eng",
		fmt.Sprintf("answer=%d body=%s dispatched=%q", ansStatus, truncate(ansBody, 80), resumed), "")

	// ── (e) the blocking-request chat gate is channel-scoped ────────────────
	// A blocking approval in the task channel parks chat THERE, not
	// everywhere: the human keeps talking in #general.
	if _, _, err := client.postJSON("/requests", map[string]any{
		"kind": "approval", "channel": taskC.Channel, "from": "eng",
		"title": "Approve the send", "question": "Send the three renewal emails now?",
	}); err != nil {
		return err
	}
	blockedStatus, _, err := client.postJSON("/messages", map[string]any{
		"from": "you", "channel": taskC.Channel, "content": "Trying to chat past the gate.",
	})
	if err != nil {
		return err
	}
	openStatus, _, err := client.postJSON("/messages", map[string]any{
		"from": "you", "channel": "general", "content": "Office still talks while one channel waits on an approval.",
	})
	if err != nil {
		return err
	}
	r.add(job, "a blocking request parks chat in its own channel only",
		blockedStatus == http.StatusConflict && openStatus == http.StatusOK,
		fmt.Sprintf("blockedChannel=%d general=%d", blockedStatus, openStatus), "")

	return nil
}
