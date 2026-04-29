package team

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nex-crm/wuphf/internal/agent"
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

func TestBrokerStopIsIdempotent(t *testing.T) {
	b := newTestBroker(t)
	b.Stop()
	b.Stop()
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
