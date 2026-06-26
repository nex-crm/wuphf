package team

// Regression tests for killed-turn honesty (Wave F2, headless_turn_kill.go).
// The bug lived at the queue's turn-failure surface: a SIGKILLed provider
// process left raw `signal: killed` exhaust as the only trace (ICP-eval v3
// [19:05:30]). These tests pin (a) kill detection, (b) the humanized detail,
// and (c) the queue posting one human-readable system note through the real
// worker path.

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestIsTurnKilledError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"sigkill", errors.New("signal: killed"), true},
		{"wrapped sigkill", errors.New("headless turn: signal: killed: "), true},
		{"sigterm", errors.New("signal: terminated"), true},
		{"plain exit", errors.New("exit status 1"), false},
		{"timeout", context.DeadlineExceeded, false},
	}
	for _, tc := range cases {
		if got := isTurnKilledError(tc.err); got != tc.want {
			t.Errorf("%s: isTurnKilledError(%v) = %v, want %v", tc.name, tc.err, got, tc.want)
		}
	}
}

func TestTurnKilledHumanDetailHasNoRawExhaust(t *testing.T) {
	detail := turnKilledHumanDetail("eng")
	if !strings.Contains(detail, "@eng") {
		t.Fatalf("detail must name the agent: %q", detail)
	}
	for _, raw := range []string{"signal:", "exit status", "SIGKILL"} {
		if strings.Contains(detail, raw) {
			t.Fatalf("detail must be human-readable, found %q in %q", raw, detail)
		}
	}
}

// TestNoteChatTurnStallTasklessVsTask pins the gate: a chat/DM reply (no task)
// that fails gets one honest system note, while a task-attached turn stays
// silent here because its failure already surfaces via BlockTask + self-healing.
func TestNoteChatTurnStallTasklessVsTask(t *testing.T) {
	b := brokerWithTasks(t,
		teamTask{ID: "task-x", Title: "x", Owner: "eng", status: "in_progress", ExecutionMode: "office"},
	)
	l := newHeadlessLauncherForTest(t)
	l.broker = b

	// Task-attached turn: no chat note (the task path owns the surfacing).
	l.noteChatTurnStall("eng", headlessCodexTurn{TaskID: "task-x", Channel: "general"}, "the reply hit an error")
	for _, m := range b.AllMessages() {
		if m.From == "system" && strings.Contains(m.Content, "couldn't finish replying") {
			t.Fatalf("task-attached turn must not post a chat stall note: %q", m.Content)
		}
	}

	// Taskless chat reply (ceo has no active task): one honest note.
	l.noteChatTurnStall("ceo", headlessCodexTurn{Channel: "general"}, "the reply timed out after 4m0s")
	var note string
	for _, m := range b.AllMessages() {
		if m.From == "system" && strings.Contains(m.Content, "couldn't finish replying") {
			note = m.Content
			break
		}
	}
	if note == "" {
		t.Fatal("taskless chat reply must post a stall note")
	}
	if !strings.Contains(note, "@ceo") || !strings.Contains(note, "timed out") {
		t.Fatalf("note must name the agent and the reason: %q", note)
	}
}

// TestNoteChatTurnNoReplyGating pins the three-way gate on the silent-success
// note: it fires only for a human-prompted, taskless turn where the agent
// posted nothing, and stays silent for agent-to-agent turns, task turns, and
// turns where the agent already replied.
func TestNoteChatTurnNoReplyGating(t *testing.T) {
	b := brokerWithTasks(t,
		teamTask{ID: "task-x", Title: "x", Owner: "eng", status: "in_progress", ExecutionMode: "office"},
	)
	l := newHeadlessLauncherForTest(t)
	l.broker = b
	start := time.Now().Add(-time.Minute)

	noReplyNotes := func() int {
		n := 0
		for _, m := range b.AllMessages() {
			if m.From == "system" && strings.Contains(m.Content, "finished without posting a reply") {
				n++
			}
		}
		return n
	}

	// Not human-prompted: agent-to-agent turns may legitimately stay silent.
	l.noteChatTurnNoReply("ceo", headlessCodexTurn{Channel: "general"}, start)
	// Task-attached (even if human-prompted): the task path owns surfacing.
	l.noteChatTurnNoReply("eng", headlessCodexTurn{TaskID: "task-x", Channel: "general", FromHuman: true}, start)
	if got := noReplyNotes(); got != 0 {
		t.Fatalf("expected no notes for non-human / task turns, got %d", got)
	}

	// Human-prompted, taskless, silent: one honest note.
	l.noteChatTurnNoReply("ceo", headlessCodexTurn{Channel: "general", FromHuman: true}, start)
	if got := noReplyNotes(); got != 1 {
		t.Fatalf("expected exactly one no-reply note, got %d", got)
	}
	var note string
	for _, m := range b.AllMessages() {
		if m.From == "system" && strings.Contains(m.Content, "finished without posting a reply") {
			note = m.Content
		}
	}
	if !strings.Contains(note, "@ceo") {
		t.Fatalf("note must name the agent: %q", note)
	}

	// Human-prompted, taskless, but the agent already replied: no extra note.
	b.mu.Lock()
	b.counter++
	b.messages = append(b.messages, channelMessage{
		ID:        "seed-gtm",
		From:      "gtm",
		Channel:   "general",
		Content:   "here is my answer",
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	})
	b.mu.Unlock()
	l.noteChatTurnNoReply("gtm", headlessCodexTurn{Channel: "general", FromHuman: true}, start)
	for _, m := range b.AllMessages() {
		if m.From == "system" && strings.Contains(m.Content, "finished without posting a reply") && strings.Contains(m.Content, "@gtm") {
			t.Fatalf("no note expected when the agent already replied: %q", m.Content)
		}
	}
}

