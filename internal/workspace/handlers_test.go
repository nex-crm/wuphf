package workspace

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newMux wires the workspace routes onto a fresh ServeMux for each test so
// parallel runs don't share state. Passes a nil middleware so the test doesn't
// have to fake broker auth — RegisterRoutes substitutes a passthrough.
func newMux() *http.ServeMux {
	mux := http.NewServeMux()
	RegisterRoutes(mux, nil)
	return mux
}

func decodeBody(t *testing.T, body string) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("decode response: %v (body=%s)", err, body)
	}
	return out
}

func TestResetHandlerRejectsNonPost(t *testing.T) {
	withRuntimeHome(t)
	req := httptest.NewRequest(http.MethodGet, "/workspace/reset", nil)
	w := httptest.NewRecorder()
	newMux().ServeHTTP(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", w.Code)
	}
}

func TestResetHandlerClearsBrokerRuntimeOnly(t *testing.T) {
	dir := withRuntimeHome(t)
	paths := seedWorkspace(t, dir)

	req := httptest.NewRequest(http.MethodPost, "/workspace/reset", nil)
	w := httptest.NewRecorder()
	newMux().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	body := decodeBody(t, w.Body.String())
	if ok, _ := body["ok"].(bool); !ok {
		t.Fatalf("expected ok=true, got %v", body)
	}
	// Reset wipes broker runtime state...
	assertGone(t, "brokerState", paths["brokerState"])
	// ...but keeps everything else a Shred would have taken.
	for _, label := range []string{"onboarded", "company", "officeTasks", "workflow", "session", "worktree"} {
		assertStays(t, label, paths[label])
	}
}

func TestShredHandlerRejectsNonPost(t *testing.T) {
	withRuntimeHome(t)
	req := httptest.NewRequest(http.MethodGet, "/workspace/shred", nil)
	w := httptest.NewRecorder()
	newMux().ServeHTTP(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", w.Code)
	}
}

func TestShredHandlerWipesWorkspace(t *testing.T) {
	dir := withRuntimeHome(t)
	paths := seedWorkspace(t, dir)

	req := httptest.NewRequest(http.MethodPost, "/workspace/shred", nil)
	w := httptest.NewRecorder()
	newMux().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	body := decodeBody(t, w.Body.String())
	if ok, _ := body["ok"].(bool); !ok {
		t.Fatalf("expected ok=true, got %v", body)
	}
	// Redirect hint present so the web UI can navigate.
	if redirect, _ := body["redirect"].(string); redirect != "/" {
		t.Fatalf("expected redirect=/, got %q", redirect)
	}
	// Full wipe targets are all gone.
	for _, label := range []string{"onboarded", "company", "brokerState", "officeTasks", "workflow"} {
		assertGone(t, label, paths[label])
	}
	// Runtime history is wiped, but in-flight task worktrees remain on disk.
	assertGone(t, "session", paths["session"])
	assertStays(t, "worktree", paths["worktree"])
}

func TestShredHandlerReportsRemovedPaths(t *testing.T) {
	dir := withRuntimeHome(t)
	seedWorkspace(t, dir)

	req := httptest.NewRequest(http.MethodPost, "/workspace/shred", nil)
	w := httptest.NewRecorder()
	newMux().ServeHTTP(w, req)

	body := decodeBody(t, w.Body.String())
	removed, ok := body["removed"].([]any)
	if !ok {
		t.Fatalf("expected removed[] in response, got %v", body)
	}
	// The response should list concrete paths the user can audit. We don't
	// pin the exact count because internal path composition may shift, but
	// we do require that the wuphf home surfaces somewhere in the list.
	if len(removed) == 0 {
		t.Fatalf("expected non-empty removed list")
	}
	wantSubstring := filepath.Join(dir, ".wuphf")
	var found bool
	for _, entry := range removed {
		s, _ := entry.(string)
		if strings.HasPrefix(s, wantSubstring) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected at least one removed path under %q, got %v", wantSubstring, removed)
	}
}

func TestShredHandlerOnEmptyHomeIsOK(t *testing.T) {
	dir := withRuntimeHome(t)
	// Ensure the home exists but is completely empty.
	if err := os.MkdirAll(filepath.Join(dir, ".wuphf"), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/workspace/shred", nil)
	w := httptest.NewRecorder()
	newMux().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 on empty home, got %d", w.Code)
	}
}
