package team

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHandleExecuteApproveForwardsDecision(t *testing.T) {
	var buf bytes.Buffer
	activeRuns.add("run-1", &buf)
	defer activeRuns.remove("run-1")

	r := httptest.NewRequest(http.MethodPost, "/execute/approve", strings.NewReader(`{"run_id":"run-1","decision":"approve"}`))
	w := httptest.NewRecorder()
	(&Broker{}).handleExecuteApprove(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	if buf.String() != "approve\n" {
		t.Fatalf("forwarded stdin = %q, want approve", buf.String())
	}
}

func TestHandleExecuteApproveDenyAndUnknownRun(t *testing.T) {
	var buf bytes.Buffer
	activeRuns.add("run-2", &buf)
	defer activeRuns.remove("run-2")
	// Any non-approve decision forwards "deny" (never auto-send).
	r := httptest.NewRequest(http.MethodPost, "/execute/approve", strings.NewReader(`{"run_id":"run-2","decision":"whatever"}`))
	(&Broker{}).handleExecuteApprove(httptest.NewRecorder(), r)
	if buf.String() != "deny\n" {
		t.Fatalf("forwarded stdin = %q, want deny", buf.String())
	}
	// Unknown run → 404.
	r2 := httptest.NewRequest(http.MethodPost, "/execute/approve", strings.NewReader(`{"run_id":"missing","decision":"approve"}`))
	w2 := httptest.NewRecorder()
	(&Broker{}).handleExecuteApprove(w2, r2)
	if w2.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 for unknown run", w2.Code)
	}
}

func TestExecuteBrowserArgs(t *testing.T) {
	got := executeBrowserArgs("/r/cua.py", executeBrowserRequest{Goal: "open menu", App: "Google Chrome", WindowID: 42})
	want := []string{"/r/cua.py", "--goal", "open menu", "--app", "Google Chrome", "--window-id", "42"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("args = %v, want %v", got, want)
	}
	// No app / no window: only the goal is passed.
	got = executeBrowserArgs("/r/cua.py", executeBrowserRequest{Goal: "g"})
	if strings.Join(got, "|") != "/r/cua.py|--goal|g" {
		t.Fatalf("minimal args = %v", got)
	}
}

func TestDecodeExecuteBrowserRequest(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/execute/browser", strings.NewReader(`{"goal":"  do it  ","app":"Chrome"}`))
	req, err := decodeExecuteBrowserRequest(r)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if req.Goal != "do it" || req.App != "Chrome" {
		t.Fatalf("trim failed: %+v", req)
	}
	// Missing goal is rejected.
	r = httptest.NewRequest(http.MethodPost, "/execute/browser", strings.NewReader(`{"goal":"   "}`))
	if _, err := decodeExecuteBrowserRequest(r); err == nil {
		t.Fatal("expected error for empty goal")
	}
}

// TestHandleExecuteBrowserStreams drives the handler with a FAKE runner (a tiny
// shell script emitting JSON lines) and asserts each line is forwarded as an SSE
// data frame followed by the closing boundary. This is the end-to-end proxy
// contract the FE depends on, without needing OpenAI or cua-driver.
func TestHandleExecuteBrowserStreams(t *testing.T) {
	dir := t.TempDir()
	runner := filepath.Join(dir, "fake_runner.sh")
	script := "#!/bin/sh\n" +
		"echo '{\"type\":\"status\",\"status\":\"running\"}'\n" +
		"echo '{\"type\":\"action\",\"label\":\"Clicked Search\"}'\n" +
		"echo '{\"type\":\"done\",\"result\":\"ok\"}'\n"
	if err := os.WriteFile(runner, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WUPHF_OPENAI_API_KEY", "test-key")
	t.Setenv("WUPHF_CUA_PYTHON", "sh")
	t.Setenv("WUPHF_CUA_RUNNER", runner)

	r := httptest.NewRequest(http.MethodPost, "/execute/browser", strings.NewReader(`{"goal":"open the search"}`))
	w := httptest.NewRecorder()
	(&Broker{}).handleExecuteBrowser(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content-type = %q", ct)
	}
	body := w.Body.String()
	for _, want := range []string{
		`data: {"type":"status","status":"running"}`,
		`data: {"type":"action","label":"Clicked Search"}`,
		`data: {"type":"done","result":"ok"}`,
		"event: end",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q in stream:\n%s", want, body)
		}
	}
}

func TestHandleExecuteReplayStreams(t *testing.T) {
	dir := t.TempDir()
	runner := filepath.Join(dir, "fake_replay.sh")
	script := "#!/bin/sh\n" +
		"echo '{\"type\":\"status\",\"status\":\"replaying\"}'\n" +
		"echo '{\"type\":\"action\",\"label\":\"Click Search\",\"replayed\":true}'\n" +
		"echo '{\"type\":\"done\",\"result\":\"Replayed the workflow.\"}'\n"
	if err := os.WriteFile(runner, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WUPHF_OPENAI_API_KEY", "test-key")
	t.Setenv("WUPHF_CUA_PYTHON", "sh")
	t.Setenv("WUPHF_CUA_RUNNER", runner)

	body := `{"trajectory":{"goal":"g","app":"Google Chrome","steps":[{"action":"click","role":"Button","label":"Search"}]}}`
	r := httptest.NewRequest(http.MethodPost, "/execute/replay", strings.NewReader(body))
	w := httptest.NewRecorder()
	(&Broker{}).handleExecuteReplay(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	out := w.Body.String()
	for _, want := range []string{`"replayed":true`, "event: end"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in stream:\n%s", want, out)
		}
	}
}

func TestHandleExecuteReplayRejectsEmptyTrajectory(t *testing.T) {
	t.Setenv("WUPHF_OPENAI_API_KEY", "test-key")
	r := httptest.NewRequest(http.MethodPost, "/execute/replay", strings.NewReader(`{}`))
	w := httptest.NewRecorder()
	(&Broker{}).handleExecuteReplay(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for missing trajectory", w.Code)
	}
}

func TestHandleExecuteBrowser503WithoutKey(t *testing.T) {
	// Isolate the runtime home so config.Load() can't pick up a real key.
	t.Setenv("WUPHF_RUNTIME_HOME", t.TempDir())
	t.Setenv("WUPHF_OPENAI_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	r := httptest.NewRequest(http.MethodPost, "/execute/browser", strings.NewReader(`{"goal":"x"}`))
	w := httptest.NewRecorder()
	(&Broker{}).handleExecuteBrowser(w, r)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 (so FE falls back to mock)", w.Code)
	}
}
