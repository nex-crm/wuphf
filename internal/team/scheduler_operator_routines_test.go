package team

// Operator routine dispatch: a scheduler job owned by a CUSTOM APP fires the
// operator agent service (POST /routines/run) instead of posting into an
// office channel, and the outcome lands in the per-slug run ring. The agent
// service is stubbed with httptest — everything here is offline.

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const testOperatorAppID = "app_00000000000000ab"

// registerOperatorRoutine creates an operator routine (owner = custom app id)
// via the same POST /scheduler/routines contract agents use, then backdates
// NextRun so the next watchdog tick fires it.
func registerOperatorRoutine(t *testing.T, b *Broker, srv *httptest.Server, prompt string) string {
	t.Helper()
	status, body := postRoutine(t, srv, b.Token(), map[string]any{
		"purpose":    "Monday pipeline recap",
		"schedule":   "0 9 * * 1",
		"owner":      testOperatorAppID,
		"prompt":     prompt,
		"created_by": "operator",
	})
	if status != http.StatusCreated && status != http.StatusOK {
		t.Fatalf("register routine: %d %s", status, body)
	}
	var created struct {
		Job struct {
			Slug string `json:"slug"`
		} `json:"job"`
	}
	if err := json.Unmarshal([]byte(body), &created); err != nil || created.Job.Slug == "" {
		t.Fatalf("register routine response: %v %s", err, body)
	}
	b.mu.Lock()
	for i := range b.scheduler {
		if b.scheduler[i].Slug == created.Job.Slug {
			b.scheduler[i].NextRun = "2000-01-01T00:00:00Z"
			b.scheduler[i].DueAt = "2000-01-01T00:00:00Z"
		}
	}
	b.mu.Unlock()
	return created.Job.Slug
}

// operatorWatchdog builds a watchdog whose operatorRunRecorded seam signals
// the returned channel — tests await the async fire deterministically (no
// polling, no sleeps).
func operatorWatchdog(b *Broker) (*watchdogScheduler, <-chan string) {
	recorded := make(chan string, 4)
	w := &watchdogScheduler{
		broker: b,
		clock:  realClock{},
		operatorRunRecorded: func(slug string) {
			recorded <- slug
		},
	}
	return w, recorded
}

// awaitRun blocks until the seam reports a recorded run for slug, then reads
// it from the ring.
func awaitRun(t *testing.T, b *Broker, recorded <-chan string, slug string) schedulerRun {
	t.Helper()
	for {
		select {
		case got := <-recorded:
			if got != slug {
				continue
			}
			runs := b.SchedulerRuns(slug)
			if len(runs) == 0 {
				t.Fatalf("run seam fired for %s but the ring is empty", slug)
			}
			return runs[0]
		case <-time.After(5 * time.Second):
			t.Fatalf("no run recorded for %s", slug)
		}
	}
}

func TestOperatorRoutineFireHitsAgentServiceAndRecordsRun(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "broker-state.json")
	b := NewBrokerAt(statePath)
	srv := newRoutineTestServer(t, b)

	var gotBody atomic.Value
	agentSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/routines/run" {
			http.NotFound(w, r)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		gotBody.Store(string(raw))
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "digest": "the recap", "session_id": "s1"})
	}))
	t.Cleanup(agentSrv.Close)
	t.Setenv("WUPHF_AGENT_URL", agentSrv.URL)

	slug := registerOperatorRoutine(t, b, srv, "Summarize last week's pipeline movement")
	w, recorded := operatorWatchdog(b)
	w.processOnce()

	run := awaitRun(t, b, recorded, slug)
	if run.Status != "ok" {
		t.Fatalf("run status = %q (%+v), want ok", run.Status, run)
	}
	if run.OutputSummary != "the recap" {
		t.Fatalf("run summary = %q, want the digest", run.OutputSummary)
	}
	raw, _ := gotBody.Load().(string)
	for _, want := range []string{testOperatorAppID, slug, "Monday pipeline recap", "Summarize last week's pipeline movement"} {
		if !strings.Contains(raw, want) {
			t.Fatalf("agent service body missing %q: %s", want, raw)
		}
	}
	// No office channel got the prompt (operator routines never post to chat).
	if msgs := b.ChannelMessages("general"); len(msgs) != 0 {
		t.Fatalf("operator routine leaked into #general: %+v", msgs)
	}
	// The cron stays alive and re-armed into the future.
	b.mu.Lock()
	defer b.mu.Unlock()
	for i := range b.scheduler {
		if b.scheduler[i].Slug != slug {
			continue
		}
		if b.scheduler[i].Status != "scheduled" {
			t.Fatalf("job status = %q, want scheduled", b.scheduler[i].Status)
		}
		next, err := time.Parse(time.RFC3339, b.scheduler[i].NextRun)
		if err != nil || !next.After(time.Now().Add(-time.Minute)) {
			t.Fatalf("job not re-armed: next_run=%q err=%v", b.scheduler[i].NextRun, err)
		}
	}
}

