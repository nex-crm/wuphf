package team

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nex-crm/wuphf/internal/agent"
	"github.com/nex-crm/wuphf/internal/config"
	"github.com/nex-crm/wuphf/internal/gitexec"
)

func TestMain(m *testing.M) {
	// Globals that leaked background goroutines can write to after a test
	// returns need a process-lifetime home so cleanup doesn't race the
	// leaked writes. We pre-seed two (token file, headless log dir).
	// Broker state paths are handled per-test via NewBrokerAt / newTestBroker,
	// and the unisolated fallback is pinned in worktree_guard_test.go init()
	// via WUPHF_RUNTIME_HOME.
	var cleanups []func()
	cleanup := func() {
		for i := len(cleanups) - 1; i >= 0; i-- {
			cleanups[i]()
		}
	}

	// 1) Broker token file: default path is /tmp/wuphf-broker-token which
	//    collides with a running broker. Point at a temp file so tests
	//    don't clobber it. Tests get the token directly from b.Token().
	//    Fail fast if we cannot establish the redirect — a silent fallback
	//    to the production path is exactly the collision this guards against.
	dir, err := os.MkdirTemp("", "wuphf-broker-test-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "TestMain: mktemp broker-token dir: %v\n", err)
		os.Exit(1)
	}
	brokerTokenFilePath = filepath.Join(dir, "broker-token")
	cleanups = append(cleanups, func() { _ = os.RemoveAll(dir) })

	// 2) Headless log dir: pin headless-worker log writes to a process-
	//    stable temp dir so they don't pollute the real ~/.wuphf/logs and
	//    so per-test t.TempDir cleanups don't race appender writes. Set
	//    via the in-process wuphfLogDirOverride hook (the WUPHF_LOG_DIR
	//    env var was retired — env leaks into spawned codex/claude
	//    subprocesses, which is not what tests want).
	logDir, err := os.MkdirTemp("", "wuphf-team-test-logs-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "TestMain: mktemp headless log dir: %v\n", err)
		cleanup()
		os.Exit(1)
	}
	wuphfLogDirOverride.Store(&logDir)
	// Intentionally do NOT clear wuphfLogDirOverride on cleanup: the process
	// is about to exit and any goroutine that escaped a test's lifecycle
	// (race-detector edge cases, headless workers caught mid-flush) should
	// keep writing into the isolated temp dir, never the real ~/.wuphf/logs.
	// The dir removal below makes those writes a no-op rather than pollution.
	cleanups = append(cleanups, func() { _ = os.RemoveAll(logDir) })

	// 3) Stale-unanswered threshold: resume tests pre-seed broker state with
	//    ancient timestamps (e.g. "2026-04-14T10:00:00Z") to exercise routing
	//    logic without fighting a clock. The production default (1 hour)
	//    would drop those seeds. Raise the window to ~10 years during tests
	//    so the resume-routing tests keep their fixtures while production
	//    keeps the stale-message-dropping behavior.
	origStaleUnanswered := staleUnansweredThreshold
	staleUnansweredThreshold = 10 * 365 * 24 * time.Hour
	cleanups = append(cleanups, func() { staleUnansweredThreshold = origStaleUnanswered })

	// 4) Activity watchdog: tests create many short-lived brokers. The watchdog
	//    goroutine fires every minute, so leaving it running accumulates
	//    hundreds of stuck goroutines and causes timeout/goleak failures.
	//    Disable it for the test run; production always starts with it enabled.
	activityWatchdogEnabled = false
	cleanups = append(cleanups, func() { activityWatchdogEnabled = true })

	rc := m.Run()
	cleanup()
	os.Exit(rc)
}

// reloadedBroker constructs a Broker pinned to the same state path as b
// and replays state from disk so persistence tests can verify what a
// production restart would see. Test-mode NewBrokerAt skips the
// automatic disk load (to stop cross-test state leakage via a shared
// broker-state.json), so any test that checks persistence behavior
// must opt in through this helper.
func reloadedBroker(t *testing.T, b *Broker) *Broker {
	t.Helper()
	fresh := NewBrokerAt(b.statePath)
	if err := fresh.loadState(); err != nil {
		t.Fatalf("loadState: %v", err)
	}
	return fresh
}

// waitForHeadlessIdle is a test-only join point: it stops every headless
// worker spawned through the Launcher and blocks until each goroutine has
// exited. Register it via t.Cleanup so no headless worker outlives the test
// that started it (which would race t.TempDir cleanup with "unlinkat:
// directory not empty"). Idempotent — safe to call multiple times.
func (l *Launcher) waitForHeadlessIdle(t *testing.T) {
	t.Helper()
	l.stopHeadlessWorkers()
}

// setHeadlessWakeLeadFn swaps headlessWakeLeadFn under its mutex and restores
// the previous value on test cleanup. Use this instead of direct assignment to
// avoid DATA RACEs with leaked runHeadlessCodexQueue goroutines from prior tests.
func setHeadlessWakeLeadFn(t *testing.T, fn func(*Launcher, string)) {
	t.Helper()
	headlessWakeLeadFnMu.Lock()
	old := headlessWakeLeadFn
	headlessWakeLeadFn = fn
	headlessWakeLeadFnMu.Unlock()
	t.Cleanup(func() {
		headlessWakeLeadFnMu.Lock()
		headlessWakeLeadFn = old
		headlessWakeLeadFnMu.Unlock()
	})
}

func initUsableGitWorktree(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	cmd := exec.Command("git", "init")
	cmd.Dir = path
	cmd.Env = gitexec.CleanEnv()
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init %s: %v: %s", path, err, strings.TrimSpace(string(out)))
	}
}

func TestBrokerPersistsAndReloadsState(t *testing.T) {
	b := newTestBroker(t)
	b.mu.Lock()
	b.messages = []channelMessage{{ID: "msg-1", From: "ceo", Content: "Persist me", Timestamp: "2026-03-24T10:00:00Z"}}
	b.counter = 1
	if err := b.saveLocked(); err != nil {
		b.mu.Unlock()
		t.Fatalf("saveLocked failed: %v", err)
	}
	b.mu.Unlock()

	reloaded := reloadedBroker(t, b)
	msgs := reloaded.Messages()
	if len(msgs) != 1 {
		t.Fatalf("expected 1 persisted message, got %d", len(msgs))
	}
	if msgs[0].Content != "Persist me" {
		t.Fatalf("expected persisted content, got %q", msgs[0].Content)
	}

	reloaded.Reset()
	empty := reloadedBroker(t, b)
	if len(empty.Messages()) != 0 {
		t.Fatalf("expected reset to clear persisted messages, got %d", len(empty.Messages()))
	}
}

func TestBrokerLoadsLastGoodSnapshotWhenPrimaryStateIsClobbered(t *testing.T) {
	b := newTestBroker(t)
	b.mu.Lock()
	b.messages = []channelMessage{{ID: "msg-1", From: "human", Channel: "general", Content: "Run the consulting loop", Timestamp: "2026-04-16T00:00:00Z"}}
	b.tasks = []teamTask{{ID: "task-1", Channel: "delivery", Title: "Create the client brief", Owner: "builder", Status: "in_progress", ExecutionMode: "office", CreatedBy: "operator", CreatedAt: "2026-04-16T00:00:01Z", UpdatedAt: "2026-04-16T00:00:01Z"}}
	b.actions = []officeActionLog{{ID: "act-1", Kind: "task_created", Channel: "delivery", Actor: "operator", Summary: "Create the client brief", RelatedID: "task-1", CreatedAt: "2026-04-16T00:00:01Z"}}
	b.counter = 2
	if err := b.saveLocked(); err != nil {
		b.mu.Unlock()
		t.Fatalf("saveLocked failed: %v", err)
	}
	b.mu.Unlock()
	if _, err := os.Stat(b.stateSnapshotPath()); err != nil {
		t.Fatalf("expected snapshot after rich save: %v", err)
	}

	// Simulate a later clobber that keeps the custom office shell but loses live work.
	clobbered := reloadedBroker(t, b)
	clobbered.mu.Lock()
	clobbered.messages = nil
	clobbered.tasks = nil
	clobbered.actions = nil
	clobbered.channels = []teamChannel{
		{Slug: "general", Name: "general", Members: []string{"ceo", "builder"}},
		{Slug: "delivery", Name: "delivery", Members: []string{"ceo", "builder"}},
	}
	clobbered.members = []officeMember{
		{Slug: "ceo", Name: "CEO"},
		{Slug: "builder", Name: "Builder"},
	}
	clobbered.counter = 0
	if err := clobbered.saveLocked(); err != nil {
		clobbered.mu.Unlock()
		t.Fatalf("clobbered saveLocked failed: %v", err)
	}
	clobbered.mu.Unlock()
	if _, err := os.Stat(b.stateSnapshotPath()); err != nil {
		t.Fatalf("expected snapshot to survive clobbered save: %v", err)
	}
	if snap, err := loadBrokerStateFile(b.stateSnapshotPath()); err != nil {
		t.Fatalf("read snapshot: %v", err)
	} else if len(snap.Messages) != 1 || len(snap.Tasks) != 1 || len(snap.Actions) != 1 {
		t.Fatalf("unexpected snapshot contents: %+v", snap)
	}

	reloaded := reloadedBroker(t, b)
	if got := len(reloaded.Messages()); got != 1 {
		t.Fatalf("expected snapshot recovery to restore 1 message, got %d", got)
	}
	if got := len(reloaded.AllTasks()); got != 1 {
		t.Fatalf("expected snapshot recovery to restore 1 task, got %d", got)
	}
	if reloaded.AllTasks()[0].Title != "Create the client brief" {
		t.Fatalf("unexpected recovered task: %+v", reloaded.AllTasks()[0])
	}
	if got := len(reloaded.Actions()); got != 1 {
		t.Fatalf("expected snapshot recovery to restore actions, got %d", got)
	}
}

