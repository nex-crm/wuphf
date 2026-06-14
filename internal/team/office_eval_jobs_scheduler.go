package team

// office_eval_jobs_scheduler.go — the `scheduler-truth` eval job
// (ten-out-of-ten Wave D / D1).
//
// ICP-eval v3 [18:30–18:36]: one "weekly Monday 9am summary" ask produced
// TWO provider-session cron jobs from two agents, each with a "dies in 7
// days with the session" caveat, and the Scheduled Tasks app showed
// neither. Standing automations must be REAL (run through the broker
// scheduler), PERSISTENT (survive a state save/load round-trip), VISIBLE
// (returned by GET /scheduler, the Scheduled Tasks app's data source), and
// DEDUPED (same normalized purpose+schedule updates instead of
// duplicating).
//
// This job drives the exact wire the team_routine MCP tool speaks
// (POST /scheduler/routines behind the broker auth middleware) — the live
// layer, not the intended in-process path.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
)

// schedulerJobWire is the slice of the GET /scheduler job shape these
// checks read. Field names pin the wire contract the Scheduled Tasks app
// (web/src/api/scheduler.ts + schedulerJobClassification.ts) consumes.
type schedulerJobWire struct {
	Slug          string `json:"slug"`
	Kind          string `json:"kind"`
	Label         string `json:"label"`
	TargetType    string `json:"target_type"`
	TargetID      string `json:"target_id"`
	Channel       string `json:"channel"`
	ScheduleExpr  string `json:"schedule_expr"`
	Payload       string `json:"payload"`
	NextRun       string `json:"next_run"`
	Enabled       bool   `json:"enabled"`
	SystemManaged bool   `json:"system_managed"`
}

func decodeSchedulerJobs(body string) ([]schedulerJobWire, error) {
	var parsed struct {
		Jobs []schedulerJobWire `json:"jobs"`
	}
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		return nil, err
	}
	return parsed.Jobs, nil
}

