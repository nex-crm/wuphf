package team

// broker_scheduler_routines_test.go — live-layer regression tests for
// POST /scheduler/routines (Wave D / D1): standing automations must be
// real, persistent, visible, and deduped. Every test speaks the HTTP wire
// behind the same auth middleware the live broker uses.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func newRoutineTestServer(t *testing.T, b *Broker) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/scheduler", b.requireAuth(b.handleScheduler))
	mux.HandleFunc("/scheduler/", b.requireAuth(b.handleSchedulerSubpath))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func postRoutine(t *testing.T, srv *httptest.Server, token string, body map[string]any) (int, string) {
	t.Helper()
	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/scheduler/routines", bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer res.Body.Close()
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(res.Body); err != nil {
		t.Fatalf("read body: %v", err)
	}
	return res.StatusCode, buf.String()
}

func validRoutineBody() map[string]any {
	return map[string]any{
		"purpose":    "Weekly Monday 9am renewal risk summary to #general",
		"schedule":   "0 9 * * 1",
		"channel":    "general",
		"owner":      "eng",
		"prompt":     "Pull renewals within 60 days and post a risk summary.",
		"created_by": "ceo",
	}
}

// TestRegisterRoutine_CreatesPersistentVisibleJob pins the full D1
// contract on the create path: 201, agent-targeted user routine (the
// Scheduled Tasks classification), persisted to disk so a reloaded broker
// still has it.
func TestRegisterRoutine_CreatesPersistentVisibleJob(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "broker-state.json")
	b := NewBrokerAt(statePath)
	defer b.Stop()
	srv := newRoutineTestServer(t, b)

	status, raw := postRoutine(t, srv, b.Token(), validRoutineBody())
	if status != http.StatusCreated {
		t.Fatalf("expected 201, got %d (%s)", status, raw)
	}
	var created struct {
		Job     schedulerJob `json:"job"`
		Updated bool         `json:"updated"`
	}
	if err := json.Unmarshal([]byte(raw), &created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if created.Updated {
		t.Fatal("fresh registration must not report updated=true")
	}
	job := created.Job
	if job.Slug == "" || job.SystemManaged || job.TargetType != "agent" ||
		job.TargetID != "eng" || job.ScheduleExpr != "0 9 * * 1" || !job.Enabled ||
		job.Kind != "agent_routine" || job.NextRun == "" {
		t.Fatalf("job does not satisfy the user-routine shape: %+v", job)
	}

	// Visible through the Scheduled Tasks data source (GET /scheduler).
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/scheduler", nil)
	req.Header.Set("Authorization", "Bearer "+b.Token())
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get scheduler: %v", err)
	}
	defer res.Body.Close()
	var listed struct {
		Jobs []schedulerJob `json:"jobs"`
	}
	if err := json.NewDecoder(res.Body).Decode(&listed); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	found := false
	for _, j := range listed.Jobs {
		if j.Slug == job.Slug {
			found = true
		}
	}
	if !found {
		t.Fatalf("registered routine %q missing from GET /scheduler", job.Slug)
	}

	// Persistent: a fresh broker at the same state path loads the routine.
	// The team test suite disables auto-load on construct (test_support.go);
	// re-enable it for this reload exactly like broker_phase6_migration_test.
	withDiskLoad(t)
	reloaded := NewBrokerAt(statePath)
	defer reloaded.Stop()
	reloaded.mu.Lock()
	persisted := false
	for _, j := range reloaded.scheduler {
		if j.Slug == job.Slug && j.ScheduleExpr == "0 9 * * 1" && j.TargetID == "eng" {
			persisted = true
		}
	}
	reloaded.mu.Unlock()
	if !persisted {
		t.Fatal("routine did not survive a broker state save/load round-trip")
	}
}

