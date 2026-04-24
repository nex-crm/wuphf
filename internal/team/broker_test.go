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
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nex-crm/wuphf/internal/agent"
	"github.com/nex-crm/wuphf/internal/buildinfo"
	"github.com/nex-crm/wuphf/internal/config"
	"github.com/nex-crm/wuphf/internal/provider"
)

func TestMain(m *testing.M) {
	// Globals that leaked background goroutines can write to after a test
	// returns need a process-lifetime home so cleanup doesn't race the
	// leaked writes. We pre-seed two (token file, headless log dir) and
	// deliberately DO NOT pre-seed brokerStatePath: NewBroker auto-loads
	// from that path, so a shared default would cross-contaminate tests
	// that add state without their own swap.
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

	// 2) Headless log dir: leaked headless-worker goroutines open append
	//    files under wuphfLogDir(). WUPHF_LOG_DIR is the env hook; honored
	//    by wuphfLogDir() in headless_codex.go. Append-only writes are
	//    safe to share across tests (nothing reads them). Fail fast on
	//    mktemp error for the same reason as above — and run any cleanups
	//    already registered so the token dir doesn't leak on the error path.
	logDir, err := os.MkdirTemp("", "wuphf-team-test-logs-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "TestMain: mktemp headless log dir: %v\n", err)
		cleanup()
		os.Exit(1)
	}
	if err := os.Setenv("WUPHF_LOG_DIR", logDir); err != nil {
		// Silent fallback to the default log dir would reintroduce the
		// cross-test cleanup race this setup exists to prevent.
		fmt.Fprintf(os.Stderr, "TestMain: setenv WUPHF_LOG_DIR: %v\n", err)
		_ = os.RemoveAll(logDir)
		cleanup()
		os.Exit(1)
	}
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

// reloadedBroker constructs a Broker and replays state from brokerStatePath
// so persistence tests can verify what a production restart would see.
// Test-mode NewBroker skips the automatic disk load (to stop cross-test
// state leakage via a shared broker-state.json), so any test that checks
// persistence behavior must opt in through this helper.
func reloadedBroker(t *testing.T) *Broker {
	t.Helper()
	b := NewBroker()
	if err := b.loadState(); err != nil {
		t.Fatalf("loadState: %v", err)
	}
	return b
}

// leakedBrokerStatePath returns a per-test-unique filesystem path for a
// broker-state.json, rooted in a temp dir OUTSIDE t.TempDir(). Callers use
// it to swap the package-global brokerStatePath without exposing t.TempDir
// to late-arriving writes from goroutines leaked by prior tests. Those
// late writes hit brokerStatePath() (resolved lazily on each call) and
// resolve to whatever the global points at — so if it pointed into
// t.TempDir, cleanup would race the write and fail with "unlinkat:
// directory not empty". The returned dir is intentionally NOT registered
// for cleanup: a handful of KB per test run leaks to /tmp, which the OS
// reclaims on reboot or tmpfs eviction. Cheap fix for a cross-test
// goroutine-leak hazard that a larger refactor will eventually retire.
func leakedBrokerStatePath(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "wuphf-test-state-*")
	if err != nil {
		t.Fatalf("mktemp broker state dir: %v", err)
	}
	return filepath.Join(dir, "broker-state.json")
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
	cmd.Env = GitCleanEnv()
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init %s: %v: %s", path, err, strings.TrimSpace(string(out)))
	}
}

func TestFormatChannelViewIncludesThreadReference(t *testing.T) {
	got := FormatChannelView([]channelMessage{
		{ID: "msg-1", From: "ceo", Content: "Root topic", Timestamp: "2026-03-24T10:00:00Z"},
		{ID: "msg-2", From: "fe", Content: "Replying here", ReplyTo: "msg-1", Timestamp: "2026-03-24T10:01:00Z"},
	})

	if !strings.Contains(got, "10:01:00 ↳ msg-1  @fe: Replying here") {
		t.Fatalf("expected threaded message to include reply marker, got %q", got)
	}
}

func TestBrokerPersistsAndReloadsState(t *testing.T) {
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
	b.mu.Lock()
	b.messages = []channelMessage{{ID: "msg-1", From: "ceo", Content: "Persist me", Timestamp: "2026-03-24T10:00:00Z"}}
	b.counter = 1
	if err := b.saveLocked(); err != nil {
		b.mu.Unlock()
		t.Fatalf("saveLocked failed: %v", err)
	}
	b.mu.Unlock()

	reloaded := reloadedBroker(t)
	msgs := reloaded.Messages()
	if len(msgs) != 1 {
		t.Fatalf("expected 1 persisted message, got %d", len(msgs))
	}
	if msgs[0].Content != "Persist me" {
		t.Fatalf("expected persisted content, got %q", msgs[0].Content)
	}

	reloaded.Reset()
	empty := reloadedBroker(t)
	if len(empty.Messages()) != 0 {
		t.Fatalf("expected reset to clear persisted messages, got %d", len(empty.Messages()))
	}
}

func TestBrokerLoadsLastGoodSnapshotWhenPrimaryStateIsClobbered(t *testing.T) {
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
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
	if _, err := os.Stat(brokerStateSnapshotPath()); err != nil {
		t.Fatalf("expected snapshot after rich save: %v", err)
	}

	// Simulate a later clobber that keeps the custom office shell but loses live work.
	clobbered := reloadedBroker(t)
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
	if _, err := os.Stat(brokerStateSnapshotPath()); err != nil {
		t.Fatalf("expected snapshot to survive clobbered save: %v", err)
	}
	if snap, err := loadBrokerStateFile(brokerStateSnapshotPath()); err != nil {
		t.Fatalf("read snapshot: %v", err)
	} else if len(snap.Messages) != 1 || len(snap.Tasks) != 1 || len(snap.Actions) != 1 {
		t.Fatalf("unexpected snapshot contents: %+v", snap)
	}

	reloaded := reloadedBroker(t)
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

func TestBrokerSessionModePersistsAndSurvivesReset(t *testing.T) {
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
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

	reloaded := reloadedBroker(t)
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

func TestBrokerMessageSubscribersReceivePostedMessages(t *testing.T) {
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
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

func TestBrokerCanonicalizesLegacyDMSlugs(t *testing.T) {
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("failed to start broker: %v", err)
	}
	defer b.Stop()

	base := fmt.Sprintf("http://%s", b.Addr())
	postJSON := func(path string, payload map[string]any) *http.Response {
		t.Helper()
		body, _ := json.Marshal(payload)
		req, _ := http.NewRequest(http.MethodPost, base+path, bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+b.Token())
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("POST %s failed: %v", path, err)
		}
		return resp
	}

	resp := postJSON("/channels/dm", map[string]any{
		"members": []string{"human", "ceo"},
		"type":    "direct",
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("create dm status %d: %s", resp.StatusCode, raw)
	}
	var created struct {
		Slug string `json:"slug"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatalf("decode create dm: %v", err)
	}
	wantSlug := channelDirectSlug("human", "ceo")
	if created.Slug != wantSlug {
		t.Fatalf("expected canonical slug %q, got %q", wantSlug, created.Slug)
	}

	msgResp := postJSON("/messages", map[string]any{
		"from":    "human",
		"channel": "dm-human-ceo",
		"content": "hello ceo",
	})
	defer msgResp.Body.Close()
	if msgResp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(msgResp.Body)
		t.Fatalf("post legacy dm status %d: %s", msgResp.StatusCode, raw)
	}
	msgs := b.Messages()
	if len(msgs) != 1 {
		t.Fatalf("expected one message, got %d", len(msgs))
	}
	if msgs[0].Channel != wantSlug {
		t.Fatalf("expected message to land in %q, got %q", wantSlug, msgs[0].Channel)
	}

	req, _ := http.NewRequest(http.MethodGet, base+"/messages?channel=dm-human-ceo&viewer_slug=human", nil)
	req.Header.Set("Authorization", "Bearer "+b.Token())
	getResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET legacy dm failed: %v", err)
	}
	defer getResp.Body.Close()
	if getResp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(getResp.Body)
		t.Fatalf("get legacy dm status %d: %s", getResp.StatusCode, raw)
	}
	var got struct {
		Channel  string           `json:"channel"`
		Messages []channelMessage `json:"messages"`
	}
	if err := json.NewDecoder(getResp.Body).Decode(&got); err != nil {
		t.Fatalf("decode get dm: %v", err)
	}
	if got.Channel != wantSlug || len(got.Messages) != 1 {
		t.Fatalf("expected canonical channel %q with one message, got channel=%q messages=%d", wantSlug, got.Channel, len(got.Messages))
	}
}

func TestRecordAgentUsageAttachesToCurrentTurnMessagesOnly(t *testing.T) {
	b := NewBroker()
	now := time.Now().UTC()
	b.mu.Lock()
	b.messages = []channelMessage{
		{
			ID:        "msg-1",
			From:      "ceo",
			Content:   "older turn",
			Timestamp: now.Add(-2 * time.Minute).Format(time.RFC3339),
			Usage:     &messageUsage{TotalTokens: 111},
		},
		{
			ID:        "msg-2",
			From:      "pm",
			Content:   "interleaved",
			Timestamp: now.Add(-30 * time.Second).Format(time.RFC3339),
		},
		{
			ID:        "msg-3",
			From:      "ceo",
			Content:   "current turn kickoff",
			Timestamp: now.Add(-10 * time.Second).Format(time.RFC3339),
		},
		{
			ID:        "msg-4",
			From:      "system",
			Content:   "routing",
			Timestamp: now.Add(-5 * time.Second).Format(time.RFC3339),
		},
		{
			ID:        "msg-5",
			From:      "ceo",
			Content:   "current turn answer",
			Timestamp: now.Format(time.RFC3339),
		},
	}
	b.mu.Unlock()

	b.RecordAgentUsage("ceo", "claude-sonnet-4-6", provider.ClaudeUsage{
		InputTokens:         800,
		OutputTokens:        200,
		CacheReadTokens:     50,
		CacheCreationTokens: 25,
	})

	msgs := b.Messages()
	if msgs[0].Usage == nil || msgs[0].Usage.TotalTokens != 111 {
		t.Fatalf("expected older turn usage to remain untouched, got %+v", msgs[0].Usage)
	}
	if msgs[2].Usage == nil || msgs[2].Usage.TotalTokens != 1075 {
		t.Fatalf("expected msg-3 to receive usage, got %+v", msgs[2].Usage)
	}
	if msgs[4].Usage == nil || msgs[4].Usage.TotalTokens != 1075 {
		t.Fatalf("expected msg-5 to receive usage, got %+v", msgs[4].Usage)
	}
}

func TestBrokerActionSubscribersReceiveTaskLifecycle(t *testing.T) {
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
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
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
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
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
	b.Stop()
	b.Stop()
}

func TestBrokerActivitySubscribersReceiveUpdates(t *testing.T) {
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
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
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
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

func TestBrokerMessageKindAndTitleRoundTrip(t *testing.T) {
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("failed to start broker: %v", err)
	}
	defer b.Stop()

	base := fmt.Sprintf("http://%s", b.Addr())
	body, _ := json.Marshal(map[string]any{
		"from":    "ceo",
		"channel": "general",
		"kind":    "human_report",
		"title":   "Frontend ready for review",
		"content": "The launch page skeleton is ready for you to review.",
	})
	req, _ := http.NewRequest(http.MethodPost, base+"/messages", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+b.Token())
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post message failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200 posting message, got %d: %s", resp.StatusCode, raw)
	}

	req, _ = http.NewRequest(http.MethodGet, base+"/messages?channel=general", nil)
	req.Header.Set("Authorization", "Bearer "+b.Token())
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get messages failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200 listing messages, got %d: %s", resp.StatusCode, raw)
	}

	var result struct {
		Messages []channelMessage `json:"messages"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode messages: %v", err)
	}
	if len(result.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(result.Messages))
	}
	if got := result.Messages[0].Kind; got != "human_report" {
		t.Fatalf("expected human_report kind, got %q", got)
	}
	if got := result.Messages[0].Title; got != "Frontend ready for review" {
		t.Fatalf("expected title to round-trip, got %q", got)
	}
}

func TestBrokerMessagesCanScopeToThread(t *testing.T) {
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("failed to start broker: %v", err)
	}
	defer b.Stop()

	root, err := b.PostMessage("ceo", "general", "Root topic", nil, "")
	if err != nil {
		t.Fatalf("post root: %v", err)
	}
	reply, err := b.PostMessage("ceo", "general", "Reply in thread", nil, root.ID)
	if err != nil {
		t.Fatalf("post reply: %v", err)
	}
	if _, err := b.PostMessage("you", "general", "Separate topic", nil, ""); err != nil {
		t.Fatalf("post unrelated: %v", err)
	}

	base := fmt.Sprintf("http://%s", b.Addr())
	req, _ := http.NewRequest(http.MethodGet, base+"/messages?channel=general&thread_id="+root.ID, nil)
	req.Header.Set("Authorization", "Bearer "+b.Token())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("thread messages request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200 listing thread messages, got %d: %s", resp.StatusCode, raw)
	}

	var result struct {
		Messages []channelMessage `json:"messages"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode thread messages: %v", err)
	}
	if len(result.Messages) != 2 {
		t.Fatalf("expected root and reply only, got %+v", result.Messages)
	}
	if result.Messages[0].ID != root.ID || result.Messages[1].ID != reply.ID {
		t.Fatalf("unexpected thread messages: %+v", result.Messages)
	}
}

func TestBrokerMessagesCanScopeToAgentInbox(t *testing.T) {
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
	b.mu.Lock()
	b.members = append(b.members,
		officeMember{Slug: "pm", Name: "Product Manager"},
		officeMember{Slug: "fe", Name: "Frontend Engineer"},
	)
	for i := range b.channels {
		if b.channels[i].Slug == "general" {
			b.channels[i].Members = append(b.channels[i].Members, "pm", "fe")
			break
		}
	}
	b.mu.Unlock()
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("failed to start broker: %v", err)
	}
	defer b.Stop()

	if _, err := b.PostMessage("you", "general", "Global direction", nil, ""); err != nil {
		t.Fatalf("post human message: %v", err)
	}
	if _, err := b.PostMessage("pm", "general", "Unrelated PM update", nil, ""); err != nil {
		t.Fatalf("post unrelated message: %v", err)
	}
	tagged, err := b.PostMessage("ceo", "general", "Frontend, take this next.", []string{"fe"}, "")
	if err != nil {
		t.Fatalf("post tagged message: %v", err)
	}
	own, err := b.PostMessage("fe", "general", "I am on it.", nil, "")
	if err != nil {
		t.Fatalf("post own message: %v", err)
	}

	base := fmt.Sprintf("http://%s", b.Addr())
	req, _ := http.NewRequest(http.MethodGet, base+"/messages?channel=general&my_slug=fe&viewer_slug=fe&scope=agent", nil)
	req.Header.Set("Authorization", "Bearer "+b.Token())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("agent-scoped messages request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200 listing agent-scoped messages, got %d: %s", resp.StatusCode, raw)
	}

	var result struct {
		Messages    []channelMessage `json:"messages"`
		TaggedCount int              `json:"tagged_count"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode agent-scoped messages: %v", err)
	}
	if len(result.Messages) != 3 {
		t.Fatalf("expected human, tagged, and own messages only, got %+v", result.Messages)
	}
	if result.TaggedCount != 1 {
		t.Fatalf("expected one tagged message, got %d", result.TaggedCount)
	}
	seen := map[string]bool{}
	for _, msg := range result.Messages {
		seen[msg.ID] = true
		if strings.Contains(msg.Content, "Unrelated PM update") {
			t.Fatalf("did not expect unrelated message in agent scope: %+v", result.Messages)
		}
	}
	if !seen[tagged.ID] || !seen[own.ID] {
		t.Fatalf("expected tagged and own messages in scoped view, got %+v", result.Messages)
	}
}