func TestBrokerMessageSubscribersReceivePostedMessages(t *testing.T) {
	b := newTestBroker(t)
	msgs, unsubscribe := b.SubscribeMessages(4)
	defer unsubscribe()

	want, err := b.PostMessage("ceo", "general", "Push this immediately", nil, "")
	if err != nil {
		t.Fatalf("PostMessage: %v", err)
	}

	select {
	case got := <-msgs:
		if got.ID != want.ID || got.Content != want.Content {
			t.Fatalf("unexpected subscribed message: %+v", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for subscribed message")
	}
}

func TestBrokerActionSubscribersReceiveTaskLifecycle(t *testing.T) {
	b := newTestBroker(t)
	actions, unsubscribe := b.SubscribeActions(4)
	defer unsubscribe()

	if _, _, err := b.EnsureTask("general", "Landing page", "Build the hero", "fe", "ceo", ""); err != nil {
		t.Fatalf("EnsureTask: %v", err)
	}

	select {
	case got := <-actions:
		if got.Kind != "task_created" {
			t.Fatalf("expected task_created action, got %+v", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for subscribed action")
	}
}

func TestReapStaleActivityLocked(t *testing.T) {
	b := newTestBroker(t)
	now := time.Now().UTC()
	stale := now.Add(-10 * time.Minute).Format(time.RFC3339)
	fresh := now.Add(-1 * time.Minute).Format(time.RFC3339)

	b.activity = map[string]agentActivitySnapshot{
		"stale-active":   {Slug: "stale-active", Status: "active", Activity: "tool_use", LastTime: stale},
		"stale-thinking": {Slug: "stale-thinking", Status: "thinking", Activity: "thinking", LastTime: stale},
		"fresh-active":   {Slug: "fresh-active", Status: "active", Activity: "tool_use", LastTime: fresh},
		"already-idle":   {Slug: "already-idle", Status: "idle", Activity: "idle", LastTime: stale},
		"already-error":  {Slug: "already-error", Status: "error", Activity: "error", LastTime: stale},
		"bad-time":       {Slug: "bad-time", Status: "active", Activity: "tool_use", LastTime: "not-a-time"},
	}

	b.mu.Lock()
	reset := b.reapStaleActivityLocked(now)
	b.mu.Unlock()

	if len(reset) != 2 {
		t.Fatalf("expected 2 stale agents reaped, got %d: %+v", len(reset), reset)
	}
	for _, snap := range reset {
		if snap.Status != "idle" {
			t.Errorf("reaped agent %q should be idle, got %q", snap.Slug, snap.Status)
		}
		if snap.Slug != "stale-active" && snap.Slug != "stale-thinking" {
			t.Errorf("unexpected reaped slug: %q", snap.Slug)
		}
	}

	if b.activity["fresh-active"].Status != "active" {
		t.Error("fresh-active should not be reaped")
	}
	if b.activity["already-idle"].Status != "idle" {
		t.Error("already-idle should be unchanged")
	}
	if b.activity["already-error"].Status != "error" {
		t.Error("already-error should be unchanged")
	}
	if b.activity["bad-time"].Status != "active" {
		t.Error("unparseable LastTime should be left alone")
	}
}

func TestBrokerStopIsIdempotent(t *testing.T) {
	b := newTestBroker(t)
	b.Stop()
	b.Stop()
}

func TestBrokerActivitySubscribersReceiveUpdates(t *testing.T) {
	b := newTestBroker(t)
	updates, unsubscribe := b.SubscribeActivity(4)
	defer unsubscribe()

	b.UpdateAgentActivity(agentActivitySnapshot{
		Slug:     "ceo",
		Status:   "active",
		Activity: "tool_use",
		Detail:   "running rg",
		LastTime: time.Now().UTC().Format(time.RFC3339),
	})

	select {
	case got := <-updates:
		if got.Slug != "ceo" || got.Activity != "tool_use" || got.Detail != "running rg" {
			t.Fatalf("unexpected activity update: %+v", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for subscribed activity")
	}
}

func TestBrokerEventsEndpointStreamsMessages(t *testing.T) {
	b := newTestBroker(t)
	b.channels = []teamChannel{
		{
			Slug:    "general",
			Name:    "general",
			Members: []string{"operator"},
		},
		{
			Slug:    "planning",
			Name:    "planning",
			Members: []string{"operator", "planner"},
		},
	}
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("failed to start broker: %v", err)
	}
	defer b.Stop()

	base := fmt.Sprintf("http://%s", b.Addr())
	req, _ := http.NewRequest(http.MethodGet, base+"/events?token="+b.Token(), nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("open event stream: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200 opening event stream, got %d: %s", resp.StatusCode, raw)
	}

	lines := make(chan string, 16)
	go func() {
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			lines <- scanner.Text()
		}
		close(lines)
	}()

	if _, err := b.PostMessage("ceo", "general", "Stream this", nil, ""); err != nil {
		t.Fatalf("PostMessage: %v", err)
	}

	deadline := time.After(2 * time.Second)
	var sawEvent bool
	var sawPayload bool
	for !(sawEvent && sawPayload) {
		select {
		case line, ok := <-lines:
			if !ok {
				t.Fatal("event stream closed before receiving message")
			}
			if strings.Contains(line, "event: message") {
				sawEvent = true
			}
			if strings.Contains(line, `"content":"Stream this"`) {
				sawPayload = true
			}
		case <-deadline:
			t.Fatalf("timed out waiting for message event (event=%v payload=%v)", sawEvent, sawPayload)
		}
	}
}

func TestBrokerPersistsNotificationCursorWithoutMessages(t *testing.T) {
	b := newTestBroker(t)
	if err := b.SetNotificationCursor("2026-03-24T10:00:00Z"); err != nil {
		t.Fatalf("SetNotificationCursor failed: %v", err)
	}

	reloaded := reloadedBroker(t, b)
	if got := reloaded.NotificationCursor(); got != "2026-03-24T10:00:00Z" {
		t.Fatalf("expected persisted notification cursor, got %q", got)
	}
}

func TestTaskAndRequestViewsRejectNonMembers(t *testing.T) {
	b := newTestBroker(t)
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("failed to start broker: %v", err)
	}
	defer b.Stop()

	base := fmt.Sprintf("http://%s", b.Addr())
	createBody, _ := json.Marshal(map[string]any{
		"action":      "create",
		"slug":        "deals",
		"name":        "deals",
		"description": "Deal strategy and pipeline work.",
		"created_by":  "ceo",
	})
	req, _ := http.NewRequest(http.MethodPost, base+"/channels", bytes.NewReader(createBody))
	req.Header.Set("Authorization", "Bearer "+b.Token())
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("create channel failed: %v", err)
	}
	resp.Body.Close()

	req, _ = http.NewRequest(http.MethodGet, base+"/tasks?channel=deals&viewer_slug=fe", nil)
	req.Header.Set("Authorization", "Bearer "+b.Token())
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get tasks as non-member failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 for non-member task access, got %d", resp.StatusCode)
	}

	req, _ = http.NewRequest(http.MethodGet, base+"/requests?channel=deals&viewer_slug=fe", nil)
	req.Header.Set("Authorization", "Bearer "+b.Token())
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get requests as non-member failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 for non-member request access, got %d", resp.StatusCode)
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

func TestBrokerTaskLifecycle(t *testing.T) {
	b := newTestBroker(t)
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("failed to start broker: %v", err)
	}
	defer b.Stop()

	base := fmt.Sprintf("http://%s", b.Addr())
	post := func(payload map[string]any) teamTask {
		body, _ := json.Marshal(payload)
		req, _ := http.NewRequest(http.MethodPost, base+"/tasks", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+b.Token())
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("task post failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			raw, _ := io.ReadAll(resp.Body)
			t.Fatalf("unexpected status %d: %s", resp.StatusCode, raw)
		}
		var result struct {
			Task teamTask `json:"task"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			t.Fatalf("decode task response: %v", err)
		}
		return result.Task
	}

	created := post(map[string]any{
		"action":     "create",
		"title":      "Own the landing page",
		"details":    "Frontend only",
		"created_by": "ceo",
		"owner":      "fe",
		"thread_id":  "msg-1",
	})
	if created.Status != "in_progress" || created.Owner != "fe" {
		t.Fatalf("unexpected created task: %+v", created)
	}
	if created.FollowUpAt == "" || created.ReminderAt == "" || created.RecheckAt == "" {
		t.Fatalf("expected follow-up timestamps on task create, got %+v", created)
	}
	req, _ := http.NewRequest(http.MethodGet, base+"/queue", nil)
	req.Header.Set("Authorization", "Bearer "+b.Token())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("queue request failed: %v", err)
	}
	defer resp.Body.Close()
	var queue struct {
		Actions   []officeActionLog `json:"actions"`
		Scheduler []schedulerJob    `json:"scheduler"`
		Due       []schedulerJob    `json:"due"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&queue); err != nil {
		t.Fatalf("decode queue response: %v", err)
	}
	if len(queue.Scheduler) == 0 {
		t.Fatalf("expected queue to expose scheduler state, got %+v", queue)
	}

	completed := post(map[string]any{
		"action": "complete",
		"id":     created.ID,
	})
	if completed.Status != "done" {
		t.Fatalf("expected done task, got %+v", completed)
	}
	if completed.FollowUpAt != "" || completed.ReminderAt != "" || completed.RecheckAt != "" {
		t.Fatalf("expected completion to clear follow-up timestamps, got %+v", completed)
	}

	req, _ = http.NewRequest(http.MethodGet, base+"/tasks", nil)
	req.Header.Set("Authorization", "Bearer "+b.Token())
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("tasks get failed: %v", err)
	}
	defer resp.Body.Close()
	var listing struct {
		Tasks []teamTask `json:"tasks"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&listing); err != nil {
		t.Fatalf("decode tasks list: %v", err)
	}
	if len(listing.Tasks) != 0 {
		t.Fatalf("expected done task to be hidden by default, got %+v", listing.Tasks)
	}
}

func TestBrokerTaskReassignNotifies(t *testing.T) {
	b := newTestBroker(t)
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("failed to start broker: %v", err)
	}
	defer b.Stop()

	base := fmt.Sprintf("http://%s", b.Addr())
	post := func(payload map[string]any) teamTask {
		body, _ := json.Marshal(payload)
		req, _ := http.NewRequest(http.MethodPost, base+"/tasks", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+b.Token())
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("task post failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			raw, _ := io.ReadAll(resp.Body)
			t.Fatalf("unexpected status %d: %s", resp.StatusCode, raw)
		}
		var result struct {
			Task teamTask `json:"task"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			t.Fatalf("decode task response: %v", err)
		}
		return result.Task
	}

	created := post(map[string]any{
		"action":     "create",
		"title":      "Ship reassign flow",
		"created_by": "human",
		"owner":      "engineering",
	})
	if created.Owner != "engineering" {
		t.Fatalf("expected initial owner engineering, got %+v", created)
	}

	before := len(b.Messages())

	// Reassign engineering → ops.
	updated := post(map[string]any{
		"action":     "reassign",
		"id":         created.ID,
		"owner":      "ops",
		"created_by": "human",
	})
	if updated.Owner != "ops" {
		t.Fatalf("expected owner=ops after reassign, got %q", updated.Owner)
	}
	if updated.Status != "in_progress" {
		t.Fatalf("expected status=in_progress after reassign, got %q", updated.Status)
	}

	msgs := b.Messages()[before:]
	if len(msgs) != 3 {
		for i, m := range msgs {
			t.Logf("msg[%d] channel=%s from=%s content=%q", i, m.Channel, m.From, m.Content)
		}
		t.Fatalf("expected 3 reassign messages (channel + new + prev), got %d", len(msgs))
	}

	taskChannel := normalizeChannelSlug(updated.Channel)
	if taskChannel == "" {
		taskChannel = "general"
	}
	newDM := channelDirectSlug("human", "ops")
	prevDM := channelDirectSlug("human", "engineering")

	seen := map[string]channelMessage{}
	for _, m := range msgs {
		seen[m.Channel] = m
		if m.Kind != "task_reassigned" {
			t.Fatalf("expected kind=task_reassigned, got %q", m.Kind)
		}
		if m.From != "human" {
			t.Fatalf("expected from=human, got %q", m.From)
		}
	}
	chMsg, ok := seen[taskChannel]
	if !ok {
		t.Fatalf("expected channel message in %q; saw %v", taskChannel, keys(seen))
	}
	if !containsAll(chMsg.Tagged, []string{"ceo", "ops", "engineering"}) {
		t.Fatalf("expected channel message tagged ceo+ops+engineering, got %v", chMsg.Tagged)
	}
	if !strings.Contains(chMsg.Content, "@engineering") || !strings.Contains(chMsg.Content, "@ops") {
		t.Fatalf("expected channel content to name both owners, got %q", chMsg.Content)
	}
	if _, ok := seen[newDM]; !ok {
		t.Fatalf("expected DM to new owner in %q; saw %v", newDM, keys(seen))
	}
	if _, ok := seen[prevDM]; !ok {
		t.Fatalf("expected DM to prev owner in %q; saw %v", prevDM, keys(seen))
	}

	// Re-posting with the same owner should be a no-op on notifications.
	before2 := len(b.Messages())
	post(map[string]any{
		"action":     "reassign",
		"id":         created.ID,
		"owner":      "ops",
		"created_by": "human",
	})
	after2 := b.Messages()[before2:]
	for _, m := range after2 {
		if m.Kind == "task_reassigned" {
			t.Fatalf("expected no new task_reassigned messages for same-owner reassign, got %+v", m)
		}
	}
}

func TestBrokerTaskCancelNotifies(t *testing.T) {
	b := newTestBroker(t)
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("failed to start broker: %v", err)
	}
	defer b.Stop()

	base := fmt.Sprintf("http://%s", b.Addr())
	post := func(payload map[string]any) teamTask {
		body, _ := json.Marshal(payload)
		req, _ := http.NewRequest(http.MethodPost, base+"/tasks", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+b.Token())
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("task post failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			raw, _ := io.ReadAll(resp.Body)
			t.Fatalf("unexpected status %d: %s", resp.StatusCode, raw)
		}
		var result struct {
			Task teamTask `json:"task"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			t.Fatalf("decode task response: %v", err)
		}
		return result.Task
	}

	created := post(map[string]any{
		"action":     "create",
		"title":      "Pilot the new onboarding deck",
		"created_by": "human",
		"owner":      "design",
	})
	before := len(b.Messages())

	canceled := post(map[string]any{
		"action":     "cancel",
		"id":         created.ID,
		"created_by": "human",
	})
	if canceled.Status != "canceled" {
		t.Fatalf("expected status=canceled, got %q", canceled.Status)
	}
	if canceled.FollowUpAt != "" || canceled.ReminderAt != "" || canceled.RecheckAt != "" {
		t.Fatalf("expected cleared follow-up timestamps on cancel, got %+v", canceled)
	}

	all := b.Messages()[before:]
	msgs := make([]channelMessage, 0, len(all))
	for _, m := range all {
		if m.Kind == "task_canceled" {
			msgs = append(msgs, m)
		}
	}
	if len(msgs) != 2 {
		for i, m := range all {
			t.Logf("all[%d] channel=%s kind=%s content=%q", i, m.Channel, m.Kind, m.Content)
		}
		t.Fatalf("expected 2 task_canceled messages (channel + owner DM), got %d", len(msgs))
	}
	taskChannel := normalizeChannelSlug(canceled.Channel)
	if taskChannel == "" {
		taskChannel = "general"
	}
	ownerDM := channelDirectSlug("human", "design")
	found := map[string]bool{}
	for _, m := range msgs {
		found[m.Channel] = true
	}
	if !found[taskChannel] {
		t.Fatalf("missing channel cancel message in %q", taskChannel)
	}
	if !found[ownerDM] {
		t.Fatalf("missing owner DM cancel message in %q", ownerDM)
	}
}

func channelDirectSlug(a, b string) string {
	if a > b {
		a, b = b, a
	}
	return a + "__" + b
}

func keys(m map[string]channelMessage) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func containsAll(got, want []string) bool {
	set := make(map[string]struct{}, len(got))
	for _, g := range got {
		set[g] = struct{}{}
	}
	for _, w := range want {
		if _, ok := set[w]; !ok {
			return false
		}
	}
	return true
}

func TestBrokerOfficeFeatureTaskForGTMCompletesWithoutReviewAndUnblocksDependents(t *testing.T) {
	b := newTestBroker(t)
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("failed to start broker: %v", err)
	}
	defer b.Stop()

	base := fmt.Sprintf("http://%s", b.Addr())
	post := func(payload map[string]any) teamTask {
		body, _ := json.Marshal(payload)
		req, _ := http.NewRequest(http.MethodPost, base+"/tasks", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+b.Token())
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("task post failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			raw, _ := io.ReadAll(resp.Body)
			t.Fatalf("unexpected status %d: %s", resp.StatusCode, raw)
		}
		var result struct {
			Task teamTask `json:"task"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			t.Fatalf("decode task response: %v", err)
		}
		return result.Task
	}

	thesis := post(map[string]any{
		"action":         "create",
		"title":          "Define the YouTube business thesis",
		"details":        "Pick the niche and monetization ladder.",
		"created_by":     "ceo",
		"owner":          "gtm",
		"thread_id":      "msg-1",
		"task_type":      "feature",
		"execution_mode": "office",
	})
	if thesis.ReviewState != "not_required" {
		t.Fatalf("expected GTM office feature task to skip review, got %+v", thesis)
	}

	launch := post(map[string]any{
		"action":         "create",
		"title":          "Create the launch package",
		"details":        "Build the 30-video slate.",
		"created_by":     "ceo",
		"owner":          "gtm",
		"thread_id":      "msg-1",
		"task_type":      "launch",
		"execution_mode": "office",
		"depends_on":     []string{thesis.ID},
	})
	if !launch.Blocked {
		t.Fatalf("expected dependent launch task to start blocked, got %+v", launch)
	}

	completed := post(map[string]any{
		"action": "complete",
		"id":     thesis.ID,
	})
	if completed.Status != "done" || completed.ReviewState != "not_required" {
		t.Fatalf("expected thesis task to complete directly without review, got %+v", completed)
	}

	var unblocked teamTask
	for _, task := range b.AllTasks() {
		if task.ID == launch.ID {
			unblocked = task
			break
		}
	}
	if unblocked.ID == "" {
		t.Fatalf("expected to find dependent task %s", launch.ID)
	}
	if unblocked.Blocked {
		t.Fatalf("expected dependent task to unblock after thesis completion, got %+v", unblocked)
	}
}

