package team

// Operator agent-routines: a scheduler job whose TargetID is a CUSTOM APP id
// (app_<16hex>) fires against the operator agent service (the pi-mono agent/
// package), not an office channel. The service runs the routine's prompt in
// the routine's pi chat session (POST /routines/run) and reports the outcome,
// which lands in this registry's per-slug run ring like any other fire.
//
// The DIVISION OF LABOR is deliberate: cron parsing, enable/disable, revision
// history (versioning with change notes), and run/activity history all live in
// THIS registry — the previous product avatar's proven engine. The agent
// service holds no routine definitions; it only executes a fired prompt and
// persists the transcript into the routine's pi session.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// operatorRoutineCallTimeout bounds one fire's HTTP round-trip. Tool runs can
// legitimately take tens of seconds (capability calls); the fire runs in its
// own goroutine so this never blocks the watchdog's dispatch loop.
const operatorRoutineCallTimeout = 90 * time.Second

// operatorAgentBaseURL resolves the operator agent service. WUPHF_AGENT_URL
// overrides; the default matches the agent/ package's dev port.
func operatorAgentBaseURL() string {
	if v := strings.TrimSpace(os.Getenv("WUPHF_AGENT_URL")); v != "" {
		return strings.TrimRight(v, "/")
	}
	return "http://127.0.0.1:8820"
}

// isOperatorAgentTarget reports whether a routine's owner is a custom app —
// the discriminator between operator dispatch and office channel-post
// dispatch inside processAgentJob.
func isOperatorAgentTarget(id string) bool {
	return validateCustomAppID(strings.TrimSpace(id)) == nil
}

// operatorRoutineRunResult mirrors agent/src/service.ts POST /routines/run.
type operatorRoutineRunResult struct {
	Status    string `json:"status"` // ok | needs_approval | error
	Digest    string `json:"digest"`
	SessionID string `json:"session_id"`
	Error     string `json:"error"`
}

// postOperatorRoutineRun fires one routine at the agent service.
func postOperatorRoutineRun(baseURL, appID, slug, name, prompt string) (operatorRoutineRunResult, error) {
	body, err := json.Marshal(map[string]any{
		"schema_version": 1,
		"agent":          appID,
		"slug":           slug,
		"name":           name,
		"prompt":         prompt,
	})
	if err != nil {
		return operatorRoutineRunResult{}, fmt.Errorf("encode routine run: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), operatorRoutineCallTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/routines/run", bytes.NewReader(body))
	if err != nil {
		return operatorRoutineRunResult{}, fmt.Errorf("build routine run request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return operatorRoutineRunResult{}, fmt.Errorf("agent service unreachable: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return operatorRoutineRunResult{}, fmt.Errorf("read agent response: %w", err)
	}
	var out operatorRoutineRunResult
	if err := json.Unmarshal(raw, &out); err != nil {
		return operatorRoutineRunResult{}, fmt.Errorf("decode agent response (%d): %w", resp.StatusCode, err)
	}
	if resp.StatusCode != http.StatusOK {
		msg := strings.TrimSpace(out.Error)
		if msg == "" {
			msg = strings.TrimSpace(string(raw))
		}
		return operatorRoutineRunResult{}, fmt.Errorf("agent service %d: %s", resp.StatusCode, msg)
	}
	return out, nil
}

// processOperatorRoutineJob fires an operator routine. It re-arms NextRun
// BEFORE the HTTP call (the call can outlive several watchdog ticks; a due
// job must not double-fire meanwhile), then completes the run record from a
// goroutine when the agent service answers.
func (w *watchdogScheduler) processOperatorRoutineJob(job schedulerJob) {
	now := w.clock.Now().UTC()
	nextRun := nextRoutineRun(job, now)
	startedAt := now.Format(time.RFC3339)

	prompt := strings.TrimSpace(job.Payload)
	if prompt == "" {
		prompt = strings.TrimSpace(job.Label)
	}
	name := strings.TrimSpace(job.Label)
	if name == "" {
		name = job.Slug
	}
	if prompt == "" {
		_ = w.broker.CompleteSchedulerRun(job.Slug, nextRun, "scheduled", schedulerRun{
			Slug:        job.Slug,
			StartedAt:   startedAt,
			Status:      "failed",
			Message:     "Routine has no prompt — set the prompt in Edit",
			TriggeredBy: "scheduler",
			TargetType:  job.TargetType,
			TargetID:    job.TargetID,
			Events:      []string{"Skipped: empty prompt"},
		})
		return
	}

	// Re-arm first: the fire below runs async and can span multiple ticks.
	_ = w.broker.UpdateSchedulerJobState(job.Slug, nextRun, "scheduled")

	baseURL := operatorAgentBaseURL()
	events := []string{
		fmt.Sprintf("Scheduler tick at %s UTC", now.Format(time.RFC3339)),
		fmt.Sprintf("Running prompt in %s's chat via %s", job.TargetID, baseURL),
	}
	go func() {
		res, err := postOperatorRoutineRun(baseURL, strings.TrimSpace(job.TargetID), job.Slug, name, prompt)
		run := schedulerRun{
			Slug:        job.Slug,
			StartedAt:   startedAt,
			TriggeredBy: "scheduler",
			TargetType:  job.TargetType,
			TargetID:    job.TargetID,
		}
		switch {
		case err != nil:
			run.Status = "failed"
			run.Message = fmt.Sprintf("Routine fire failed: %v", err)
			run.ErrorDetail = err.Error()
			run.Events = append(events, fmt.Sprintf("Fire failed: %v", err))
		case res.Status == "error":
			run.Status = "failed"
			run.Message = "Routine ran with an error outcome"
			run.ErrorDetail = res.Digest
			run.Events = append(events, "Agent reported an error outcome", "Session "+res.SessionID)
		case res.Status == "needs_approval":
			// The run HAPPENED and paused at the send-gate — an ok fire whose
			// outcome awaits the human. It must not read as a failure.
			run.Status = "ok"
			run.OutputSummary = "Paused for approval: " + res.Digest
			run.Events = append(events, "Paused at the send-gate (approval required)", "Session "+res.SessionID)
		default:
			run.Status = "ok"
			run.OutputSummary = res.Digest
			run.Events = append(events, "Completed", "Session "+res.SessionID)
		}
		// Status "scheduled" (not "done") keeps the cron alive — same contract
		// as processAgentJob.
		_ = w.broker.CompleteSchedulerRun(job.Slug, nextRun, "scheduled", run)
		if w.operatorRunRecorded != nil {
			w.operatorRunRecorded(job.Slug)
		}
	}()
}