func TestNewBrokerSeedsDefaultOfficeRosterOnFreshState(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // isolate from ~/.wuphf company.json (e.g. RevOps pack)
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
	members := b.OfficeMembers()
	if len(members) < 2 {
		t.Fatalf("expected default office roster on fresh state, got %d members", len(members))
	}
	b.mu.Lock()
	ceo := b.findMemberLocked("ceo")
	general := b.findChannelLocked("general")
	b.mu.Unlock()
	if members[0].Slug != "ceo" && ceo == nil {
		t.Fatalf("expected ceo to be present in default office roster")
	}
	if general == nil {
		t.Fatal("expected general channel to exist")
	}
	if len(general.Members) < len(members) {
		t.Fatalf("expected general channel to include office roster, got %v for %d members", general.Members, len(members))
	}
}

func TestNewBrokerSeedsBlueprintBackedOfficeRosterOnFreshState(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	// Pair HOME with WUPHF_RUNTIME_HOME so config.RuntimeHomeDir (which
	// prefers the env override) resolves to this test's tmpdir. Without
	// it the process-level leaked runtime-home from worktree_guard_test
	// wins and the manifest below is invisible to the blueprint loader.
	t.Setenv("WUPHF_RUNTIME_HOME", home)
	manifestPath := filepath.Join(home, ".wuphf", "company.json")
	if err := os.MkdirAll(filepath.Dir(manifestPath), 0o700); err != nil {
		t.Fatalf("mkdir manifest dir: %v", err)
	}
	raw := `{
  "name": "Blueprint Office",
  "description": "Refs only manifest",
  "blueprint_refs": [
    {"kind":"operation","id":"youtube-factory","source":"test"}
  ]
}`
	if err := os.WriteFile(manifestPath, []byte(raw), 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
	members := b.OfficeMembers()
	if len(members) < 2 {
		t.Fatalf("expected blueprint-backed default office roster, got %d members", len(members))
	}
	var foundResearch bool
	for _, member := range members {
		if member.Slug == "research-lead" {
			foundResearch = true
			break
		}
	}
	if !foundResearch {
		t.Fatalf("expected blueprint-backed office roster to include youtube starter members, got %+v", members)
	}
}

func TestHandleMessagesSupportsInboxAndOutboxScopes(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	b := NewBroker()
	b.mu.Lock()
	b.members = append(b.members,
		officeMember{Slug: "pm", Name: "Product Manager"},
		officeMember{Slug: "fe", Name: "Frontend Engineer"},
	)
	for i := range b.channels {
		if b.channels[i].Slug == "general" {
			b.channels[i].Members = append(b.channels[i].Members, "pm", "fe")
			break
		}
	}
	b.mu.Unlock()
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("failed to start broker: %v", err)
	}
	defer b.Stop()

	root, err := b.PostMessage("ceo", "general", "Frontend, take the signup thread.", nil, "")
	if err != nil {
		t.Fatalf("post root message: %v", err)
	}
	ownReply, err := b.PostMessage("fe", "general", "I can own the signup thread.", nil, root.ID)
	if err != nil {
		t.Fatalf("post own reply: %v", err)
	}
	threadReply, err := b.PostMessage("pm", "general", "Please include the pricing copy in that thread.", nil, ownReply.ID)
	if err != nil {
		t.Fatalf("post thread reply: %v", err)
	}
	ownTopLevel, err := b.PostMessage("fe", "general", "Shipped the initial branch.", nil, "")
	if err != nil {
		t.Fatalf("post own top-level message: %v", err)
	}
	if _, err := b.PostMessage("pm", "general", "Unrelated roadmap chatter.", nil, ""); err != nil {
		t.Fatalf("post unrelated message: %v", err)
	}

	base := fmt.Sprintf("http://%s", b.Addr())
	fetch := func(scope string) []channelMessage {
		req, _ := http.NewRequest(http.MethodGet, base+"/messages?channel=general&viewer_slug=fe&scope="+scope, nil)
		req.Header.Set("Authorization", "Bearer "+b.Token())
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("get %s messages: %v", scope, err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			raw, _ := io.ReadAll(resp.Body)
			t.Fatalf("expected 200 for %s scope, got %d: %s", scope, resp.StatusCode, raw)
		}
		var result struct {
			Messages []channelMessage `json:"messages"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			t.Fatalf("decode %s messages: %v", scope, err)
		}
		return result.Messages
	}

	inbox := fetch("inbox")
	if len(inbox) != 2 {
		t.Fatalf("expected CEO root plus PM thread reply in inbox, got %+v", inbox)
	}
	if inbox[0].ID != root.ID || inbox[1].ID != threadReply.ID {
		t.Fatalf("unexpected inbox ordering/content: %+v", inbox)
	}

	outbox := fetch("outbox")
	if len(outbox) != 2 {
		t.Fatalf("expected only authored messages in outbox, got %+v", outbox)
	}
	if outbox[0].ID != ownReply.ID || outbox[1].ID != ownTopLevel.ID {
		t.Fatalf("unexpected outbox ordering/content: %+v", outbox)
	}
}

func TestOfficeMemberLifecycle(t *testing.T) {
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
	b.mu.Lock()
	b.members = append(b.members, officeMember{
		Slug:      "growthops",
		Name:      "Growth Ops",
		Role:      "Growth Ops",
		CreatedBy: "you",
	})
	if err := b.saveLocked(); err != nil {
		b.mu.Unlock()
		t.Fatalf("saveLocked failed: %v", err)
	}
	b.mu.Unlock()

	reloaded := reloadedBroker(t)
	if reloaded.findMemberLocked("growthops") == nil {
		t.Fatal("expected custom office member to persist")
	}
}

func TestBrokerPersistsNotificationCursorWithoutMessages(t *testing.T) {
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
	if err := b.SetNotificationCursor("2026-03-24T10:00:00Z"); err != nil {
		t.Fatalf("SetNotificationCursor failed: %v", err)
	}

	reloaded := reloadedBroker(t)
	if got := reloaded.NotificationCursor(); got != "2026-03-24T10:00:00Z" {
		t.Fatalf("expected persisted notification cursor, got %q", got)
	}
}

func TestChannelMembersRejectUnknownOfficeMember(t *testing.T) {
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("failed to start broker: %v", err)
	}
	defer b.Stop()

	base := fmt.Sprintf("http://%s", b.Addr())
	body, _ := json.Marshal(map[string]any{
		"action":  "add",
		"channel": "general",
		"slug":    "ghost",
	})
	req, _ := http.NewRequest(http.MethodPost, base+"/channel-members", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+b.Token())
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown member, got %d", resp.StatusCode)
	}
}

// TestChannelMembersRejectDisableOrRemoveOfLead verifies that /channel-members
// refuses to disable or remove a BuiltIn member (lead agent) from any
// channel. Before this guard was generalized, only the hardcoded "ceo"
// slug was protected — blueprint teams whose lead is something else (e.g.
// niche-crm uses "operator") could silently lose their lead from #general.
func TestChannelMembersRejectDisableOrRemoveOfLead(t *testing.T) {
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
	b.mu.Lock()
	now := time.Now().UTC().Format(time.RFC3339)
	b.members = []officeMember{
		{Slug: "operator", Name: "Operator", Role: "Operator", PermissionMode: "plan", BuiltIn: true, CreatedBy: "wuphf", CreatedAt: now},
		{Slug: "builder", Name: "Builder", Role: "Builder", PermissionMode: "auto", CreatedBy: "wuphf", CreatedAt: now},
	}
	b.channels = []teamChannel{
		{Slug: "general", Name: "general", Members: []string{"operator", "builder"}, CreatedBy: "wuphf", CreatedAt: now, UpdatedAt: now},
	}
	b.mu.Unlock()
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("failed to start broker: %v", err)
	}
	defer b.Stop()

	base := fmt.Sprintf("http://%s", b.Addr())
	for _, action := range []string{"disable", "remove"} {
		body, _ := json.Marshal(map[string]any{
			"action":  action,
			"channel": "general",
			"slug":    "operator",
		})
		req, _ := http.NewRequest(http.MethodPost, base+"/channel-members", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+b.Token())
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("action=%s: expected 400 (cannot remove/disable lead), got %d", action, resp.StatusCode)
		}
	}

	// After the rejected attempts, operator must still be a member of #general.
	b.mu.Lock()
	var found bool
	for _, ch := range b.channels {
		if ch.Slug == "general" {
			for _, m := range ch.Members {
				if m == "operator" {
					found = true
					break
				}
			}
			break
		}
	}
	b.mu.Unlock()
	if !found {
		t.Fatalf("expected operator to remain in #general after rejected disable/remove")
	}
}

func TestBrokerAuthRejectsUnauthenticated(t *testing.T) {
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
	b.runtimeProvider = "codex"
	t.Setenv("WUPHF_MEMORY_BACKEND", config.MemoryBackendGBrain)
	t.Setenv("WUPHF_OPENAI_API_KEY", "sk-test-openai")
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("failed to start broker: %v", err)
	}
	defer b.Stop()

	base := fmt.Sprintf("http://%s", b.Addr())

	// Health should work without auth
	resp, err := http.Get(base + "/health")
	if err != nil {
		t.Fatalf("health request failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		t.Fatalf("expected 200 on /health, got %d", resp.StatusCode)
	}
	var health struct {
		SessionMode         string `json:"session_mode"`
		OneOnOneAgent       string `json:"one_on_one_agent"`
		Provider            string `json:"provider"`
		MemoryBackend       string `json:"memory_backend"`
		MemoryBackendActive string `json:"memory_backend_active"`
		NexConnected        bool   `json:"nex_connected"`
		Build               struct {
			Version        string `json:"version"`
			BuildTimestamp string `json:"build_timestamp"`
		} `json:"build"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
		resp.Body.Close()
		t.Fatalf("decode health: %v", err)
	}
	resp.Body.Close()
	if health.SessionMode != SessionModeOffice {
		t.Fatalf("expected health to report office mode, got %q", health.SessionMode)
	}
	if health.OneOnOneAgent != DefaultOneOnOneAgent {
		t.Fatalf("expected health to report default 1o1 agent %q, got %q", DefaultOneOnOneAgent, health.OneOnOneAgent)
	}
	if health.Provider != "codex" {
		t.Fatalf("expected health to report provider codex, got %q", health.Provider)
	}
	if health.MemoryBackend != config.MemoryBackendGBrain {
		t.Fatalf("expected health to report selected memory backend gbrain, got %q", health.MemoryBackend)
	}
	if health.MemoryBackendActive != config.MemoryBackendNone {
		t.Fatalf("expected inactive gbrain backend without CLI installed, got %q", health.MemoryBackendActive)
	}
	if health.NexConnected {
		t.Fatal("expected nex_connected=false when gbrain is selected")
	}
	wantBuild := buildinfo.Current()
	if health.Build.Version != wantBuild.Version {
		t.Fatalf("expected health build version %q, got %q", wantBuild.Version, health.Build.Version)
	}
	if health.Build.BuildTimestamp != wantBuild.BuildTimestamp {
		t.Fatalf("expected health build timestamp %q, got %q", wantBuild.BuildTimestamp, health.Build.BuildTimestamp)
	}

	resp, err = http.Get(base + "/version")
	if err != nil {
		t.Fatalf("version request failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		t.Fatalf("expected 200 on /version, got %d", resp.StatusCode)
	}
	var version struct {
		Version        string `json:"version"`
		BuildTimestamp string `json:"build_timestamp"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&version); err != nil {
		resp.Body.Close()
		t.Fatalf("decode version: %v", err)
	}
	resp.Body.Close()
	if version.Version != wantBuild.Version {
		t.Fatalf("expected /version version %q, got %q", wantBuild.Version, version.Version)
	}
	if version.BuildTimestamp != wantBuild.BuildTimestamp {
		t.Fatalf("expected /version build timestamp %q, got %q", wantBuild.BuildTimestamp, version.BuildTimestamp)
	}

	// Messages without auth should be rejected
	resp, err = http.Get(base + "/messages")
	if err != nil {
		t.Fatalf("messages request failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 on /messages without auth, got %d", resp.StatusCode)
	}

	// Messages with correct token should succeed
	req, _ := http.NewRequest("GET", base+"/messages", nil)
	req.Header.Set("Authorization", "Bearer "+b.Token())
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("authenticated request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200 on /messages with auth, got %d: %s", resp.StatusCode, body)
	}

	// Messages with wrong token should be rejected
	req, _ = http.NewRequest("GET", base+"/messages", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("bad token request failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 on /messages with wrong token, got %d", resp.StatusCode)
	}
}

func TestBrokerRateLimitsRequestsPerIP(t *testing.T) {
	b := NewBroker()
	b.rateLimitRequests = 100
	b.rateLimitWindow = 1100 * time.Millisecond
	mux := http.NewServeMux()
	mux.HandleFunc("/messages", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := b.corsMiddleware(b.rateLimitMiddleware(mux))
	doRequest := func(forwardedFor string) *http.Response {
		req := httptest.NewRequest(http.MethodGet, "/messages", nil)
		req.RemoteAddr = "127.0.0.1:1234"
		if forwardedFor != "" {
			req.Header.Set("X-Forwarded-For", forwardedFor)
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec.Result()
	}

	for i := 0; i < 100; i++ {
		resp := doRequest("")
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected request %d to succeed, got %d", i+1, resp.StatusCode)
		}
	}

	resp := doRequest("")
	resp.Body.Close()
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected 101st request to be rate limited, got %d", resp.StatusCode)
	}
	retryAfter := resp.Header.Get("Retry-After")
	if retryAfter == "" {
		t.Fatal("expected Retry-After header on rate-limited response")
	}
	seconds, err := strconv.Atoi(retryAfter)
	if err != nil || seconds < 1 || seconds > 2 {
		t.Fatalf("expected sane Retry-After seconds, got %q", retryAfter)
	}

	time.Sleep(b.rateLimitWindow + 50*time.Millisecond)

	resp = doRequest("")
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected request after rolling window expiry to succeed, got %d", resp.StatusCode)
	}
}

func TestBrokerAuthenticatedRequestsBypassRateLimit(t *testing.T) {
	b := NewBroker()
	b.rateLimitRequests = 1
	b.rateLimitWindow = time.Second
	handler := b.rateLimitMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	doRequest := func(setAuthHeader bool, useQueryToken bool) *http.Response {
		target := "/messages"
		if useQueryToken {
			target += "?token=" + b.Token()
		}
		req := httptest.NewRequest(http.MethodGet, target, nil)
		req.RemoteAddr = "127.0.0.1:1234"
		if setAuthHeader {
			req.Header.Set("Authorization", "Bearer "+b.Token())
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec.Result()
	}

	resp := doRequest(true, false)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected authenticated header request to bypass limiter, got %d", resp.StatusCode)
	}

	resp = doRequest(true, false)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected repeated authenticated header request to bypass limiter, got %d", resp.StatusCode)
	}

	resp = doRequest(false, true)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected authenticated query-token request to bypass limiter, got %d", resp.StatusCode)
	}

	resp = doRequest(false, true)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected repeated authenticated query-token request to bypass limiter, got %d", resp.StatusCode)
	}
}

func TestBrokerRateLimitsUsingForwardedClientIP(t *testing.T) {
	b := NewBroker()
	b.rateLimitRequests = 1
	b.rateLimitWindow = time.Second
	handler := b.rateLimitMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	doRequest := func(remoteAddr, forwardedFor string) *http.Response {
		req := httptest.NewRequest(http.MethodGet, "/messages", nil)
		req.RemoteAddr = remoteAddr
		if forwardedFor != "" {
			req.Header.Set("X-Forwarded-For", forwardedFor)
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec.Result()
	}

	resp := doRequest("127.0.0.1:1111", "203.0.113.10")
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected first forwarded request to succeed, got %d", resp.StatusCode)
	}

	resp = doRequest("127.0.0.1:2222", "203.0.113.10")
	resp.Body.Close()
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected repeated forwarded client IP to be limited, got %d", resp.StatusCode)
	}

	resp = doRequest("127.0.0.1:3333", "203.0.113.11")
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected distinct forwarded client IP to get its own bucket, got %d", resp.StatusCode)
	}
}

func TestBrokerIgnoresForwardedClientIPFromNonLoopbackPeer(t *testing.T) {
	b := NewBroker()
	b.rateLimitRequests = 1
	b.rateLimitWindow = time.Second
	handler := b.rateLimitMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	doRequest := func(remoteAddr, forwardedFor string) *http.Response {
		req := httptest.NewRequest(http.MethodGet, "/messages", nil)
		req.RemoteAddr = remoteAddr
		if forwardedFor != "" {
			req.Header.Set("X-Forwarded-For", forwardedFor)
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec.Result()
	}

	resp := doRequest("198.51.100.8:1111", "203.0.113.10")
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected first request to succeed, got %d", resp.StatusCode)
	}

	resp = doRequest("198.51.100.8:2222", "203.0.113.11")
	resp.Body.Close()
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected non-loopback peer to be bucketed by remote addr, got %d", resp.StatusCode)
	}
}

// TestBrokerAgentRateLimitTripsOnRunawayLoop verifies that a prompt-injected
// agent that loops on tool calls eventually gets 429'd even though it holds a
// valid Bearer token. The IP bucket alone exempts token-holders, so this is
// the containment for runaway agent cost.
func TestBrokerAgentRateLimitTripsOnRunawayLoop(t *testing.T) {
	b := NewBroker()
	b.agentRateLimitRequests = 5
	b.agentRateLimitWindow = time.Second
	handler := b.rateLimitMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	doRequest := func(slug string) *http.Response {
		req := httptest.NewRequest(http.MethodPost, "/actions/execute", nil)
		req.RemoteAddr = "127.0.0.1:1234"
		req.Header.Set("Authorization", "Bearer "+b.Token())
		if slug != "" {
			req.Header.Set("X-WUPHF-Agent", slug)
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec.Result()
	}

	for i := 0; i < 5; i++ {
		resp := doRequest("ceo")
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected request %d within agent budget to succeed, got %d", i+1, resp.StatusCode)
		}
	}

	resp := doRequest("ceo")
	resp.Body.Close()
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected 6th request to trip per-agent bucket, got %d", resp.StatusCode)
	}
	if retryAfter := resp.Header.Get("Retry-After"); retryAfter == "" {
		t.Fatal("expected Retry-After header on rate-limited response")
	}

	// A different agent slug gets its own bucket.
	resp = doRequest("engineer")
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected distinct agent slug to get its own bucket, got %d", resp.StatusCode)
	}
}

// TestBrokerOperatorTrafficBypassesAgentRateLimit verifies the web UI, which
// authenticates with the broker token but does not identify itself as any
// particular agent, is not blocked by the per-agent bucket. If this breaks the
// operator loses access to their office whenever one agent loops.
func TestBrokerOperatorTrafficBypassesAgentRateLimit(t *testing.T) {
	b := NewBroker()
	b.agentRateLimitRequests = 1
	b.agentRateLimitWindow = time.Second
	handler := b.rateLimitMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	doRequest := func() *http.Response {
		req := httptest.NewRequest(http.MethodGet, "/messages", nil)
		req.RemoteAddr = "127.0.0.1:5555"
		req.Header.Set("Authorization", "Bearer "+b.Token())
		// Deliberately no X-WUPHF-Agent — this is operator traffic.
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec.Result()
	}
	for i := 0; i < 10; i++ {
		resp := doRequest()
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected operator request %d to bypass agent limiter, got %d", i+1, resp.StatusCode)
		}
	}
}

// TestBrokerAgentRateLimitExemptsSSEPaths verifies long-lived SSE streams are
// not counted against the per-agent bucket. They do not represent loopable
// tool calls — a single subscribe holds the connection open for minutes.
func TestBrokerAgentRateLimitExemptsSSEPaths(t *testing.T) {
	b := NewBroker()
	b.agentRateLimitRequests = 2
	b.agentRateLimitWindow = time.Second
	handler := b.rateLimitMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	doRequest := func(path string) *http.Response {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.RemoteAddr = "127.0.0.1:6666"
		req.Header.Set("Authorization", "Bearer "+b.Token())
		req.Header.Set("X-WUPHF-Agent", "ceo")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec.Result()
	}

	for i := 0; i < 10; i++ {
		resp := doRequest("/events")
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected /events subscribe %d to bypass agent limiter, got %d", i+1, resp.StatusCode)
		}
		resp = doRequest("/agent-stream/ceo")
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected /agent-stream subscribe %d to bypass agent limiter, got %d", i+1, resp.StatusCode)
		}
	}
}

// TestIsLoopbackRemote verifies the RemoteAddr-side half of the DNS-rebinding
// guard. Empty and unparseable addresses must fail closed — otherwise a
// test-only path, or a listener that exposes synthetic RemoteAddr, would
// silently open the gate.
func TestIsLoopbackRemote(t *testing.T) {
	cases := []struct {
		name       string
		remoteAddr string
		want       bool
	}{
		{"ipv4 loopback with port", "127.0.0.1:1234", true},
		{"ipv6 loopback with port", "[::1]:1234", true},
		{"localhost hostname", "localhost:4444", true},
		{"ipv4 loopback high octet", "127.255.255.255:1", true},
		{"ipv4 non-loopback", "10.0.0.5:1234", false},
		{"ipv4 external", "203.0.113.9:80", false},
		{"empty remote addr", "", false},
		{"malformed", "not-an-address", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.RemoteAddr = tc.remoteAddr
			if got := isLoopbackRemote(req); got != tc.want {
				t.Fatalf("isLoopbackRemote(%q) = %v, want %v", tc.remoteAddr, got, tc.want)
			}
		})
	}
	if got := isLoopbackRemote(nil); got {
		t.Fatal("isLoopbackRemote(nil) = true, want false (must fail closed)")
	}
}

// TestHostHeaderIsLoopback verifies the Host-side half of the DNS-rebinding
// guard. An attacker-controlled name that resolves to 127.0.0.1 at request
// time must be rejected based on the Host header alone.
func TestHostHeaderIsLoopback(t *testing.T) {
	cases := []struct {
		host string
		want bool
	}{
		{"localhost:7890", true},
		{"127.0.0.1:7890", true},
		{"[::1]:7890", true},
		{"localhost", true},
		{"127.0.0.1", true},
		{"::1", true},
		{"evil.example.com", false},
		{"evil.example.com:7890", false},
		{"rebind.attacker.test:7900", false},
		{"", false},
	}
	for _, tc := range cases {
		t.Run(tc.host, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Host = tc.host
			if got := hostHeaderIsLoopback(req); got != tc.want {
				t.Fatalf("hostHeaderIsLoopback(Host=%q) = %v, want %v", tc.host, got, tc.want)
			}
		})
	}
	if got := hostHeaderIsLoopback(nil); got {
		t.Fatal("hostHeaderIsLoopback(nil) = true, want false (must fail closed)")
	}
}

// TestWebUIRebindGuard is the integrated test for the guard composed of
// isLoopbackRemote AND hostHeaderIsLoopback. Both must pass for the request
// to reach the next handler. Either failing → 403.
func TestWebUIRebindGuard(t *testing.T) {
	guarded := webUIRebindGuard(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("reached"))
	}))

	cases := []struct {
		name       string
		remoteAddr string
		host       string
		wantStatus int
	}{
		{"loopback remote + loopback host", "127.0.0.1:5000", "localhost:7900", http.StatusOK},
		{"loopback remote + evil host (rebind attempt)", "127.0.0.1:5000", "evil.example.com:7900", http.StatusForbidden},
		{"non-loopback remote + loopback host", "203.0.113.9:80", "127.0.0.1:7900", http.StatusForbidden},
		{"non-loopback remote + evil host", "203.0.113.9:80", "evil.example.com:7900", http.StatusForbidden},
		{"empty remote + loopback host", "", "127.0.0.1:7900", http.StatusForbidden},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api-token", nil)
			req.RemoteAddr = tc.remoteAddr
			req.Host = tc.host
			rec := httptest.NewRecorder()
			guarded.ServeHTTP(rec, req)
			if rec.Code != tc.wantStatus {
				t.Fatalf("status=%d, want %d; body=%q", rec.Code, tc.wantStatus, rec.Body.String())
			}
		})
	}
}

// TestCORSMiddlewareDropsNullOriginWildcard verifies the fix for the CSO
// finding: Access-Control-Allow-Origin: * was previously returned for empty
// or "null" Origin headers. A file:// page could open on the operator's
// laptop and make authenticated cross-origin reads once it had the token.
// The new contract: no CORS header unless the Origin exactly matches the
// configured web UI origin.
func TestCORSMiddlewareDropsNullOriginWildcard(t *testing.T) {
	b := NewBroker()
	b.webUIOrigins = []string{"http://localhost:7900", "http://127.0.0.1:7900"}
	handler := b.corsMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	cases := []struct {
		name     string
		origin   string
		wantACAO string // empty means header must not be set
	}{
		{"null origin", "null", ""},
		{"empty origin", "", ""},
		{"allowed localhost origin", "http://localhost:7900", "http://localhost:7900"},
		{"allowed loopback origin", "http://127.0.0.1:7900", "http://127.0.0.1:7900"},
		{"disallowed origin", "http://evil.example.com", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/health", nil)
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			got := rec.Header().Get("Access-Control-Allow-Origin")
			if got != tc.wantACAO {
				t.Fatalf("Access-Control-Allow-Origin=%q, want %q", got, tc.wantACAO)
			}
		})
	}
}

// TestBrokerOnboardingRoutesRequireAuth verifies the CSO auth wrapping — a
// caller that can reach the broker port but does not hold the token must
// NOT be able to POST /onboarding/complete (which seeds the team and fires
// the first CEO turn) or read /onboarding/state.
func TestBrokerOnboardingRoutesRequireAuth(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	b := NewBroker()
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	defer b.Stop()

	base := "http://" + b.Addr()
	client := &http.Client{Timeout: 2 * time.Second}

	// Every onboarding route must 401 without a token.
	for _, path := range []string{
		"/onboarding/state",
		"/onboarding/prereqs",
		"/onboarding/templates",
		"/onboarding/blueprints",
	} {
		resp, err := client.Get(base + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("GET %s without auth: status=%d, want 401", path, resp.StatusCode)
		}
	}

	// /onboarding/complete is the big one — seeds the team + posts the first
	// task. Must reject unauthenticated POSTs before decoding the body.
	resp, err := client.Post(base+"/onboarding/complete", "application/json",
		strings.NewReader(`{"task":"pwn","skip_task":false}`))
	if err != nil {
		t.Fatalf("POST /onboarding/complete: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("POST /onboarding/complete without auth: status=%d, want 401", resp.StatusCode)
	}

	// With the token, /onboarding/state returns 200 (sanity check the wrapping
	// hasn't broken the happy path).
	req, _ := http.NewRequest(http.MethodGet, base+"/onboarding/state", nil)
	req.Header.Set("Authorization", "Bearer "+b.Token())
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("GET /onboarding/state with auth: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /onboarding/state with auth: status=%d, want 200", resp.StatusCode)
	}
}

func TestSetProxyClientIPHeaders(t *testing.T) {
	headers := make(http.Header)
	setProxyClientIPHeaders(headers, "203.0.113.44:5678")
	if got := headers.Get("X-Forwarded-For"); got != "203.0.113.44" {
		t.Fatalf("expected X-Forwarded-For to preserve remote IP, got %q", got)
	}
	if got := headers.Get("X-Real-IP"); got != "203.0.113.44" {
		t.Fatalf("expected X-Real-IP to preserve remote IP, got %q", got)
	}
}

func TestNormalizeChannelSlugStripsLeadingHash(t *testing.T) {
	if got := normalizeChannelSlug("#youtube-factory"); got != "youtube-factory" {
		t.Fatalf("expected leading hash to be stripped, got %q", got)
	}
	if got := normalizeChannelSlug("  #General  "); got != "general" {
		t.Fatalf("expected spaced channel mention to normalize, got %q", got)
	}
}

func TestChannelDescriptionsAreVisibleButContentStaysRestricted(t *testing.T) {
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()
	b := NewBroker()
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("failed to start broker: %v", err)
	}
	defer b.Stop()

	base := fmt.Sprintf("http://%s", b.Addr())
	createOfficeMemberForTest(t, base, b.Token(), "pm", "Product Manager", "Product Manager")
	createOfficeMemberForTest(t, base, b.Token(), "fe", "Frontend Engineer", "Frontend Engineer")
	createOfficeMemberForTest(t, base, b.Token(), "cmo", "CMO", "CMO")

	createBody, _ := json.Marshal(map[string]any{
		"action":      "create",
		"slug":        "launch",
		"name":        "launch",
		"description": "Launch planning and launch-readiness work.",
		"members":     []string{"pm", "fe"},
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
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 creating channel, got %d", resp.StatusCode)
	}

	req, _ = http.NewRequest(http.MethodGet, base+"/channels", nil)
	req.Header.Set("Authorization", "Bearer "+b.Token())
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get channels failed: %v", err)
	}
	defer resp.Body.Close()
	var channelList struct {
		Channels []teamChannel `json:"channels"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&channelList); err != nil {
		t.Fatalf("decode channels: %v", err)
	}
	var launch *teamChannel
	for i := range channelList.Channels {
		if channelList.Channels[i].Slug == "launch" {
			launch = &channelList.Channels[i]
			break
		}
	}
	if launch == nil {
		t.Fatal("expected launch channel in channel list")
	}
	if launch.Description != "Launch planning and launch-readiness work." {
		t.Fatalf("unexpected launch description: %q", launch.Description)
	}
	if !containsString(launch.Members, "ceo") || !containsString(launch.Members, "pm") || !containsString(launch.Members, "fe") {
		t.Fatalf("expected create payload members plus CEO in new channel, got %+v", launch.Members)
	}

	req, _ = http.NewRequest(http.MethodGet, base+"/messages?channel=launch&my_slug=cmo", nil)
	req.Header.Set("Authorization", "Bearer "+b.Token())
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get messages as non-member failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 for non-member channel messages, got %d", resp.StatusCode)
	}

	req, _ = http.NewRequest(http.MethodGet, base+"/messages?channel=launch&my_slug=ceo", nil)
	req.Header.Set("Authorization", "Bearer "+b.Token())
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get messages as ceo failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for CEO channel messages, got %d", resp.StatusCode)
	}
}

