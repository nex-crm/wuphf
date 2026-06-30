package team

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
