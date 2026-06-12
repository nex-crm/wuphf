package team

// office_eval_jobs_workspace.go — the `workspace-isolation` eval job
// (ICP-eval v3 round-2 fix family #3, V3-N5/V3-N6).
//
// The v3 live run had agents executing in the FOUNDER'S host git worktree
// (the broker process's launch cwd): the CEO wrote landing/index.html into
// the host repo ([19:53], explaining the recurring "Git index lock"
// contention), a Stop-response ran `git checkout HEAD` there and destroyed
// the session's deliverable ([20:03]), and Sam's stated DoD never became a
// machine gate across three consecutive live runs ([19:52–20:08]).
//
// Checks, all at the live dispatch/HTTP layer:
//  (a) a dispatched turn for a task WITHOUT a worktree carries a
//      working_directory inside the office runtime home — asserted on the
//      queue's launch-param record, never the broker process cwd;
//  (b) a chat-turn (no task) gets the per-agent scratch dir;
//  (c) the J3 done-means-done chain end-to-end over HTTP: create with
//      Sam's verbatim DoD phrasing → verification derived at create →
//      the task lands running (creation is the authorization) → agent
//      complete WITHOUT the file → 409 verification_failed → deliverable
//      created in the agent's working dir → complete succeeds → Verified
//      visible on the task detail endpoint;
//  (d) a Halt note's packet carries the do-not-revert instruction.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func evalJobWorkspaceIsolation(fx *officeEvalFixture, r *OfficeEvalReport) error {
	const job = "workspace-isolation"

	// Pin the office runtime home to this job's scratch dir — the same env
	// knob the live office launches with (WUPHF_RUNTIME_HOME) — so agent
	// scratch dirs land in the fixture, never the developer's real home.
	// Workers are stopped before the env is restored (defers run LIFO).
	priorHome, hadHome := os.LookupEnv("WUPHF_RUNTIME_HOME")
	if err := os.Setenv("WUPHF_RUNTIME_HOME", fx.scratchDir); err != nil {
		return err
	}
	defer func() {
		if hadHome {
			_ = os.Setenv("WUPHF_RUNTIME_HOME", priorHome)
		} else {
			_ = os.Unsetenv("WUPHF_RUNTIME_HOME")
		}
	}()
	defer fx.launcher.stopHeadlessWorkers()

	runtimeHome := normalizeHeadlessWorkspaceDir(fx.scratchDir)
	processCwd, _ := os.Getwd()
	processCwd = normalizeHeadlessWorkspaceDir(processCwd)

	// The exact routes the FE drives, behind the same auth middleware.
	mux := http.NewServeMux()
	mux.HandleFunc("/tasks", fx.broker.requireAuth(fx.broker.handleTasks))
	mux.HandleFunc("/tasks/", fx.broker.requireAuth(fx.broker.handleTaskByID))
	srv := httptest.NewServer(mux)
	defer srv.Close()
	client := &livePathsClient{srv: srv, token: fx.broker.Token()}

	createTask := func(title, details, owner string) (id, channel string, err error) {
		status, raw, err := client.postJSON("/tasks", map[string]any{
			"action": "create", "channel": "general", "title": title,
			"details": details, "owner": owner, "created_by": "ceo",
		})
		if err != nil {
			return "", "", fmt.Errorf("create %q: %w", title, err)
		}
		if status != http.StatusOK {
			return "", "", fmt.Errorf("create %q: status=%d body=%s", title, status, raw)
		}
		var parsed struct {
			Task struct {
				ID      string `json:"id"`
				Channel string `json:"channel"`
			} `json:"task"`
		}
		if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
			return "", "", err
		}
		return parsed.Task.ID, parsed.Task.Channel, nil
	}

	// Park-stub the runner: the queue still computes the launch params
	// (working directory record) on the real dispatch path; the stub only
	// replaces the provider subprocess.
	woke := make(chan struct{}, 4)
	stub := func(_ *Launcher, ctx context.Context, _, _ string, _ ...string) error {
		select {
		case woke <- struct{}{}:
		default:
		}
		<-ctx.Done()
		return ctx.Err()
	}
	prior := headlessCodexRunTurnOverride.Load()
	headlessCodexRunTurnOverride.Store(&stub)
	defer headlessCodexRunTurnOverride.Store(prior)

	activeWorkspaceDir := func(taskID string) string {
		fx.launcher.headless.mu.Lock()
		defer fx.launcher.headless.mu.Unlock()
		for _, active := range fx.launcher.headless.active {
			if active != nil && strings.TrimSpace(active.Turn.TaskID) == strings.TrimSpace(taskID) {
				return strings.TrimSpace(active.WorkspaceDir)
			}
		}
		return ""
	}
	awaitWake := func() bool {
		select {
		case <-woke:
			return true
		case <-time.After(8 * time.Second):
			return false
		}
	}

	// ── (a) task turn WITHOUT a worktree: working dir inside runtime home ──
	aID, aChannel, err := createTask("Draft the launch brief (workspace a)", "Office-mode probe work — no worktree.", "eng")
	if err != nil {
		return err
	}
	aTask := fx.broker.TaskByID(aID)
	if aTask == nil {
		return fmt.Errorf("task %s vanished after create", aID)
	}
	fx.launcher.sendTaskUpdate(
		notificationTarget{Slug: "eng"},
		officeActionLog{Kind: "task_updated", Actor: "ceo", Channel: aChannel, RelatedID: aID},
		*aTask,
		"Work packet for the launch brief.",
	)
	awaitWake()
	aDir := activeWorkspaceDir(aID)
	r.add(job, "task turn without a worktree runs inside the runtime home, never the process cwd",
		strings.TrimSpace(aTask.WorktreePath) == "" && aDir != "" &&
			pathWithinRoot(aDir, runtimeHome) && aDir != processCwd,
		fmt.Sprintf("worktree=%q working_dir=%q runtime_home=%q process_cwd=%q", aTask.WorktreePath, aDir, runtimeHome, processCwd), "")

	// ── (b) chat turn (no task): per-agent scratch dir ─────────────────────
	fx.launcher.enqueueHeadlessCodexTurnRecord("eng", headlessCodexTurn{
		Prompt:    "Quick question from the human in #general — no task attached.",
		Channel:   "general",
		FromHuman: true,
	})
	awaitWake()
	bDir := activeWorkspaceDir("")
	wantScratch := normalizeHeadlessWorkspaceDir(filepath.Join(fx.scratchDir, ".wuphf", "agent-scratch", "eng"))
	r.add(job, "chat turn (no task) runs in the agent's scratch dir under the runtime home",
		bDir != "" && bDir == wantScratch && pathWithinRoot(bDir, runtimeHome) && bDir != processCwd,
		fmt.Sprintf("working_dir=%q want=%q process_cwd=%q", bDir, wantScratch, processCwd), "")
	fx.launcher.stopHeadlessWorkers()

	// ── (c) the J3 done-means-done chain, end-to-end over HTTP ────────────
	jID, jChannel, err := createTask(
		"Build a single-page landing page for MeetingMind",
		"Ship the MeetingMind landing page. Definition of done: a file landing/index.html exists. Don't tell me it's done unless that check passes.",
		"ceo",
	)
	if err != nil {
		return err
	}
	created := fx.broker.TaskByID(jID)
	derived := created != nil && created.Verification != nil && created.Verification.Required &&
		created.Verification.Kind == taskVerificationKindCommand &&
		strings.Contains(created.Verification.Spec, "landing/index.html")
	r.add(job, "J3: verbatim DoD phrasing derives a required machine check at create",
		derived, fmt.Sprintf("verification=%+v", created.Verification), "")

	// No activation step: the create above landed the task running —
	// the DoD gate is what stands between the agent and "done" now.

	// Complete WITHOUT the deliverable: the DoD gate must refuse with a
	// structured verification failure — not pass because a stale
	// landing/index.html happens to exist in the broker process cwd.
	missStatus, missBody, err := client.postJSON("/tasks", map[string]any{
		"action": "complete", "id": jID, "channel": jChannel, "created_by": "ceo",
	})
	if err != nil {
		return err
	}
	missTask := fx.broker.TaskByID(jID)
	r.add(job, "J3: complete without the deliverable fails the DoD check with 409",
		missStatus == http.StatusConflict && strings.Contains(missBody, "definition-of-done check failed") &&
			missTask != nil && missTask.VerificationResult != nil && !missTask.VerificationResult.Pass &&
			!strings.EqualFold(strings.TrimSpace(missTask.status), "done"),
		fmt.Sprintf("status=%d body=%s result=%+v", missStatus, truncate(missBody, 160), missTask.VerificationResult), "")

	// Produce the deliverable where the owner's turns actually run: the
	// agent scratch dir (the task has no worktree). This is the isolation
	// contract — the gate must look at the agent's working dir.
	deliverable := filepath.Join(agentScratchDir("ceo"), "landing", "index.html")
	if err := os.MkdirAll(filepath.Dir(deliverable), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(deliverable, []byte("<!doctype html><title>MeetingMind</title>"), 0o644); err != nil {
		return err
	}
	hitStatus, hitBody, err := client.postJSON("/tasks", map[string]any{
		"action": "complete", "id": jID, "channel": jChannel, "created_by": "ceo",
	})
	if err != nil {
		return err
	}
	r.add(job, "J3: complete succeeds once the deliverable exists in the agent's working dir",
		hitStatus == http.StatusOK,
		fmt.Sprintf("status=%d body=%s", hitStatus, truncate(hitBody, 160)), "")

	// Verified state visible on the task detail endpoint (the surface the
	// FE pill reads).
	_, detailBody, err := client.getJSON("/tasks/" + jID)
	if err != nil {
		return err
	}
	var detail struct {
		Task struct {
			LifecycleState     string                  `json:"lifecycle_state"`
			VerificationResult *TaskVerificationResult `json:"verification_result"`
		} `json:"task"`
	}
	_ = json.Unmarshal([]byte(detailBody), &detail)
	r.add(job, "J3: Verified (passing check) is visible on the task detail endpoint",
		detail.Task.VerificationResult != nil && detail.Task.VerificationResult.Pass &&
			detail.Task.LifecycleState != string(LifecycleStateDrafting),
		fmt.Sprintf("lifecycle=%s result=%+v", detail.Task.LifecycleState, detail.Task.VerificationResult), "")

	// ── (d) Halt note packet carries the do-not-revert instruction ────────
	dID, dChannel, err := createTask("Refresh the pricing page copy (workspace d)", "Probe for the Stop contract.", "ceo")
	if err != nil {
		return err
	}
	if _, err := fx.broker.PostMessage("you", dChannel, "Stop — leave it exactly as it is.", nil, ""); err != nil {
		return err
	}
	dTask := fx.broker.TaskByID(dID)
	halted := dTask != nil && dTask.HumanNotePending != nil && dTask.HumanNotePending.Halt
	packet := ""
	if dTask != nil {
		packet = fx.launcher.notifyCtx().BuildTaskExecutionPacket("ceo", officeActionLog{Actor: "ceo"}, *dTask, "continue")
	}
	r.add(job, "Halt note's packet says do-not-revert: pause, report state, wait",
		halted && strings.Contains(packet, "Do NOT discard or revert any work. Report state and wait.") &&
			strings.Contains(packet, "STOP order"),
		fmt.Sprintf("halted=%v packetHead=%q", halted, truncate(packet, 200)), "")
	return nil
}
