package team

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestHandleHealth_ReturnsOKAndVersionFields pins the wire shape of
// /health: 200 with a JSON body that includes status, session_mode,
// provider, memory_backend, and a build sub-object. The web UI's
// status bar reads all five.
func TestHandleHealth_ReturnsOKAndVersionFields(t *testing.T) {
	b := newTestBroker(t)
	srv := httptest.NewServer(http.HandlerFunc(b.handleHealth))
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var got map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got["status"] != "ok" {
		t.Errorf("status: want ok, got %v", got["status"])
	}
	for _, k := range []string{"session_mode", "one_on_one_agent", "provider", "memory_backend", "memory_backend_active", "memory_backend_ready", "build"} {
		if _, ok := got[k]; !ok {
			t.Errorf("missing field %q", k)
		}
	}
	build, _ := got["build"].(map[string]any)
	if _, ok := build["version"]; !ok {
		t.Errorf("missing build.version")
	}
}

// TestHandleResetDM_RequiresPOST locks the method gate. A GET against
// the reset-DM endpoint must not silently succeed and remove messages.
func TestHandleResetDM_RequiresPOST(t *testing.T) {
	b := newTestBroker(t)
	srv := httptest.NewServer(http.HandlerFunc(b.handleResetDM))
	defer srv.Close()

	for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodDelete} {
		req, _ := http.NewRequest(method, srv.URL, nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s: %v", method, err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusMethodNotAllowed {
			t.Errorf("%s: expected 405, got %d", method, resp.StatusCode)
		}
	}
}

// TestHandleWatchdogs_GETReturnsCurrentAlerts pins the read-side
// contract: GET /watchdogs returns a JSON object with key "watchdogs"
// carrying an array of all current alerts (no filtering).
func TestHandleWatchdogs_GETReturnsCurrentAlerts(t *testing.T) {
	b := newTestBroker(t)
	b.mu.Lock()
	b.watchdogs = []watchdogAlert{
		{ID: "alert-1", Kind: "stalled", Owner: "ceo", Summary: "CEO stalled", Channel: "general"},
		{ID: "alert-2", Kind: "stalled", Owner: "pm", Summary: "PM stalled", Channel: "general"},
	}
	b.mu.Unlock()

	req := httptest.NewRequest(http.MethodGet, "/watchdogs", nil)
	rec := httptest.NewRecorder()
	b.handleWatchdogs(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var resp struct {
		Watchdogs []watchdogAlert `json:"watchdogs"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Watchdogs) != 2 {
		t.Errorf("want 2 alerts, got %d", len(resp.Watchdogs))
	}

	// Non-GET must 405.
	for _, m := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		rec2 := httptest.NewRecorder()
		b.handleWatchdogs(rec2, httptest.NewRequest(m, "/watchdogs", nil))
		if rec2.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s: expected 405, got %d", m, rec2.Code)
		}
	}
}

// TestHandleQueue_ReturnsDueJobsArray pins the response shape of
// GET /queue: a JSON object with key "due" carrying scheduler jobs
// whose DueAt is at-or-before now. Future-dated jobs must be filtered.
func TestHandleQueue_ReturnsDueJobsArray(t *testing.T) {
	b := newTestBroker(t)
	if err := b.SetSchedulerJob(schedulerJob{
		Slug:       "due-job",
		Kind:       "task_follow_up",
		Label:      "Due now",
		TargetType: "task",
		TargetID:   "t-1",
		Channel:    "general",
		DueAt:      time.Now().UTC().Add(-time.Minute).Format(time.RFC3339),
		NextRun:    time.Now().UTC().Add(-time.Minute).Format(time.RFC3339),
		Status:     "scheduled",
	}); err != nil {
		t.Fatalf("SetSchedulerJob: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/queue", nil)
	rec := httptest.NewRecorder()
	b.handleQueue(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var got struct {
		Due []schedulerJob `json:"due"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Due) != 1 || got.Due[0].Slug != "due-job" {
		t.Errorf("expected due-job in queue, got %+v", got.Due)
	}
}

func TestBrokerSessionModePersistsAndSurvivesReset(t *testing.T) {
	b := newTestBroker(t)
	b.mu.Lock()
	b.members = append(b.members, officeMember{Slug: "pm", Name: "Product Manager"})
	for i := range b.channels {
		if b.channels[i].Slug == "general" {
			b.channels[i].Members = append(b.channels[i].Members, "pm")
			break
		}
	}
	b.mu.Unlock()
	if err := b.SetSessionMode(SessionModeOneOnOne, "pm"); err != nil {
		t.Fatalf("SetSessionMode failed: %v", err)
	}
	if _, err := b.PostMessage("pm", "general", "hello", nil, ""); err != nil {
		t.Fatalf("seed direct message: %v", err)
	}

	reloaded := reloadedBroker(t, b)
	mode, agent := reloaded.SessionModeState()
	if mode != SessionModeOneOnOne {
		t.Fatalf("expected persisted 1o1 mode, got %q", mode)
	}
	if agent != "pm" {
		t.Fatalf("expected persisted 1o1 agent pm, got %q", agent)
	}

	reloaded.Reset()
	mode, agent = reloaded.SessionModeState()
	if mode != SessionModeOneOnOne {
		t.Fatalf("expected reset to preserve 1o1 mode, got %q", mode)
	}
	if agent != "pm" {
		t.Fatalf("expected reset to preserve 1o1 agent pm, got %q", agent)
	}
	if len(reloaded.Messages()) != 0 {
		t.Fatalf("expected reset to clear direct messages, got %d", len(reloaded.Messages()))
	}
}

func TestBrokerActionsAndSchedulerEndpoints(t *testing.T) {
	b := newTestBroker(t)
	b.mu.Lock()
	b.appendActionLocked("request_created", "office", "general", "ceo", "Asked for approval", "request-1")
	b.mu.Unlock()
	if err := b.SetSchedulerJob(schedulerJob{
		Slug:            "nex-insights",
		Label:           "Nex insights",
		IntervalMinutes: 15,
		Status:          "sleeping",
		NextRun:         "2026-03-24T10:15:00Z",
	}); err != nil {
		t.Fatalf("SetSchedulerJob failed: %v", err)
	}
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("failed to start broker: %v", err)
	}
	defer b.Stop()

	base := fmt.Sprintf("http://%s", b.Addr())
	for _, path := range []string{"/actions", "/scheduler"} {
		req, _ := http.NewRequest(http.MethodGet, base+path, nil)
		req.Header.Set("Authorization", "Bearer "+b.Token())
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s request failed: %v", path, err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("expected 200 on %s, got %d: %s", path, resp.StatusCode, body)
		}
	}
}

