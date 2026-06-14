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