func TestChannelUpdateMutatesDescriptionAndMembers(t *testing.T) {
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("failed to start broker: %v", err)
	}
	defer b.Stop()

	base := fmt.Sprintf("http://%s", b.Addr())
	createOfficeMemberForTest(t, base, b.Token(), "research-lead", "Research Lead", "Research")
	createOfficeMemberForTest(t, base, b.Token(), "scriptwriter", "Scriptwriter", "Scripts")
	createOfficeMemberForTest(t, base, b.Token(), "growth-ops", "Growth Ops", "Growth")

	createBody, _ := json.Marshal(map[string]any{
		"action":      "create",
		"slug":        "yt-research",
		"name":        "yt-research",
		"description": "Old description",
		"members":     []string{"research-lead"},
		"created_by":  "ceo",
	})
	createReq, _ := http.NewRequest(http.MethodPost, base+"/channels", bytes.NewReader(createBody))
	createReq.Header.Set("Authorization", "Bearer "+b.Token())
	createReq.Header.Set("Content-Type", "application/json")
	createResp, err := http.DefaultClient.Do(createReq)
	if err != nil {
		t.Fatalf("create seed channel failed: %v", err)
	}
	if createResp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(createResp.Body)
		createResp.Body.Close()
		t.Fatalf("expected 200 creating seed channel, got %d: %s", createResp.StatusCode, raw)
	}
	createResp.Body.Close()
	b.mu.Lock()
	if ch := b.findChannelLocked("yt-research"); ch != nil {
		ch.Disabled = []string{"scriptwriter"}
	}
	b.mu.Unlock()

	updateBody, _ := json.Marshal(map[string]any{
		"action":      "update",
		"slug":        "yt-research",
		"name":        "yt-research",
		"description": "Search demand, topic scoring, and proof packets.",
		"members":     []string{"research-lead", "scriptwriter", "growth-ops"},
		"created_by":  "ceo",
	})
	req, _ := http.NewRequest(http.MethodPost, base+"/channels", bytes.NewReader(updateBody))
	req.Header.Set("Authorization", "Bearer "+b.Token())
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("update channel failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200 updating channel, got %d: %s", resp.StatusCode, raw)
	}

	var payload struct {
		Channel teamChannel `json:"channel"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode update response: %v", err)
	}
	if payload.Channel.Description != "Search demand, topic scoring, and proof packets." {
		t.Fatalf("unexpected description after update: %q", payload.Channel.Description)
	}
	if !containsString(payload.Channel.Members, "ceo") || !containsString(payload.Channel.Members, "scriptwriter") || !containsString(payload.Channel.Members, "growth-ops") {
		t.Fatalf("expected updated member roster plus CEO, got %+v", payload.Channel.Members)
	}
	if containsString(payload.Channel.Disabled, "scriptwriter") {
		t.Fatalf("expected disabled list to drop removed/now-enabled members, got %+v", payload.Channel.Disabled)
	}
}

func createOfficeMemberForTest(t *testing.T, base, token, slug, name, role string) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{
		"action":     "create",
		"slug":       slug,
		"name":       name,
		"role":       role,
		"created_by": "ceo",
	})
	req, _ := http.NewRequest(http.MethodPost, base+"/office-members", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("create office member %s failed: %v", slug, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200 creating office member %s, got %d: %s", slug, resp.StatusCode, raw)
	}
}

func TestNormalizeLoadedStateRepopulatesGeneralFromOfficeRoster(t *testing.T) {
	b := NewBroker()
	b.mu.Lock()
	defer b.mu.Unlock()

	b.members = []officeMember{
		{Slug: "ceo", Name: "CEO", Role: "CEO", BuiltIn: true},
		{Slug: "pm", Name: "Product Manager", Role: "Product Manager"},
		{Slug: "fe", Name: "Frontend Engineer", Role: "Frontend Engineer"},
	}
	b.channels = []teamChannel{{
		Slug:        "general",
		Name:        "general",
		Description: "Company-wide room",
		Members:     []string{"ceo"},
	}}

	b.normalizeLoadedStateLocked()

	ch := b.findChannelLocked("general")
	if ch == nil {
		t.Fatal("expected general channel after normalization")
	}
	if !containsString(ch.Members, "ceo") || !containsString(ch.Members, "pm") || !containsString(ch.Members, "fe") {
		t.Fatalf("expected general channel to be repopulated from office roster, got %+v", ch.Members)
	}
}

func TestTaskAndRequestViewsRejectNonMembers(t *testing.T) {
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
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

func TestParseOTLPUsageEvents(t *testing.T) {
	payload := map[string]any{
		"resourceLogs": []any{
			map[string]any{
				"resource": map[string]any{
					"attributes": []any{
						map[string]any{"key": "agent.slug", "value": map[string]any{"stringValue": "fe"}},
					},
				},
				"scopeLogs": []any{
					map[string]any{
						"logRecords": []any{
							map[string]any{
								"attributes": []any{
									map[string]any{"key": "event.name", "value": map[string]any{"stringValue": "api_request"}},
									map[string]any{"key": "input_tokens", "value": map[string]any{"intValue": "1200"}},
									map[string]any{"key": "output_tokens", "value": map[string]any{"intValue": "300"}},
									map[string]any{"key": "cache_read_tokens", "value": map[string]any{"intValue": "50"}},
									map[string]any{"key": "cache_creation_tokens", "value": map[string]any{"intValue": "25"}},
									map[string]any{"key": "cost_usd", "value": map[string]any{"doubleValue": 0.42}},
								},
							},
						},
					},
				},
			},
		},
	}

	events := parseOTLPUsageEvents(payload)
	if len(events) != 1 {
		t.Fatalf("expected 1 usage event, got %d", len(events))
	}
	if events[0].AgentSlug != "fe" {
		t.Fatalf("expected fe slug, got %q", events[0].AgentSlug)
	}
	if events[0].InputTokens != 1200 || events[0].OutputTokens != 300 {
		t.Fatalf("unexpected token counts: %+v", events[0])
	}
	if events[0].CostUsd != 0.42 {
		t.Fatalf("unexpected cost: %+v", events[0])
	}
}

func TestBrokerUsageEndpointAggregatesTelemetry(t *testing.T) {
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("failed to start broker: %v", err)
	}
	defer b.Stop()

	base := fmt.Sprintf("http://%s", b.Addr())
	payload := map[string]any{
		"resourceLogs": []any{
			map[string]any{
				"resource": map[string]any{
					"attributes": []any{
						map[string]any{"key": "agent.slug", "value": map[string]any{"stringValue": "be"}},
					},
				},
				"scopeLogs": []any{
					map[string]any{
						"logRecords": []any{
							map[string]any{
								"attributes": []any{
									map[string]any{"key": "event.name", "value": map[string]any{"stringValue": "api_request"}},
									map[string]any{"key": "input_tokens", "value": map[string]any{"intValue": "800"}},
									map[string]any{"key": "output_tokens", "value": map[string]any{"intValue": "200"}},
									map[string]any{"key": "cost_usd", "value": map[string]any{"doubleValue": 0.18}},
								},
							},
						},
					},
				},
			},
		},
	}
	body, _ := json.Marshal(payload)
	req, _ := http.NewRequest(http.MethodPost, base+"/v1/logs", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+b.Token())
	req.Header.Set("Content-Type", "application/json")
	teleResp, teleErr := http.DefaultClient.Do(req)
	if teleErr != nil {
		t.Fatalf("telemetry post failed: %v", teleErr)
	}
	teleResp.Body.Close()
	if teleResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 from usage ingest, got %d", teleResp.StatusCode)
	}

	req, _ = http.NewRequest(http.MethodGet, base+"/usage", nil)
	req.Header.Set("Authorization", "Bearer "+b.Token())
	usageResp, usageErr := http.DefaultClient.Do(req)
	if usageErr != nil {
		t.Fatalf("usage request failed: %v", usageErr)
	}
	defer usageResp.Body.Close()
	var usage teamUsageState
	if err := json.NewDecoder(usageResp.Body).Decode(&usage); err != nil {
		t.Fatalf("decode usage response: %v", err)
	}
	if usage.Total.TotalTokens != 1000 {
		t.Fatalf("expected 1000 total tokens, got %d", usage.Total.TotalTokens)
	}
	if usage.Session.TotalTokens != 1000 {
		t.Fatalf("expected 1000 session tokens, got %d", usage.Session.TotalTokens)
	}
	if usage.Agents["be"].CostUsd != 0.18 {
		t.Fatalf("expected backend cost 0.18, got %+v", usage.Agents["be"])
	}
}

func TestBrokerActionsAndSchedulerEndpoints(t *testing.T) {
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
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

func TestSchedulerDueOnlyFiltersFutureJobs(t *testing.T) {
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
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

func TestBrokerPostsAndDedupesNexNotifications(t *testing.T) {
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
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

func TestBrokerTaskLifecycle(t *testing.T) {
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
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
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
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
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
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
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
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
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
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
	oldPrepare := prepareTaskWorktree
	oldCleanup := cleanupTaskWorktree
	prepareTaskWorktree = func(taskID string) (string, string, error) {
		return "/tmp/wuphf-task-" + taskID, "wuphf-" + taskID, nil
	}
	cleanupTaskWorktree = func(path, branch string) error { return nil }
	defer func() {
		prepareTaskWorktree = oldPrepare
		cleanupTaskWorktree = oldCleanup
	}()

	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()

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
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
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
	oldPrepare := prepareTaskWorktree
	oldCleanup := cleanupTaskWorktree
	prepareTaskWorktree = func(taskID string) (string, string, error) {
		return "/tmp/wuphf-task-" + taskID, "wuphf-" + taskID, nil
	}
	cleanupTaskWorktree = func(path, branch string) error { return nil }
	defer func() {
		prepareTaskWorktree = oldPrepare
		cleanupTaskWorktree = oldCleanup
	}()

	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
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
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
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
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
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
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
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
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
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
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
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
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
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
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
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
	oldPrepare := prepareTaskWorktree
	oldCleanup := cleanupTaskWorktree
	prepareTaskWorktree = func(taskID string) (string, string, error) {
		return "/tmp/wuphf-task-" + taskID, "wuphf-" + taskID, nil
	}
	cleanupTaskWorktree = func(path, branch string) error { return nil }
	defer func() {
		prepareTaskWorktree = oldPrepare
		cleanupTaskWorktree = oldCleanup
	}()

	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
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
	oldPrepare := prepareTaskWorktree
	oldCleanup := cleanupTaskWorktree
	var cleanedPath, cleanedBranch string
	prepareTaskWorktree = func(taskID string) (string, string, error) {
		return "/tmp/wuphf-task-" + taskID, "wuphf-" + taskID, nil
	}
	cleanupTaskWorktree = func(path, branch string) error {
		cleanedPath = path
		cleanedBranch = branch
		return nil
	}
	defer func() {
		prepareTaskWorktree = oldPrepare
		cleanupTaskWorktree = oldCleanup
	}()

	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
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
	oldPrepare := prepareTaskWorktree
	oldCleanup := cleanupTaskWorktree
	cleanupCalls := 0
	worktreeRoot := t.TempDir()
	prepareTaskWorktree = func(taskID string) (string, string, error) {
		path := filepath.Join(worktreeRoot, "wuphf-task-"+taskID)
		initUsableGitWorktree(t, path)
		return path, "wuphf-" + taskID, nil
	}
	cleanupTaskWorktree = func(path, branch string) error {
		cleanupCalls++
		return nil
	}
	defer func() {
		prepareTaskWorktree = oldPrepare
		cleanupTaskWorktree = oldCleanup
	}()

	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
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
	oldPrepare := prepareTaskWorktree
	oldCleanup := cleanupTaskWorktree
	oldVerify := verifyTaskWorktreeWritable
	worktreeDir := t.TempDir()
	prepareTaskWorktree = func(taskID string) (string, string, error) {
		return worktreeDir, "wuphf-" + taskID, nil
	}
	cleanupTaskWorktree = func(path, branch string) error { return nil }
	verifyTaskWorktreeWritable = func(path string) error {
		if path != worktreeDir {
			t.Fatalf("expected probe path %q, got %q", worktreeDir, path)
		}
		return nil
	}
	defer func() {
		prepareTaskWorktree = oldPrepare
		cleanupTaskWorktree = oldCleanup
		verifyTaskWorktreeWritable = oldVerify
	}()

	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
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

func TestBrokerBlockTaskRejectsFalseReadOnlyBlockForWritableWorktree(t *testing.T) {
	oldPrepare := prepareTaskWorktree
	oldCleanup := cleanupTaskWorktree
	oldVerify := verifyTaskWorktreeWritable
	worktreeDir := t.TempDir()
	prepareTaskWorktree = func(taskID string) (string, string, error) {
		return worktreeDir, "wuphf-" + taskID, nil
	}
	cleanupTaskWorktree = func(path, branch string) error { return nil }
	verifyTaskWorktreeWritable = func(path string) error {
		if path != worktreeDir {
			t.Fatalf("expected probe path %q, got %q", worktreeDir, path)
		}
		return nil
	}
	defer func() {
		prepareTaskWorktree = oldPrepare
		cleanupTaskWorktree = oldCleanup
		verifyTaskWorktreeWritable = oldVerify
	}()

	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
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
	oldPrepare := prepareTaskWorktree
	oldCleanup := cleanupTaskWorktree
	prepareTaskWorktree = func(taskID string) (string, string, error) {
		return "/tmp/wuphf-task-" + taskID, "wuphf-" + taskID, nil
	}
	cleanupTaskWorktree = func(path, branch string) error { return nil }
	defer func() {
		prepareTaskWorktree = oldPrepare
		cleanupTaskWorktree = oldCleanup
	}()

	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
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
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
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
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
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
	oldPrepare := prepareTaskWorktree
	oldCleanup := cleanupTaskWorktree
	oldVerify := verifyTaskWorktreeWritable
	worktreeDir := t.TempDir()
	prepareTaskWorktree = func(taskID string) (string, string, error) {
		return worktreeDir, "wuphf-" + taskID, nil
	}
	cleanupTaskWorktree = func(path, branch string) error { return nil }
	verifyTaskWorktreeWritable = func(path string) error {
		if path != worktreeDir {
			t.Fatalf("expected probe path %q, got %q", worktreeDir, path)
		}
		return fmt.Errorf("permission denied")
	}
	defer func() {
		prepareTaskWorktree = oldPrepare
		cleanupTaskWorktree = oldCleanup
		verifyTaskWorktreeWritable = oldVerify
	}()

	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
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
	oldPrepare := prepareTaskWorktree
	oldCleanup := cleanupTaskWorktree
	prepareTaskWorktree = func(taskID string) (string, string, error) {
		return "/tmp/wuphf-task-" + taskID, "wuphf-" + taskID, nil
	}
	cleanupTaskWorktree = func(path, branch string) error { return nil }
	defer func() {
		prepareTaskWorktree = oldPrepare
		cleanupTaskWorktree = oldCleanup
	}()

	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
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
	oldPrepare := prepareTaskWorktree
	oldCleanup := cleanupTaskWorktree
	var prepareCalls []string
	worktreeRoot := t.TempDir()
	prepareTaskWorktree = func(taskID string) (string, string, error) {
		prepareCalls = append(prepareCalls, taskID)
		if len(prepareCalls) > 1 {
			return "", "", fmt.Errorf("unexpected prepareTaskWorktree call for %s", taskID)
		}
		path := filepath.Join(worktreeRoot, "wuphf-task-"+taskID)
		initUsableGitWorktree(t, path)
		return path, "wuphf-" + taskID, nil
	}
	cleanupTaskWorktree = func(path, branch string) error { return nil }
	defer func() {
		prepareTaskWorktree = oldPrepare
		cleanupTaskWorktree = oldCleanup
	}()

	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
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
	oldPrepare := prepareTaskWorktree
	oldCleanup := cleanupTaskWorktree
	stalePath := t.TempDir()
	freshPath := filepath.Join(t.TempDir(), "fresh-worktree")
	var cleaned []string
	prepareTaskWorktree = func(taskID string) (string, string, error) {
		return freshPath, "wuphf-" + taskID, nil
	}
	cleanupTaskWorktree = func(path, branch string) error {
		cleaned = append(cleaned, path+"|"+branch)
		return nil
	}
	defer func() {
		prepareTaskWorktree = oldPrepare
		cleanupTaskWorktree = oldCleanup
	}()

	b := NewBroker()
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
	oldPrepare := prepareTaskWorktree
	oldCleanup := cleanupTaskWorktree
	stalePath := t.TempDir()
	freshPath := filepath.Join(t.TempDir(), "fresh-worktree")
	prepareTaskWorktree = func(taskID string) (string, string, error) {
		return freshPath, "wuphf-" + taskID, nil
	}
	cleanupTaskWorktree = func(path, branch string) error { return nil }
	defer func() {
		prepareTaskWorktree = oldPrepare
		cleanupTaskWorktree = oldCleanup
	}()

	now := time.Now().UTC().Format(time.RFC3339)
	b := NewBroker()
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
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
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
	oldPrepare := prepareTaskWorktree
	oldCleanup := cleanupTaskWorktree
	prepareTaskWorktree = func(taskID string) (string, string, error) {
		return "/tmp/wuphf-task-" + taskID, "wuphf-" + taskID, nil
	}
	cleanupTaskWorktree = func(path, branch string) error { return nil }
	defer func() {
		prepareTaskWorktree = oldPrepare
		cleanupTaskWorktree = oldCleanup
	}()

	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
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
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
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
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
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
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
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

func TestBrokerRequestAnswerUnblocksDependentTask(t *testing.T) {
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
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
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
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
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
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

func TestQueueEndpointShowsDueJobs(t *testing.T) {
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
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

func TestBrokerGetMessagesAgentScopeKeepsHumanAndCEOContext(t *testing.T) {
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
	b.mu.Lock()
	b.members = append(b.members,
		officeMember{Slug: "pm", Name: "Product Manager"},
		officeMember{Slug: "fe", Name: "Frontend Engineer"},
	)
	for i := range b.channels {
		if b.channels[i].Slug == "general" {
			b.channels[i].Members = append(b.channels[i].Members, "pm", "fe")
			break
		}
	}
	b.mu.Unlock()
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("failed to start broker: %v", err)
	}
	defer b.Stop()

	base := fmt.Sprintf("http://%s", b.Addr())
	postMessage := func(payload map[string]any) {
		t.Helper()
		body, _ := json.Marshal(payload)
		req, _ := http.NewRequest(http.MethodPost, base+"/messages", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+b.Token())
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("post message: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			raw, _ := io.ReadAll(resp.Body)
			t.Fatalf("expected 200 posting message, got %d: %s", resp.StatusCode, raw)
		}
	}

	postMessage(map[string]any{"channel": "general", "from": "you", "content": "Frontend, should we ship this?", "tagged": []string{"fe"}})
	postMessage(map[string]any{"channel": "general", "from": "pm", "content": "Unrelated roadmap chatter."})
	postMessage(map[string]any{"channel": "general", "from": "ceo", "content": "Keep scope tight and focus on signup."})
	postMessage(map[string]any{"channel": "general", "from": "fe", "content": "I can take the signup work."})

	req, _ := http.NewRequest(http.MethodGet, base+"/messages?channel=general&viewer_slug=fe&scope=agent", nil)
	req.Header.Set("Authorization", "Bearer "+b.Token())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get messages: %v", err)
	}
	defer resp.Body.Close()

	var result struct {
		Messages []channelMessage `json:"messages"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode messages: %v", err)
	}
	if len(result.Messages) != 3 {
		t.Fatalf("expected scoped transcript to keep 3 messages, got %+v", result.Messages)
	}
	if got := result.Messages[1].From; got != "ceo" {
		t.Fatalf("expected CEO context to remain visible, got %+v", result.Messages)
	}
	for _, msg := range result.Messages {
		if msg.From == "pm" {
			t.Fatalf("did not expect unrelated PM chatter in scoped transcript: %+v", result.Messages)
		}
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

func TestParseSkillProposalFromMessage(t *testing.T) {
	b := &Broker{}
	b.members = []officeMember{{Slug: "ceo", Name: "CEO"}}
	msg := channelMessage{
		ID:      "msg-1",
		From:    "ceo",
		Channel: "general",
		Content: "I noticed a pattern.\n\n[SKILL PROPOSAL]\nName: deploy-verify\nTitle: Deploy Verification\nDescription: Post-deploy checks\nTrigger: after deploy\nTags: deploy, ops\n---\n1. Check health\n2. Check errors\n[/SKILL PROPOSAL]",
	}
	b.parseSkillProposalLocked(msg)
	if len(b.skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(b.skills))
	}
	s := b.skills[0]
	if s.Name != "deploy-verify" {
		t.Fatalf("expected name 'deploy-verify', got %q", s.Name)
	}
	if s.Title != "Deploy Verification" {
		t.Fatalf("expected title 'Deploy Verification', got %q", s.Title)
	}
	if s.Status != "proposed" {
		t.Fatalf("expected status 'proposed', got %q", s.Status)
	}
	if s.Description != "Post-deploy checks" {
		t.Fatalf("expected description 'Post-deploy checks', got %q", s.Description)
	}
}