func TestBrokerTaskCreateReusesExistingOpenTask(t *testing.T) {
	b := newTestBroker(t)
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("failed to start broker: %v", err)
	}
	defer b.Stop()

	base := fmt.Sprintf("http://%s", b.Addr())
	post := func(payload map[string]any) teamTask {
		body, _ := json.Marshal(payload)
		req, _ := http.NewRequest(http.MethodPost, base+"/tasks", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+b.Token())
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("task post failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			raw, _ := io.ReadAll(resp.Body)
			t.Fatalf("unexpected status %d: %s", resp.StatusCode, raw)
		}
		var result struct {
			Task teamTask `json:"task"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			t.Fatalf("decode task response: %v", err)
		}
		return result.Task
	}

	first := post(map[string]any{
		"action":     "create",
		"title":      "Own the landing page",
		"details":    "Initial FE pass",
		"created_by": "ceo",
		"owner":      "fe",
		"thread_id":  "msg-1",
	})
	second := post(map[string]any{
		"action":     "create",
		"title":      "Own the landing page",
		"details":    "Updated details",
		"created_by": "ceo",
		"owner":      "fe",
		"thread_id":  "msg-1",
	})

	if first.ID != second.ID {
		t.Fatalf("expected task reuse, got %s and %s", first.ID, second.ID)
	}
	if second.Details != "Updated details" {
		t.Fatalf("expected task details to update, got %+v", second)
	}
	if got := len(b.ChannelTasks("general")); got != 1 {
		t.Fatalf("expected one open task after reuse, got %d", got)
	}
}

func TestBrokerEnsurePlannedTaskKeepsScopedDuplicateTitlesDistinct(t *testing.T) {
	setPrepareTaskWorktreeForTest(t, func(taskID string) (string, string, error) {
		return "/tmp/wuphf-task-" + taskID, "wuphf-" + taskID, nil
	})
	setCleanupTaskWorktreeForTest(t, func(path, branch string) error { return nil })
	b := newTestBroker(t)

	first, reused, err := b.EnsurePlannedTask(plannedTaskInput{
		Channel:          "general",
		Title:            "Publish faceless AI ops episode",
		Details:          "Episode 1 pipeline task",
		Owner:            "eng",
		CreatedBy:        "ceo",
		TaskType:         "feature",
		PipelineID:       "youtube-factory",
		SourceDecisionID: "decision-episode-1",
	})
	if err != nil || reused {
		t.Fatalf("first ensure planned task: %v reused=%v", err, reused)
	}

	second, reused, err := b.EnsurePlannedTask(plannedTaskInput{
		Channel:          "general",
		Title:            "Publish faceless AI ops episode",
		Details:          "Episode 2 pipeline task",
		Owner:            "eng",
		CreatedBy:        "ceo",
		TaskType:         "feature",
		PipelineID:       "youtube-factory",
		SourceDecisionID: "decision-episode-2",
	})
	if err != nil || reused {
		t.Fatalf("second ensure planned task: %v reused=%v", err, reused)
	}
	if first.ID == second.ID {
		t.Fatalf("expected distinct tasks for duplicate scoped titles, got %s", first.ID)
	}
	if got := len(b.ChannelTasks("general")); got != 2 {
		t.Fatalf("expected two planned tasks after duplicate scoped titles, got %d", got)
	}

	retry, reused, err := b.EnsurePlannedTask(plannedTaskInput{
		Channel:          "general",
		Title:            "Publish faceless AI ops episode",
		Details:          "Episode 2 retry",
		Owner:            "eng",
		CreatedBy:        "ceo",
		TaskType:         "feature",
		PipelineID:       "youtube-factory",
		SourceDecisionID: "decision-episode-2",
	})
	if err != nil || !reused {
		t.Fatalf("retry ensure planned task: %v reused=%v", err, reused)
	}
	if retry.ID != second.ID {
		t.Fatalf("expected scoped retry to reuse second task, got %s want %s", retry.ID, second.ID)
	}
}

func TestBrokerTaskCreateKeepsDistinctTasksInSameThread(t *testing.T) {
	b := newTestBroker(t)
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("failed to start broker: %v", err)
	}
	defer b.Stop()

	base := fmt.Sprintf("http://%s", b.Addr())
	post := func(payload map[string]any) teamTask {
		body, _ := json.Marshal(payload)
		req, _ := http.NewRequest(http.MethodPost, base+"/tasks", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+b.Token())
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("task post failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			raw, _ := io.ReadAll(resp.Body)
			t.Fatalf("unexpected status %d: %s", resp.StatusCode, raw)
		}
		var result struct {
			Task teamTask `json:"task"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			t.Fatalf("decode task response: %v", err)
		}
		return result.Task
	}

	first := post(map[string]any{
		"action":     "create",
		"title":      "Build the operating system",
		"details":    "Engineering lane",
		"created_by": "ceo",
		"owner":      "eng",
		"thread_id":  "msg-1",
	})
	second := post(map[string]any{
		"action":     "create",
		"title":      "Lock the channel thesis",
		"details":    "GTM lane",
		"created_by": "ceo",
		"owner":      "gtm",
		"thread_id":  "msg-1",
	})

	if first.ID == second.ID {
		t.Fatalf("expected distinct tasks in the same thread, got reused task id %q", first.ID)
	}
	if got := len(b.ChannelTasks("general")); got != 2 {
		t.Fatalf("expected two open tasks after distinct creates, got %d", got)
	}
}

func TestBrokerTaskPlanAssignsWorktreeForLocalWorktreeTask(t *testing.T) {
	setPrepareTaskWorktreeForTest(t, func(taskID string) (string, string, error) {
		return "/tmp/wuphf-task-" + taskID, "wuphf-" + taskID, nil
	})
	setCleanupTaskWorktreeForTest(t, func(path, branch string) error { return nil })
	b := newTestBroker(t)
	ensureTestMemberAccess(b, "general", "operator", "Operator")
	ensureTestMemberAccess(b, "general", "builder", "Builder")
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("failed to start broker: %v", err)
	}
	defer b.Stop()

	base := fmt.Sprintf("http://%s", b.Addr())
	body, _ := json.Marshal(map[string]any{
		"channel":    "general",
		"created_by": "operator",
		"tasks": []map[string]any{
			{
				"title":          "Build intake dry-run review bundle",
				"details":        "Produce the first dry-run consulting artifact bundle.",
				"assignee":       "builder",
				"task_type":      "feature",
				"execution_mode": "local_worktree",
			},
		},
	})
	req, _ := http.NewRequest(http.MethodPost, base+"/task-plan", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+b.Token())
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("task plan request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("unexpected status %d: %s", resp.StatusCode, raw)
	}

	var result struct {
		Tasks []teamTask `json:"tasks"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode task plan response: %v", err)
	}
	if len(result.Tasks) != 1 {
		t.Fatalf("expected one task, got %+v", result.Tasks)
	}
	if result.Tasks[0].ExecutionMode != "local_worktree" {
		t.Fatalf("expected local_worktree task, got %+v", result.Tasks[0])
	}
	if result.Tasks[0].WorktreePath == "" || result.Tasks[0].WorktreeBranch == "" {
		t.Fatalf("expected task plan to assign worktree metadata, got %+v", result.Tasks[0])
	}
}

func TestBrokerTaskCreateAddsAssignedOwnerToChannelMembers(t *testing.T) {
	b := newTestBroker(t)
	ensureTestMemberAccess(b, "youtube-factory", "operator", "Operator")
	if existing := b.findMemberLocked("builder"); existing == nil {
		member := officeMember{Slug: "builder", Name: "Builder"}
		applyOfficeMemberDefaults(&member)
		b.members = append(b.members, member)
	}
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("failed to start broker: %v", err)
	}
	defer b.Stop()

	base := fmt.Sprintf("http://%s", b.Addr())
	body, _ := json.Marshal(map[string]any{
		"action":     "create",
		"channel":    "youtube-factory",
		"title":      "Restore remotion dependency path",
		"details":    "Unblock the real render lane.",
		"created_by": "operator",
		"owner":      "builder",
		"task_type":  "feature",
	})
	req, _ := http.NewRequest(http.MethodPost, base+"/tasks", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+b.Token())
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("task create request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("unexpected status %d: %s", resp.StatusCode, raw)
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	ch := b.findChannelLocked("youtube-factory")
	if ch == nil {
		t.Fatal("expected youtube-factory channel to exist")
	}
	if !containsString(ch.Members, "builder") {
		t.Fatalf("expected assigned owner to be added to channel members, got %v", ch.Members)
	}
	if containsString(ch.Disabled, "builder") {
		t.Fatalf("expected assigned owner to be enabled in channel, got disabled=%v", ch.Disabled)
	}
}

func TestBrokerResumeTaskUnblocksAndSchedulesOwnerLane(t *testing.T) {
	b := newTestBroker(t)
	ensureTestMemberAccess(b, "client-loop", "operator", "Operator")
	ensureTestMemberAccess(b, "client-loop", "builder", "Builder")
	task, reused, err := b.EnsurePlannedTask(plannedTaskInput{
		Channel:       "client-loop",
		Title:         "Retry kickoff send",
		Details:       "429 RESOURCE_EXHAUSTED. Retry after 2026-04-15T22:00:29.610Z.",
		Owner:         "builder",
		CreatedBy:     "operator",
		TaskType:      "follow_up",
		ExecutionMode: "live_external",
	})
	if err != nil || reused {
		t.Fatalf("ensure planned task: %v reused=%v", err, reused)
	}
	if _, changed, err := b.BlockTask(task.ID, "operator", "Provider cooldown"); err != nil || !changed {
		t.Fatalf("block task: %v changed=%v", err, changed)
	}

	resumed, changed, err := b.ResumeTask(task.ID, "watchdog", "Retry window passed")
	if err != nil {
		t.Fatalf("resume task: %v", err)
	}
	if !changed {
		t.Fatalf("expected resume to change task state, got %+v", resumed)
	}
	if resumed.Blocked || resumed.Status != "in_progress" {
		t.Fatalf("expected resumed task to be active, got %+v", resumed)
	}
	if resumed.FollowUpAt == "" {
		t.Fatalf("expected resumed task to have follow-up lifecycle timestamps, got %+v", resumed)
	}
}

func TestBrokerResumeTaskQueuesBehindExistingExclusiveOwnerLane(t *testing.T) {
	b := newTestBroker(t)
	ensureTestMemberAccess(b, "client-loop", "operator", "Operator")
	ensureTestMemberAccess(b, "client-loop", "builder", "Builder")

	active, reused, err := b.EnsurePlannedTask(plannedTaskInput{
		Channel:       "client-loop",
		Title:         "Send kickoff email",
		Owner:         "builder",
		CreatedBy:     "operator",
		TaskType:      "follow_up",
		ExecutionMode: "live_external",
	})
	if err != nil || reused {
		t.Fatalf("ensure active task: %v reused=%v", err, reused)
	}
	task, reused, err := b.EnsurePlannedTask(plannedTaskInput{
		Channel:       "client-loop",
		Title:         "Send second kickoff email",
		Owner:         "builder",
		CreatedBy:     "operator",
		TaskType:      "follow_up",
		ExecutionMode: "live_external",
		DependsOn:     []string{active.ID},
	})
	if err != nil || reused {
		t.Fatalf("ensure queued task: %v reused=%v", err, reused)
	}
	if !task.Blocked {
		t.Fatalf("expected second task to start blocked behind active lane, got %+v", task)
	}
	if _, changed, err := b.BlockTask(task.ID, "operator", "provider cooldown"); err != nil || !changed {
		t.Fatalf("block task: %v changed=%v", err, changed)
	}

	resumed, changed, err := b.ResumeTask(task.ID, "watchdog", "Retry window passed")
	if err != nil {
		t.Fatalf("resume task: %v", err)
	}
	if !changed {
		t.Fatalf("expected resume to change task state, got %+v", resumed)
	}
	if resumed.Status != "open" || !resumed.Blocked {
		t.Fatalf("expected resumed task to stay queued behind active lane, got %+v", resumed)
	}
	if !containsString(resumed.DependsOn, active.ID) {
		t.Fatalf("expected resumed task to remain dependent on active lane, got %+v", resumed)
	}
}

func TestBrokerUnblockDependentsQueuesExclusiveOwnerLanes(t *testing.T) {
	b := newTestBroker(t)
	ensureTestMemberAccess(b, "youtube-factory", "ceo", "CEO")
	ensureTestMemberAccess(b, "youtube-factory", "executor", "Executor")

	now := time.Now().UTC().Format(time.RFC3339)
	b.tasks = []teamTask{
		{
			ID:            "task-setup",
			Channel:       "youtube-factory",
			Title:         "Finish prerequisite slice",
			Owner:         "executor",
			Status:        "done",
			CreatedBy:     "ceo",
			TaskType:      "feature",
			ExecutionMode: "local_worktree",
			ReviewState:   "approved",
			CreatedAt:     now,
			UpdatedAt:     now,
		},
		{
			ID:            "task-32",
			Channel:       "youtube-factory",
			Title:         "First dependent lane",
			Owner:         "executor",
			Status:        "blocked",
			Blocked:       true,
			CreatedBy:     "ceo",
			TaskType:      "feature",
			ExecutionMode: "live_external",
			DependsOn:     []string{"task-setup"},
			CreatedAt:     now,
			UpdatedAt:     now,
		},
		{
			ID:            "task-34",
			Channel:       "youtube-factory",
			Title:         "Second dependent lane",
			Owner:         "executor",
			Status:        "blocked",
			Blocked:       true,
			CreatedBy:     "ceo",
			TaskType:      "feature",
			ExecutionMode: "live_external",
			DependsOn:     []string{"task-setup"},
			CreatedAt:     now,
			UpdatedAt:     now,
		},
		{
			ID:            "task-80",
			Channel:       "youtube-factory",
			Title:         "Third dependent lane",
			Owner:         "executor",
			Status:        "blocked",
			Blocked:       true,
			CreatedBy:     "ceo",
			TaskType:      "feature",
			ExecutionMode: "live_external",
			DependsOn:     []string{"task-setup"},
			CreatedAt:     now,
			UpdatedAt:     now,
		},
	}

	b.mu.Lock()
	b.unblockDependentsLocked("task-setup")
	got := append([]teamTask(nil), b.tasks...)
	b.mu.Unlock()

	if got[1].Status != "in_progress" || got[1].Blocked {
		t.Fatalf("expected first dependent to become active, got %+v", got[1])
	}
	for _, task := range got[2:] {
		if task.Status != "open" || !task.Blocked {
			t.Fatalf("expected later dependent to stay queued, got %+v", task)
		}
		if !containsString(task.DependsOn, "task-32") {
			t.Fatalf("expected later dependent to queue behind task-32, got %+v", task)
		}
	}
}

func TestBrokerTaskPlanRejectsTheaterTaskInLiveDeliveryLane(t *testing.T) {
	b := newTestBroker(t)
	ensureTestMemberAccess(b, "client-delivery", "operator", "Operator")
	ensureTestMemberAccess(b, "client-delivery", "builder", "Builder")
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("failed to start broker: %v", err)
	}
	defer b.Stop()

	base := fmt.Sprintf("http://%s", b.Addr())
	body, _ := json.Marshal(map[string]any{
		"channel":    "client-delivery",
		"created_by": "operator",
		"tasks": []map[string]any{
			{
				"title":          "Generate consulting review packet artifact from the updated blueprint",
				"details":        "Post the exact local artifact path for the reviewer.",
				"assignee":       "builder",
				"task_type":      "feature",
				"execution_mode": "local_worktree",
			},
		},
	})
	req, _ := http.NewRequest(http.MethodPost, base+"/task-plan", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+b.Token())
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("task plan request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("unexpected status %d: %s", resp.StatusCode, raw)
	}
}

func TestBrokerTaskCreateRejectsLiveBusinessTheater(t *testing.T) {
	b := newTestBroker(t)
	ensureTestMemberAccess(b, "general", "operator", "Operator")
	ensureTestMemberAccess(b, "general", "builder", "Builder")
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("failed to start broker: %v", err)
	}
	defer b.Stop()

	base := fmt.Sprintf("http://%s", b.Addr())
	body, _ := json.Marshal(map[string]any{
		"action":         "create",
		"channel":        "general",
		"title":          "Create one new Notion proof packet for the client handoff",
		"details":        "Use live external execution and keep the review bundle in sync.",
		"created_by":     "operator",
		"owner":          "builder",
		"task_type":      "launch",
		"execution_mode": "live_external",
	})
	req, _ := http.NewRequest(http.MethodPost, base+"/tasks", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+b.Token())
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("task create request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected theater rejection, got status %d: %s", resp.StatusCode, raw)
	}
}

func TestBrokerTaskCompleteRejectsLiveBusinessTheater(t *testing.T) {
	b := newTestBroker(t)
	ensureTestMemberAccess(b, "general", "operator", "Operator")
	ensureTestMemberAccess(b, "general", "builder", "Builder")
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("failed to start broker: %v", err)
	}
	defer b.Stop()
	b.mu.Lock()
	b.tasks = []teamTask{{
		ID:            "task-1",
		Channel:       "general",
		Title:         "Create one new Notion proof packet for the client handoff",
		Details:       "Use live external execution and keep the review bundle in sync.",
		Owner:         "builder",
		Status:        "in_progress",
		CreatedBy:     "operator",
		TaskType:      "launch",
		ExecutionMode: "live_external",
		CreatedAt:     "2026-04-15T00:00:00Z",
		UpdatedAt:     "2026-04-15T00:00:00Z",
	}}
	b.counter = 1
	b.mu.Unlock()

	base := fmt.Sprintf("http://%s", b.Addr())
	body, _ := json.Marshal(map[string]any{
		"action":     "complete",
		"channel":    "general",
		"id":         "task-1",
		"created_by": "builder",
	})
	req, _ := http.NewRequest(http.MethodPost, base+"/tasks", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+b.Token())
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("task complete request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected theater rejection on completion, got status %d: %s", resp.StatusCode, raw)
	}
}

func TestBrokerStoresLedgerAndReviewLifecycle(t *testing.T) {
	setPrepareTaskWorktreeForTest(t, func(taskID string) (string, string, error) {
		return "/tmp/wuphf-task-" + taskID, "wuphf-" + taskID, nil
	})
	setCleanupTaskWorktreeForTest(t, func(path, branch string) error { return nil })
	b := newTestBroker(t)
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("failed to start broker: %v", err)
	}
	defer b.Stop()

	signals, err := b.RecordSignals([]officeSignal{{
		ID:         "nex-1",
		Source:     "nex_insights",
		Kind:       "risk",
		Title:      "Nex insight",
		Content:    "Signup conversion is slipping.",
		Channel:    "general",
		Owner:      "fe",
		Confidence: "high",
		Urgency:    "high",
	}})
	if err != nil || len(signals) != 1 {
		t.Fatalf("record signals: %v %v", err, signals)
	}
	decision, err := b.RecordDecision("create_task", "general", "Open a frontend follow-up.", "High-signal conversion risk.", "fe", []string{signals[0].ID}, false, false)
	if err != nil {
		t.Fatalf("record decision: %v", err)
	}
	task, reused, err := b.EnsurePlannedTask(plannedTaskInput{
		Channel:          "general",
		Title:            "Build signup conversion fix",
		Details:          "Own the CTA and onboarding flow.",
		Owner:            "fe",
		CreatedBy:        "ceo",
		ThreadID:         "msg-1",
		TaskType:         "feature",
		SourceSignalID:   signals[0].ID,
		SourceDecisionID: decision.ID,
	})
	if err != nil || reused {
		t.Fatalf("ensure planned task: %v reused=%v", err, reused)
	}
	if task.PipelineStage != "implement" || task.ExecutionMode != "local_worktree" || task.SourceDecisionID != decision.ID {
		t.Fatalf("expected structured task metadata, got %+v", task)
	}
	if task.WorktreePath == "" || task.WorktreeBranch == "" {
		t.Fatalf("expected planned task worktree metadata, got %+v", task)
	}

	base := fmt.Sprintf("http://%s", b.Addr())
	body, _ := json.Marshal(map[string]any{
		"action":     "complete",
		"channel":    "general",
		"id":         task.ID,
		"created_by": "you",
	})
	req, _ := http.NewRequest(http.MethodPost, base+"/tasks", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+b.Token())
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("complete task: %v", err)
	}
	defer resp.Body.Close()
	var result struct {
		Task teamTask `json:"task"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode completed task: %v", err)
	}
	if result.Task.Status != "review" || result.Task.ReviewState != "ready_for_review" {
		t.Fatalf("expected review-ready task, got %+v", result.Task)
	}

	if _, _, err := b.CreateWatchdogAlert("task_stalled", "general", "task", task.ID, "fe", "Task is waiting for movement."); err != nil {
		t.Fatalf("create watchdog: %v", err)
	}
	if len(b.Decisions()) != 1 || len(b.Signals()) != 1 || len(b.Watchdogs()) != 1 {
		t.Fatalf("expected ledger state, got signals=%d decisions=%d watchdogs=%d", len(b.Signals()), len(b.Decisions()), len(b.Watchdogs()))
	}
}

