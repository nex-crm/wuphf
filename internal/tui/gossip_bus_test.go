package tui

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"
)

// --- Test helpers ---

type mockTarget struct {
	slug     string
	mu       sync.Mutex
	received []string
}

func newMockTarget(slug string) *mockTarget {
	return &mockTarget{slug: slug}
}

func (m *mockTarget) SendText(text string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.received = append(m.received, text)
}

func (m *mockTarget) Slug() string {
	return m.slug
}

func (m *mockTarget) messages() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, len(m.received))
	copy(out, m.received)
	return out
}

func (m *mockTarget) allText() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return strings.Join(m.received, "")
}

// fakeClock returns a controllable now function and an advance helper.
func fakeClock(start time.Time) (func() time.Time, func(d time.Duration)) {
	mu := sync.Mutex{}
	cur := start
	return func() time.Time {
			mu.Lock()
			defer mu.Unlock()
			return cur
		}, func(d time.Duration) {
			mu.Lock()
			defer mu.Unlock()
			cur = cur.Add(d)
		}
}

// --- Tests ---

func TestGossipBroadcast(t *testing.T) {
	bus := NewGossipBus("ceo")
	nowFn, advance := fakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	bus.now = nowFn

	a := newMockTarget("ceo")
	b := newMockTarget("fe")
	c := newMockTarget("be")
	bus.RegisterTarget(a)
	bus.RegisterTarget(b)
	bus.RegisterTarget(c)

	// Advance past any floor hold so events aren't blocked
	advance(10 * time.Second)

	bus.Emit(GossipEvent{
		FromSlug: "ceo",
		Type:     "text",
		Content:  "Let's build the landing page",
	})

	// ceo should NOT receive its own message
	if len(a.messages()) > 0 {
		t.Errorf("emitter received own message: %v", a.messages())
	}

	// fe and be should receive it
	feText := b.allText()
	beText := c.allText()

	if !strings.Contains(feText, "[TEAM @ceo]") {
		t.Errorf("fe did not receive ceo message, got: %q", feText)
	}
	if !strings.Contains(beText, "[TEAM @ceo]") {
		t.Errorf("be did not receive ceo message, got: %q", beText)
	}
	if !strings.Contains(feText, "Let's build the landing page") {
		t.Errorf("fe message missing content, got: %q", feText)
	}
}

func TestGossipThrottle(t *testing.T) {
	bus := NewGossipBus("ceo")
	nowFn, advance := fakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	bus.now = nowFn

	a := newMockTarget("agent-a")
	b := newMockTarget("agent-b")
	bus.RegisterTarget(a)
	bus.RegisterTarget(b)

	// Advance past any initial state
	advance(10 * time.Second)

	// Emit several events rapidly from agent-a (within throttle window)
	bus.Emit(GossipEvent{FromSlug: "agent-a", Type: "text", Content: "msg1"})

	// Advance only 1 second (within 3s throttle)
	advance(1 * time.Second)
	bus.Emit(GossipEvent{FromSlug: "agent-a", Type: "text", Content: "msg2"})

	advance(1 * time.Second)
	bus.Emit(GossipEvent{FromSlug: "agent-a", Type: "text", Content: "msg3"})

	// Only first message should have been injected; rest should be batched
	bMessages := b.messages()
	if len(bMessages) != 1 {
		t.Errorf("expected 1 injection (throttled), got %d: %v", len(bMessages), bMessages)
	}

	// Advance past throttle and flush
	advance(5 * time.Second)
	bus.FlushPending()

	bMessages = b.messages()
	if len(bMessages) != 2 {
		t.Errorf("expected 2 total injections after flush, got %d", len(bMessages))
	}

	// Second injection should contain the batched messages
	if len(bMessages) >= 2 && !strings.Contains(bMessages[1], "msg2") {
		t.Errorf("flushed batch missing msg2, got: %q", bMessages[1])
	}
}