// TestRegisterRoutine_DedupesSamePurposeAndSchedule pins the v3 failure
// fix: a second agent registering the same automation with different
// wording converges onto the existing job — one ask, one routine.
func TestRegisterRoutine_DedupesSamePurposeAndSchedule(t *testing.T) {
	b := newTestBroker(t)
	defer b.Stop()
	srv := newRoutineTestServer(t, b)

	status, raw := postRoutine(t, srv, b.Token(), validRoutineBody())
	if status != http.StatusCreated {
		t.Fatalf("first registration: expected 201, got %d (%s)", status, raw)
	}
	var first struct {
		Job schedulerJob `json:"job"`
	}
	if err := json.Unmarshal([]byte(raw), &first); err != nil {
		t.Fatalf("decode first: %v", err)
	}

	// Different agent, different word order/punctuation, same schedule.
	second := validRoutineBody()
	second["purpose"] = "Monday 9am weekly renewal-risk summary to #general!"
	second["prompt"] = "Include champion stability in the score."
	second["created_by"] = "outreach"
	status, raw = postRoutine(t, srv, b.Token(), second)
	if status != http.StatusOK {
		t.Fatalf("re-registration: expected 200, got %d (%s)", status, raw)
	}
	var dedup struct {
		Job     schedulerJob `json:"job"`
		Updated bool         `json:"updated"`
	}
	if err := json.Unmarshal([]byte(raw), &dedup); err != nil {
		t.Fatalf("decode second: %v", err)
	}
	if !dedup.Updated || dedup.Job.Slug != first.Job.Slug {
		t.Fatalf("expected update of %q, got updated=%v slug=%q", first.Job.Slug, dedup.Updated, dedup.Job.Slug)
	}
	if !strings.Contains(dedup.Job.Payload, "champion stability") {
		t.Fatalf("re-registration must refresh the prompt, got payload=%q", dedup.Job.Payload)
	}

	b.mu.Lock()
	count := 0
	for _, j := range b.scheduler {
		if isAgentRoutineJob(j) {
			count++
		}
	}
	b.mu.Unlock()
	if count != 1 {
		t.Fatalf("expected exactly 1 agent routine after dedupe, got %d", count)
	}
}

// TestRegisterRoutine_DifferentScheduleCreatesSecondJob pins the dedupe
// boundary: same purpose at a different cadence is a different automation.
func TestRegisterRoutine_DifferentScheduleCreatesSecondJob(t *testing.T) {
	b := newTestBroker(t)
	defer b.Stop()
	srv := newRoutineTestServer(t, b)

	if status, raw := postRoutine(t, srv, b.Token(), validRoutineBody()); status != http.StatusCreated {
		t.Fatalf("first: expected 201, got %d (%s)", status, raw)
	}
	other := validRoutineBody()
	other["purpose"] = "Daily renewal risk pulse to #general"
	other["schedule"] = "daily"
	status, raw := postRoutine(t, srv, b.Token(), other)
	if status != http.StatusCreated {
		t.Fatalf("different automation: expected 201, got %d (%s)", status, raw)
	}

	b.mu.Lock()
	count := 0
	for _, j := range b.scheduler {
		if isAgentRoutineJob(j) {
			count++
		}
	}
	b.mu.Unlock()
	if count != 2 {
		t.Fatalf("expected 2 distinct routines, got %d", count)
	}
}

// TestRegisterRoutine_DoesNotResurrectHumanPausedRoutine pins human
// sovereignty: an agent re-registering a routine the human paused in
// Scheduled Tasks must not flip it back on.
func TestRegisterRoutine_DoesNotResurrectHumanPausedRoutine(t *testing.T) {
	b := newTestBroker(t)
	defer b.Stop()
	srv := newRoutineTestServer(t, b)

	status, raw := postRoutine(t, srv, b.Token(), validRoutineBody())
	if status != http.StatusCreated {
		t.Fatalf("expected 201, got %d (%s)", status, raw)
	}
	var created struct {
		Job schedulerJob `json:"job"`
	}
	if err := json.Unmarshal([]byte(raw), &created); err != nil {
		t.Fatalf("decode: %v", err)
	}

	// Human pauses from Scheduled Tasks (PATCH enabled=false).
	patch, _ := json.Marshal(map[string]any{"enabled": false})
	req, _ := http.NewRequest(http.MethodPatch, srv.URL+"/scheduler/"+created.Job.Slug, bytes.NewReader(patch))
	req.Header.Set("Authorization", "Bearer "+b.Token())
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("patch: %v", err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("pause: expected 200, got %d", res.StatusCode)
	}

	status, raw = postRoutine(t, srv, b.Token(), validRoutineBody())
	if status != http.StatusOK {
		t.Fatalf("re-registration: expected 200, got %d (%s)", status, raw)
	}
	var dedup struct {
		Job schedulerJob `json:"job"`
	}
	if err := json.Unmarshal([]byte(raw), &dedup); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if dedup.Job.Enabled {
		t.Fatal("agent re-registration must not resurrect a human-paused routine")
	}
}