func TestBrokerPostsAndDedupesNexNotifications(t *testing.T) {
	b := newTestBroker(t)
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("failed to start broker: %v", err)
	}
	defer b.Stop()

	base := fmt.Sprintf("http://%s", b.Addr())
	body := map[string]any{
		"event_id":     "feed-item-1",
		"title":        "Context alert",
		"content":      "Important: Acme mentioned budget pressure",
		"tagged":       []string{"ceo"},
		"source":       "context_graph",
		"source_label": "Nex",
	}
	payload, _ := json.Marshal(body)

	for i := 0; i < 2; i++ {
		req, _ := http.NewRequest(http.MethodPost, base+"/notifications/nex", bytes.NewReader(payload))
		req.Header.Set("Authorization", "Bearer "+b.Token())
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("notification post failed: %v", err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200 from nex notification ingest, got %d", resp.StatusCode)
		}
	}

	msgs := b.Messages()
	if len(msgs) != 1 {
		t.Fatalf("expected deduped single notification, got %d", len(msgs))
	}
	if msgs[0].Kind != "automation" || msgs[0].From != "nex" {
		t.Fatalf("expected automation message from nex, got %+v", msgs[0])
	}
	if msgs[0].EventID != "feed-item-1" {
		t.Fatalf("expected event id to persist, got %+v", msgs[0])
	}
}

func TestQueueEndpointShowsDueJobs(t *testing.T) {
	b := newTestBroker(t)
	if err := b.SetSchedulerJob(schedulerJob{
		Slug:       "request-follow-up:general:request-1",
		Kind:       "request_follow_up",
		Label:      "Follow up on approval",
		TargetType: "request",
		TargetID:   "request-1",
		Channel:    "general",
		DueAt:      time.Now().UTC().Add(-2 * time.Minute).Format(time.RFC3339),
		NextRun:    time.Now().UTC().Add(-2 * time.Minute).Format(time.RFC3339),
		Status:     "scheduled",
	}); err != nil {
		t.Fatalf("SetSchedulerJob failed: %v", err)
	}
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("failed to start broker: %v", err)
	}
	defer b.Stop()

	base := fmt.Sprintf("http://%s", b.Addr())
	req, _ := http.NewRequest(http.MethodGet, base+"/queue", nil)
	req.Header.Set("Authorization", "Bearer "+b.Token())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("queue request failed: %v", err)
	}
	defer resp.Body.Close()
	var queue struct {
		Due []schedulerJob `json:"due"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&queue); err != nil {
		t.Fatalf("decode queue response: %v", err)
	}
	if len(queue.Due) != 1 {
		t.Fatalf("expected due scheduler job to surface, got %+v", queue.Due)
	}
}