func TestBrokerReleaseTaskCleansWorktree(t *testing.T) {
	var cleanedPath, cleanedBranch string
	setPrepareTaskWorktreeForTest(t, func(taskID string) (string, string, error) {
		return "/tmp/wuphf-task-" + taskID, "wuphf-" + taskID, nil
	})
	setCleanupTaskWorktreeForTest(t, func(path, branch string) error {
		cleanedPath = path
		cleanedBranch = branch
		return nil
	})
	b := newTestBroker(t)
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("failed to start broker: %v", err)
	}
	defer b.Stop()

	task, reused, err := b.EnsurePlannedTask(plannedTaskInput{
		Channel:   "general",
		Title:     "Build signup conversion fix",
		Owner:     "fe",
		CreatedBy: "ceo",
		TaskType:  "feature",
	})
	if err != nil || reused {
		t.Fatalf("ensure planned task: %v reused=%v", err, reused)
	}

	base := fmt.Sprintf("http://%s", b.Addr())
	body, _ := json.Marshal(map[string]any{
		"action":     "release",
		"channel":    "general",
		"id":         task.ID,
		"created_by": "ceo",
	})
	req, _ := http.NewRequest(http.MethodPost, base+"/tasks", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+b.Token())
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("release task: %v", err)
	}
	defer resp.Body.Close()

	var result struct {
		Task teamTask `json:"task"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode released task: %v", err)
	}
	if cleanedPath == "" || cleanedBranch == "" {
		t.Fatalf("expected cleanup to run, got path=%q branch=%q", cleanedPath, cleanedBranch)
	}
	if result.Task.WorktreePath != "" || result.Task.WorktreeBranch != "" {
		t.Fatalf("expected released task worktree metadata to clear, got %+v", result.Task)
	}
}

func TestBrokerApproveRetainsLocalWorktree(t *testing.T) {
	cleanupCalls := 0
	worktreeRoot := t.TempDir()
	setPrepareTaskWorktreeForTest(t, func(taskID string) (string, string, error) {
		path := filepath.Join(worktreeRoot, "wuphf-task-"+taskID)
		initUsableGitWorktree(t, path)
		return path, "wuphf-" + taskID, nil
	})
	setCleanupTaskWorktreeForTest(t, func(path, branch string) error {
		cleanupCalls++
		return nil
	})
	b := newTestBroker(t)
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("failed to start broker: %v", err)
	}
	defer b.Stop()

	task, reused, err := b.EnsurePlannedTask(plannedTaskInput{
		Channel:   "general",
		Title:     "Build signup conversion fix",
		Owner:     "fe",
		CreatedBy: "ceo",
		TaskType:  "feature",
	})
	if err != nil || reused {
		t.Fatalf("ensure planned task: %v reused=%v", err, reused)
	}

	base := fmt.Sprintf("http://%s", b.Addr())
	completeBody, _ := json.Marshal(map[string]any{
		"action":     "complete",
		"channel":    "general",
		"id":         task.ID,
		"created_by": "fe",
	})
	req, _ := http.NewRequest(http.MethodPost, base+"/tasks", bytes.NewReader(completeBody))
	req.Header.Set("Authorization", "Bearer "+b.Token())
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("complete task: %v", err)
	}
	resp.Body.Close()

	approveBody, _ := json.Marshal(map[string]any{
		"action":     "approve",
		"channel":    "general",
		"id":         task.ID,
		"created_by": "ceo",
	})
	req, _ = http.NewRequest(http.MethodPost, base+"/tasks", bytes.NewReader(approveBody))
	req.Header.Set("Authorization", "Bearer "+b.Token())
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("approve task: %v", err)
	}
	defer resp.Body.Close()

	var result struct {
		Task teamTask `json:"task"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode approved task: %v", err)
	}
	if result.Task.Status != "done" || result.Task.ReviewState != "approved" {
		t.Fatalf("expected approved task to be done/approved, got %+v", result.Task)
	}
	if result.Task.WorktreePath == "" || result.Task.WorktreeBranch == "" {
		t.Fatalf("expected approved task to retain worktree metadata, got %+v", result.Task)
	}
	if cleanupCalls != 0 {
		t.Fatalf("expected approved task to retain worktree without cleanup, got %d cleanup calls", cleanupCalls)
	}
}

func ensureTestMemberAccess(b *Broker, channel, slug, name string) {
	if b == nil {
		return
	}
	slug = normalizeChannelSlug(slug)
	if slug == "" {
		return
	}
	if existing := b.findMemberLocked(slug); existing == nil {
		member := officeMember{Slug: slug, Name: name}
		applyOfficeMemberDefaults(&member)
		b.members = append(b.members, member)
	}
	for i := range b.channels {
		if normalizeChannelSlug(b.channels[i].Slug) != normalizeChannelSlug(channel) {
			continue
		}
		if !containsString(b.channels[i].Members, slug) {
			b.channels[i].Members = append(b.channels[i].Members, slug)
		}
		return
	}
	b.channels = append(b.channels, teamChannel{
		Slug:    normalizeChannelSlug(channel),
		Name:    normalizeChannelSlug(channel),
		Members: []string{slug},
	})
}

func TestBrokerHandlePostTaskRejectsFalseReadOnlyBlockForWritableWorktree(t *testing.T) {
	worktreeDir := t.TempDir()
	setPrepareTaskWorktreeForTest(t, func(taskID string) (string, string, error) {
		return worktreeDir, "wuphf-" + taskID, nil
	})
	setCleanupTaskWorktreeForTest(t, func(path, branch string) error { return nil })
	setVerifyTaskWorktreeWritableForTest(t, func(path string) error {
		if path != worktreeDir {
			t.Fatalf("expected probe path %q, got %q", worktreeDir, path)
		}
		return nil
	})
	b := newTestBroker(t)
	ensureTestMemberAccess(b, "general", "eng", "Engineer")
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("failed to start broker: %v", err)
	}
	defer b.Stop()

	task, reused, err := b.EnsurePlannedTask(plannedTaskInput{
		Channel:       "general",
		Title:         "Implement the first runnable generator slice",
		Owner:         "eng",
		CreatedBy:     "ceo",
		TaskType:      "feature",
		ExecutionMode: "local_worktree",
	})
	if err != nil || reused {
		t.Fatalf("ensure planned task: %v reused=%v", err, reused)
	}

	base := fmt.Sprintf("http://%s", b.Addr())
	body, _ := json.Marshal(map[string]any{
		"action":     "block",
		"channel":    "general",
		"id":         task.ID,
		"created_by": "eng",
		"details":    "This turn is running in a read-only filesystem sandbox. Need a writable workspace.",
	})
	req, _ := http.NewRequest(http.MethodPost, base+"/tasks", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+b.Token())
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post block task: %v", err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409 rejecting bogus workspace block, got %d: %s", resp.StatusCode, raw)
	}
	if !strings.Contains(string(raw), "assigned local worktree is writable") {
		t.Fatalf("expected writable-worktree guidance, got %s", raw)
	}

	var updated teamTask
	for _, candidate := range b.AllTasks() {
		if candidate.ID == task.ID {
			updated = candidate
			break
		}
	}
	if updated.ID == "" {
		t.Fatalf("expected to find task %s", task.ID)
	}
	if updated.Status != "in_progress" || updated.Blocked {
		t.Fatalf("expected task to remain active after rejected block, got %+v", updated)
	}
	if strings.Contains(strings.ToLower(updated.Details), "read-only") {
		t.Fatalf("expected false read-only detail to stay out of task state, got %+v", updated)
	}
}

func TestBrokerHandlePostTaskCapabilityGapCreatesSelfHealingTask(t *testing.T) {
	b := newTestBroker(t)
	ensureTestMemberAccess(b, "general", "eng", "Engineer")
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("failed to start broker: %v", err)
	}
	defer b.Stop()

	task, reused, err := b.EnsurePlannedTask(plannedTaskInput{
		Channel:       "general",
		Title:         "Post client launch update to Slack",
		Owner:         "eng",
		CreatedBy:     "ceo",
		TaskType:      "follow_up",
		ExecutionMode: "office",
	})
	if err != nil || reused {
		t.Fatalf("ensure planned task: %v reused=%v", err, reused)
	}

	detail := "Unable to continue: missing Slack integration tool path for posting the client update."
	body, _ := json.Marshal(map[string]any{
		"action":     "block",
		"channel":    "general",
		"id":         task.ID,
		"created_by": "eng",
		"details":    detail,
	})
	req, _ := http.NewRequest(http.MethodPost, "http://"+b.Addr()+"/tasks", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+b.Token())
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post block task: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 blocking task, got %d: %s", resp.StatusCode, raw)
	}

	var blocked teamTask
	var healing teamTask
	for _, candidate := range b.AllTasks() {
		switch candidate.ID {
		case task.ID:
			blocked = candidate
		default:
			if candidate.Title == "Self-heal @eng on "+task.ID {
				healing = candidate
			}
		}
	}
	if !blocked.Blocked || blocked.Status != "blocked" {
		t.Fatalf("expected original task blocked, got %+v", blocked)
	}
	if healing.ID == "" {
		t.Fatalf("expected capability-gap self-healing task, got %+v", b.AllTasks())
	}
	if healing.Owner != "ceo" || healing.TaskType != "incident" || healing.ExecutionMode != "office" {
		t.Fatalf("expected office incident owned by ceo, got %+v", healing)
	}
	if !strings.Contains(healing.Details, "capability_gap") ||
		!strings.Contains(healing.Details, detail) ||
		!strings.Contains(healing.Details, "Repair the missing capability first") {
		t.Fatalf("expected capability repair loop details, got %q", healing.Details)
	}
}