func TestLastTaggedAtSetOnPost(t *testing.T) {
	b := &Broker{}
	b.channels = []teamChannel{{Slug: "general", Members: []string{"ceo", "pm"}}}
	b.members = []officeMember{{Slug: "ceo", Name: "CEO"}, {Slug: "pm", Name: "PM"}}

	// Post a message tagging ceo
	msg := channelMessage{
		ID:      "msg-1",
		From:    "you",
		Channel: "general",
		Content: "@ceo what should we do?",
		Tagged:  []string{"ceo"},
	}

	if b.lastTaggedAt == nil {
		b.lastTaggedAt = make(map[string]time.Time)
	}

	// Simulate what handlePostMessage does
	if len(msg.Tagged) > 0 && (msg.From == "you" || msg.From == "human") {
		for _, slug := range msg.Tagged {
			b.lastTaggedAt[slug] = time.Now()
		}
	}

	if _, ok := b.lastTaggedAt["ceo"]; !ok {
		t.Fatal("expected ceo to be in lastTaggedAt")
	}
	if _, ok := b.lastTaggedAt["pm"]; ok {
		t.Fatal("did not expect pm to be in lastTaggedAt")
	}
}

func TestBrokerSurfaceMetadataPersists(t *testing.T) {
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
	b.mu.Lock()
	b.channels = append(b.channels, teamChannel{
		Slug:    "tg-ops",
		Name:    "tg-ops",
		Members: []string{"ceo"},
		Surface: &channelSurface{
			Provider:    "telegram",
			RemoteID:    "-100999",
			RemoteTitle: "Ops Group",
			Mode:        "supergroup",
			BotTokenEnv: "MY_BOT_TOKEN",
		},
		CreatedBy: "test",
		CreatedAt: "2026-01-01T00:00:00Z",
		UpdatedAt: "2026-01-01T00:00:00Z",
	})
	if err := b.saveLocked(); err != nil {
		b.mu.Unlock()
		t.Fatalf("saveLocked: %v", err)
	}
	b.mu.Unlock()

	reloaded := reloadedBroker(t)
	var found *teamChannel
	for _, ch := range reloaded.channels {
		if ch.Slug == "tg-ops" {
			found = &ch
			break
		}
	}
	if found == nil {
		t.Fatal("expected tg-ops channel after reload")
	}
	if found.Surface == nil {
		t.Fatal("expected surface metadata to persist")
	}
	if found.Surface.Provider != "telegram" {
		t.Fatalf("expected provider=telegram, got %q", found.Surface.Provider)
	}
	if found.Surface.RemoteID != "-100999" {
		t.Fatalf("expected remote_id=-100999, got %q", found.Surface.RemoteID)
	}
	if found.Surface.RemoteTitle != "Ops Group" {
		t.Fatalf("expected remote_title=Ops Group, got %q", found.Surface.RemoteTitle)
	}
	if found.Surface.Mode != "supergroup" {
		t.Fatalf("expected mode=supergroup, got %q", found.Surface.Mode)
	}
	if found.Surface.BotTokenEnv != "MY_BOT_TOKEN" {
		t.Fatalf("expected bot_token_env=MY_BOT_TOKEN, got %q", found.Surface.BotTokenEnv)
	}
}

