package team

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestAgentStreamBuffer_RecentReturnsBoundedHistory pins the buffer
// trim semantics: pushing past the cap (2000 lines) drops the oldest
// entries while preserving the trailing window. Drift here would
// either grow the buffer unboundedly (memory leak) or drop too
// aggressively (lose visible history).
func TestAgentStreamBuffer_RecentReturnsBoundedHistory(t *testing.T) {
	s := &agentStreamBuffer{subs: make(map[int]agentStreamSubscriber)}
	for i := 0; i < 2100; i++ {
		s.Push(fmt.Sprintf("line-%d", i))
	}
	got := s.recent()
	if len(got) != 2000 {
		t.Errorf("recent length: want 2000 (cap), got %d", len(got))
	}
	if len(got) > 0 && got[len(got)-1] != "line-2099" {
		t.Errorf("expected most recent line preserved, got %q", got[len(got)-1])
	}
	if len(got) > 0 && got[0] != "line-100" {
		t.Errorf("expected trailing window starts at line-100 after dropping 100 oldest, got %q", got[0])
	}
	// Subscribers must observe live writes.
	out, cancel := s.subscribe()
	defer cancel()
	go s.Push("live-line")
	select {
	case msg := <-out:
		if msg != "live-line" {
			t.Errorf("subscriber received %q, want live-line", msg)
		}
	case <-time.After(time.Second):
		t.Error("subscriber did not receive Push within 1s")
	}
}

