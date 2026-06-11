package team

// office_eval_jobs_platform_honesty.go — Wave F2 (ten-out-of-ten): the
// platform must be honest about silent failure. Two contracts:
//
//  1. A RUNNING task with no observable trace (no non-system message, no
//     non-system action, no ledger bump) past taskStallThreshold gets a
//     visible stall marker (teamTask.StalledSince) plus exactly ONE honest
//     chat line in its channel — and the marker clears when activity
//     resumes. ICP-eval v3 [19:05:30]: 21 minutes running-but-silent with
//     no signal to the human.
//
//  2. A turn killed from outside (signal: killed) posts one human-readable
//     system note instead of leaving raw signal exhaust as the only trace.
//
// Both checks drive the REAL watchdog/recovery helpers with a fixture
// clock — no sleeps, no LLM.

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

func (fx *officeEvalFixture) taskByID(id string) (teamTask, bool) {
	fx.broker.mu.Lock()
	defer fx.broker.mu.Unlock()
	if t := fx.broker.findTaskByIDLocked(id); t != nil {
		return cloneTeamTaskForRollback(*t), true
	}
	return teamTask{}, false
}

func (fx *officeEvalFixture) channelMessagesByKind(channel, kind string) []channelMessage {
	fx.broker.mu.Lock()
	defer fx.broker.mu.Unlock()
	var out []channelMessage
	for _, m := range fx.broker.messages {
		if normalizeChannelSlug(m.Channel) != normalizeChannelSlug(channel) {
			continue
		}
		if kind != "" && m.Kind != kind {
			continue
		}
		out = append(out, m)
	}
	return out
}

func evalJobPlatformHonesty(fx *officeEvalFixture, r *OfficeEvalReport) error {
	const job = "platform-honesty"

	// ── (1) silent-stall marker ─────────────────────────────────────────
	created, err := fx.broker.MutateTask(TaskPostRequest{
		Action:    "create",
		Channel:   "general",
		Title:     "Research the launch plan",
		Details:   "Long-running research task used to exercise the stall watchdog.",
		Owner:     "eng",
		CreatedBy: "ceo",
		TaskType:  "issue",
	})
	if err != nil {
		return fmt.Errorf("create task: %w", err)
	}
	taskID := created.Task.ID
	if err := fx.activateTask(taskID); err != nil {
		return fmt.Errorf("activate task: %w", err)
	}
	running, ok := fx.taskByID(taskID)
	if !ok {
		return fmt.Errorf("task %s vanished after activation", taskID)
	}
	r.add(job, "fixture task is running before the sweep",
		taskIsObservablyRunning(&running) && running.StalledSince == "",
		fmt.Sprintf("state=%s stalled_since=%q", running.LifecycleState, running.StalledSince), "")

	// Fixture clock: one minute past the stall threshold relative to the
	// newest real trace. The sweep runs the same code the production
	// watchdog tick runs.
	stallNow := time.Now().UTC().Add(taskStallThreshold + time.Minute)
	fx.broker.mu.Lock()
	changed := fx.broker.markSilentRunningTasksLocked(stallNow, taskStallThreshold)
	fx.broker.mu.Unlock()

	stalled, _ := fx.taskByID(taskID)
	r.add(job, "silent running task gets the stall marker",
		changed && stalled.StalledSince != "",
		fmt.Sprintf("changed=%v stalled_since=%q", changed, stalled.StalledSince), "")

	notes := fx.channelMessagesByKind(stalled.Channel, taskStalledMessageKind)
	lineOK := len(notes) == 1 &&
		strings.Contains(notes[0].Content, "@eng") &&
		strings.Contains(notes[0].Content, "no visible activity") &&
		strings.Contains(notes[0].Content, taskID)
	detail := fmt.Sprintf("lines=%d", len(notes))
	if len(notes) > 0 {
		detail += " first=" + truncate(notes[0].Content, 120)
	}
	r.add(job, "stall posts one honest chat line naming the owner", lineOK, detail, "")

	// Second sweep while still silent: marker holds, no duplicate line.
	fx.broker.mu.Lock()
	fx.broker.markSilentRunningTasksLocked(stallNow.Add(time.Minute), taskStallThreshold)
	fx.broker.mu.Unlock()
	notes = fx.channelMessagesByKind(stalled.Channel, taskStalledMessageKind)
	r.add(job, "repeat sweep does not duplicate the stall line",
		len(notes) == 1, fmt.Sprintf("lines=%d", len(notes)), "")

	// Fresh observable trace (a ledger bump, as a real turn produces)
	// clears the marker on the next sweep.
	fx.broker.AppendTaskLedgerEntry(taskID, TaskLedgerEntry{
		Agent:   "eng",
		At:      stallNow.Add(2 * time.Minute).Format(time.RFC3339),
		Outcome: "ok",
		Said:    "back to work — drafting the plan now",
	})
	fx.broker.mu.Lock()
	fx.broker.markSilentRunningTasksLocked(stallNow.Add(3*time.Minute), taskStallThreshold)
	fx.broker.mu.Unlock()
	resumed, _ := fx.taskByID(taskID)
	r.add(job, "fresh activity clears the stall marker",
		resumed.StalledSince == "",
		fmt.Sprintf("stalled_since=%q", resumed.StalledSince), "")

	// ── (2) killed-turn honesty ─────────────────────────────────────────
	killErr := fmt.Errorf("headless turn: %w", errors.New("signal: killed"))
	r.add(job, "kill signal is recognized as a killed turn",
		isTurnKilledError(killErr) && !isTurnKilledError(errors.New("exit status 1")),
		"", "")

	fx.launcher.postTurnKilledNote("eng", "general")
	killNotes := fx.channelMessagesByKind("general", "error")
	var killNote *channelMessage
	for i := range killNotes {
		if strings.Contains(killNotes[i].Content, "killed by the system") {
			killNote = &killNotes[i]
			break
		}
	}
	noteOK := killNote != nil &&
		killNote.From == "system" &&
		strings.Contains(killNote.Content, "@eng") &&
		!strings.Contains(killNote.Content, "signal: killed") &&
		!strings.Contains(killNote.Content, "exit status")
	noteDetail := "no kill note found"
	if killNote != nil {
		noteDetail = truncate(killNote.Content, 140)
	}
	r.add(job, "killed turn posts a human-readable system note", noteOK, noteDetail, "")

	// The detail that rides into retry prompts / block reasons must be the
	// humanized line, not raw exhaust.
	humanDetail := turnKilledHumanDetail("eng")
	r.add(job, "recovery detail for killed turns is humanized",
		strings.Contains(humanDetail, "@eng") && !strings.Contains(humanDetail, "signal:"),
		truncate(humanDetail, 140), "")

	return nil
}