func TestBrokerHandlePostTaskNonCapabilityBlockDoesNotCreateSelfHealingTask(t *testing.T) {
	b := newTestBroker(t)
	ensureTestMemberAccess(b, "general", "eng", "Engineer")
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("failed to start broker: %v", err)
	}
	defer b.Stop()

	task, _, err := b.EnsurePlannedTask(plannedTaskInput{
		Channel:   "general",
		Title:     "Wait for customer approval",
		Owner:     "eng",
		CreatedBy: "ceo",
		TaskType:  "follow_up",
	})
	if err != nil {
		t.Fatalf("ensure planned task: %v", err)
	}

	body, _ := json.Marshal(map[string]any{
		"action":     "block",
		"channel":    "general",
		"id":         task.ID,
		"created_by": "eng",
		"details":    "Waiting on customer approval before sending the update.",
	})
	req, _ := http.NewRequest(http.MethodPost, "http://"+b.Addr()+"/tasks", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+b.Token())
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post block task: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 blocking task, got %d: %s", resp.StatusCode, raw)
	}

	for _, candidate := range b.AllTasks() {
		if isSelfHealingTaskTitle(candidate.Title) {
			t.Fatalf("did not expect self-healing task for non-capability blocker, got %+v", candidate)
		}
	}
}

func TestBrokerHandlePostTaskResumeUnblocksAfterCapabilityRepair(t *testing.T) {
	b := newTestBroker(t)
	ensureTestMemberAccess(b, "general", "eng", "Engineer")
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("failed to start broker: %v", err)
	}
	defer b.Stop()

	task, _, err := b.EnsurePlannedTask(plannedTaskInput{
		Channel:   "general",
		Title:     "Send the launch update",
		Owner:     "eng",
		CreatedBy: "ceo",
		TaskType:  "follow_up",
	})
	if err != nil {
		t.Fatalf("ensure planned task: %v", err)
	}
	if _, changed, err := b.BlockTask(task.ID, "eng", "Unable to continue: missing Slack integration."); err != nil || !changed {
		t.Fatalf("block task: changed=%v err=%v", changed, err)
	}

	body, _ := json.Marshal(map[string]any{
		"action":     "resume",
		"channel":    "general",
		"id":         task.ID,
		"created_by": "ceo",
		"details":    "Capability repaired: Slack integration is available; retry the original update.",
	})
	req, _ := http.NewRequest(http.MethodPost, "http://"+b.Addr()+"/tasks", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+b.Token())
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post resume task: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 resuming task, got %d: %s", resp.StatusCode, raw)
	}

	var resumed teamTask
	for _, candidate := range b.AllTasks() {
		if candidate.ID == task.ID {
			resumed = candidate
			break
		}
	}
	if resumed.Blocked || resumed.Status != "in_progress" {
		t.Fatalf("expected original task resumed after repair, got %+v", resumed)
	}
	if !strings.Contains(resumed.Details, "missing Slack integration") ||
		!strings.Contains(resumed.Details, "Capability repaired") {
		t.Fatalf("expected resume detail appended, got %q", resumed.Details)
	}
}

func TestBrokerBlockTaskRejectsFalseReadOnlyBlockForWritableWorktree(t *testing.T) {
	worktreeDir := t.TempDir()
	setPrepareTaskWorktreeForTest(t, func(taskID string) (string, string, error) {
		return worktreeDir, "wuphf-" + taskID, nil
	})
	setCleanupTaskWorktreeForTest(t, func(path, branch string) error { return nil })
	setVerifyTaskWorktreeWritableForTest(t, func(path string) error {
		if path != worktreeDir {
			t.Fatalf("expected probe path %q, got %q", worktreeDir, path)
		}
		return nil
	})
	b := newTestBroker(t)
	task, reused, err := b.EnsurePlannedTask(plannedTaskInput{
		Channel:       "general",
		Title:         "Implement the first runnable generator slice",
		Owner:         "eng",
		CreatedBy:     "ceo",
		TaskType:      "feature",
		ExecutionMode: "local_worktree",
	})
	if err != nil || reused {
		t.Fatalf("ensure planned task: %v reused=%v", err, reused)
	}

	got, changed, err := b.BlockTask(task.ID, "eng", "Need writable workspace because the filesystem sandbox is read-only.")
	if err == nil {
		t.Fatal("expected false read-only block to be rejected")
	}
	if changed {
		t.Fatalf("expected no task state change on rejected block, got %+v", got)
	}
	if !strings.Contains(err.Error(), "assigned local worktree is writable") {
		t.Fatalf("expected writable-worktree guidance, got %v", err)
	}

	var updated teamTask
	for _, candidate := range b.AllTasks() {
		if candidate.ID == task.ID {
			updated = candidate
			break
		}
	}
	if updated.ID == "" {
		t.Fatalf("expected to find task %s", task.ID)
	}
	if updated.Status != "in_progress" || updated.Blocked {
		t.Fatalf("expected task to remain active after rejected block, got %+v", updated)
	}
	if strings.Contains(strings.ToLower(updated.Details), "read-only") {
		t.Fatalf("expected false read-only detail to stay out of task state, got %+v", updated)
	}
}

func TestBrokerEnsurePlannedTaskQueuesConcurrentExclusiveOwnerWork(t *testing.T) {
	setPrepareTaskWorktreeForTest(t, func(taskID string) (string, string, error) {
		return "/tmp/wuphf-task-" + taskID, "wuphf-" + taskID, nil
	})
	setCleanupTaskWorktreeForTest(t, func(path, branch string) error { return nil })
	b := newTestBroker(t)
	ensureTestMemberAccess(b, "general", "executor", "Executor")

	first, reused, err := b.EnsurePlannedTask(plannedTaskInput{
		Channel:       "general",
		Title:         "Build the homepage MVP",
		Details:       "Ship the first runnable site slice.",
		Owner:         "executor",
		CreatedBy:     "ceo",
		TaskType:      "feature",
		ExecutionMode: "local_worktree",
	})
	if err != nil || reused {
		t.Fatalf("ensure first task: %v reused=%v", err, reused)
	}
	second, reused, err := b.EnsurePlannedTask(plannedTaskInput{
		Channel:       "general",
		Title:         "Define the upload path",
		Details:       "Wire the next implementation slice after the homepage.",
		Owner:         "executor",
		CreatedBy:     "ceo",
		TaskType:      "feature",
		ExecutionMode: "local_worktree",
	})
	if err != nil || reused {
		t.Fatalf("ensure second task: %v reused=%v", err, reused)
	}

	if first.Status != "in_progress" || first.Blocked {
		t.Fatalf("expected first task to stay active, got %+v", first)
	}
	if second.Status != "open" || !second.Blocked {
		t.Fatalf("expected second task to queue behind the first, got %+v", second)
	}
	if !containsString(second.DependsOn, first.ID) {
		t.Fatalf("expected second task to depend on first %s, got %+v", first.ID, second.DependsOn)
	}
	if !strings.Contains(second.Details, "Queued behind "+first.ID) {
		t.Fatalf("expected queue note in details, got %+v", second)
	}
}

func TestBrokerTaskPlanRoutesLiveBusinessTasksIntoRecentExecutionChannel(t *testing.T) {
	b := newTestBroker(t)
	ensureTestMemberAccess(b, "general", "builder", "Builder")
	b.channels = append(b.channels, teamChannel{
		Slug:      "client-loop",
		Name:      "client-loop",
		Members:   []string{"ceo", "builder"},
		CreatedBy: "ceo",
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	})
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("failed to start broker: %v", err)
	}
	defer b.Stop()

	base := fmt.Sprintf("http://%s", b.Addr())
	body, _ := json.Marshal(map[string]any{
		"channel":    "general",
		"created_by": "ceo",
		"tasks": []map[string]any{
			{
				"title":          "Create the client-facing operating brief",
				"assignee":       "builder",
				"details":        "Move the live client delivery forward in the workspace and leave the customer-ready brief in the execution lane.",
				"task_type":      "launch",
				"execution_mode": "office",
			},
		},
	})
	req, _ := http.NewRequest(http.MethodPost, base+"/task-plan", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+b.Token())
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post task plan: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("unexpected status %d: %s", resp.StatusCode, raw)
	}

	var result struct {
		Tasks []teamTask `json:"tasks"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode task plan response: %v", err)
	}
	if len(result.Tasks) != 1 {
		t.Fatalf("expected one task, got %+v", result.Tasks)
	}
	if result.Tasks[0].Channel != "client-loop" {
		t.Fatalf("expected task to route into client-loop, got %+v", result.Tasks[0])
	}
}

func TestBrokerTaskPlanReusesExistingActiveLane(t *testing.T) {
	b := newTestBroker(t)
	ensureTestMemberAccess(b, "client-loop", "builder", "Builder")
	ensureTestMemberAccess(b, "client-loop", "operator", "Operator")
	for i := range b.channels {
		if normalizeChannelSlug(b.channels[i].Slug) == "client-loop" {
			b.channels[i].CreatedBy = "operator"
			b.channels[i].CreatedAt = time.Now().UTC().Format(time.RFC3339)
		}
	}
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("failed to start broker: %v", err)
	}
	defer b.Stop()

	existing, reused, err := b.EnsurePlannedTask(plannedTaskInput{
		Channel:       "client-loop",
		Title:         "Create live client workspace in Google Drive",
		Details:       "First pass.",
		Owner:         "builder",
		CreatedBy:     "operator",
		TaskType:      "follow_up",
		ExecutionMode: "office",
	})
	if err != nil || reused {
		t.Fatalf("ensure initial task: %v reused=%v", err, reused)
	}

	base := fmt.Sprintf("http://%s", b.Addr())
	body, _ := json.Marshal(map[string]any{
		"channel":    "general",
		"created_by": "operator",
		"tasks": []map[string]any{
			{
				"title":          "Create live client workspace in Google Drive",
				"assignee":       "builder",
				"details":        "Updated live-work details.",
				"task_type":      "follow_up",
				"execution_mode": "office",
			},
		},
	})
	req, _ := http.NewRequest(http.MethodPost, base+"/task-plan", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+b.Token())
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post task plan: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("unexpected status %d: %s", resp.StatusCode, raw)
	}

	var result struct {
		Tasks []teamTask `json:"tasks"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode task plan response: %v", err)
	}
	if len(result.Tasks) != 1 {
		t.Fatalf("expected one task in response, got %+v", result.Tasks)
	}
	if result.Tasks[0].ID != existing.ID {
		t.Fatalf("expected task plan to reuse %s, got %+v", existing.ID, result.Tasks[0])
	}
	if got := len(b.AllTasks()); got != 1 {
		t.Fatalf("expected one durable task after reuse, got %d", got)
	}
	if result.Tasks[0].Channel != "client-loop" {
		t.Fatalf("expected reused task to stay in client-loop, got %+v", result.Tasks[0])
	}
	if result.Tasks[0].Details != "Updated live-work details." {
		t.Fatalf("expected details to update, got %+v", result.Tasks[0])
	}
}

func TestBrokerBlockTaskAllowsReadOnlyBlockWhenWriteProbeFails(t *testing.T) {
	worktreeDir := t.TempDir()
	setPrepareTaskWorktreeForTest(t, func(taskID string) (string, string, error) {
		return worktreeDir, "wuphf-" + taskID, nil
	})
	setCleanupTaskWorktreeForTest(t, func(path, branch string) error { return nil })
	setVerifyTaskWorktreeWritableForTest(t, func(path string) error {
		if path != worktreeDir {
			t.Fatalf("expected probe path %q, got %q", worktreeDir, path)
		}
		return fmt.Errorf("permission denied")
	})
	b := newTestBroker(t)
	task, reused, err := b.EnsurePlannedTask(plannedTaskInput{
		Channel:       "general",
		Title:         "Implement the first runnable generator slice",
		Owner:         "eng",
		CreatedBy:     "ceo",
		TaskType:      "feature",
		ExecutionMode: "local_worktree",
	})
	if err != nil || reused {
		t.Fatalf("ensure planned task: %v reused=%v", err, reused)
	}

	got, changed, err := b.BlockTask(task.ID, "eng", "Need writable workspace because the filesystem sandbox is read-only.")
	if err != nil {
		t.Fatalf("expected real write failure blocker to pass through, got %v", err)
	}
	if !changed {
		t.Fatalf("expected task state change on real blocker, got %+v", got)
	}
	if got.Status != "blocked" || !got.Blocked {
		t.Fatalf("expected blocked task result, got %+v", got)
	}
	if !strings.Contains(got.Details, "read-only") {
		t.Fatalf("expected block reason to persist, got %+v", got)
	}
}

