package team

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"
)

// TestNormalizeSchedulerSlug_StripsAndJoinsParts pins the slug
// composition rule: empty parts are dropped, surviving parts are
// joined with ":". Drift here would cause the scheduler dedupe path
// (schedulerJobMatches) to silently lose its key invariants.
func TestNormalizeSchedulerSlug_StripsAndJoinsParts(t *testing.T) {
	cases := []struct {
		in   []string
		want string
	}{
		// Underscores collapse to dashes inside each part.
		{[]string{"task_follow_up", "general", "task-1"}, "task-follow-up:general:task-1"},
		{[]string{"  Task Follow Up  ", "", "task-1"}, "task-follow-up:task-1"},
		{[]string{"", "", ""}, ""},
		{[]string{"only"}, "only"},
	}
	for _, tc := range cases {
		if got := normalizeSchedulerSlug(tc.in...); got != tc.want {
			t.Errorf("normalizeSchedulerSlug(%v): want %q, got %q", tc.in, tc.want, got)
		}
	}
}

// TestSchedulerJobDue_BoundaryAtExactNow pins the comparison semantics
// of schedulerJobDue: a job whose DueAt or NextRun equals now (to the
// second) MUST count as due, not future. The handler treats !After as
// due, so tests must ride that exact boundary.
func TestSchedulerJobDue_BoundaryAtExactNow(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	exact := schedulerJob{Status: "scheduled", DueAt: now.Format(time.RFC3339)}
	if !schedulerJobDue(exact, now) {
		t.Error("DueAt == now: expected due=true")
	}

	future := schedulerJob{Status: "scheduled", DueAt: now.Add(time.Second).Format(time.RFC3339)}
	if schedulerJobDue(future, now) {
		t.Error("DueAt > now: expected due=false")
	}

	past := schedulerJob{Status: "scheduled", DueAt: now.Add(-time.Hour).Format(time.RFC3339)}
	if !schedulerJobDue(past, now) {
		t.Error("DueAt past: expected due=true")
	}

	// Done/canceled never count as due regardless of timestamps.
	done := schedulerJob{Status: "done", DueAt: now.Format(time.RFC3339)}
	if schedulerJobDue(done, now) {
		t.Error("Status=done: expected due=false")
	}
	canceled := schedulerJob{Status: "Canceled", NextRun: now.Format(time.RFC3339)}
	if schedulerJobDue(canceled, now) {
		t.Error("Status=Canceled: expected due=false (case-insensitive)")
	}
}

// TestCompleteSchedulerJobsLocked_NoOpForUnknownTarget pins that
// completing jobs for a target that has none does not mutate b.scheduler
// — important for the broker-shutdown / task-completion flows that may
// fire spuriously when no matching job exists.
func TestCompleteSchedulerJobsLocked_NoOpForUnknownTarget(t *testing.T) {
	b := newTestBroker(t)
	if err := b.SetSchedulerJob(schedulerJob{
		Slug: "task-follow-up:general:task-1", Kind: "task_follow_up",
		TargetType: "task", TargetID: "task-1", Channel: "general",
		Status: "scheduled",
	}); err != nil {
		t.Fatalf("SetSchedulerJob: %v", err)
	}
	b.mu.Lock()
	before := append([]schedulerJob(nil), b.scheduler...)
	b.completeSchedulerJobsLocked("task", "ghost-task", "general")
	after := append([]schedulerJob(nil), b.scheduler...)
	b.mu.Unlock()

	if len(before) != len(after) {
		t.Fatalf("scheduler length changed: before=%d after=%d", len(before), len(after))
	}
	if before[0].Status != after[0].Status {
		t.Errorf("status flipped on unknown-target call: before=%q after=%q", before[0].Status, after[0].Status)
	}
}