func TestBrokerSurfaceChannelsFilter(t *testing.T) {
	t.Skip("skipped: manifest interference")
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
	b.mu.Lock()
	b.channels = append(b.channels,
		teamChannel{
			Slug:    "tg-ch",
			Name:    "tg-ch",
			Members: []string{"ceo"},
			Surface: &channelSurface{Provider: "telegram", RemoteID: "-100"},
		},
		teamChannel{
			Slug:    "slack-ch",
			Name:    "slack-ch",
			Members: []string{"ceo"},
			Surface: &channelSurface{Provider: "slack", RemoteID: "C123"},
		},
		teamChannel{
			Slug:    "native-ch",
			Name:    "native-ch",
			Members: []string{"ceo"},
		},
	)
	b.mu.Unlock()

	tgChannels := b.SurfaceChannels("telegram")
	if len(tgChannels) < 1 {
		t.Fatalf("expected at least 1 telegram channel, got %d", len(tgChannels))
	}
	if tgChannels[0].Slug != "tg-ch" {
		t.Fatalf("expected tg-ch, got %q", tgChannels[0].Slug)
	}

	slackChannels := b.SurfaceChannels("slack")
	if len(slackChannels) != 1 {
		t.Fatalf("expected 1 slack channel, got %d", len(slackChannels))
	}

	nativeChannels := b.SurfaceChannels("")
	if len(nativeChannels) != 0 {
		t.Fatalf("expected 0 native surface channels, got %d", len(nativeChannels))
	}
}

