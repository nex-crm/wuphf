package team

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"testing"
	"time"
)

// PR 8 Lane G: cron registry tests. Five cases per the task spec, each
// exercising a different slice of the registry contract.

// startSchedulerTestBroker boots a broker on an ephemeral port and returns
// (b, base url). Caller defers b.Stop(). Mirrors what Broker.Start() does
// — registerSystemCrons + StartOnPort(0) — minus the fixed-port listen so
// parallel tests don't collide.
func startSchedulerTestBroker(t *testing.T) (*Broker, string) {
	t.Helper()
	b := newTestBroker(t)
	b.registerSystemCrons()
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("StartOnPort: %v", err)
	}
	return b, fmt.Sprintf("http://%s", b.Addr())
}

// patchScheduler sends PATCH /scheduler/{slug} and returns the (status, body).
func patchScheduler(t *testing.T, b *Broker, base, slug string, payload map[string]any) (int, []byte) {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	req, _ := http.NewRequest(http.MethodPatch, base+"/scheduler/"+slug, bytes.NewReader(raw))
	req.Header.Set("Authorization", "Bearer "+b.Token())
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PATCH /scheduler/%s: %v", slug, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, body
}

// TestSchedulerSelfRegistersSystemCrons asserts every spec'd system cron
// is visible in /scheduler GET after broker startup with SystemManaged=true
// and Enabled=true.
func TestSchedulerSelfRegistersSystemCrons(t *testing.T) {
	b, base := startSchedulerTestBroker(t)
	defer b.Stop()

	req, _ := http.NewRequest(http.MethodGet, base+"/scheduler", nil)
	req.Header.Set("Authorization", "Bearer "+b.Token())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /scheduler: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /scheduler: status %d", resp.StatusCode)
	}
	var listing struct {
		Jobs []schedulerJob `json:"jobs"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&listing); err != nil {
		t.Fatalf("decode: %v", err)
	}

	want := map[string]bool{
		"nex-insights":      false,
		"nex-notifications": false,
		"one-relay-events":  false,
		"request_follow_up": false,
		"review-expiry":     false,
		"task_follow_up":    false,
		"task_recheck":      false,
		"task_reminder":     false,
	}
	for _, job := range listing.Jobs {
		if _, ok := want[job.Slug]; !ok {
			continue
		}
		if !job.SystemManaged {
			t.Errorf("%s: SystemManaged=false, want true", job.Slug)
		}
		if !job.Enabled {
			t.Errorf("%s: Enabled=false, want true", job.Slug)
		}
		want[job.Slug] = true
	}
	for slug, found := range want {
		if !found {
			missing := make([]string, 0)
			for s, f := range want {
				if !f {
					missing = append(missing, s)
				}
			}
			sort.Strings(missing)
			t.Fatalf("system cron %q missing from /scheduler. all missing: %v", slug, missing)
		}
	}
}

// TestPatchScheduler_UpdatesIntervalOverride covers the happy path:
// PATCH a configurable cron with a valid interval_override, GET back to
// confirm the field landed.
func TestPatchScheduler_UpdatesIntervalOverride(t *testing.T) {
	b, base := startSchedulerTestBroker(t)
	defer b.Stop()

	status, body := patchScheduler(t, b, base, "task_reminder", map[string]any{
		"interval_override": 90,
	})
	if status != http.StatusOK {
		t.Fatalf("status %d, body %s", status, body)
	}

	var out struct {
		Job schedulerJob `json:"job"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Job.IntervalOverride != 90 {
		t.Errorf("IntervalOverride: got %d, want 90", out.Job.IntervalOverride)
	}

	// Confirm SchedulerJobControl now reports the override (not the default).
	enabled, interval := b.SchedulerJobControl("task_reminder", 30*time.Minute) // 30m placeholder default
	if !enabled {
		t.Error("Enabled flipped unexpectedly")
	}
	if interval.Minutes() != 90 {
		t.Errorf("interval: got %v, want 90m", interval)
	}
}

// TestPatchScheduler_RejectsBelowFloor walks every system cron and asserts
// an interval_override below its MinFloor returns 400.
func TestPatchScheduler_RejectsBelowFloor(t *testing.T) {
	b, base := startSchedulerTestBroker(t)
	defer b.Stop()

	tests := []struct {
		slug    string
		below   int
		atFloor int
	}{
		{slug: "nex-insights", below: 29, atFloor: 30},
		{slug: "nex-notifications", below: 4, atFloor: 5},
		{slug: "task_reminder", below: 4, atFloor: 5},
		{slug: "task_recheck", below: 4, atFloor: 5},
		{slug: "task_follow_up", below: 4, atFloor: 5},
		{slug: "request_follow_up", below: 4, atFloor: 5},
		{slug: "review-expiry", below: 4, atFloor: 5},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.slug+"_below_floor", func(t *testing.T) {
			status, body := patchScheduler(t, b, base, tc.slug, map[string]any{
				"interval_override": tc.below,
			})
			if status != http.StatusBadRequest {
				t.Errorf("%s below floor (%d): got status %d, body %s; want 400",
					tc.slug, tc.below, status, body)
			}
		})
		t.Run(tc.slug+"_at_floor", func(t *testing.T) {
			status, body := patchScheduler(t, b, base, tc.slug, map[string]any{
				"interval_override": tc.atFloor,
			})
			if status != http.StatusOK {
				t.Errorf("%s at floor (%d): got status %d, body %s; want 200",
					tc.slug, tc.atFloor, status, body)
			}
		})
	}
}