func evalJobSchedulerTruth(fx *officeEvalFixture, r *OfficeEvalReport) error {
	const job = "scheduler-truth"

	// Serve the scheduler routes exactly as the live broker does: same
	// handlers, same auth middleware, same paths the MCP tool and the
	// Scheduled Tasks app hit.
	mux := http.NewServeMux()
	mux.HandleFunc("/scheduler", fx.broker.requireAuth(fx.broker.handleScheduler))
	mux.HandleFunc("/scheduler/", fx.broker.requireAuth(fx.broker.handleSchedulerSubpath))
	srv := httptest.NewServer(mux)
	defer srv.Close()
	client := &livePathsClient{srv: srv, token: fx.broker.Token()}

	// ── (a) An agent registers a standing automation over the live wire ──
	status, raw, err := client.postJSON("/scheduler/routines", map[string]any{
		"purpose":    "Weekly Monday 9am renewal risk summary to #general",
		"schedule":   "0 9 * * 1",
		"channel":    "general",
		"owner":      "eng",
		"prompt":     "Pull all accounts with renewals within 60 days, score by risk, post a one-line-per-account summary.",
		"created_by": "ceo",
	})
	if err != nil {
		return fmt.Errorf("register routine: %w", err)
	}
	var created struct {
		Job     schedulerJobWire `json:"job"`
		Updated bool             `json:"updated"`
	}
	if err := json.Unmarshal([]byte(raw), &created); err != nil {
		return fmt.Errorf("decode register response: %w (body=%s)", err, raw)
	}
	r.add(job, "agent cron registration lands in the broker scheduler",
		status == http.StatusCreated && !created.Updated && created.Job.Slug != "" &&
			created.Job.NextRun != "" && created.Job.Enabled,
		fmt.Sprintf("status=%d slug=%q next_run=%q", status, created.Job.Slug, created.Job.NextRun), "")

	// The Scheduled Tasks app reads GET /scheduler and classifies a USER
	// routine as: not system-managed, agent-targeted, with a recurring
	// trigger (schedulerJobClassification.ts). The registered automation
	// must satisfy that classification or it renders in the hidden system
	// drawer — the v3 "Scheduled Tasks 0" failure.
	status, raw, err = client.getJSON("/scheduler")
	if err != nil {
		return fmt.Errorf("list scheduler: %w", err)
	}
	jobs, err := decodeSchedulerJobs(raw)
	if err != nil {
		return fmt.Errorf("decode scheduler list: %w", err)
	}
	var listed *schedulerJobWire
	for i := range jobs {
		if jobs[i].Slug == created.Job.Slug {
			listed = &jobs[i]
			break
		}
	}
	visible := status == http.StatusOK && listed != nil &&
		!listed.SystemManaged &&
		strings.EqualFold(listed.TargetType, "agent") && listed.TargetID == "eng" &&
		strings.TrimSpace(listed.ScheduleExpr) != "" && listed.Enabled
	detail := "job missing from GET /scheduler"
	if listed != nil {
		detail = fmt.Sprintf("system_managed=%v target_type=%q target_id=%q schedule_expr=%q enabled=%v",
			listed.SystemManaged, listed.TargetType, listed.TargetID, listed.ScheduleExpr, listed.Enabled)
	}
	r.add(job, "automation is visible as a user routine in the Scheduled Tasks data source", visible, detail, "")

	// ── persistence: survives a broker state save/load round-trip ──
	// The registration path saved to broker-state.json; a fresh broker at
	// the same path must come up with the routine intact. This is the
	// "survives restart / session end" contract the v3 session-scoped
	// cron path broke.
	// The in-package test suite disables auto-load on construct
	// (test_support.go); re-enable it for this reload so the eval measures
	// the production load path in both `go test` and `go run
	// ./cmd/office-eval` (where the flip is a no-op). The team suite runs
	// serially, so toggling the global is safe — same pattern as
	// broker_phase6_migration_test.go.
	prevSkip := skipBrokerStateLoadOnConstruct
	skipBrokerStateLoadOnConstruct = false
	reloaded := NewBrokerAt(filepath.Join(fx.scratchDir, "broker-state.json"))
	skipBrokerStateLoadOnConstruct = prevSkip
	defer reloaded.Stop()
	reloaded.mu.Lock()
	var persisted *schedulerJob
	for i := range reloaded.scheduler {
		if reloaded.scheduler[i].Slug == created.Job.Slug {
			cp := reloaded.scheduler[i]
			persisted = &cp
			break
		}
	}
	reloaded.mu.Unlock()
	r.add(job, "automation survives a broker state save/load round-trip",
		persisted != nil && persisted.ScheduleExpr == "0 9 * * 1" &&
			persisted.TargetType == "agent" && persisted.TargetID == "eng" && persisted.Enabled,
		fmt.Sprintf("persisted=%+v", persisted), "")

	// ── (b) duplicate registration converges instead of duplicating ──
	// A SECOND agent registers the same automation with cosmetically
	// different wording (punctuation, case, word order) and the same
	// schedule — the v3 failure mode (CEO + Outreach each minted a cron
	// for one ask). The broker must update the existing job, not append.
	status, raw, err = client.postJSON("/scheduler/routines", map[string]any{
		"purpose":    "Monday 9am weekly renewal-risk summary to #general!",
		"schedule":   "0 9 * * 1",
		"channel":    "general",
		"owner":      "eng",
		"prompt":     "Updated instructions: include champion-stability in the risk score.",
		"created_by": "outreach",
	})
	if err != nil {
		return fmt.Errorf("re-register routine: %w", err)
	}
	var updated struct {
		Job     schedulerJobWire `json:"job"`
		Updated bool             `json:"updated"`
	}
	if err := json.Unmarshal([]byte(raw), &updated); err != nil {
		return fmt.Errorf("decode re-register response: %w (body=%s)", err, raw)
	}
	r.add(job, "same purpose+schedule re-registration reports an update, not a new job",
		status == http.StatusOK && updated.Updated && updated.Job.Slug == created.Job.Slug &&
			strings.Contains(updated.Job.Payload, "champion-stability"),
		fmt.Sprintf("status=%d updated=%v slug=%q", status, updated.Updated, updated.Job.Slug), "")

	status, raw, err = client.getJSON("/scheduler")
	if err != nil {
		return fmt.Errorf("list scheduler after dedupe: %w", err)
	}
	jobs, err = decodeSchedulerJobs(raw)
	if err != nil {
		return fmt.Errorf("decode scheduler list after dedupe: %w", err)
	}
	wantNorm := normalizeRoutinePurpose("Weekly Monday 9am renewal risk summary to #general")
	matches := 0
	for _, j := range jobs {
		if !j.SystemManaged && strings.EqualFold(j.TargetType, "agent") &&
			normalizeRoutinePurpose(j.Label) == wantNorm {
			matches++
		}
	}
	r.add(job, "one ask yields exactly one standing automation",
		status == http.StatusOK && matches == 1,
		fmt.Sprintf("matching agent routines=%d (want 1)", matches), "")

	// ── (c) the persistent path actually FIRES through the scheduler ──
	// Force the job due and drive one watchdog tick: the routine must
	// post into its channel tagging the owner — proof the registration
	// is wired to a real dispatcher, not a row that never runs.
	fx.broker.mu.Lock()
	for i := range fx.broker.scheduler {
		if fx.broker.scheduler[i].Slug == created.Job.Slug {
			fx.broker.scheduler[i].NextRun = "2000-01-01T00:00:00Z"
			fx.broker.scheduler[i].DueAt = "2000-01-01T00:00:00Z"
		}
	}
	fx.broker.mu.Unlock()
	w := &watchdogScheduler{broker: fx.broker, clock: realClock{}}
	w.processOnce()
	fired := false
	for _, msg := range fx.broker.ChannelMessages("general") {
		if strings.Contains(msg.Content, "@eng") && strings.Contains(msg.Content, "champion-stability") {
			fired = true
			break
		}
	}
	r.add(job, "due automation fires through the office scheduler into its channel", fired,
		"forced next_run into the past, ran one scheduler tick, expected the routine prompt tagged at @eng in #general", "")
	return nil
}
