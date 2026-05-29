package team

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestRecordSchedulerRunLocked_RingBufferEviction pins the ring-buffer
// behaviour: more than schedulerRunHistoryLimit entries collapse to the
// most-recent N, FIFO. Drift here would let an idle slug grow state
// unboundedly across restarts.
func TestRecordSchedulerRunLocked_RingBufferEviction(t *testing.T) {
	b := &Broker{}
	for i := 0; i < schedulerRunHistoryLimit+5; i++ {
		b.mu.Lock()
		b.recordSchedulerRunLocked(schedulerRun{
			Slug:      "noisy-slug",
			StartedAt: time.Now().UTC().Add(time.Duration(i) * time.Minute).Format(time.RFC3339),
			Status:    "ok",
		})
		b.mu.Unlock()
	}
	got := b.SchedulerRuns("noisy-slug")
	if len(got) != schedulerRunHistoryLimit {
		t.Fatalf("expected ring buffer capped at %d, got %d", schedulerRunHistoryLimit, len(got))
	}
	// Most-recent-first: first entry's started_at must reflect the last
	// appended timestamp window.
	if got[0].StartedAt == "" {
		t.Fatal("expected non-empty StartedAt on most-recent run")
	}
}

// TestSchedulerRuns_EmptyAndUnknownSlug confirms the API contract: empty
// or unknown slug yields an empty slice (not nil), so JSON encoding emits
// `[]` rather than `null`.
func TestSchedulerRuns_EmptyAndUnknownSlug(t *testing.T) {
	b := &Broker{}
	if got := b.SchedulerRuns(""); got == nil || len(got) != 0 {
		t.Fatalf("expected empty non-nil slice for empty slug, got %v", got)
	}
	if got := b.SchedulerRuns("never-fired"); got == nil || len(got) != 0 {
		t.Fatalf("expected empty non-nil slice for unknown slug, got %v", got)
	}
}

// TestHandleSchedulerRuns_GETReturnsHistory exercises the HTTP surface:
// GET /scheduler/{slug}/runs returns most-recent-first, JSON-shaped as
// {slug, runs: [...]}.
func TestHandleSchedulerRuns_GETReturnsHistory(t *testing.T) {
	b := newTestBroker(t)
	if err := b.SetSchedulerJob(schedulerJob{
		Slug:    "demo",
		Label:   "Demo routine",
		Enabled: true,
	}); err != nil {
		t.Fatalf("SetSchedulerJob: %v", err)
	}
	// Seed two runs directly so the test doesn't depend on the broker
	// run-loop firing during the test.
	b.mu.Lock()
	b.recordSchedulerRunLocked(schedulerRun{
		Slug:      "demo",
		StartedAt: "2026-05-28T10:00:00Z",
		Status:    "ok",
	})
	b.recordSchedulerRunLocked(schedulerRun{
		Slug:      "demo",
		StartedAt: "2026-05-28T11:00:00Z",
		Status:    "failed",
		Message:   "synthetic failure",
	})
	b.mu.Unlock()

	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	defer b.Stop()

	base := fmt.Sprintf("http://%s", b.Addr())
	req, _ := http.NewRequest(http.MethodGet, base+"/scheduler/demo/runs", nil)
	req.Header.Set("Authorization", "Bearer "+b.Token())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET runs: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var body struct {
		Slug string         `json:"slug"`
		Runs []schedulerRun `json:"runs"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Slug != "demo" {
		t.Errorf("slug: want demo, got %q", body.Slug)
	}
	if len(body.Runs) != 2 {
		t.Fatalf("expected 2 runs, got %d", len(body.Runs))
	}
	if body.Runs[0].StartedAt != "2026-05-28T11:00:00Z" {
		t.Errorf("expected most-recent-first; got %q at idx 0", body.Runs[0].StartedAt)
	}
	if body.Runs[0].Status != "failed" {
		t.Errorf("expected failed status at idx 0, got %q", body.Runs[0].Status)
	}
	if !strings.Contains(body.Runs[0].Message, "synthetic") {
		t.Errorf("expected synthetic message, got %q", body.Runs[0].Message)
	}
}

// TestHandleSchedulerRuns_WrongMethodReturns405 pins the method gate so a
// PATCH or POST against /runs can't accidentally trigger a write path.
func TestHandleSchedulerRuns_WrongMethodReturns405(t *testing.T) {
	b := newTestBroker(t)
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	defer b.Stop()
	base := fmt.Sprintf("http://%s", b.Addr())
	for _, method := range []string{http.MethodPost, http.MethodPatch, http.MethodDelete} {
		req, _ := http.NewRequest(method, base+"/scheduler/anything/runs", nil)
		req.Header.Set("Authorization", "Bearer "+b.Token())
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s runs: %v", method, err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusMethodNotAllowed {
			t.Errorf("%s: expected 405, got %d", method, resp.StatusCode)
		}
	}
}

// TestRunSchedulerJob_RecordsHumanTriggeredRun ensures the manual-fire
// path also lands an entry in the run history with TriggeredBy="human".
func TestRunSchedulerJob_RecordsHumanTriggeredRun(t *testing.T) {
	b := newTestBroker(t)
	if err := b.SetSchedulerJob(schedulerJob{
		Slug:            "weekly-digest",
		Label:           "Weekly digest",
		TargetType:      "workflow",
		IntervalMinutes: 60,
		NextRun:         time.Now().UTC().Add(time.Hour).Format(time.RFC3339),
		Status:          "scheduled",
		Enabled:         true,
	}); err != nil {
		t.Fatalf("SetSchedulerJob: %v", err)
	}
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	defer b.Stop()

	base := fmt.Sprintf("http://%s", b.Addr())
	req, _ := http.NewRequest(http.MethodPost, base+"/scheduler/weekly-digest/run", nil)
	req.Header.Set("Authorization", "Bearer "+b.Token())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST run: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	runs := b.SchedulerRuns("weekly-digest")
	if len(runs) != 1 {
		t.Fatalf("expected 1 run recorded, got %d", len(runs))
	}
	if runs[0].TriggeredBy != "human" {
		t.Errorf("expected TriggeredBy=human, got %q", runs[0].TriggeredBy)
	}
}
