package team

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestHandleObserveBrowserStreams drives the endpoint with a FAKE observer (a
// shell script emitting snapshot/navigate lines) and asserts each is forwarded
// as an SSE data frame. No cua-driver or key needed.
func TestHandleObserveBrowserStreams(t *testing.T) {
	dir := t.TempDir()
	runner := filepath.Join(dir, "fake_observe.sh")
	script := "#!/bin/sh\n" +
		"echo '{\"type\":\"status\",\"status\":\"observing\"}'\n" +
		"echo '{\"type\":\"event\",\"type2\":\"navigate\",\"app\":\"Google Chrome\",\"title\":\"HubSpot\"}'\n" +
		"echo '{\"type\":\"snapshot\",\"app\":\"Google Chrome\",\"title\":\"HubSpot\",\"components\":[]}'\n"
	if err := os.WriteFile(runner, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WUPHF_CUA_PYTHON", "sh")
	t.Setenv("WUPHF_CUA_OBSERVE_RUNNER", runner)

	r := httptest.NewRequest(http.MethodPost, "/observe/browser", nil)
	w := httptest.NewRecorder()
	(&Broker{}).handleObserveBrowser(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content-type = %q", ct)
	}
	body := w.Body.String()
	for _, want := range []string{
		`data: {"type":"status","status":"observing"}`,
		`"app":"Google Chrome","title":"HubSpot"`,
		"event: end",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q in stream:\n%s", want, body)
		}
	}
}

func TestHandleObserveBrowser503WithoutRunner(t *testing.T) {
	// Point the override at a path that doesn't exist → no runner → 503.
	t.Setenv("WUPHF_CUA_OBSERVE_RUNNER", filepath.Join(t.TempDir(), "missing.py"))
	r := httptest.NewRequest(http.MethodPost, "/observe/browser", nil)
	w := httptest.NewRecorder()
	(&Broker{}).handleObserveBrowser(w, r)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", w.Code)
	}
}