// TestRunSchedulerJob_TriggersJobAndUpdatesLastRun verifies that
// POST /scheduler/{slug}/run returns triggered=true, records LastRun,
// and does NOT advance NextRun (the recurring schedule is unaffected).
func TestRunSchedulerJob_TriggersJobAndUpdatesLastRun(t *testing.T) {
	b := newTestBroker(t)
	nextRun := time.Now().UTC().Add(30 * time.Minute).Format(time.RFC3339)
	if err := b.SetSchedulerJob(schedulerJob{
		Slug:            "nex-notifications",
		Label:           "Nex notifications",
		IntervalMinutes: 10,
		NextRun:         nextRun,
		Status:          "sleeping",
		SystemManaged:   true,
		Enabled:         true,
	}); err != nil {
		t.Fatalf("SetSchedulerJob: %v", err)
	}
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("failed to start broker: %v", err)
	}
	defer b.Stop()

	base := fmt.Sprintf("http://%s", b.Addr())
	req, _ := http.NewRequest(http.MethodPost, base+"/scheduler/nex-notifications/run", nil)
	req.Header.Set("Authorization", "Bearer "+b.Token())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("run request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var body struct {
		Triggered bool   `json:"triggered"`
		Slug      string `json:"slug"`
		At        string `json:"at"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !body.Triggered {
		t.Error("expected triggered=true")
	}
	if body.Slug != "nex-notifications" {
		t.Errorf("expected slug=nex-notifications, got %q", body.Slug)
	}
	if body.At == "" {
		t.Error("expected non-empty at timestamp")
	}

	// Verify NextRun is unchanged — force-run must not clobber the schedule.
	b.mu.Lock()
	var found *schedulerJob
	for i := range b.scheduler {
		if b.scheduler[i].Slug == "nex-notifications" {
			found = &b.scheduler[i]
			break
		}
	}
	b.mu.Unlock()
	if found == nil {
		t.Fatal("job not found after run")
	}
	if found.NextRun != nextRun {
		t.Errorf("NextRun mutated: want %q, got %q", nextRun, found.NextRun)
	}
	if found.LastRun != body.At {
		t.Errorf("LastRun mismatch: want %q, got %q", body.At, found.LastRun)
	}
	if found.LastRunStatus != "triggered" {
		t.Errorf("LastRunStatus mismatch: want %q, got %q", "triggered", found.LastRunStatus)
	}
}

// TestRunSchedulerJob_UnknownSlugReturns404 ensures that triggering a
// non-existent slug returns 404 rather than silently succeeding.
func TestRunSchedulerJob_UnknownSlugReturns404(t *testing.T) {
	b := newTestBroker(t)
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("failed to start broker: %v", err)
	}
	defer b.Stop()

	base := fmt.Sprintf("http://%s", b.Addr())
	req, _ := http.NewRequest(http.MethodPost, base+"/scheduler/does-not-exist/run", nil)
	req.Header.Set("Authorization", "Bearer "+b.Token())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("run request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

// TestRunSchedulerJob_WrongMethodReturns405 ensures that GET/PATCH on the
// /run path returns 405 instead of being routed to the PATCH handler.
func TestRunSchedulerJob_WrongMethodReturns405(t *testing.T) {
	b := newTestBroker(t)
	if err := b.SetSchedulerJob(schedulerJob{
		Slug:    "nex-notifications",
		Label:   "Nex notifications",
		Status:  "sleeping",
		Enabled: true,
	}); err != nil {
		t.Fatalf("SetSchedulerJob: %v", err)
	}
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("failed to start broker: %v", err)
	}
	defer b.Stop()

	base := fmt.Sprintf("http://%s", b.Addr())
	for _, method := range []string{http.MethodGet, http.MethodPatch} {
		req, _ := http.NewRequest(method, base+"/scheduler/nex-notifications/run", nil)
		req.Header.Set("Authorization", "Bearer "+b.Token())
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s /run failed: %v", method, err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusMethodNotAllowed {
			t.Errorf("%s /run: expected 405, got %d", method, resp.StatusCode)
		}
	}
}

// TestRunSchedulerJob_WorkflowJobBumpsNextRun verifies that force-triggering a
// workflow-type cron sets next_run to now (making it due on the next scheduler
// poll) while system crons leave next_run untouched.
func TestRunSchedulerJob_WorkflowJobBumpsNextRun(t *testing.T) {
	b := newTestBroker(t)
	futureNextRun := time.Now().UTC().Add(60 * time.Minute).Format(time.RFC3339)
	if err := b.SetSchedulerJob(schedulerJob{
		Slug:        "weekly-digest",
		Label:       "Weekly digest",
		TargetType:  "workflow",
		WorkflowKey: "weekly-digest-wf",
		NextRun:     futureNextRun,
		Status:      "sleeping",
		Enabled:     true,
	}); err != nil {
		t.Fatalf("SetSchedulerJob: %v", err)
	}
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("failed to start broker: %v", err)
	}
	defer b.Stop()

	before := time.Now().UTC()

	base := fmt.Sprintf("http://%s", b.Addr())
	req, _ := http.NewRequest(http.MethodPost, base+"/scheduler/weekly-digest/run", nil)
	req.Header.Set("Authorization", "Bearer "+b.Token())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("run request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	b.mu.Lock()
	var found *schedulerJob
	for i := range b.scheduler {
		if b.scheduler[i].Slug == "weekly-digest" {
			found = &b.scheduler[i]
			break
		}
	}
	b.mu.Unlock()
	if found == nil {
		t.Fatal("job not found after run")
	}
	// next_run must have been moved to ≤ now so the scheduler sees it as due.
	nextRun, err := time.Parse(time.RFC3339, found.NextRun)
	if err != nil {
		t.Fatalf("parse NextRun %q: %v", found.NextRun, err)
	}
	if nextRun.After(before) {
		t.Errorf("workflow NextRun not bumped: got %q, want <= %q", found.NextRun, before.Format(time.RFC3339))
	}
}

func TestSchedulerDueOnlyFiltersFutureJobs(t *testing.T) {
	b := newTestBroker(t)
	if err := b.SetSchedulerJob(schedulerJob{
		Slug:            "task-follow-up:general:task-1",
		Kind:            "task_follow_up",
		Label:           "Follow up",
		TargetType:      "task",
		TargetID:        "task-1",
		Channel:         "general",
		IntervalMinutes: 15,
		DueAt:           time.Now().UTC().Add(10 * time.Minute).Format(time.RFC3339),
		NextRun:         time.Now().UTC().Add(10 * time.Minute).Format(time.RFC3339),
		Status:          "scheduled",
	}); err != nil {
		t.Fatalf("SetSchedulerJob failed: %v", err)
	}
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("failed to start broker: %v", err)
	}
	defer b.Stop()

	base := fmt.Sprintf("http://%s", b.Addr())
	req, _ := http.NewRequest(http.MethodGet, base+"/scheduler?due_only=true", nil)
	req.Header.Set("Authorization", "Bearer "+b.Token())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("scheduler request failed: %v", err)
	}
	defer resp.Body.Close()
	var listing struct {
		Jobs []schedulerJob `json:"jobs"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&listing); err != nil {
		t.Fatalf("decode scheduler list: %v", err)
	}
	if len(listing.Jobs) != 0 {
		t.Fatalf("expected future job to be filtered out, got %+v", listing.Jobs)
	}
}