// TestPatchScheduler_RejectsOnReadOnlyCron verifies one-relay-events
// refuses any PATCH in v1 with a 400.
func TestPatchScheduler_RejectsOnReadOnlyCron(t *testing.T) {
	b, base := startSchedulerTestBroker(t)
	defer b.Stop()

	cases := []map[string]any{
		{"interval_override": 5},
		{"enabled": false},
		{"interval_override": 10, "enabled": true},
	}
	for i, payload := range cases {
		status, body := patchScheduler(t, b, base, "one-relay-events", payload)
		if status != http.StatusBadRequest {
			t.Errorf("case %d (%v): got status %d, body %s; want 400", i, payload, status, body)
		}
	}
}

// TestPatchScheduler_DisabledSkipsRun asserts that disabling a system cron
// flips Enabled=false in the registry and SchedulerJobControl reports it
// so the run-loop will skip its tick.
func TestPatchScheduler_DisabledSkipsRun(t *testing.T) {
	b, base := startSchedulerTestBroker(t)
	defer b.Stop()

	status, body := patchScheduler(t, b, base, "nex-insights", map[string]any{
		"enabled": false,
	})
	if status != http.StatusOK {
		t.Fatalf("PATCH disable: status %d, body %s", status, body)
	}

	enabled, _ := b.SchedulerJobControl("nex-insights", 30*time.Minute)
	if enabled {
		t.Error("Enabled stayed true after PATCH enabled=false")
	}

	// And re-enable round-trips.
	if status, body := patchScheduler(t, b, base, "nex-insights", map[string]any{
		"enabled": true,
	}); status != http.StatusOK {
		t.Fatalf("PATCH re-enable: status %d, body %s", status, body)
	}
	if enabled, _ := b.SchedulerJobControl("nex-insights", 30*time.Minute); !enabled {
		t.Error("Enabled didn't flip back to true")
	}
}

// TestPatchScheduler_UnknownSlugReturns404 covers the lookup miss path.
func TestPatchScheduler_UnknownSlugReturns404(t *testing.T) {
	b, base := startSchedulerTestBroker(t)
	defer b.Stop()

	status, _ := patchScheduler(t, b, base, "does-not-exist", map[string]any{
		"interval_override": 10,
	})
	if status != http.StatusNotFound {
		t.Errorf("status %d, want 404", status)
	}
}

// TestSchedulerHeartbeatPreservesUserFields guards the regression where a
// run-loop heartbeat (updateSchedulerJob) clobbered Enabled /
// IntervalOverride / SystemManaged / LastRunStatus by fully replacing the
// scheduler entry.
func TestSchedulerHeartbeatPreservesUserFields(t *testing.T) {
	b, base := startSchedulerTestBroker(t)
	defer b.Stop()

	// Operator state: interval override + LastRunStatus.
	if status, _ := patchScheduler(t, b, base, "nex-insights", map[string]any{
		"interval_override": 60,
	}); status != http.StatusOK {
		t.Fatalf("PATCH override: status %d", status)
	}
	b.mu.Lock()
	for i := range b.scheduler {
		if b.scheduler[i].Slug == "nex-insights" {
			b.scheduler[i].LastRunStatus = "ok"
		}
	}
	b.mu.Unlock()

	// Simulate a heartbeat: run-loop reports it's running with the env
	// default interval. Heartbeat must NOT clobber the override / status.
	b.updateSchedulerHeartbeat("nex-insights", "Nex insights", 30, /* default */
		time.Time{}, "running", "")

	b.mu.Lock()
	defer b.mu.Unlock()
	var job *schedulerJob
	for i := range b.scheduler {
		if b.scheduler[i].Slug == "nex-insights" {
			job = &b.scheduler[i]
			break
		}
	}
	if job == nil {
		t.Fatal("nex-insights missing after heartbeat")
	}
	if job.IntervalOverride != 60 {
		t.Errorf("IntervalOverride: got %d, want 60 (heartbeat must preserve)", job.IntervalOverride)
	}
	if !job.SystemManaged {
		t.Error("SystemManaged dropped to false")
	}
	if !job.Enabled {
		t.Error("Enabled dropped to false")
	}
	if job.LastRunStatus != "ok" {
		t.Errorf("LastRunStatus: got %q, want ok", job.LastRunStatus)
	}
	if job.Status != "running" {
		t.Errorf("Status: got %q, want running", job.Status)
	}
}