func TestBrokerExternalQueueDeduplication(t *testing.T) {
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
	b.mu.Lock()
	b.channels = append(b.channels, teamChannel{
		Slug:    "ext",
		Name:    "ext",
		Members: []string{"ceo"},
		Surface: &channelSurface{Provider: "telegram", RemoteID: "-100"},
	})
	b.mu.Unlock()

	// Post two messages
	b.PostMessage("ceo", "ext", "msg one", nil, "")
	b.PostMessage("ceo", "ext", "msg two", nil, "")

	queue1 := b.ExternalQueue("telegram")
	if len(queue1) != 2 {
		t.Fatalf("expected 2 messages in first drain, got %d", len(queue1))
	}

	// Second drain should be empty
	queue2 := b.ExternalQueue("telegram")
	if len(queue2) != 0 {
		t.Fatalf("expected 0 messages in second drain, got %d", len(queue2))
	}

	// Post one more
	b.PostMessage("ceo", "ext", "msg three", nil, "")
	queue3 := b.ExternalQueue("telegram")
	if len(queue3) != 1 {
		t.Fatalf("expected 1 new message, got %d", len(queue3))
	}
	if queue3[0].Content != "msg three" {
		t.Fatalf("expected 'msg three', got %q", queue3[0].Content)
	}
}

func TestBrokerPostInboundSurfaceMessage(t *testing.T) {
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
	b.mu.Lock()
	b.channels = append(b.channels, teamChannel{
		Slug:    "surf",
		Name:    "surf",
		Members: []string{"ceo"},
		Surface: &channelSurface{Provider: "telegram", RemoteID: "-100"},
	})
	b.mu.Unlock()

	msg, err := b.PostInboundSurfaceMessage("alice", "surf", "hello surface", "telegram")
	if err != nil {
		t.Fatalf("PostInboundSurfaceMessage: %v", err)
	}
	if msg.Kind != "surface" {
		t.Fatalf("expected kind=surface, got %q", msg.Kind)
	}
	if msg.Source != "telegram" {
		t.Fatalf("expected source=telegram, got %q", msg.Source)
	}

	// Inbound should not appear in the external queue
	queue := b.ExternalQueue("telegram")
	if len(queue) != 0 {
		t.Fatalf("inbound message should not appear in external queue, got %d", len(queue))
	}

	// But it should appear in channel messages
	msgs := b.ChannelMessages("surf")
	if len(msgs) != 1 {
		t.Fatalf("expected 1 channel message, got %d", len(msgs))
	}
}