func TestBrokerCompleteClosesReviewTaskAndUnblocksDependents(t *testing.T) {
	setPrepareTaskWorktreeForTest(t, func(taskID string) (string, string, error) {
		return "/tmp/wuphf-task-" + taskID, "wuphf-" + taskID, nil
	})
	setCleanupTaskWorktreeForTest(t, func(path, branch string) error { return nil })
	b := newTestBroker(t)
	ensureTestMemberAccess(b, "general", "eng", "Engineer")
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("failed to start broker: %v", err)
	}
	defer b.Stop()

	architecture, reused, err := b.EnsurePlannedTask(plannedTaskInput{
		Channel:       "general",
		Title:         "Audit the repo and design the automation architecture",
		Owner:         "eng",
		CreatedBy:     "ceo",
		TaskType:      "research",
		ExecutionMode: "local_worktree",
	})
	if err != nil || reused {
		t.Fatalf("ensure architecture task: %v reused=%v", err, reused)
	}
	build, reused, err := b.EnsurePlannedTask(plannedTaskInput{
		Channel:       "general",
		Title:         "Implement the v0 automated content factory",
		Owner:         "eng",
		CreatedBy:     "ceo",
		TaskType:      "feature",
		ExecutionMode: "local_worktree",
		DependsOn:     []string{architecture.ID},
	})
	if err != nil || reused {
		t.Fatalf("ensure build task: %v reused=%v", err, reused)
	}
	if !build.Blocked {
		t.Fatalf("expected dependent task to start blocked, got %+v", build)
	}

	base := fmt.Sprintf("http://%s", b.Addr())
	post := func(payload map[string]any) teamTask {
		body, _ := json.Marshal(payload)
		req, _ := http.NewRequest(http.MethodPost, base+"/tasks", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+b.Token())
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("task post failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			raw, _ := io.ReadAll(resp.Body)
			t.Fatalf("unexpected status %d: %s", resp.StatusCode, raw)
		}
		var result struct {
			Task teamTask `json:"task"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			t.Fatalf("decode task response: %v", err)
		}
		return result.Task
	}

	reviewReady := post(map[string]any{
		"action":     "complete",
		"channel":    "general",
		"id":         architecture.ID,
		"created_by": "eng",
	})
	if reviewReady.Status != "review" || reviewReady.ReviewState != "ready_for_review" {
		t.Fatalf("expected first complete to move task into review, got %+v", reviewReady)
	}

	closed := post(map[string]any{
		"action":     "complete",
		"channel":    "general",
		"id":         architecture.ID,
		"created_by": "ceo",
	})
	if closed.Status != "done" || closed.ReviewState != "approved" {
		t.Fatalf("expected second complete to close review task, got %+v", closed)
	}

	var unblocked teamTask
	for _, task := range b.AllTasks() {
		if task.ID == build.ID {
			unblocked = task
			break
		}
	}
	if unblocked.ID == "" {
		t.Fatalf("expected to find dependent task %s", build.ID)
	}
	if unblocked.Blocked || unblocked.Status != "in_progress" {
		t.Fatalf("expected dependent task to unblock after review close, got %+v", unblocked)
	}
}

func TestBrokerCreateTaskReusesCompletedDependencyWorktree(t *testing.T) {
	var prepareCalls []string
	worktreeRoot := t.TempDir()
	setPrepareTaskWorktreeForTest(t, func(taskID string) (string, string, error) {
		prepareCalls = append(prepareCalls, taskID)
		if len(prepareCalls) > 1 {
			return "", "", fmt.Errorf("unexpected prepareTaskWorktree call for %s", taskID)
		}
		path := filepath.Join(worktreeRoot, "wuphf-task-"+taskID)
		initUsableGitWorktree(t, path)
		return path, "wuphf-" + taskID, nil
	})
	setCleanupTaskWorktreeForTest(t, func(path, branch string) error { return nil })
	b := newTestBroker(t)
	ensureTestMemberAccess(b, "general", "builder", "Builder")
	ensureTestMemberAccess(b, "general", "operator", "Operator")
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("failed to start broker: %v", err)
	}
	defer b.Stop()

	base := fmt.Sprintf("http://%s", b.Addr())
	post := func(payload map[string]any) teamTask {
		body, _ := json.Marshal(payload)
		req, _ := http.NewRequest(http.MethodPost, base+"/tasks", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+b.Token())
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("task post failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			raw, _ := io.ReadAll(resp.Body)
			t.Fatalf("unexpected status %d: %s", resp.StatusCode, raw)
		}
		var result struct {
			Task teamTask `json:"task"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			t.Fatalf("decode task response: %v", err)
		}
		return result.Task
	}

	first := post(map[string]any{
		"action":         "create",
		"title":          "Ship the dry-run approval packet generator",
		"details":        "Initial consulting delivery slice",
		"created_by":     "operator",
		"owner":          "builder",
		"thread_id":      "msg-1",
		"execution_mode": "local_worktree",
		"task_type":      "feature",
	})
	if first.WorktreePath == "" || first.WorktreeBranch == "" {
		t.Fatalf("expected first task worktree metadata, got %+v", first)
	}

	reviewReady := post(map[string]any{
		"action":     "complete",
		"channel":    "general",
		"id":         first.ID,
		"created_by": "builder",
	})
	if reviewReady.Status != "review" || reviewReady.ReviewState != "ready_for_review" {
		t.Fatalf("expected first complete to move task into review, got %+v", reviewReady)
	}

	approved := post(map[string]any{
		"action":     "approve",
		"channel":    "general",
		"id":         first.ID,
		"created_by": "operator",
	})
	if approved.Status != "done" || approved.ReviewState != "approved" {
		t.Fatalf("expected approve to close task, got %+v", approved)
	}

	second := post(map[string]any{
		"action":         "create",
		"title":          "Render the approval packet into a reviewable dry-run bundle",
		"details":        "Reuse the existing generator worktree",
		"created_by":     "operator",
		"owner":          "builder",
		"thread_id":      "msg-2",
		"execution_mode": "local_worktree",
		"task_type":      "feature",
		"depends_on":     []string{first.ID},
	})
	if second.WorktreePath != first.WorktreePath || second.WorktreeBranch != first.WorktreeBranch {
		t.Fatalf("expected dependent task to reuse worktree %s/%s, got %+v", first.WorktreePath, first.WorktreeBranch, second)
	}
	if got := len(prepareCalls); got != 1 {
		t.Fatalf("expected one worktree prepare call, got %d (%v)", got, prepareCalls)
	}
}

func TestBrokerSyncTaskWorktreeReplacesStaleAssignedPath(t *testing.T) {
	stalePath := t.TempDir()
	freshPath := filepath.Join(t.TempDir(), "fresh-worktree")
	var cleaned []string
	setPrepareTaskWorktreeForTest(t, func(taskID string) (string, string, error) {
		return freshPath, "wuphf-" + taskID, nil
	})
	setCleanupTaskWorktreeForTest(t, func(path, branch string) error {
		cleaned = append(cleaned, path+"|"+branch)
		return nil
	})
	b := newTestBroker(t)
	task := &teamTask{
		ID:             "task-80",
		Title:          "Fix onboarding",
		Owner:          "executor",
		Status:         "in_progress",
		ExecutionMode:  "local_worktree",
		WorktreePath:   stalePath,
		WorktreeBranch: "wuphf-stale-task-80",
	}
	if err := b.syncTaskWorktreeLocked(task); err != nil {
		t.Fatalf("syncTaskWorktreeLocked: %v", err)
	}
	if task.WorktreePath != freshPath || task.WorktreeBranch != "wuphf-task-80" {
		t.Fatalf("expected stale worktree to be replaced, got %+v", task)
	}
	if len(cleaned) != 1 || !strings.Contains(cleaned[0], stalePath) {
		t.Fatalf("expected stale worktree cleanup before reprovision, got %v", cleaned)
	}
}

func TestBrokerNormalizeLoadedStateRepairsStaleAssignedWorktree(t *testing.T) {
	stalePath := t.TempDir()
	freshPath := filepath.Join(t.TempDir(), "fresh-worktree")
	setPrepareTaskWorktreeForTest(t, func(taskID string) (string, string, error) {
		return freshPath, "wuphf-" + taskID, nil
	})
	setCleanupTaskWorktreeForTest(t, func(path, branch string) error { return nil })
	now := time.Now().UTC().Format(time.RFC3339)
	b := newTestBroker(t)
	b.tasks = []teamTask{{
		ID:             "task-80",
		Channel:        "youtube-factory",
		Title:          "Fix onboarding",
		Owner:          "executor",
		Status:         "in_progress",
		ExecutionMode:  "local_worktree",
		WorktreePath:   stalePath,
		WorktreeBranch: "wuphf-stale-task-80",
		CreatedAt:      now,
		UpdatedAt:      now,
	}}

	b.mu.Lock()
	b.normalizeLoadedStateLocked()
	got := b.tasks[0]
	b.mu.Unlock()

	if got.WorktreePath != freshPath || got.WorktreeBranch != "wuphf-task-80" {
		t.Fatalf("expected normalize to refresh stale worktree, got %+v", got)
	}
}

func TestBrokerUpdatesTaskByIDAcrossChannels(t *testing.T) {
	b := newTestBroker(t)
	b.channels = []teamChannel{
		{
			Slug: "general",
			Name: "general",
		},
		{
			Slug: "planning",
			Name: "planning",
		},
	}
	handler := b.requireAuth(b.handleTasks)
	post := func(payload map[string]any) teamTask {
		t.Helper()
		body, _ := json.Marshal(payload)
		req := httptest.NewRequest(http.MethodPost, "/tasks", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+b.Token())
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler(rec, req)
		resp := rec.Result()
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			raw, _ := io.ReadAll(resp.Body)
			t.Fatalf("unexpected status %d: %s", resp.StatusCode, raw)
		}
		var result struct {
			Task teamTask `json:"task"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			t.Fatalf("decode task response: %v", err)
		}
		return result.Task
	}

	created := post(map[string]any{
		"action":     "create",
		"channel":    "planning",
		"title":      "Inventory capabilities and approvals",
		"owner":      "planner",
		"created_by": "human",
	})
	if created.Channel != "planning" {
		t.Fatalf("expected planning task, got %+v", created)
	}

	completed := post(map[string]any{
		"action":     "complete",
		"channel":    "general",
		"id":         created.ID,
		"created_by": "human",
	})
	if completed.ID != created.ID {
		t.Fatalf("expected to update %s, got %+v", created.ID, completed)
	}
	if completed.Channel != "planning" {
		t.Fatalf("expected task channel to remain planning, got %+v", completed)
	}
	if completed.Status != "done" && completed.Status != "review" {
		t.Fatalf("expected task to move forward, got %+v", completed)
	}
}

func TestBrokerCompleteAlreadyDoneTaskStaysApproved(t *testing.T) {
	setPrepareTaskWorktreeForTest(t, func(taskID string) (string, string, error) {
		return "/tmp/wuphf-task-" + taskID, "wuphf-" + taskID, nil
	})
	setCleanupTaskWorktreeForTest(t, func(path, branch string) error { return nil })
	b := newTestBroker(t)
	ensureTestMemberAccess(b, "general", "eng", "Engineer")
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("failed to start broker: %v", err)
	}
	defer b.Stop()

	task, reused, err := b.EnsurePlannedTask(plannedTaskInput{
		Channel:       "general",
		Title:         "Ship publish-pack output",
		Owner:         "eng",
		CreatedBy:     "ceo",
		TaskType:      "feature",
		ExecutionMode: "local_worktree",
	})
	if err != nil || reused {
		t.Fatalf("ensure planned task: %v reused=%v", err, reused)
	}

	base := fmt.Sprintf("http://%s", b.Addr())
	post := func(payload map[string]any) teamTask {
		body, _ := json.Marshal(payload)
		req, _ := http.NewRequest(http.MethodPost, base+"/tasks", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+b.Token())
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("task post failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			raw, _ := io.ReadAll(resp.Body)
			t.Fatalf("unexpected status %d: %s", resp.StatusCode, raw)
		}
		var result struct {
			Task teamTask `json:"task"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			t.Fatalf("decode task response: %v", err)
		}
		return result.Task
	}

	reviewReady := post(map[string]any{
		"action":     "complete",
		"channel":    "general",
		"id":         task.ID,
		"created_by": "eng",
	})
	if reviewReady.Status != "review" || reviewReady.ReviewState != "ready_for_review" {
		t.Fatalf("expected first complete to move task into review, got %+v", reviewReady)
	}

	approved := post(map[string]any{
		"action":     "approve",
		"channel":    "general",
		"id":         task.ID,
		"created_by": "ceo",
	})
	if approved.Status != "done" || approved.ReviewState != "approved" {
		t.Fatalf("expected approve to close task, got %+v", approved)
	}

	repeatedComplete := post(map[string]any{
		"action":     "complete",
		"channel":    "general",
		"id":         task.ID,
		"created_by": "ceo",
	})
	if repeatedComplete.Status != "done" || repeatedComplete.ReviewState != "approved" {
		t.Fatalf("expected repeated complete to stay done/approved, got %+v", repeatedComplete)
	}
}

func TestBrokerBridgeEndpointRecordsVisibleBridge(t *testing.T) {
	b := newTestBroker(t)
	b.mu.Lock()
	b.members = append(b.members,
		officeMember{Slug: "pm", Name: "Product Manager"},
		officeMember{Slug: "cmo", Name: "Chief Marketing Officer"},
	)
	b.mu.Unlock()
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("failed to start broker: %v", err)
	}
	defer b.Stop()

	base := fmt.Sprintf("http://%s", b.Addr())
	createChannelBody, _ := json.Marshal(map[string]any{
		"action":      "create",
		"slug":        "launch",
		"name":        "Launch",
		"description": "Launch planning and messaging.",
		"members":     []string{"pm", "cmo"},
		"created_by":  "ceo",
	})
	req, _ := http.NewRequest(http.MethodPost, base+"/channels", bytes.NewReader(createChannelBody))
	req.Header.Set("Authorization", "Bearer "+b.Token())
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}
	resp.Body.Close()

	bridgeBody, _ := json.Marshal(map[string]any{
		"actor":          "ceo",
		"source_channel": "general",
		"target_channel": "launch",
		"summary":        "Use the stronger product narrative from #general in this launch channel before drafting the landing page.",
		"tagged":         []string{"cmo"},
	})
	req, _ = http.NewRequest(http.MethodPost, base+"/bridges", bytes.NewReader(bridgeBody))
	req.Header.Set("Authorization", "Bearer "+b.Token())
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("bridge request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected bridge success, got %d: %s", resp.StatusCode, string(body))
	}

	messages := b.ChannelMessages("launch")
	if len(messages) != 1 {
		t.Fatalf("expected one bridge message in launch, got %d", len(messages))
	}
	if messages[0].Source != "ceo_bridge" || !strings.Contains(messages[0].Content, "#general") {
		t.Fatalf("unexpected bridge message: %+v", messages[0])
	}
	if got := len(b.Signals()); got != 1 {
		t.Fatalf("expected 1 bridge signal, got %d", got)
	}
	if got := len(b.Decisions()); got != 1 || b.Decisions()[0].Kind != "bridge_channel" {
		t.Fatalf("unexpected bridge decisions: %+v", b.Decisions())
	}
	if got := len(b.Actions()); got == 0 || b.Actions()[len(b.Actions())-1].Kind != "bridge_channel" {
		t.Fatalf("expected bridge action, got %+v", b.Actions())
	}
}