func TestOperatorRoutineOutcomeMapping(t *testing.T) {
	cases := []struct {
		name        string
		respond     func(w http.ResponseWriter)
		wantStatus  string
		wantContain string
	}{
		{
			name: "needs_approval is an OK fire paused at the send-gate",
			respond: func(w http.ResponseWriter) {
				_ = json.NewEncoder(w).Encode(map[string]any{"status": "needs_approval", "digest": "paused for your approval: send to #sales", "session_id": "s2"})
			},
			wantStatus:  "ok",
			wantContain: "Paused for approval:",
		},
		{
			name: "an error outcome records a failed run",
			respond: func(w http.ResponseWriter) {
				_ = json.NewEncoder(w).Encode(map[string]any{"status": "error", "digest": "kaput", "session_id": "s3"})
			},
			wantStatus:  "failed",
			wantContain: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			statePath := filepath.Join(t.TempDir(), "broker-state.json")
			b := NewBrokerAt(statePath)
			srv := newRoutineTestServer(t, b)
			agentSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { tc.respond(w) }))
			t.Cleanup(agentSrv.Close)
			t.Setenv("WUPHF_AGENT_URL", agentSrv.URL)

			slug := registerOperatorRoutine(t, b, srv, "do the thing")
			w, recorded := operatorWatchdog(b)
			w.processOnce()

			run := awaitRun(t, b, recorded, slug)
			if run.Status != tc.wantStatus {
				t.Fatalf("run status = %q (%+v), want %q", run.Status, run, tc.wantStatus)
			}
			if tc.wantContain != "" && !strings.Contains(run.OutputSummary, tc.wantContain) {
				t.Fatalf("run summary = %q, want it to contain %q", run.OutputSummary, tc.wantContain)
			}
		})
	}
}

func TestOperatorRoutineUnreachableServiceRecordsFailureAndKeepsCron(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "broker-state.json")
	b := NewBrokerAt(statePath)
	srv := newRoutineTestServer(t, b)
	// A dead endpoint: the port is closed the moment the server stops.
	dead := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	deadURL := dead.URL
	dead.Close()
	t.Setenv("WUPHF_AGENT_URL", deadURL)

	slug := registerOperatorRoutine(t, b, srv, "do the thing")
	w, recorded := operatorWatchdog(b)
	w.processOnce()

	run := awaitRun(t, b, recorded, slug)
	if run.Status != "failed" {
		t.Fatalf("run status = %q, want failed", run.Status)
	}
	if !strings.Contains(run.Message, "Routine fire failed") {
		t.Fatalf("run message = %q, want a fire failure", run.Message)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for i := range b.scheduler {
		if b.scheduler[i].Slug == slug && b.scheduler[i].Status != "scheduled" {
			t.Fatalf("cron died on failure: status = %q", b.scheduler[i].Status)
		}
	}
}
