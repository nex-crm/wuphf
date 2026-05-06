package team

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestHandleAgentStreamEmitsReplayEndBoundary pins the contract that
// handleAgentStream sends a named `event: replay-end` SSE entry between
// the recent-history replay and the live subscription. The frontend
// uses this boundary to flip its `phase` ref from "replay" to "live"
// so terminal events (HeadlessEvent idle) emitted during the live
// window can close the EventSource — but replayed terminal events
// from the history buffer must not.
//
// We pre-seed the agent's stream buffer with a fake history line, hit
// the SSE handler, and assert the response body order is:
//
//	data: <history-line>
//	event: replay-end
//	data: {}
//	... (live messages or heartbeat)
//
// The exact heartbeat/live entries are out of scope — we only need to
// see the boundary in the right place.
func TestHandleAgentStreamEmitsReplayEndBoundary(t *testing.T) {
	b := newTestBroker(t)
	stream := b.AgentStream("ceo")

	// Pre-seed history. PushTask appends to both the global history and
	// the per-task slot; we use a task ID so the test exercises the
	// task-scoped subscribeTaskWithRecent branch (the most common path
	// in production today).
	stream.PushTask("task-1", `{"type":"system","subtype":"init"}`+"\n")

	srv := httptest.NewServer(b.requireAuth(b.handleAgentStream))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/agent-stream/ceo?task=task-1", nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+b.Token())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: want 200, got %d", resp.StatusCode)
	}

	// Read just enough of the stream to see history → boundary → first
	// live entry (or heartbeat). A small fixed-size buffer is enough;
	// we cancel the context after to unblock the server goroutine.
	buf := make([]byte, 4096)
	n, _ := resp.Body.Read(buf)
	got := string(buf[:n])
	cancel()

	historyIdx := strings.Index(got, `"type":"system"`)
	boundaryIdx := strings.Index(got, "event: replay-end\ndata: {}\n\n")
	if historyIdx < 0 {
		t.Fatalf("history line not seen in stream: %q", got)
	}
	if boundaryIdx < 0 {
		t.Fatalf("replay-end boundary not emitted: %q", got)
	}
	if boundaryIdx <= historyIdx {
		t.Fatalf("replay-end boundary must come after history (history=%d, boundary=%d): %q", historyIdx, boundaryIdx, got)
	}
}