func TestTurnTakingLeaderFirst(t *testing.T) {
	bus := NewGossipBus("ceo")
	nowFn, advance := fakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	bus.now = nowFn

	ceo := newMockTarget("ceo")
	fe := newMockTarget("fe")
	be := newMockTarget("be")
	bus.RegisterTarget(ceo)
	bus.RegisterTarget(fe)
	bus.RegisterTarget(be)

	// User broadcasts — leader gets 5s floor
	bus.BroadcastUserMessage("Build a landing page")

	// Within leader's floor window, non-leader tries to speak
	advance(2 * time.Second)
	bus.Emit(GossipEvent{FromSlug: "fe", Type: "text", Content: "I'll do the CSS"})

	// fe's message should be batched (leader has floor), not injected to ceo
	ceoText := ceo.allText()
	if strings.Contains(ceoText, "I'll do the CSS") {
		t.Error("non-leader message was injected while leader holds floor")
	}

	// Leader speaks — should go through
	advance(1 * time.Second)
	bus.Emit(GossipEvent{FromSlug: "ceo", Type: "text", Content: "Start with the hero section"})

	feText := fe.allText()
	if !strings.Contains(feText, "Start with the hero section") {
		t.Errorf("leader message not delivered to fe, got: %q", feText)
	}

	// After leader speaks, floor should open — flush pending
	advance(4 * time.Second) // past throttle
	bus.FlushPending()

	// Now fe's batched message should be delivered
	ceoText = ceo.allText()
	if !strings.Contains(ceoText, "I'll do the CSS") {
		t.Errorf("batched fe message not flushed to ceo after floor release, got: %q", ceoText)
	}
}

func TestTurnTakingCooldown(t *testing.T) {
	bus := NewGossipBus("ceo")
	nowFn, advance := fakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	bus.now = nowFn

	a := newMockTarget("agent-a")
	b := newMockTarget("agent-b")
	bus.RegisterTarget(a)
	bus.RegisterTarget(b)

	// Clear any floor
	advance(10 * time.Second)

	// agent-a speaks (text triggers cooldown)
	bus.Emit(GossipEvent{FromSlug: "agent-a", Type: "text", Content: "first message"})
	firstCount := len(b.messages())

	// agent-a tries to speak again within cooldown (3s)
	advance(1 * time.Second)
	bus.Emit(GossipEvent{FromSlug: "agent-a", Type: "text", Content: "second message"})

	// Second message should be batched due to cooldown
	if len(b.messages()) != firstCount {
		t.Errorf("message delivered during cooldown period")
	}

	// Advance past cooldown + throttle and flush
	advance(5 * time.Second)
	bus.FlushPending()

	if len(b.messages()) <= firstCount {
		t.Errorf("batched message not delivered after cooldown expired")
	}
}

func TestObserverParsesNDJSON(t *testing.T) {
	bus := NewGossipBus("ceo")
	nowFn, _ := fakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	bus.now = nowFn

	// We don't register targets — just check eventLog
	// Build NDJSON input
	lines := []claudeStreamEvent{
		{Type: "system"}, // should be skipped
		{
			Type: "assistant",
			Message: &claudeEventMessage{
				Content: []claudeEventBlock{
					{Type: "thinking", Thinking: "Let me analyze the requirements"},
				},
			},
		},
		{
			Type: "assistant",
			Message: &claudeEventMessage{
				Content: []claudeEventBlock{
					{Type: "text", Text: "I'll create the component now"},
				},
			},
		},
		{
			Type: "assistant",
			Message: &claudeEventMessage{
				Content: []claudeEventBlock{
					{Type: "tool_use", Name: "Bash", Input: map[string]string{"command": "mkdir -p src"}},
				},
			},
		},
		{
			Type: "assistant",
			Message: &claudeEventMessage{
				Content: []claudeEventBlock{
					{Type: "tool_result", Content: "directory created"},
				},
			},
		},
	}

	var ndjson strings.Builder
	for _, l := range lines {
		raw, _ := json.Marshal(l)
		ndjson.Write(raw)
		ndjson.WriteByte('\n')
	}

	reader := strings.NewReader(ndjson.String())
	obs := NewOutputObserver("fe", bus, reader)

	// Run synchronously (not Start, which uses goroutine)
	obs.run()

	log := bus.EventLog()

	if len(log) != 4 {
		t.Fatalf("expected 4 events (system skipped), got %d", len(log))
	}

	expected := []struct {
		typ     string
		content string
	}{
		{"thinking", "Let me analyze the requirements"},
		{"text", "I'll create the component now"},
		{"tool_use", "Bash"},
		{"tool_result", "directory created"},
	}

	for i, want := range expected {
		got := log[i]
		if got.Type != want.typ {
			t.Errorf("event[%d] type = %q, want %q", i, got.Type, want.typ)
		}
		if got.FromSlug != "fe" {
			t.Errorf("event[%d] slug = %q, want 'fe'", i, got.FromSlug)
		}
		if !strings.Contains(got.Content, want.content) {
			t.Errorf("event[%d] content = %q, want substring %q", i, got.Content, want.content)
		}
	}
}