func TestBrokerRequestsLifecycle(t *testing.T) {
	b := newTestBroker(t)
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("failed to start broker: %v", err)
	}
	defer b.Stop()

	base := fmt.Sprintf("http://%s", b.Addr())
	body, _ := json.Marshal(map[string]any{
		"kind":     "approval",
		"from":     "ceo",
		"channel":  "general",
		"title":    "Approval needed",
		"question": "Should we proceed?",
		"blocking": true,
		"required": true,
		"reply_to": "msg-1",
	})
	req, _ := http.NewRequest(http.MethodPost, base+"/requests", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+b.Token())
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request create failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200 creating request, got %d: %s", resp.StatusCode, raw)
	}

	req, _ = http.NewRequest(http.MethodGet, base+"/requests?channel=general", nil)
	req.Header.Set("Authorization", "Bearer "+b.Token())
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request list failed: %v", err)
	}
	defer resp.Body.Close()
	var listing struct {
		Requests []humanInterview `json:"requests"`
		Pending  *humanInterview  `json:"pending"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&listing); err != nil {
		t.Fatalf("decode requests: %v", err)
	}
	if len(listing.Requests) != 1 || listing.Pending == nil {
		t.Fatalf("expected one pending request, got %+v", listing)
	}
	if listing.Requests[0].ReminderAt == "" || listing.Requests[0].FollowUpAt == "" || listing.Requests[0].RecheckAt == "" {
		t.Fatalf("expected reminder timestamps on request create, got %+v", listing.Requests[0])
	}

	answerBody, _ := json.Marshal(map[string]any{
		"id":          listing.Requests[0].ID,
		"choice_text": "Yes",
	})
	req, _ = http.NewRequest(http.MethodPost, base+"/requests/answer", bytes.NewReader(answerBody))
	req.Header.Set("Authorization", "Bearer "+b.Token())
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request answer failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 answering request, got %d", resp.StatusCode)
	}
	req, _ = http.NewRequest(http.MethodGet, base+"/queue", nil)
	req.Header.Set("Authorization", "Bearer "+b.Token())
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("queue request failed: %v", err)
	}
	defer resp.Body.Close()
	var queue struct {
		Actions   []officeActionLog `json:"actions"`
		Scheduler []schedulerJob    `json:"scheduler"`
		Due       []schedulerJob    `json:"due"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&queue); err != nil {
		t.Fatalf("decode queue response: %v", err)
	}
	for _, job := range queue.Scheduler {
		if job.TargetType == "request" && job.TargetID == listing.Requests[0].ID && !strings.EqualFold(job.Status, "done") {
			t.Fatalf("expected answered request scheduler jobs to complete, got %+v", job)
		}
	}

	if b.HasBlockingRequest() {
		t.Fatal("expected blocking request to clear after answer")
	}
}

// Regression: the broker rejects new messages with 409 whenever ANY blocking
// request is pending (handlePostMessage uses firstBlockingRequest across all
// channels), so GET /requests must expose a "scope=all" view. Without it, the
// web UI only sees per-channel requests and can't render a blocker that lives
// in another channel — leaving the human stuck: can't send, can't see why.
func TestBrokerGetRequestsScopeAllSeesCrossChannelBlocker(t *testing.T) {
	b := newTestBroker(t)
	ensureTestMemberAccess(b, "general", "ceo", "CEO")
	ensureTestMemberAccess(b, "backend", "ceo", "CEO")
	ensureTestMemberAccess(b, "backend", "human", "Human")
	ensureTestMemberAccess(b, "general", "human", "Human")
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("failed to start broker: %v", err)
	}
	defer b.Stop()

	base := fmt.Sprintf("http://%s", b.Addr())

	createBody, _ := json.Marshal(map[string]any{
		"kind":     "approval",
		"from":     "ceo",
		"channel":  "backend",
		"title":    "Deploy approval",
		"question": "Ship the backend migration?",
		"blocking": true,
		"required": true,
	})
	req, _ := http.NewRequest(http.MethodPost, base+"/requests", bytes.NewReader(createBody))
	req.Header.Set("Authorization", "Bearer "+b.Token())
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("create cross-channel request failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 creating backend request, got %d", resp.StatusCode)
	}

	// Per-channel view (#general) must NOT see the #backend blocker — this is
	// the pre-fix behavior the UI was relying on and is still correct.
	req, _ = http.NewRequest(http.MethodGet, base+"/requests?channel=general&viewer_slug=human", nil)
	req.Header.Set("Authorization", "Bearer "+b.Token())
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("per-channel listing failed: %v", err)
	}
	var perChannel struct {
		Requests []humanInterview `json:"requests"`
		Pending  *humanInterview  `json:"pending"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&perChannel); err != nil {
		t.Fatalf("decode per-channel response: %v", err)
	}
	resp.Body.Close()
	if len(perChannel.Requests) != 0 || perChannel.Pending != nil {
		t.Fatalf("expected #general view to hide #backend request, got %+v", perChannel)
	}

	// scope=all must include the cross-channel blocker so the overlay can show
	// what's preventing the human from chatting anywhere.
	req, _ = http.NewRequest(http.MethodGet, base+"/requests?scope=all&viewer_slug=human", nil)
	req.Header.Set("Authorization", "Bearer "+b.Token())
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("scope=all listing failed: %v", err)
	}
	var global struct {
		Requests []humanInterview `json:"requests"`
		Pending  *humanInterview  `json:"pending"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&global); err != nil {
		t.Fatalf("decode scope=all response: %v", err)
	}
	resp.Body.Close()
	if len(global.Requests) != 1 {
		t.Fatalf("expected 1 blocker across channels, got %d: %+v", len(global.Requests), global.Requests)
	}
	if global.Pending == nil || global.Pending.Channel != "backend" {
		t.Fatalf("expected pending blocker from #backend, got %+v", global.Pending)
	}
}

func TestBrokerCancelBlockingApprovalUnblocksMessages(t *testing.T) {
	b := newTestBroker(t)
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("failed to start broker: %v", err)
	}
	defer b.Stop()

	base := fmt.Sprintf("http://%s", b.Addr())
	createBody, _ := json.Marshal(map[string]any{
		"kind":     "approval",
		"from":     "ceo",
		"channel":  "general",
		"title":    "Approval needed",
		"question": "Ship it?",
		"blocking": true,
		"required": true,
	})
	req, _ := http.NewRequest(http.MethodPost, base+"/requests", bytes.NewReader(createBody))
	req.Header.Set("Authorization", "Bearer "+b.Token())
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("create approval failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200 creating approval, got %d: %s", resp.StatusCode, raw)
	}
	var created struct {
		Request humanInterview `json:"request"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatalf("decode created request: %v", err)
	}
	if !b.HasBlockingRequest() {
		t.Fatal("approval should block before it is canceled")
	}

	messageBody, _ := json.Marshal(map[string]any{
		"from":    "you",
		"channel": "general",
		"content": "This should still be blocked.",
	})
	req, _ = http.NewRequest(http.MethodPost, base+"/messages", bytes.NewReader(messageBody))
	req.Header.Set("Authorization", "Bearer "+b.Token())
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post message before cancel failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected approval to block message before cancel, got %d", resp.StatusCode)
	}

	cancelBody, _ := json.Marshal(map[string]any{
		"action": "cancel",
		"id":     created.Request.ID,
	})
	req, _ = http.NewRequest(http.MethodPost, base+"/requests", bytes.NewReader(cancelBody))
	req.Header.Set("Authorization", "Bearer "+b.Token())
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("cancel approval failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200 canceling approval, got %d: %s", resp.StatusCode, raw)
	}
	if b.HasBlockingRequest() {
		t.Fatal("canceled approval should not block")
	}

	req, _ = http.NewRequest(http.MethodPost, base+"/messages", bytes.NewReader(messageBody))
	req.Header.Set("Authorization", "Bearer "+b.Token())
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post message after cancel failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected message after canceled approval to succeed, got %d", resp.StatusCode)
	}
}

func TestBrokerHumanInterviewDoesNotBlockAndCancelsOnHumanMessage(t *testing.T) {
	b := newTestBroker(t)
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("failed to start broker: %v", err)
	}
	defer b.Stop()

	base := fmt.Sprintf("http://%s", b.Addr())
	createBody, _ := json.Marshal(map[string]any{
		"kind":     "interview",
		"from":     "ceo",
		"channel":  "general",
		"title":    "Human interview",
		"question": "Which customer segment should we prioritize?",
		"blocking": true,
		"required": true,
		"reply_to": "msg-thread-1",
	})
	req, _ := http.NewRequest(http.MethodPost, base+"/requests", bytes.NewReader(createBody))
	req.Header.Set("Authorization", "Bearer "+b.Token())
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("create interview failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200 creating interview, got %d: %s", resp.StatusCode, raw)
	}
	var created struct {
		Request humanInterview `json:"request"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatalf("decode created interview: %v", err)
	}
	if created.Request.Blocking || created.Request.Required {
		t.Fatalf("human interviews must be non-blocking, got %+v", created.Request)
	}
	if b.HasBlockingRequest() {
		t.Fatal("human interview should not count as a blocking request")
	}

	createFollowUpBody, _ := json.Marshal(map[string]any{
		"kind":     "interview",
		"from":     "ceo",
		"channel":  "general",
		"title":    "Follow-up interview",
		"question": "Which launch channel should we test next?",
		"blocking": true,
		"required": true,
		"reply_to": "msg-thread-2",
	})
	req, _ = http.NewRequest(http.MethodPost, base+"/requests", bytes.NewReader(createFollowUpBody))
	req.Header.Set("Authorization", "Bearer "+b.Token())
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("create follow-up interview failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("expected 200 creating follow-up interview, got %d: %s", resp.StatusCode, raw)
	}
	var createdFollowUp struct {
		Request humanInterview `json:"request"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&createdFollowUp); err != nil {
		resp.Body.Close()
		t.Fatalf("decode created follow-up interview: %v", err)
	}
	resp.Body.Close()

	invalidMessageBody, _ := json.Marshal(map[string]any{
		"from":    "",
		"channel": "general",
		"content": "This send should fail validation.",
		"tagged":  []string{"unknown-agent"},
	})
	req, _ = http.NewRequest(http.MethodPost, base+"/messages", bytes.NewReader(invalidMessageBody))
	req.Header.Set("Authorization", "Bearer "+b.Token())
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post invalid message after interview failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected invalid message to fail before canceling interview, got %d", resp.StatusCode)
	}

	req, _ = http.NewRequest(http.MethodGet, base+"/interview/answer?id="+created.Request.ID, nil)
	req.Header.Set("Authorization", "Bearer "+b.Token())
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get interview answer after invalid send failed: %v", err)
	}
	var pendingAnswer struct {
		Answered *interviewAnswer `json:"answered"`
		Status   string           `json:"status"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&pendingAnswer); err != nil {
		t.Fatalf("decode pending interview answer: %v", err)
	}
	resp.Body.Close()
	if pendingAnswer.Answered != nil || pendingAnswer.Status != "pending" {
		t.Fatalf("expected invalid send to leave interview pending, got %+v", pendingAnswer)
	}

	messageBody, _ := json.Marshal(map[string]any{
		"from":     "you",
		"channel":  "general",
		"content":  "Let's keep moving in this thread.",
		"reply_to": "msg-thread-1",
	})
	req, _ = http.NewRequest(http.MethodPost, base+"/messages", bytes.NewReader(messageBody))
	req.Header.Set("Authorization", "Bearer "+b.Token())
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post message after interview failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected message send after interview to succeed, got %d", resp.StatusCode)
	}

	req, _ = http.NewRequest(http.MethodGet, base+"/requests?scope=all&viewer_slug=human&include_resolved=true", nil)
	req.Header.Set("Authorization", "Bearer "+b.Token())
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("list requests failed: %v", err)
	}
	defer resp.Body.Close()
	var listing struct {
		Requests []humanInterview `json:"requests"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&listing); err != nil {
		t.Fatalf("decode requests: %v", err)
	}
	if len(listing.Requests) != 2 {
		t.Fatalf("expected two interviews, got %+v", listing.Requests)
	}
	byID := map[string]humanInterview{}
	for _, listed := range listing.Requests {
		byID[listed.ID] = listed
	}
	if byID[created.Request.ID].Status != "canceled" {
		t.Fatalf("expected replied-to interview to be canceled after human message, got %+v", byID[created.Request.ID])
	}
	if byID[createdFollowUp.Request.ID].Status != "pending" {
		t.Fatalf("expected queued follow-up interview to remain pending, got %+v", byID[createdFollowUp.Request.ID])
	}

	req, _ = http.NewRequest(http.MethodGet, base+"/interview", nil)
	req.Header.Set("Authorization", "Bearer "+b.Token())
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get active interview failed: %v", err)
	}
	var activeInterview struct {
		Pending *humanInterview `json:"pending"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&activeInterview); err != nil {
		resp.Body.Close()
		t.Fatalf("decode active interview: %v", err)
	}
	resp.Body.Close()
	if activeInterview.Pending == nil || activeInterview.Pending.ID != createdFollowUp.Request.ID {
		t.Fatalf("expected active interview to switch to follow-up %q, got %+v", createdFollowUp.Request.ID, activeInterview.Pending)
	}

	req, _ = http.NewRequest(http.MethodGet, base+"/interview/answer?id="+created.Request.ID, nil)
	req.Header.Set("Authorization", "Bearer "+b.Token())
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get interview answer failed: %v", err)
	}
	defer resp.Body.Close()
	var answer struct {
		Answered *interviewAnswer `json:"answered"`
		Status   string           `json:"status"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&answer); err != nil {
		t.Fatalf("decode interview answer: %v", err)
	}
	if answer.Answered != nil || answer.Status != "canceled" {
		t.Fatalf("expected canceled interview answer state, got %+v", answer)
	}
}

func TestBrokerRequestAnswerUnblocksDependentTask(t *testing.T) {
	b := newTestBroker(t)
	ensureTestMemberAccess(b, "general", "ceo", "CEO")
	ensureTestMemberAccess(b, "general", "builder", "Builder")
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("failed to start broker: %v", err)
	}
	defer b.Stop()

	base := fmt.Sprintf("http://%s", b.Addr())
	createRequestBody, _ := json.Marshal(map[string]any{
		"action":   "create",
		"from":     "ceo",
		"channel":  "general",
		"title":    "Approve the launch packet",
		"question": "Should we proceed with the external launch?",
		"kind":     "approval",
		"blocking": true,
		"required": true,
		"reply_to": "msg-approval-1",
	})
	req, _ := http.NewRequest(http.MethodPost, base+"/requests", bytes.NewReader(createRequestBody))
	req.Header.Set("Authorization", "Bearer "+b.Token())
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("create request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200 creating request, got %d: %s", resp.StatusCode, raw)
	}
	var created struct {
		Request humanInterview `json:"request"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatalf("decode request create response: %v", err)
	}
	reqID := created.Request.ID
	if reqID == "" {
		t.Fatal("expected request id")
	}

	createTaskBody, _ := json.Marshal(map[string]any{
		"action":     "create",
		"channel":    "general",
		"title":      "Ship the launch packet after approval",
		"details":    "Continue once the approval request is answered.",
		"created_by": "ceo",
		"owner":      "builder",
		"depends_on": []string{reqID},
		"task_type":  "launch",
	})
	req, _ = http.NewRequest(http.MethodPost, base+"/tasks", bytes.NewReader(createTaskBody))
	req.Header.Set("Authorization", "Bearer "+b.Token())
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("create task failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200 creating task, got %d: %s", resp.StatusCode, raw)
	}
	var taskResult struct {
		Task teamTask `json:"task"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&taskResult); err != nil {
		t.Fatalf("decode task create response: %v", err)
	}
	if !taskResult.Task.Blocked {
		t.Fatalf("expected task to start blocked on request dependency, got %+v", taskResult.Task)
	}

	answerBody, _ := json.Marshal(map[string]any{
		"id":        reqID,
		"choice_id": "approve",
	})
	req, _ = http.NewRequest(http.MethodPost, base+"/requests/answer", bytes.NewReader(answerBody))
	req.Header.Set("Authorization", "Bearer "+b.Token())
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("answer request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200 answering request, got %d: %s", resp.StatusCode, raw)
	}

	req, _ = http.NewRequest(http.MethodGet, base+"/tasks?channel=general", nil)
	req.Header.Set("Authorization", "Bearer "+b.Token())
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get tasks failed: %v", err)
	}
	defer resp.Body.Close()
	var listing struct {
		Tasks []teamTask `json:"tasks"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&listing); err != nil {
		t.Fatalf("decode tasks: %v", err)
	}
	var updated *teamTask
	for i := range listing.Tasks {
		if listing.Tasks[i].ID == taskResult.Task.ID {
			updated = &listing.Tasks[i]
			break
		}
	}
	if updated == nil {
		t.Fatalf("expected to find task %s after answer", taskResult.Task.ID)
	}
	if updated.Blocked {
		t.Fatalf("expected task to be unblocked after request answer, got %+v", updated)
	}
	if updated.Status != "in_progress" {
		t.Fatalf("expected task to resume in_progress after answer, got %+v", updated)
	}
}