// TestHeadlessQueueHumanChatSilentSuccessPostsNote drives the real worker: a
// human-prompted chat turn that returns success but posts nothing must leave one
// honest line so a human DM never vanishes into silence.
func TestHeadlessQueueHumanChatSilentSuccessPostsNote(t *testing.T) {
	setHeadlessCodexRunTurnForTest(t, func(_ *Launcher, _ context.Context, _ string, _ string, _ ...string) error {
		return nil // success, but the agent posts nothing
	})

	l := newHeadlessLauncherForTest(t)
	b := NewBrokerAt(filepath.Join(t.TempDir(), "state.json"))
	l.installBroker(b)

	l.enqueueHeadlessCodexTurnRecord("ceo", headlessCodexTurn{
		Prompt:     "are you there?",
		Channel:    "general",
		FromHuman:  true,
		EnqueuedAt: time.Now(),
	})

	deadline := time.After(2 * time.Second)
	for {
		b.mu.Lock()
		var content string
		for i := range b.messages {
			if b.messages[i].From == "system" && strings.Contains(b.messages[i].Content, "finished without posting a reply") {
				content = b.messages[i].Content
				break
			}
		}
		b.mu.Unlock()
		if content != "" {
			if !strings.Contains(content, "@ceo") {
				t.Fatalf("silent-success note must name the agent: %q", content)
			}
			return
		}
		select {
		case <-deadline:
			t.Fatal("timed out waiting for the silent-success no-reply note")
		case <-time.After(10 * time.Millisecond):
		}
	}
}

// TestHeadlessQueueChatTimeoutPostsStallNote drives the real worker: a taskless
// chat turn that times out must leave one honest line in the channel instead of
// silence. Before the fix the timeout recovery early-returned (no task to
// block) and the user was left staring at a stalled agent that never replied.
func TestHeadlessQueueChatTimeoutPostsStallNote(t *testing.T) {
	setHeadlessCodexRunTurnForTest(t, func(_ *Launcher, _ context.Context, _ string, _ string, _ ...string) error {
		return context.DeadlineExceeded
	})

	l := newHeadlessLauncherForTest(t)
	b := NewBrokerAt(filepath.Join(t.TempDir(), "state.json"))
	l.installBroker(b)

	l.enqueueHeadlessCodexTurn("fe", "are you there?")

	deadline := time.After(2 * time.Second)
	for {
		b.mu.Lock()
		var content string
		for i := range b.messages {
			if b.messages[i].From == "system" && strings.Contains(b.messages[i].Content, "couldn't finish replying") {
				content = b.messages[i].Content
				break
			}
		}
		b.mu.Unlock()
		if content != "" {
			if !strings.Contains(content, "@fe") || !strings.Contains(content, "timed out") {
				t.Fatalf("timeout stall note must name the agent and reason: %q", content)
			}
			return
		}
		select {
		case <-deadline:
			t.Fatal("timed out waiting for the chat-timeout stall note")
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func TestHeadlessQueueKilledTurnPostsHumanReadableNote(t *testing.T) {
	setHeadlessCodexRunTurnForTest(t, func(_ *Launcher, _ context.Context, _ string, _ string, _ ...string) error {
		return errors.New("signal: killed")
	})

	l := newHeadlessLauncherForTest(t)
	b := NewBrokerAt(filepath.Join(t.TempDir(), "state.json"))
	l.installBroker(b)

	l.enqueueHeadlessCodexTurn("fe", "do the thing")

	deadline := time.After(2 * time.Second)
	for {
		b.mu.Lock()
		var note *channelMessage
		for i := range b.messages {
			if b.messages[i].From == "system" && strings.Contains(b.messages[i].Content, "killed by the system") {
				note = &b.messages[i]
				break
			}
		}
		var content string
		if note != nil {
			content = note.Content
		}
		b.mu.Unlock()
		if note != nil {
			if !strings.Contains(content, "@fe") {
				t.Fatalf("kill note must name the agent: %q", content)
			}
			if strings.Contains(content, "signal: killed") || strings.Contains(content, "exit status") {
				t.Fatalf("kill note must not carry raw exhaust: %q", content)
			}
			return
		}
		select {
		case <-deadline:
			t.Fatal("timed out waiting for the killed-turn system note")
		case <-time.After(10 * time.Millisecond):
		}
	}
}