// TestRegisterRoutine_Validation pins the 400 contract for bad input on
// the live wire: missing purpose/schedule/owner, malformed cron, and a
// cadence under the routine floor.
func TestRegisterRoutine_Validation(t *testing.T) {
	b := newTestBroker(t)
	defer b.Stop()
	srv := newRoutineTestServer(t, b)

	cases := []struct {
		name   string
		mutate func(map[string]any)
	}{
		{"missing purpose", func(m map[string]any) { m["purpose"] = " " }},
		{"missing schedule", func(m map[string]any) { m["schedule"] = "" }},
		{"missing owner and created_by", func(m map[string]any) { m["owner"] = ""; m["created_by"] = "" }},
		{"malformed cron", func(m map[string]any) { m["schedule"] = "not a cron" }},
		{"cadence under the floor", func(m map[string]any) { m["schedule"] = "* * * * *" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := validRoutineBody()
			tc.mutate(body)
			status, raw := postRoutine(t, srv, b.Token(), body)
			if status != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d (%s)", status, raw)
			}
		})
	}

	b.mu.Lock()
	leaked := len(b.scheduler)
	b.mu.Unlock()
	if leaked != 0 {
		t.Fatalf("rejected registrations must not leave scheduler rows, got %d", leaked)
	}
}

// TestRegisterRoutine_ReservedSlugUniquified guards the /scheduler/
// sub-resource namespace: a purpose that derives to a reserved path
// segment must not shadow the endpoint.
func TestRegisterRoutine_ReservedSlugUniquified(t *testing.T) {
	b := newTestBroker(t)
	defer b.Stop()
	srv := newRoutineTestServer(t, b)

	body := validRoutineBody()
	body["purpose"] = "Routines"
	status, raw := postRoutine(t, srv, b.Token(), body)
	if status != http.StatusCreated {
		t.Fatalf("expected 201, got %d (%s)", status, raw)
	}
	var created struct {
		Job schedulerJob `json:"job"`
	}
	if err := json.Unmarshal([]byte(raw), &created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if created.Job.Slug == "routines" || created.Job.Slug == "system-specs" {
		t.Fatalf("reserved slug leaked: %q", created.Job.Slug)
	}
}

// TestNormalizeRoutinePurpose pins the dedupe-key normalization:
// case/punctuation/word-order/duplicate-token insensitive.
func TestNormalizeRoutinePurpose(t *testing.T) {
	cases := []struct {
		a, b  string
		equal bool
	}{
		{"Weekly Monday 9am renewal risk summary to #general", "monday 9am WEEKLY renewal-risk summary to general!", true},
		{"Weekly renewal summary", "Weekly renewal weekly summary", true}, // duplicate token collapses
		{"Weekly renewal summary", "Daily renewal summary", false},
		{"", "   ", true},
	}
	for _, tc := range cases {
		got := normalizeRoutinePurpose(tc.a) == normalizeRoutinePurpose(tc.b)
		if got != tc.equal {
			t.Errorf("normalizeRoutinePurpose(%q) vs (%q): equal=%v, want %v (%q vs %q)",
				tc.a, tc.b, got, tc.equal, normalizeRoutinePurpose(tc.a), normalizeRoutinePurpose(tc.b))
		}
	}
}

// TestRegisterRoutine_FiresThroughScheduler pins that a registered
// automation is wired to a REAL dispatcher: forced due, one watchdog
// tick posts the prompt into the channel tagging the owner.
func TestRegisterRoutine_FiresThroughScheduler(t *testing.T) {
	b := newTestBroker(t)
	defer b.Stop()
	b.mu.Lock()
	b.members = []officeMember{{Slug: "ceo", Name: "CEO"}, {Slug: "eng", Name: "Engineer"}}
	b.channels = []teamChannel{{Slug: "general", Name: "general", Members: []string{"human", "ceo", "eng"}}}
	b.mu.Unlock()
	srv := newRoutineTestServer(t, b)

	status, raw := postRoutine(t, srv, b.Token(), validRoutineBody())
	if status != http.StatusCreated {
		t.Fatalf("expected 201, got %d (%s)", status, raw)
	}
	var created struct {
		Job schedulerJob `json:"job"`
	}
	if err := json.Unmarshal([]byte(raw), &created); err != nil {
		t.Fatalf("decode: %v", err)
	}

	b.mu.Lock()
	for i := range b.scheduler {
		if b.scheduler[i].Slug == created.Job.Slug {
			b.scheduler[i].NextRun = "2000-01-01T00:00:00Z"
			b.scheduler[i].DueAt = "2000-01-01T00:00:00Z"
		}
	}
	b.mu.Unlock()

	w := &watchdogScheduler{broker: b, clock: realClock{}}
	w.processOnce()

	fired := false
	for _, msg := range b.ChannelMessages("general") {
		if strings.Contains(msg.Content, "@eng") && strings.Contains(msg.Content, "risk summary") {
			fired = true
		}
	}
	if !fired {
		var got []string
		for _, msg := range b.ChannelMessages("general") {
			got = append(got, fmt.Sprintf("%s: %s", msg.From, msg.Content))
		}
		t.Fatalf("due routine did not fire into #general; messages=%v", got)
	}
}