func TestFormatEvent(t *testing.T) {
	cases := []struct {
		event GossipEvent
		want  string
	}{
		{
			GossipEvent{FromSlug: "ceo", Type: "text", Content: "Hello team"},
			"[TEAM @ceo]: Hello team",
		},
		{
			GossipEvent{FromSlug: "fe", Type: "tool_use", Content: "Bash mkdir"},
			"[TEAM @fe is coding]: Bash mkdir",
		},
		{
			GossipEvent{FromSlug: "be", Type: "tool_result", Content: "OK"},
			"[TEAM @be tool done]: OK",
		},
		{
			GossipEvent{FromSlug: "ceo", Type: "thinking", Content: "hmm"},
			"[TEAM @ceo thinking]: hmm",
		},
	}

	for _, tc := range cases {
		got := formatEvent(tc.event)
		if got != tc.want {
			t.Errorf("formatEvent(%v) = %q, want %q", tc.event, got, tc.want)
		}
	}
}

func TestTruncate(t *testing.T) {
	short := "hello"
	if truncate(short, 10) != "hello" {
		t.Error("short string should not be truncated")
	}

	long := strings.Repeat("a", 350)
	result := truncate(long, maxTextLen)
	if len(result) != maxTextLen+3 { // +3 for "..."
		t.Errorf("truncated length = %d, want %d", len(result), maxTextLen+3)
	}
	if !strings.HasSuffix(result, "...") {
		t.Error("truncated string should end with ...")
	}
}

func TestGetActivity(t *testing.T) {
	bus := NewGossipBus("ceo")
	nowFn, advance := fakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	bus.now = nowFn

	// No events yet — should return "idle".
	if got := bus.GetActivity("ceo"); got != "idle" {
		t.Errorf("GetActivity with no events = %q, want 'idle'", got)
	}

	// Emit some events.
	advance(10 * time.Second)
	bus.Emit(GossipEvent{FromSlug: "ceo", Type: "thinking", Content: "hmm"})
	advance(1 * time.Second)
	bus.Emit(GossipEvent{FromSlug: "ceo", Type: "text", Content: "let's go"})
	advance(1 * time.Second)
	bus.Emit(GossipEvent{FromSlug: "fe", Type: "tool_use", Content: "Bash"})

	// Latest ceo activity should be "text".
	if got := bus.GetActivity("ceo"); got != "text" {
		t.Errorf("GetActivity('ceo') = %q, want 'text'", got)
	}

	// Latest fe activity should be "tool_use".
	if got := bus.GetActivity("fe"); got != "tool_use" {
		t.Errorf("GetActivity('fe') = %q, want 'tool_use'", got)
	}

	// Unknown agent should return "idle".
	if got := bus.GetActivity("unknown"); got != "idle" {
		t.Errorf("GetActivity('unknown') = %q, want 'idle'", got)
	}
}