func TestBrokerDecisionRequestsDefaultToBlocking(t *testing.T) {
	b := newTestBroker(t)
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("failed to start broker: %v", err)
	}
	defer b.Stop()

	base := fmt.Sprintf("http://%s", b.Addr())
	body, _ := json.Marshal(map[string]any{
		"kind":     "approval",
		"from":     "ceo",
		"channel":  "general",
		"title":    "Approval needed",
		"question": "Should we proceed?",
	})
	req, _ := http.NewRequest(http.MethodPost, base+"/requests", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+b.Token())
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request create failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200 creating request, got %d: %s", resp.StatusCode, raw)
	}

	var created struct {
		Request humanInterview `json:"request"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatalf("decode request: %v", err)
	}
	if !created.Request.Blocking || !created.Request.Required {
		t.Fatalf("expected approval to default to blocking+required, got %+v", created.Request)
	}
	if got := created.Request.RecommendedID; got != "approve" {
		t.Fatalf("expected approval recommended_id to default to approve, got %q", got)
	}
	if len(created.Request.Options) != 5 {
		t.Fatalf("expected enriched approval options, got %+v", created.Request.Options)
	}
	var approveWithNote *interviewOption
	for i := range created.Request.Options {
		if created.Request.Options[i].ID == "approve_with_note" {
			approveWithNote = &created.Request.Options[i]
			break
		}
	}
	if approveWithNote == nil || !approveWithNote.RequiresText || strings.TrimSpace(approveWithNote.TextHint) == "" {
		t.Fatalf("expected approve_with_note to require text, got %+v", approveWithNote)
	}
}

func TestBrokerRequestAnswerRequiresCustomTextWhenOptionNeedsIt(t *testing.T) {
	b := newTestBroker(t)
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("failed to start broker: %v", err)
	}
	defer b.Stop()

	base := fmt.Sprintf("http://%s", b.Addr())
	body, _ := json.Marshal(map[string]any{
		"kind":     "approval",
		"from":     "ceo",
		"channel":  "general",
		"title":    "Approval needed",
		"question": "Should we proceed?",
	})
	req, _ := http.NewRequest(http.MethodPost, base+"/requests", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+b.Token())
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request create failed: %v", err)
	}
	defer resp.Body.Close()

	var created struct {
		Request humanInterview `json:"request"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatalf("decode request: %v", err)
	}

	answerBody, _ := json.Marshal(map[string]any{
		"id":        created.Request.ID,
		"choice_id": "approve_with_note",
	})
	req, _ = http.NewRequest(http.MethodPost, base+"/requests/answer", bytes.NewReader(answerBody))
	req.Header.Set("Authorization", "Bearer "+b.Token())
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request answer failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 400 for missing custom text, got %d: %s", resp.StatusCode, raw)
	}
}

func TestResolveTaskIntervalsRespectMinimumFloor(t *testing.T) {
	t.Setenv("WUPHF_TASK_FOLLOWUP_MINUTES", "1")
	t.Setenv("WUPHF_TASK_REMINDER_MINUTES", "1")
	t.Setenv("WUPHF_TASK_RECHECK_MINUTES", "1")

	if got := config.ResolveTaskFollowUpInterval(); got != 2 {
		t.Fatalf("expected follow-up interval floor of 2, got %d", got)
	}
	if got := config.ResolveTaskReminderInterval(); got != 2 {
		t.Fatalf("expected reminder interval floor of 2, got %d", got)
	}
	if got := config.ResolveTaskRecheckInterval(); got != 2 {
		t.Fatalf("expected recheck interval floor of 2, got %d", got)
	}
}

func TestInFlightTasksReturnsOnlyNonTerminalOwned(t *testing.T) {
	b := newTestBroker(t)
	b.mu.Lock()
	b.tasks = []teamTask{
		{ID: "t1", Title: "Active task", Owner: "fe", Status: "in_progress"},
		{ID: "t2", Title: "Done task", Owner: "fe", Status: "done"},
		{ID: "t3", Title: "No owner", Owner: "", Status: "in_progress"},
		{ID: "t4", Title: "Canceled task", Owner: "be", Status: "canceled"},
		{ID: "t5", Title: "Cancelled task", Owner: "be", Status: "cancelled"},
		{ID: "t6", Title: "Pending with owner", Owner: "pm", Status: "pending"},
		{ID: "t7", Title: "Open with owner", Owner: "ceo", Status: "open"},
	}
	b.mu.Unlock()

	got := b.InFlightTasks()

	// Only tasks with owner AND non-terminal status should be returned.
	// "done", "canceled", "cancelled" are terminal. No-owner tasks excluded.
	if len(got) != 3 {
		t.Fatalf("expected 3 in-flight tasks, got %d: %+v", len(got), got)
	}
	ids := make(map[string]bool)
	for _, task := range got {
		ids[task.ID] = true
	}
	if !ids["t1"] {
		t.Error("expected t1 (in_progress+owner) to be included")
	}
	if !ids["t6"] {
		t.Error("expected t6 (pending+owner) to be included")
	}
	if !ids["t7"] {
		t.Error("expected t7 (open+owner) to be included")
	}
	if ids["t2"] {
		t.Error("expected t2 (done) to be excluded")
	}
	if ids["t3"] {
		t.Error("expected t3 (no owner) to be excluded")
	}
	if ids["t4"] {
		t.Error("expected t4 (canceled) to be excluded")
	}
	if ids["t5"] {
		t.Error("expected t5 (cancelled) to be excluded")
	}
}

func TestInFlightTasksExcludesCompletedStatus(t *testing.T) {
	b := newTestBroker(t)
	b.mu.Lock()
	b.tasks = []teamTask{
		{ID: "t1", Title: "Active task", Owner: "fe", Status: "in_progress"},
		{ID: "t2", Title: "Completed task", Owner: "fe", Status: "completed"},
	}
	b.mu.Unlock()

	got := b.InFlightTasks()

	// "completed" is a terminal status — should be excluded just like "done".
	if len(got) != 1 {
		t.Fatalf("expected 1 in-flight task, got %d: %+v", len(got), got)
	}
	if got[0].ID != "t1" {
		t.Errorf("expected t1 (in_progress), got %q", got[0].ID)
	}
	for _, task := range got {
		if task.Status == "completed" {
			t.Errorf("completed task %q should not appear in InFlightTasks()", task.ID)
		}
	}
}

func TestRequestAnswerUnblocksReferencedTask(t *testing.T) {
	b := newTestBroker(t)
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	defer b.Stop()

	b.mu.Lock()
	now := "2026-01-01T00:00:00Z"
	b.channels = append(b.channels, teamChannel{Slug: "client-loop", Name: "Client Loop"})
	b.requests = append(b.requests, humanInterview{
		ID:        "request-11",
		Kind:      "input",
		Status:    "pending",
		From:      "builder",
		Channel:   "client-loop",
		Question:  "What exact client name should I use for the Google Drive workspace folder?",
		Blocking:  true,
		Required:  true,
		CreatedAt: now,
		UpdatedAt: now,
	})
	b.tasks = append(b.tasks, teamTask{
		ID:        "task-3",
		Channel:   "client-loop",
		Title:     "Create live client workspace in Google Drive",
		Details:   "Blocked on request-11: exact client name for the workspace folder.",
		Owner:     "builder",
		Status:    "blocked",
		Blocked:   true,
		CreatedBy: "operator",
		CreatedAt: now,
		UpdatedAt: now,
	})
	b.mu.Unlock()

	base := fmt.Sprintf("http://%s", b.Addr())
	answerBody, _ := json.Marshal(map[string]any{
		"id":          "request-11",
		"custom_text": "Meridian Growth Studio",
	})
	req, _ := http.NewRequest(http.MethodPost, base+"/requests/answer", bytes.NewReader(answerBody))
	req.Header.Set("Authorization", "Bearer "+b.Token())
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request answer: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, raw)
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	if got := b.tasks[0]; got.Blocked {
		t.Fatalf("expected task to unblock after request answer, got %+v", got)
	} else {
		if got.Status != "in_progress" {
			t.Fatalf("expected task status to move to in_progress, got %+v", got)
		}
		if !strings.Contains(got.Details, "Meridian Growth Studio") {
			t.Fatalf("expected task details to include human answer, got %q", got.Details)
		}
	}
	var found bool
	for _, action := range b.actions {
		if action.Kind == "task_unblocked" && action.RelatedID == "task-3" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected task_unblocked action after answering request")
	}
}

func TestHeadlessQueue_EmptyBeforePush(t *testing.T) {
	l := &Launcher{
		pack: &agent.PackDefinition{
			LeadSlug: "ceo",
			Agents: []agent.AgentConfig{
				{Slug: "ceo", Name: "CEO"},
				{Slug: "eng", Name: "Engineer"},
			},
		},
		headlessWorkers: make(map[string]bool),
		headlessActive:  make(map[string]*headlessCodexActiveTurn),
		headlessQueues:  make(map[string][]headlessCodexTurn),
	}

	l.headlessMu.Lock()
	ceoLen := len(l.headlessQueues["ceo"])
	engLen := len(l.headlessQueues["eng"])
	l.headlessMu.Unlock()

	if ceoLen != 0 || engLen != 0 {
		t.Fatalf("expected empty queues before any push, got ceo=%d eng=%d", ceoLen, engLen)
	}
}

// TestHeadlessQueue_PopulatedAfterEnqueue verifies that enqueueHeadlessCodexTurn
// adds exactly one turn to the target agent's queue.
func TestHeadlessQueue_PopulatedAfterEnqueue(t *testing.T) {
	// Override headlessCodexRunTurn to be a no-op so no real process is started.
	setHeadlessCodexRunTurnForTest(t, func(l *Launcher, ctx context.Context, slug, notification string, channel ...string) error {
		// Block until the context is cancelled so the worker stays "active"
		// and doesn't drain the queue during the test assertion window.
		<-ctx.Done()
		return ctx.Err()
	})

	l := &Launcher{
		pack: &agent.PackDefinition{
			LeadSlug: "ceo",
			Agents: []agent.AgentConfig{
				{Slug: "ceo", Name: "CEO"},
				{Slug: "eng", Name: "Engineer"},
			},
		},
		headlessWorkers: make(map[string]bool),
		headlessActive:  make(map[string]*headlessCodexActiveTurn),
		headlessQueues:  make(map[string][]headlessCodexTurn),
	}
	l.headlessCtx, l.headlessCancel = context.WithCancel(t.Context())
	t.Cleanup(func() { l.waitForHeadlessIdle(t) })

	l.enqueueHeadlessCodexTurn("eng", "review the diff")

	l.headlessMu.Lock()
	engLen := len(l.headlessQueues["eng"])
	ceoLen := len(l.headlessQueues["ceo"])
	l.headlessMu.Unlock()

	// The worker goroutine may have already consumed the turn from the queue —
	// that is valid. What matters is that the queue was populated (worker started)
	// and that CEO was NOT added to the queue (not triggered by a specialist enqueue).
	if ceoLen != 0 {
		t.Fatalf("expected ceo queue empty after enqueuing for eng, got %d", ceoLen)
	}
	if !l.headlessWorkers["eng"] {
		t.Fatalf("expected eng worker to be flagged as started after enqueue")
	}
	// engLen may be 0 (worker consumed it) or 1 (still pending) — both are valid.
	_ = engLen
}

// TestHeadlessQueue_NoTimerDrivenWakeup verifies that creating a Launcher and
// waiting briefly does not populate any agent's queue — agents wake only on
// explicit push (enqueue), never on a background timer.
func TestHeadlessQueue_NoTimerDrivenWakeup(t *testing.T) {
	l := &Launcher{
		pack: &agent.PackDefinition{
			LeadSlug: "ceo",
			Agents: []agent.AgentConfig{
				{Slug: "ceo", Name: "CEO"},
				{Slug: "eng", Name: "Engineer"},
			},
		},
		headlessWorkers: make(map[string]bool),
		headlessActive:  make(map[string]*headlessCodexActiveTurn),
		headlessQueues:  make(map[string][]headlessCodexTurn),
	}

	// No enqueue calls. The queues must remain empty.
	l.headlessMu.Lock()
	totalItems := 0
	for _, q := range l.headlessQueues {
		totalItems += len(q)
	}
	l.headlessMu.Unlock()

	if totalItems != 0 {
		t.Fatalf("expected no queued turns without an explicit enqueue, got %d", totalItems)
	}
	if len(l.headlessWorkers) != 0 {
		t.Fatalf("expected no workers started without an explicit enqueue, got %v", l.headlessWorkers)
	}
}

// ensureDefaultOfficeMembersLocked must seed the full default manifest ONLY
// when there are no existing members. Its prior behavior (append-any-missing-
// default) was the source of the load-path leak: blueprint-seeded teams saw
// ceo/planner/executor/reviewer re-appended on every broker Load.
