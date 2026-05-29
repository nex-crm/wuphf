package team

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nex-crm/wuphf/internal/provider"
)

// TestCodexToolProgressDetail covers the D3/D4 detail phrasing: a
// visual-artifact tool must read "drafting figure" so the user (and the
// frontend skeleton) knows a figure is being built, while a generic tool
// reads "running <tool>".
func TestCodexToolProgressDetail(t *testing.T) {
	cases := []struct {
		name     string
		tool     string
		contains string
	}{
		{"visual artifact", "notebook_visual_artifact_create", "drafting figure"},
		{"visual artifact builds", "notebook_visual_artifact_create", "building visual artifact"},
		{"generic artifact", "rich_artifact_commit", "building artifact"},
		{"broadcast", "team_broadcast", "sharing update with the team"},
		{"generic tool", "team_post_message", "running team_post_message"},
		{"empty", "", "running tool"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := codexToolProgressDetail(tc.tool)
			if !strings.Contains(got, tc.contains) {
				t.Fatalf("codexToolProgressDetail(%q) = %q, want it to contain %q", tc.tool, got, tc.contains)
			}
		})
	}
}

// TestCodexProgressHeartbeatFiresOnSilence verifies the coarse heartbeat
// fires during genuine silence (point-4 fallback) so a long codex turn
// with no parseable item events is not a frozen UI.
func TestCodexProgressHeartbeatFiresOnSilence(t *testing.T) {
	old := codexHeartbeatIntervalForTest
	codexHeartbeatIntervalForTest = 20 * time.Millisecond
	defer func() { codexHeartbeatIntervalForTest = old }()

	var mu sync.Mutex
	var ticks int
	hb := newCodexProgressHeartbeat(func(elapsed time.Duration) {
		mu.Lock()
		ticks++
		mu.Unlock()
	})
	hb.Start(time.Now())
	// No Note() calls: simulate a silent stretch. Wait for a few intervals.
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := ticks
		mu.Unlock()
		if n >= 2 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	hb.Stop()

	mu.Lock()
	got := ticks
	mu.Unlock()
	if got < 2 {
		t.Fatalf("expected the heartbeat to fire at least twice during silence, got %d", got)
	}
}

// TestCodexProgressHeartbeatResetsOnNote verifies Note() suppresses the
// coarse heartbeat: while real events stream, the fine-grained per-event
// detail is the user's signal and the heartbeat must not fire over it.
func TestCodexProgressHeartbeatResetsOnNote(t *testing.T) {
	old := codexHeartbeatIntervalForTest
	codexHeartbeatIntervalForTest = 40 * time.Millisecond
	defer func() { codexHeartbeatIntervalForTest = old }()

	var mu sync.Mutex
	var ticks int
	hb := newCodexProgressHeartbeat(func(elapsed time.Duration) {
		mu.Lock()
		ticks++
		mu.Unlock()
	})
	hb.Start(time.Now())
	// Keep noting well inside the interval so the timer never elapses.
	stop := time.Now().Add(300 * time.Millisecond)
	for time.Now().Before(stop) {
		hb.Note()
		time.Sleep(10 * time.Millisecond)
	}
	hb.Stop()

	mu.Lock()
	got := ticks
	mu.Unlock()
	if got != 0 {
		t.Fatalf("expected no heartbeat ticks while events are flowing, got %d", got)
	}
}

// TestCodexStreamProgressMappingNoMinusOneMetrics is the end-to-end metric
// regression: feeding a realistic item.completed codex stream through the
// same parser the runner uses must produce a reasoning, tool_use, and text
// event so the runner stamps first_event_ms / first_tool_ms / first_text_ms
// (no longer -1). This exercises the exact onEvent the runner registers.
func TestCodexStreamProgressMappingNoMinusOneMetrics(t *testing.T) {
	stream := strings.Join([]string{
		`{"type":"item.started","item":{"id":"r1","type":"reasoning"}}`,
		`{"type":"item.completed","item":{"id":"t1","type":"function_call","name":"team_broadcast","arguments":"{}"}}`,
		`{"type":"item.completed","item":{"id":"m1","type":"agent_message","text":"Done."}}`,
	}, "\n")

	// Mirror the runner's metric-stamping logic against the parser output.
	startedAt := time.Now()
	var firstEvent, firstTool, firstText time.Time
	_, err := provider.ReadCodexJSONStream(strings.NewReader(stream), func(evt provider.CodexStreamEvent) {
		if firstEvent.IsZero() {
			firstEvent = time.Now()
		}
		switch evt.Type {
		case "tool_use":
			if firstTool.IsZero() {
				firstTool = time.Now()
			}
		case "text":
			if firstText.IsZero() && strings.TrimSpace(evt.Text) != "" {
				firstText = time.Now()
			}
		}
	})
	if err != nil {
		t.Fatalf("ReadCodexJSONStream: %v", err)
	}
	if durationMillis(startedAt, firstEvent) < 0 {
		t.Fatal("first_event_ms would be -1: parser emitted no events")
	}
	if durationMillis(startedAt, firstTool) < 0 {
		t.Fatal("first_tool_ms would be -1: parser emitted no tool_use event")
	}
	if durationMillis(startedAt, firstText) < 0 {
		t.Fatal("first_text_ms would be -1: parser emitted no text event for item.completed message")
	}
}