func TestAgentStreamBuffer_TaskScopedHistoryAndSubscribers(t *testing.T) {
	s := &agentStreamBuffer{subs: make(map[int]agentStreamSubscriber)}
	s.PushTask("task-1", "one")
	s.PushTask("task-2", "two")
	s.Push("global")

	if got := strings.Join(s.recentTask("task-1"), ","); got != "one" {
		t.Fatalf("task-1 recent = %q, want one", got)
	}
	if got := strings.Join(s.recent(), ","); got != "one,two,global" {
		t.Fatalf("all recent = %q", got)
	}

	taskOne, cancelTaskOne := s.subscribeTask("task-1")
	defer cancelTaskOne()
	all, cancelAll := s.subscribe()
	defer cancelAll()
	s.PushTask("task-2", "two-live")
	s.PushTask("task-1", "one-live")

	select {
	case got := <-all:
		if got != "two-live" {
			t.Fatalf("all subscriber first message = %q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("all subscriber did not receive task-2 line")
	}
	select {
	case got := <-taskOne:
		if got != "one-live" {
			t.Fatalf("task subscriber message = %q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("task subscriber did not receive task-1 line")
	}
}

func TestAgentStreamBuffer_TaskHistorySurvivesGlobalEviction(t *testing.T) {
	s := &agentStreamBuffer{subs: make(map[int]agentStreamSubscriber)}
	s.PushTask("task-1", "one")
	for i := 0; i < agentStreamHistoryLimit+1; i++ {
		s.PushTask("task-2", fmt.Sprintf("two-%d", i))
	}

	if got := strings.Join(s.recentTask("task-1"), ","); got != "one" {
		t.Fatalf("task-1 recent = %q, want one", got)
	}
	if got := s.recent(); len(got) != agentStreamHistoryLimit {
		t.Fatalf("global recent length = %d, want %d", len(got), agentStreamHistoryLimit)
	}
}

func TestAgentStreamBuffer_SubscribeTaskWithRecentHasNoReplayGap(t *testing.T) {
	s := &agentStreamBuffer{subs: make(map[int]agentStreamSubscriber)}
	s.PushTask("task-1", "history\n")

	history, live, cancel := s.subscribeTaskWithRecent("task-1")
	defer cancel()
	if got := strings.Join(history, ""); got != "history\n" {
		t.Fatalf("history = %q, want history line", got)
	}

	s.PushTask("task-1", "live\n")
	select {
	case got := <-live:
		if got != "live\n" {
			t.Fatalf("live = %q, want live line", got)
		}
	case <-time.After(time.Second):
		t.Fatal("subscriber did not receive live line")
	}
}

func TestHandleAgentToolEvent_ScopesLineToActiveTask(t *testing.T) {
	b := newTestBroker(t)
	task, _, err := b.EnsureTask("general", "Inspect terminal", "Verify tool output", "ceo", "ceo", "")
	if err != nil {
		t.Fatalf("EnsureTask: %v", err)
	}

	req := httptest.NewRequest(
		http.MethodPost,
		"/agent-tool-event",
		strings.NewReader(`{"slug":"ceo","phase":"call","tool":"team_broadcast","args":"{\"text\":\"hi\"}"}`),
	)
	rec := httptest.NewRecorder()
	b.handleAgentToolEvent(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	got := strings.Join(b.AgentStream("ceo").recentTask(task.ID), "\n")
	if !strings.Contains(got, `"tool":"team_broadcast"`) {
		t.Fatalf("task-scoped stream missing tool event: %q", got)
	}
}

// TestJaccardWordSimilarity_KnownPairs pins the broadcast-dedupe core
// metric. Identical text → 1.0; disjoint → 0.0; near-paraphrase >= 0.85
// (the duplicateBroadcastSimilarity threshold the dedupe path uses).
func TestJaccardWordSimilarity_KnownPairs(t *testing.T) {
	cases := []struct {
		a, b string
		want float64
	}{
		{"hello world", "hello world", 1.0},
		{"hello world", "completely different", 0.0},
		// Empty inputs short-circuit to 0 (any-empty rule) — not a
		// classical Jaccard 1.0 for {}∩{}, but the dedupe path needs
		// "no content to compare" to count as not-a-duplicate.
		{"", "", 0.0},
		{"hello", "", 0.0},
	}
	for _, tc := range cases {
		got := jaccardWordSimilarity(tc.a, tc.b)
		if got != tc.want {
			t.Errorf("jaccardWordSimilarity(%q, %q): want %v, got %v", tc.a, tc.b, tc.want, got)
		}
	}

	// Pin the duplicateBroadcastSimilarity (0.85) threshold with a pair
	// the dedupe path WOULD treat as duplicate: identical content with
	// trivial whitespace difference. Jaccard on this pair should be 1.0
	// (same token set), well above 0.85. A near-paraphrase pair like
	// "fox jumps" vs "fox jumped" only scores ~0.67 — useful as a
	// floor but doesn't pin the actual cutoff.
	near := "the quick brown fox jumps over the lazy dog"
	nearVar := "  the quick brown fox jumps over the lazy dog  "
	if got := jaccardWordSimilarity(near, nearVar); got < 0.85 {
		t.Errorf("identical-modulo-whitespace pair: want ≥ 0.85, got %v", got)
	}
	// And a clear non-duplicate pair must fall below the cutoff so a
	// regression that lowers the threshold gets caught.
	farA := "the quick brown fox jumps"
	farB := "completely unrelated tokens here"
	if got := jaccardWordSimilarity(farA, farB); got >= 0.85 {
		t.Errorf("disjoint-token pair: want < 0.85, got %v", got)
	}
}

// TestUniqueWordSet_StripsPunctuation pins the token-normalisation
// guard for jaccardWordSimilarity: trailing punctuation must collapse
// so "court," and "court" tokenize to the same set element.
func TestUniqueWordSet_StripsPunctuation(t *testing.T) {
	got := uniqueWordSet("court, lawyer; judge!")
	for _, want := range []string{"court", "lawyer", "judge"} {
		if _, ok := got[want]; !ok {
			t.Errorf("expected token %q in set, got %v", want, got)
		}
	}
	// Apostrophes inside words must NOT split.
	got = uniqueWordSet("reviewer's")
	if _, ok := got["reviewer's"]; !ok {
		t.Errorf("expected intra-word apostrophe preserved, got %v", got)
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

// TestReapStaleActivityLocked_StuckEmissionForActiveAgent locks the new
// stuck-while-active path: an active agent that has gone quiet for >=
// stuckThresholdSeconds (90s) but less than the safety reset (5m) gets a
// Kind="stuck" snapshot without losing its current Status/Activity/Detail.
// CRITICAL REGRESSION coverage: the existing "stale -> idle" path must still
// fire for agents past staleActivityThreshold even after the stuck branch
// landed.
func TestReapStaleActivityLocked_StuckEmissionForActiveAgent(t *testing.T) {
	b := newTestBroker(t)
	now := time.Now().UTC()
	stuckCandidate := now.Add(-time.Duration(stuckThresholdSeconds+5) * time.Second).Format(time.RFC3339)
	stale := now.Add(-10 * time.Minute).Format(time.RFC3339)
	fresh := now.Add(-1 * time.Second).Format(time.RFC3339)

	b.activity = map[string]agentActivitySnapshot{
		"stuck-rita": {
			Slug: "stuck-rita", Status: "active", Activity: "tool_use",
			Detail: "planning compute module", LastTime: stuckCandidate,
		},
		"reset-target": {
			Slug: "reset-target", Status: "thinking", Activity: "thinking",
			LastTime: stale,
		},
		"fresh-tess": {
			Slug: "fresh-tess", Status: "active", Activity: "tool_use",
			Detail: "running gh pr view", LastTime: fresh,
		},
	}

	b.mu.Lock()
	reset := b.reapStaleActivityLocked(now)
	b.mu.Unlock()

	// Expect exactly two emissions: stuck-rita (newly stuck) + reset-target
	// (stale-reset to idle). fresh-tess is well below both thresholds.
	if len(reset) != 2 {
		t.Fatalf("expected 2 emissions (1 stuck + 1 idle reset), got %d: %+v", len(reset), reset)
	}

	stuckSnap := b.activity["stuck-rita"]
	if stuckSnap.Kind != "stuck" {
		t.Errorf("stuck-rita Kind = %q, want stuck", stuckSnap.Kind)
	}
	if stuckSnap.Status != "active" {
		t.Errorf("stuck-rita Status = %q, want active (reaper must not change Status during stuck transition)", stuckSnap.Status)
	}
	if stuckSnap.Detail != "planning compute module" {
		t.Errorf("stuck-rita Detail = %q, want preserved original detail", stuckSnap.Detail)
	}

	resetSnap := b.activity["reset-target"]
	if resetSnap.Status != "idle" {
		t.Errorf("reset-target Status = %q, want idle (REGRESSION: existing reaper must still fire)", resetSnap.Status)
	}
	if resetSnap.Kind != "routine" {
		t.Errorf("reset-target Kind = %q, want routine (idle reset clears any prior stuck flag)", resetSnap.Kind)
	}

	freshSnap := b.activity["fresh-tess"]
	if freshSnap.Kind == "stuck" {
		t.Errorf("fresh-tess should not be stuck (well under threshold); got Kind=%q", freshSnap.Kind)
	}

	// Second tick must not re-emit stuck-rita (would spam SSE + repeatedly
	// trigger frontend assertive announcements). It should also not emit
	// fresh-tess (still fresh). reset-target is now Status=idle so it's
	// excluded from the active-set entirely.
	b.mu.Lock()
	resetAgain := b.reapStaleActivityLocked(now)
	b.mu.Unlock()
	if len(resetAgain) != 0 {
		t.Errorf("second reaper tick should be quiet for already-stuck agent, got %d emissions", len(resetAgain))
	}
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
	req, err := http.NewRequest(http.MethodGet, base+"/events?token="+b.Token(), nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("open event stream: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			t.Fatalf("expected 200 opening event stream, got %d (and body read failed: %v)", resp.StatusCode, readErr)
		}
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