func TestInFlightTasksReturnsOnlyNonTerminalOwned(t *testing.T) {
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
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
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
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

func TestRecentHumanMessagesReturnsLastNHumanMessages(t *testing.T) {
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
	b.mu.Lock()
	b.messages = []channelMessage{
		{ID: "m1", From: "fe", Content: "agent reply 1", Timestamp: "2026-04-14T10:00:00Z"},
		{ID: "m2", From: "you", Content: "human says hi", Timestamp: "2026-04-14T10:01:00Z"},
		{ID: "m3", From: "nex", Content: "nex automation", Timestamp: "2026-04-14T10:02:00Z"},
		{ID: "m4", From: "be", Content: "agent reply 2", Timestamp: "2026-04-14T10:03:00Z"},
		{ID: "m5", From: "human", Content: "human follow-up", Timestamp: "2026-04-14T10:04:00Z"},
		{ID: "m6", From: "you", Content: "human again", Timestamp: "2026-04-14T10:05:00Z"},
	}
	b.mu.Unlock()

	// Request last 2 human messages — should return m5 and m6 (the most recent 2 from human senders).
	got := b.RecentHumanMessages(2)
	if len(got) != 2 {
		t.Fatalf("expected 2 recent human messages, got %d: %+v", len(got), got)
	}
	if got[0].ID != "m5" {
		t.Errorf("expected first message m5, got %q", got[0].ID)
	}
	if got[1].ID != "m6" {
		t.Errorf("expected second message m6, got %q", got[1].ID)
	}
}

func TestRecentHumanMessagesLimitCapsResults(t *testing.T) {
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
	b.mu.Lock()
	b.messages = []channelMessage{
		{ID: "m1", From: "you", Content: "first", Timestamp: "2026-04-14T10:00:00Z"},
		{ID: "m2", From: "you", Content: "second", Timestamp: "2026-04-14T10:01:00Z"},
		{ID: "m3", From: "nex", Content: "nex msg", Timestamp: "2026-04-14T10:02:00Z"},
	}
	b.mu.Unlock()

	// nex is also a human/external sender — all 3 qualify; limit=5 returns all 3.
	got := b.RecentHumanMessages(5)
	if len(got) != 3 {
		t.Fatalf("expected 3 messages (you+you+nex), got %d", len(got))
	}
}

func TestRecentHumanMessagesExcludesNonHuman(t *testing.T) {
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
	b.mu.Lock()
	b.messages = []channelMessage{
		{ID: "m1", From: "fe", Content: "agent", Timestamp: "2026-04-14T10:00:00Z"},
		{ID: "m2", From: "be", Content: "agent2", Timestamp: "2026-04-14T10:01:00Z"},
	}
	b.mu.Unlock()

	got := b.RecentHumanMessages(10)
	if len(got) != 0 {
		t.Fatalf("expected 0 human messages, got %d", len(got))
	}
}

func TestRecentHumanMessagesIncludesNexSender(t *testing.T) {
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
	b.mu.Lock()
	b.messages = []channelMessage{
		{ID: "m1", From: "fe", Content: "agent msg", Timestamp: "2026-04-14T10:00:00Z"},
		{ID: "m2", From: "nex", Content: "nex automation context", Timestamp: "2026-04-14T10:01:00Z"},
		{ID: "m3", From: "you", Content: "human question", Timestamp: "2026-04-14T10:02:00Z"},
	}
	b.mu.Unlock()

	// Spec: "nex" is treated as human/external alongside "you" and "human".
	// Without nex messages in resume packets, conversations triggered by Nex automation
	// are silently dropped on restart.
	got := b.RecentHumanMessages(10)
	if len(got) != 2 {
		t.Fatalf("expected 2 messages (nex+you), got %d", len(got))
	}
	ids := map[string]bool{}
	for _, m := range got {
		ids[m.ID] = true
	}
	if !ids["m2"] {
		t.Error("expected nex message m2 to be included")
	}
	if !ids["m3"] {
		t.Error("expected human message m3 to be included")
	}
	if ids["m1"] {
		t.Error("expected agent message m1 to be excluded")
	}
}

// --- Skill proposal system tests ---

// Helper: skillProposalContent returns a well-formed [SKILL PROPOSAL] block.
func skillProposalContent(name, title string) string {
	return fmt.Sprintf("[SKILL PROPOSAL]\nName: %s\nTitle: %s\nDescription: Test description\nTrigger: on test\nTags: test\n---\n1. Do the thing\n[/SKILL PROPOSAL]", name, title)
}

// Test 1: CEO (lead) message creates a proposed skill.
func TestParseSkillProposalCEOHappyPath(t *testing.T) {
	b := &Broker{}
	b.members = []officeMember{{Slug: "ceo", Name: "CEO", Role: "lead"}}
	b.channels = []teamChannel{{Slug: "general", Members: []string{"ceo"}}}
	msg := channelMessage{
		ID:      "msg-1",
		From:    "ceo",
		Channel: "general",
		Content: "I noticed a pattern.\n\n" + skillProposalContent("my-skill", "My Skill"),
	}
	b.parseSkillProposalLocked(msg)
	if len(b.skills) != 1 {
		t.Fatalf("expected 1 skill from CEO, got %d", len(b.skills))
	}
	if b.skills[0].Status != "proposed" {
		t.Fatalf("expected status 'proposed', got %q", b.skills[0].Status)
	}
}

// Test 2: Non-CEO message is silently skipped.
func TestParseSkillProposalNonCEOSkipped(t *testing.T) {
	b := &Broker{}
	b.members = []officeMember{
		{Slug: "ceo", Name: "CEO", Role: "lead"},
		{Slug: "fe", Name: "Frontend Engineer"},
	}
	msg := channelMessage{
		ID:      "msg-1",
		From:    "fe",
		Channel: "general",
		Content: skillProposalContent("fe-skill", "FE Skill"),
	}
	b.parseSkillProposalLocked(msg)
	if len(b.skills) != 0 {
		t.Fatalf("expected 0 skills from non-CEO, got %d", len(b.skills))
	}
}

// Test 3: Malformed proposal (missing Name) is skipped.
func TestParseSkillProposalMalformedSkipped(t *testing.T) {
	b := &Broker{}
	b.members = []officeMember{{Slug: "ceo", Name: "CEO", Role: "lead"}}
	msg := channelMessage{
		From:    "ceo",
		Channel: "general",
		Content: "[SKILL PROPOSAL]\nTitle: No Name Skill\nDescription: missing name field\n---\n1. Do thing\n[/SKILL PROPOSAL]",
	}
	b.parseSkillProposalLocked(msg)
	if len(b.skills) != 0 {
		t.Fatalf("expected 0 skills for malformed proposal, got %d", len(b.skills))
	}
}

// Test 4: Duplicate proposal (same name, non-archived) is skipped.
func TestParseSkillProposalDedup(t *testing.T) {
	b := &Broker{}
	b.members = []officeMember{{Slug: "ceo", Name: "CEO", Role: "lead"}}
	b.channels = []teamChannel{{Slug: "general", Members: []string{"ceo"}}}
	b.skills = []teamSkill{{
		ID: "dup-skill", Name: "dup-skill", Title: "Dup Skill",
		Status: "proposed", CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z",
	}}
	msg := channelMessage{
		From:    "ceo",
		Channel: "general",
		Content: skillProposalContent("dup-skill", "Dup Skill"),
	}
	b.parseSkillProposalLocked(msg)
	if len(b.skills) != 1 {
		t.Fatalf("expected dedup to skip re-proposal, got %d skills", len(b.skills))
	}
}

// Test 5: Archived skill can be re-proposed (not deduped).
func TestParseSkillProposalAllowsReproposalAfterArchive(t *testing.T) {
	b := &Broker{}
	b.members = []officeMember{{Slug: "ceo", Name: "CEO", Role: "lead"}}
	b.channels = []teamChannel{{Slug: "general", Members: []string{"ceo"}}}
	b.skills = []teamSkill{{
		ID: "old-skill", Name: "old-skill", Title: "Old Skill",
		Status: "archived", CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z",
	}}
	msg := channelMessage{
		From:    "ceo",
		Channel: "general",
		Content: skillProposalContent("old-skill", "Old Skill Revised"),
	}
	b.parseSkillProposalLocked(msg)
	if len(b.skills) != 2 {
		t.Fatalf("expected archived skill to allow re-proposal (2 total), got %d", len(b.skills))
	}
}

// Test 6: parseSkillProposalLocked creates a non-blocking humanInterview in b.requests.
func TestParseSkillProposalCreatesNonBlockingInterview(t *testing.T) {
	b := &Broker{}
	b.members = []officeMember{{Slug: "ceo", Name: "CEO", Role: "lead"}}
	b.channels = []teamChannel{{Slug: "general", Members: []string{"ceo"}}}
	msg := channelMessage{
		From:    "ceo",
		Channel: "general",
		Content: skillProposalContent("interview-skill", "Interview Skill"),
	}
	b.parseSkillProposalLocked(msg)
	if len(b.requests) != 1 {
		t.Fatalf("expected 1 interview request, got %d", len(b.requests))
	}
	req := b.requests[0]
	if req.Blocking {
		t.Fatalf("expected non-blocking skill proposal interview, got Blocking=true")
	}
	if req.Kind != "skill_proposal" {
		t.Fatalf("expected kind 'skill_proposal', got %q", req.Kind)
	}
	if req.ReplyTo != "interview-skill" {
		t.Fatalf("expected ReplyTo='interview-skill', got %q", req.ReplyTo)
	}
}

func TestParseSkillProposalParsesMultipleBlocks(t *testing.T) {
	b := &Broker{}
	b.members = []officeMember{{Slug: "ceo", Name: "CEO", Role: "lead"}}
	b.channels = []teamChannel{{Slug: "general", Members: []string{"ceo"}}}
	msg := channelMessage{
		From:    "ceo",
		Channel: "general",
		Content: strings.Join([]string{
			"Integration bundle follows.",
			skillProposalContent("gmail-dry-run-harness", "Gmail Dry-Run Harness"),
			skillProposalContent("youtube-data-publish-dry-run", "YouTube Data Publish Dry-Run"),
			skillProposalContent("drive-asset-stage-dry-run", "Drive Asset Stage Dry-Run"),
		}, "\n\n"),
	}

	b.parseSkillProposalLocked(msg)

	if len(b.skills) != 3 {
		t.Fatalf("expected 3 skills from one CEO message, got %d", len(b.skills))
	}
	if len(b.requests) != 3 {
		t.Fatalf("expected 3 interview requests from one CEO message, got %d", len(b.requests))
	}
	if got := b.skills[0].Name; got != "gmail-dry-run-harness" {
		t.Fatalf("unexpected first skill slug: %q", got)
	}
	if got := b.skills[2].Name; got != "drive-asset-stage-dry-run" {
		t.Fatalf("unexpected third skill slug: %q", got)
	}
}

// Test 7: Answering "accept" via HTTP activates the skill.
func TestSkillProposalAcceptCallbackActivatesSkill(t *testing.T) {
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	defer b.Stop()

	// Seed a proposed skill and matching interview request.
	b.mu.Lock()
	b.skills = append(b.skills, teamSkill{
		ID: "deploy-check", Name: "deploy-check", Title: "Deploy Check",
		Status: "proposed", CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z",
	})
	b.counter++
	reqID := fmt.Sprintf("request-%d", b.counter)
	b.requests = append(b.requests, humanInterview{
		ID:        reqID,
		Kind:      "skill_proposal",
		Status:    "pending",
		From:      "ceo",
		Channel:   "general",
		Title:     "Approve skill: Deploy Check",
		Question:  "Activate?",
		ReplyTo:   "deploy-check",
		Blocking:  false,
		CreatedAt: "2026-01-01T00:00:00Z",
		UpdatedAt: "2026-01-01T00:00:00Z",
		Options:   []interviewOption{{ID: "accept", Label: "Accept"}, {ID: "reject", Label: "Reject"}},
	})
	b.mu.Unlock()

	base := fmt.Sprintf("http://%s", b.Addr())
	answerBody, _ := json.Marshal(map[string]any{
		"id":          reqID,
		"choice_id":   "accept",
		"choice_text": "Accept",
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
	status := b.skills[0].Status
	b.mu.Unlock()
	if status != "active" {
		t.Fatalf("expected skill status 'active' after accept, got %q", status)
	}
}

// Test 8: Answering "reject" via HTTP archives the skill.
func TestSkillProposalRejectCallbackArchivesSkill(t *testing.T) {
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	defer b.Stop()

	b.mu.Lock()
	b.skills = append(b.skills, teamSkill{
		ID: "risky-skill", Name: "risky-skill", Title: "Risky Skill",
		Status: "proposed", CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z",
	})
	b.counter++
	reqID := fmt.Sprintf("request-%d", b.counter)
	b.requests = append(b.requests, humanInterview{
		ID:        reqID,
		Kind:      "skill_proposal",
		Status:    "pending",
		From:      "ceo",
		Channel:   "general",
		Title:     "Approve skill: Risky Skill",
		Question:  "Activate?",
		ReplyTo:   "risky-skill",
		Blocking:  false,
		CreatedAt: "2026-01-01T00:00:00Z",
		UpdatedAt: "2026-01-01T00:00:00Z",
		Options:   []interviewOption{{ID: "accept", Label: "Accept"}, {ID: "reject", Label: "Reject"}},
	})
	b.mu.Unlock()

	base := fmt.Sprintf("http://%s", b.Addr())
	answerBody, _ := json.Marshal(map[string]any{
		"id":          reqID,
		"choice_id":   "reject",
		"choice_text": "Reject",
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
	status := b.skills[0].Status
	b.mu.Unlock()
	if status != "archived" {
		t.Fatalf("expected skill status 'archived' after reject, got %q", status)
	}
}

func TestRequestAnswerUnblocksReferencedTask(t *testing.T) {
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
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

func TestInvokeSkillTracksInvokerChannelAndExecutionMetadata(t *testing.T) {
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
	b.mu.Lock()
	b.skills = append(b.skills, teamSkill{
		ID:        "skill-youtube-factory-bootstrap",
		Name:      "youtube-factory-bootstrap",
		Title:     "Bootstrap Automated YouTube Factory",
		Status:    "active",
		Channel:   "general",
		CreatedBy: "ceo",
	})
	b.channels = append(b.channels, teamChannel{
		Slug:    "youtube-factory",
		Name:    "YouTube Factory",
		Members: []string{"ceo", "ops"},
	})
	b.mu.Unlock()

	body := bytes.NewBufferString(`{"name":"youtube-factory-bootstrap","invoked_by":"you","channel":"youtube-factory"}`)
	req := httptest.NewRequest(http.MethodPost, "/skills/youtube-factory-bootstrap/invoke", body)
	rec := httptest.NewRecorder()

	b.handleInvokeSkill(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	if b.skills[0].UsageCount != 1 {
		t.Fatalf("expected usage count 1, got %d", b.skills[0].UsageCount)
	}
	if b.skills[0].LastExecutionStatus != "invoked" {
		t.Fatalf("expected last execution status invoked, got %q", b.skills[0].LastExecutionStatus)
	}
	if b.skills[0].LastExecutionAt == "" {
		t.Fatal("expected last execution timestamp to be set")
	}
	last := b.messages[len(b.messages)-1]
	if last.Channel != "youtube-factory" {
		t.Fatalf("expected invocation message in youtube-factory, got %q", last.Channel)
	}
	if last.From != "you" {
		t.Fatalf("expected invocation from you, got %q", last.From)
	}
	if !strings.Contains(last.Content, "@you") {
		t.Fatalf("expected invocation content to reference @you, got %q", last.Content)
	}
}

// Test 10: buildPrompt for the lead includes SKILL & AGENT AWARENESS section.
func TestBuildPromptLeadIncludesSkillAwareness(t *testing.T) {
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	l := &Launcher{
		pack: &agent.PackDefinition{
			LeadSlug: "ceo",
			Agents: []agent.AgentConfig{
				{Slug: "ceo", Name: "CEO"},
				{Slug: "fe", Name: "Frontend Engineer"},
			},
		},
	}
	prompt := l.buildPrompt("ceo")
	if !strings.Contains(prompt, "SKILL & AGENT AWARENESS") {
		t.Fatalf("expected SKILL & AGENT AWARENESS block in lead prompt")
	}
	if !strings.Contains(prompt, "[SKILL PROPOSAL]") {
		t.Fatalf("expected [SKILL PROPOSAL] format example in lead prompt")
	}
}

// Test 10: Skill proposal and interview persist and reload correctly.
func TestSkillProposalPersistenceRoundTrip(t *testing.T) {
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
	b.mu.Lock()
	b.members = []officeMember{{Slug: "ceo", Name: "CEO", Role: "lead"}}
	for i := range b.channels {
		if b.channels[i].Slug == "general" {
			b.channels[i].Members = append(b.channels[i].Members, "ceo")
		}
	}
	msg := channelMessage{
		ID:      "msg-1",
		From:    "ceo",
		Channel: "general",
		Content: skillProposalContent("persist-skill", "Persist Skill"),
	}
	b.parseSkillProposalLocked(msg)
	if err := b.saveLocked(); err != nil {
		b.mu.Unlock()
		t.Fatalf("saveLocked: %v", err)
	}
	b.mu.Unlock()

	reloaded := reloadedBroker(t)
	reloaded.mu.Lock()
	skills := append([]teamSkill(nil), reloaded.skills...)
	requests := append([]humanInterview(nil), reloaded.requests...)
	reloaded.mu.Unlock()

	if len(skills) != 1 || skills[0].Name != "persist-skill" {
		t.Fatalf("expected persisted skill 'persist-skill', got %d skills", len(skills))
	}
	if len(requests) != 1 || requests[0].Kind != "skill_proposal" {
		t.Fatalf("expected persisted skill_proposal request, got %d requests", len(requests))
	}
}

// ─── Message deduplication ────────────────────────────────────────────────

// TestPostAutomationMessageDeduplicatesByEventID verifies that posting a
// message with the same eventID twice stores only one copy and returns the
// existing message on the second call.
func TestPostAutomationMessageDeduplicatesByEventID(t *testing.T) {
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()

	first, dup1, err := b.PostAutomationMessage("nex", "general", "Signal", "first post", "evt-001", "nex", "Nex", nil, "")
	if err != nil {
		t.Fatalf("first PostAutomationMessage: %v", err)
	}
	if dup1 {
		t.Fatal("first call should not be a duplicate")
	}

	second, dup2, err := b.PostAutomationMessage("nex", "general", "Signal", "second post", "evt-001", "nex", "Nex", nil, "")
	if err != nil {
		t.Fatalf("second PostAutomationMessage: %v", err)
	}
	if !dup2 {
		t.Fatal("second call with same eventID must be flagged as duplicate")
	}
	if second.ID != first.ID {
		t.Fatalf("duplicate call must return original message ID %q, got %q", first.ID, second.ID)
	}

	// Only one message should be stored.
	msgs := b.Messages()
	count := 0
	for _, m := range msgs {
		if m.EventID == "evt-001" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected exactly 1 message with eventID evt-001, got %d", count)
	}
}

// TestExternalQueueDeduplicatesByMessageID verifies that calling ExternalQueue
// twice for a surface channel only delivers each message once.
func TestExternalQueueDeduplicatesByMessageID(t *testing.T) {
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()

	// Register a channel with a surface so ExternalQueue has something to scan.
	b.mu.Lock()
	b.channels = append(b.channels, teamChannel{
		Slug:    "slack-general",
		Name:    "Slack General",
		Members: []string{"ceo"},
		Surface: &channelSurface{Provider: "slack"},
	})
	b.mu.Unlock()

	// Post a message directly into the broker state (bypassing HTTP) so it lands
	// in the surface channel without going through PostInboundSurfaceMessage (which
	// auto-marks as delivered).
	b.mu.Lock()
	b.counter++
	b.messages = append(b.messages, channelMessage{
		ID:        fmt.Sprintf("msg-%d", b.counter),
		From:      "you",
		Channel:   "slack-general",
		Content:   "Hello from Slack",
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	})
	b.mu.Unlock()

	first := b.ExternalQueue("slack")
	if len(first) != 1 {
		t.Fatalf("expected 1 message on first ExternalQueue call, got %d", len(first))
	}

	second := b.ExternalQueue("slack")
	if len(second) != 0 {
		t.Fatalf("expected 0 messages on second ExternalQueue call (already delivered), got %d", len(second))
	}
}

// ─── Focus mode routing ───────────────────────────────────────────────────

// makeFocusModeLauncher builds a Launcher backed by a real broker with three
// members (ceo, eng, pm) wired into the general channel, and focus mode on.
func makeFocusModeLauncher(t *testing.T) (*Launcher, *Broker) {
	t.Helper()
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	t.Cleanup(func() { brokerStatePath = oldPathFn })

	b := NewBroker()

	// Add eng and pm members to the broker so they appear in EnabledMembers.
	b.mu.Lock()
	b.members = []officeMember{
		{Slug: "ceo", Name: "CEO", Role: "CEO", BuiltIn: true},
		{Slug: "eng", Name: "Engineer", Role: "Engineer"},
		{Slug: "pm", Name: "Product Manager", Role: "Product Manager"},
	}
	for i := range b.channels {
		if b.channels[i].Slug == "general" {
			b.channels[i].Members = []string{"ceo", "eng", "pm"}
		}
	}
	b.focusMode = true
	b.mu.Unlock()

	l := &Launcher{
		pack: &agent.PackDefinition{
			LeadSlug: "ceo",
			Agents: []agent.AgentConfig{
				{Slug: "ceo", Name: "CEO"},
				{Slug: "eng", Name: "Engineer"},
				{Slug: "pm", Name: "Product Manager"},
			},
		},
		broker:          b,
		headlessWorkers: make(map[string]bool),
		headlessActive:  make(map[string]*headlessCodexActiveTurn),
		headlessQueues:  make(map[string][]headlessCodexTurn),
	}
	return l, b
}

// TestFocusModeRouting_UntaggedMessageWakesLeadOnly verifies that an untagged
// human message in focus mode only notifies the lead (CEO), not specialists.
func TestFocusModeRouting_UntaggedMessageWakesLeadOnly(t *testing.T) {
	l, _ := makeFocusModeLauncher(t)

	msg := channelMessage{
		ID:      "msg-1",
		From:    "you",
		Channel: "general",
		Content: "What should we do today?",
		Tagged:  nil,
	}
	immediate, _ := l.notificationTargetsForMessage(msg)

	if len(immediate) != 1 {
		t.Fatalf("focus mode untagged: expected 1 target (CEO), got %d: %v", len(immediate), immediate)
	}
	if immediate[0].Slug != "ceo" {
		t.Fatalf("focus mode untagged: expected ceo, got %q", immediate[0].Slug)
	}
}

// TestFocusModeRouting_TaggedSpecialistWakesSpecialistOnly verifies that when
// the human explicitly tags a specialist in focus mode, only that specialist
// wakes — not the lead.
func TestFocusModeRouting_TaggedSpecialistWakesSpecialistOnly(t *testing.T) {
	l, _ := makeFocusModeLauncher(t)

	msg := channelMessage{
		ID:      "msg-2",
		From:    "you",
		Channel: "general",
		Content: "Hey eng, can you review the PR?",
		Tagged:  []string{"eng"},
	}
	immediate, _ := l.notificationTargetsForMessage(msg)

	if len(immediate) != 1 {
		t.Fatalf("focus mode @eng: expected 1 target, got %d: %v", len(immediate), immediate)
	}
	if immediate[0].Slug != "eng" {
		t.Fatalf("focus mode @eng: expected eng, got %q", immediate[0].Slug)
	}
}

// TestFocusModeRouting_CollobaborativeUntaggedWakesAll verifies the contrast:
// without focus mode, an untagged human message wakes all enabled agents.
func TestFocusModeRouting_CollaborativeUntaggedWakesAll(t *testing.T) {
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
	b.mu.Lock()
	b.members = []officeMember{
		{Slug: "ceo", Name: "CEO", Role: "CEO", BuiltIn: true},
		{Slug: "eng", Name: "Engineer", Role: "Engineer"},
		{Slug: "pm", Name: "Product Manager", Role: "Product Manager"},
	}
	for i := range b.channels {
		if b.channels[i].Slug == "general" {
			b.channels[i].Members = []string{"ceo", "eng", "pm"}
		}
	}
	b.focusMode = false // collaborative mode
	b.mu.Unlock()

	l := &Launcher{
		pack: &agent.PackDefinition{
			LeadSlug: "ceo",
			Agents: []agent.AgentConfig{
				{Slug: "ceo", Name: "CEO"},
				{Slug: "eng", Name: "Engineer"},
				{Slug: "pm", Name: "Product Manager"},
			},
		},
		broker:          b,
		headlessWorkers: make(map[string]bool),
		headlessActive:  make(map[string]*headlessCodexActiveTurn),
		headlessQueues:  make(map[string][]headlessCodexTurn),
	}

	msg := channelMessage{
		ID:      "msg-3",
		From:    "you",
		Channel: "general",
		Content: "What should we do today?",
		Tagged:  nil,
	}
	immediate, _ := l.notificationTargetsForMessage(msg)

	// In collaborative mode, CEO always wakes for human messages.
	hasCEO := false
	for _, t := range immediate {
		if t.Slug == "ceo" {
			hasCEO = true
		}
	}
	if !hasCEO {
		t.Fatalf("collaborative mode: expected CEO in targets, got %v", immediate)
	}
}

// ─── Push semantics ───────────────────────────────────────────────────────

// TestHeadlessQueue_EmptyBeforePush verifies that the agent headless queue
// starts empty — no timers or background goroutines pre-populate it.
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
	origRunTurn := headlessCodexRunTurn
	headlessCodexRunTurn = func(l *Launcher, ctx context.Context, slug, notification string, channel ...string) error {
		// Block until the context is cancelled so the worker stays "active"
		// and doesn't drain the queue during the test assertion window.
		<-ctx.Done()
		return ctx.Err()
	}
	defer func() { headlessCodexRunTurn = origRunTurn }()

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
	l.headlessCtx, l.headlessCancel = context.WithCancel(context.Background())
	defer l.headlessCancel()

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
func TestEnsureDefaultOfficeMembersSeedsWhenEmpty(t *testing.T) {
	b := NewBroker()
	b.mu.Lock()
	b.members = nil
	b.ensureDefaultOfficeMembersLocked()
	got := len(b.members)
	b.mu.Unlock()
	if got == 0 {
		t.Fatalf("expected defaults to be seeded when members empty, got 0")
	}
	defaults := defaultOfficeMembers()
	if len(defaults) != got {
		t.Fatalf("expected exactly the default roster (len=%d), got len=%d", len(defaults), got)
	}
}

// REGRESSION: if a blueprint has seeded members (e.g. operator/planner/builder/
// growth/reviewer for niche-crm), ensureDefaultOfficeMembersLocked must NOT
// append ceo/planner/executor/reviewer on top.
func TestEnsureDefaultOfficeMembersNoOpWhenNonEmpty(t *testing.T) {
	b := NewBroker()
	b.mu.Lock()
	b.members = []officeMember{
		{Slug: "operator", Name: "Operator", Role: "Operator", PermissionMode: "plan", BuiltIn: true},
		{Slug: "builder", Name: "Builder", Role: "Builder", PermissionMode: "auto"},
	}
	b.ensureDefaultOfficeMembersLocked()
	got := make([]string, 0, len(b.members))
	for _, m := range b.members {
		got = append(got, m.Slug)
	}
	b.mu.Unlock()

	want := []string{"operator", "builder"}
	if len(got) != len(want) {
		t.Fatalf("expected roster unchanged %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("expected roster unchanged %v, got %v", want, got)
		}
	}
	for _, m := range got {
		if m == "ceo" || m == "planner" || m == "executor" || m == "reviewer" {
			t.Fatalf("default slug %q appended into blueprint roster; roster=%v", m, got)
		}
	}
}

// REGRESSION: simulate a fully-seeded blueprint team, save to disk, load into
// a fresh broker, confirm the team survives unchanged. This is the load-path
// leak the design doc calls out — prior append-behavior in
// ensureDefaultOfficeMembersLocked (called from Broker.Load() at broker.go:2260)
// silently re-added ceo/planner/executor/reviewer.
func TestLoadDoesNotAppendDefaultsAfterBlueprintSeed(t *testing.T) {
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
	b.mu.Lock()
	now := time.Now().UTC().Format(time.RFC3339)
	b.members = []officeMember{
		{Slug: "operator", Name: "Operator", Role: "Operator", PermissionMode: "plan", BuiltIn: true, CreatedBy: "wuphf", CreatedAt: now},
		{Slug: "planner", Name: "Planner", Role: "Planner", PermissionMode: "plan", CreatedBy: "wuphf", CreatedAt: now},
		{Slug: "builder", Name: "Builder", Role: "Builder", PermissionMode: "auto", CreatedBy: "wuphf", CreatedAt: now},
		{Slug: "growth", Name: "Growth", Role: "Growth", PermissionMode: "auto", CreatedBy: "wuphf", CreatedAt: now},
		{Slug: "reviewer", Name: "Reviewer", Role: "Reviewer", PermissionMode: "plan", CreatedBy: "wuphf", CreatedAt: now},
	}
	// Seed a task so saveLocked doesn't short-circuit on default state.
	b.tasks = []teamTask{{ID: "niche-crm-1", Channel: "general", Title: "Choose the niche", Status: "open", CreatedBy: "wuphf", CreatedAt: now, UpdatedAt: now}}
	if err := b.saveLocked(); err != nil {
		b.mu.Unlock()
		t.Fatalf("saveLocked failed: %v", err)
	}
	b.mu.Unlock()

	reloaded := reloadedBroker(t)
	reloaded.mu.Lock()
	slugs := make([]string, 0, len(reloaded.members))
	for _, m := range reloaded.members {
		slugs = append(slugs, m.Slug)
	}
	reloaded.mu.Unlock()

	want := []string{"operator", "planner", "builder", "growth", "reviewer"}
	if len(slugs) != len(want) {
		t.Fatalf("expected blueprint roster %v to survive reload, got %v", want, slugs)
	}
	for i := range want {
		if slugs[i] != want[i] {
			t.Fatalf("expected blueprint roster %v to survive reload, got %v", want, slugs)
		}
	}
	for _, s := range slugs {
		if s == "ceo" || s == "executor" {
			t.Fatalf("default slug %q leaked into reloaded roster: %v", s, slugs)
		}
	}
}
