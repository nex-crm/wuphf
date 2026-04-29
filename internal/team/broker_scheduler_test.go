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
