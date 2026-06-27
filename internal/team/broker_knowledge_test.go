package team

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nex-crm/wuphf/internal/gbrain"
)

// TestHandleKnowledgeEmbeddingOptionsShape drives the handler over httptest and
// asserts the JSON contract the onboarding UI depends on. It is deliberately
// env-tolerant: the gbrain-on-PATH / OpenAI-key / Ollama state varies by
// machine, so it asserts the field set, types, and the invariants that must hold
// regardless of environment (e.g. gbrain_installed reflects gbrain.IsInstalled()
// in this test process).
func TestHandleKnowledgeEmbeddingOptionsShape(t *testing.T) {
	b := newTestBroker(t)

	req := httptest.NewRequest(http.MethodGet, "/knowledge/embedding-options", nil)
	w := httptest.NewRecorder()
	b.handleKnowledgeEmbeddingOptions(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200\nbody: %s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type: got %q, want application/json", ct)
	}

	// Decode into a generic map first to assert every contract field is present
	// with the right JSON type.
	var raw map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode response: %v\nbody: %s", err, w.Body.String())
	}
	boolFields := []string{"gbrain_installed", "openai_key_set", "ollama_available", "embedding_available"}
	for _, f := range boolFields {
		v, ok := raw[f]
		if !ok {
			t.Fatalf("missing field %q in response: %s", f, w.Body.String())
		}
		if _, ok := v.(bool); !ok {
			t.Errorf("field %q: got %T, want bool", f, v)
		}
	}
	stringFields := []string{"ollama_model", "active_embedder", "recommended", "install_state", "install_progress", "install_error"}
	for _, f := range stringFields {
		v, ok := raw[f]
		if !ok {
			t.Fatalf("missing field %q in response: %s", f, w.Body.String())
		}
		if _, ok := v.(string); !ok {
			t.Errorf("field %q: got %T, want string", f, v)
		}
	}

	// Decode into the typed shape to assert cross-field invariants.
	var opts knowledgeEmbeddingOptions
	if err := json.Unmarshal(w.Body.Bytes(), &opts); err != nil {
		t.Fatalf("decode typed response: %v", err)
	}
	if opts.GBrainInstalled != gbrain.IsInstalled() {
		t.Errorf("gbrain_installed: got %v, want %v", opts.GBrainInstalled, gbrain.IsInstalled())
	}
	// A fresh broker has never installed: state is the normalized "idle" with
	// no progress or error.
	if opts.InstallState != "idle" {
		t.Errorf("install_state on fresh broker: got %q, want idle", opts.InstallState)
	}
	if opts.InstallProgress != "" {
		t.Errorf("install_progress on fresh broker: got %q, want empty", opts.InstallProgress)
	}
	if opts.InstallError != "" {
		t.Errorf("install_error on fresh broker: got %q, want empty", opts.InstallError)
	}
	// embedding_available must agree with active_embedder being non-empty.
	if opts.EmbeddingAvailable != (opts.ActiveEmbedder != "") {
		t.Errorf("embedding_available (%v) disagrees with active_embedder (%q)", opts.EmbeddingAvailable, opts.ActiveEmbedder)
	}
	// ollama_available must agree with ollama_model being non-empty.
	if opts.OllamaAvailable != (opts.OllamaModel != "") {
		t.Errorf("ollama_available (%v) disagrees with ollama_model (%q)", opts.OllamaAvailable, opts.OllamaModel)
	}
	// recommended is one of the documented values and matches the gate.
	switch opts.Recommended {
	case "openai", "keyword":
	default:
		t.Errorf("recommended: got %q, want one of openai|keyword", opts.Recommended)
	}
	if opts.GBrainInstalled && opts.Recommended != "openai" {
		t.Errorf("recommended must be openai when gbrain installed, got %q", opts.Recommended)
	}
	if !opts.GBrainInstalled && opts.Recommended != "keyword" {
		t.Errorf("recommended must be keyword when gbrain absent, got %q", opts.Recommended)
	}
}

// TestHandleKnowledgeEmbeddingOptionsRejectsNonGET asserts the handler follows
// the repo's method-guard pattern.
func TestHandleKnowledgeEmbeddingOptionsRejectsNonGET(t *testing.T) {
	b := newTestBroker(t)
	req := httptest.NewRequest(http.MethodPost, "/knowledge/embedding-options", nil)
	w := httptest.NewRecorder()
	b.handleKnowledgeEmbeddingOptions(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status: got %d, want 405", w.Code)
	}
}

// TestRecommendedEmbedder pins the documented recommendation logic.
func TestRecommendedEmbedder(t *testing.T) {
	if got := recommendedEmbedder(true); got != "openai" {
		t.Errorf("recommendedEmbedder(installed): got %q, want openai", got)
	}
	if got := recommendedEmbedder(false); got != "keyword" {
		t.Errorf("recommendedEmbedder(absent): got %q, want keyword", got)
	}
}

// readInstallState drives GET /knowledge/embedding-options over the handler and
// returns the install lifecycle fields. The UI polls this single endpoint for
// both capabilities and install progress.
func readInstallState(t *testing.T, b *Broker) (state, progress, errMsg string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/knowledge/embedding-options", nil)
	w := httptest.NewRecorder()
	b.handleKnowledgeEmbeddingOptions(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("embedding-options status: got %d, want 200", w.Code)
	}
	var opts knowledgeEmbeddingOptions
	if err := json.Unmarshal(w.Body.Bytes(), &opts); err != nil {
		t.Fatalf("decode embedding-options: %v", err)
	}
	return opts.InstallState, opts.InstallProgress, opts.InstallError
}

// pollInstallState polls the GET endpoint until the install state equals want or
// the deadline elapses. It returns the last observed progress and error lines.
func pollInstallState(t *testing.T, b *Broker, want string) (progress, errMsg string) {
	t.Helper()
	ok := testTickUntil(t, 2*time.Second, func() bool {
		state, _, _ := readInstallState(t, b)
		return state == want
	})
	state, p, e := readInstallState(t, b)
	if !ok {
		t.Fatalf("install_state never reached %q: last state=%q progress=%q error=%q", want, state, p, e)
	}
	return p, e
}

// postInstall drives POST /knowledge/install over the handler and returns the
// status code plus decoded install_state.
func postInstall(t *testing.T, b *Broker) (status int, state string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/knowledge/install", nil)
	w := httptest.NewRecorder()
	b.handleKnowledgeInstall(w, req)
	var resp knowledgeInstallResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode install response: %v\nbody: %s", err, w.Body.String())
	}
	return w.Code, resp.InstallState
}

// TestHandleKnowledgeInstall_Success seams the gbrain installer to record that
// it ran (streaming a progress line) and asserts the handler returns 202 and the
// background goroutine flips state to "installed" with EnsureBrain invoked.
func TestHandleKnowledgeInstall_Success(t *testing.T) {
	var installed, brained atomic.Bool

	origInstall, origBrain := gbrainEnsureInstalled, gbrainEnsureBrain
	t.Cleanup(func() { gbrainEnsureInstalled, gbrainEnsureBrain = origInstall, origBrain })

	gbrainEnsureInstalled = func(ctx context.Context, progress func(string)) error {
		installed.Store(true)
		if progress != nil {
			progress("Installing gbrain (latest)...")
		}
		return nil
	}
	gbrainEnsureBrain = func(ctx context.Context) (string, error) {
		brained.Store(true)
		return "openai:text-embedding-3-large", nil
	}

	b := newTestBroker(t)

	status, state := postInstall(t, b)
	if status != http.StatusAccepted {
		t.Fatalf("POST status: got %d, want 202", status)
	}
	if state != "installing" {
		t.Fatalf("POST install_state: got %q, want installing", state)
	}

	progress, errMsg := pollInstallState(t, b, "installed")
	if !installed.Load() {
		t.Error("EnsureInstalled was never invoked")
	}
	if !brained.Load() {
		t.Error("EnsureBrain was never invoked on success")
	}
	if progress != "Installing gbrain (latest)..." {
		t.Errorf("install_progress: got %q, want captured installer line", progress)
	}
	if errMsg != "" {
		t.Errorf("install_error: got %q, want empty on success", errMsg)
	}
}

// TestHandleKnowledgeInstall_Error seams the installer to fail and asserts the
// state becomes "error" with the message while the POST still returns 202.
func TestHandleKnowledgeInstall_Error(t *testing.T) {
	origInstall, origBrain := gbrainEnsureInstalled, gbrainEnsureBrain
	t.Cleanup(func() { gbrainEnsureInstalled, gbrainEnsureBrain = origInstall, origBrain })

	var brained atomic.Bool
	gbrainEnsureInstalled = func(ctx context.Context, progress func(string)) error {
		return errors.New("bun install gbrain: network unreachable")
	}
	gbrainEnsureBrain = func(ctx context.Context) (string, error) {
		brained.Store(true)
		return "", nil
	}

	b := newTestBroker(t)

	status, state := postInstall(t, b)
	if status != http.StatusAccepted {
		t.Fatalf("POST status: got %d, want 202", status)
	}
	if state != "installing" {
		t.Fatalf("POST install_state: got %q, want installing", state)
	}

	_, errMsg := pollInstallState(t, b, "error")
	if errMsg != "bun install gbrain: network unreachable" {
		t.Errorf("install_error: got %q, want installer message", errMsg)
	}
	if brained.Load() {
		t.Error("EnsureBrain must NOT run when EnsureInstalled failed")
	}
	// gbrain_installed reflects the real binary, not the seam; in the test
	// process gbrain is not actually installed by the seam.
	req := httptest.NewRequest(http.MethodGet, "/knowledge/embedding-options", nil)
	w := httptest.NewRecorder()
	b.handleKnowledgeEmbeddingOptions(w, req)
	var opts knowledgeEmbeddingOptions
	if err := json.Unmarshal(w.Body.Bytes(), &opts); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if opts.GBrainInstalled != gbrain.IsInstalled() {
		t.Errorf("gbrain_installed: got %v, want %v", opts.GBrainInstalled, gbrain.IsInstalled())
	}
}

// TestHandleKnowledgeInstall_SingleFlight uses a blocking seam to make the race
// deterministic: the first POST starts an install that blocks inside
// EnsureInstalled; a second POST while "installing" must be a no-op that returns
// the current state without invoking EnsureInstalled again.
func TestHandleKnowledgeInstall_SingleFlight(t *testing.T) {
	origInstall, origBrain := gbrainEnsureInstalled, gbrainEnsureBrain
	t.Cleanup(func() { gbrainEnsureInstalled, gbrainEnsureBrain = origInstall, origBrain })

	var calls atomic.Int32
	release := make(chan struct{})
	started := make(chan struct{})
	gbrainEnsureInstalled = func(ctx context.Context, progress func(string)) error {
		calls.Add(1)
		close(started)
		<-release // block until the test lets the install finish
		return nil
	}
	gbrainEnsureBrain = func(ctx context.Context) (string, error) { return "", nil }

	b := newTestBroker(t)

	// First POST kicks off the (blocked) install.
	status1, state1 := postInstall(t, b)
	if status1 != http.StatusAccepted || state1 != "installing" {
		t.Fatalf("first POST: got %d/%q, want 202/installing", status1, state1)
	}
	<-started // ensure the goroutine is inside EnsureInstalled

	// Second POST while installing: single-flight no-op, 200 + current state.
	status2, state2 := postInstall(t, b)
	if status2 != http.StatusOK {
		t.Fatalf("second POST status: got %d, want 200 (no-op)", status2)
	}
	if state2 != "installing" {
		t.Fatalf("second POST install_state: got %q, want installing", state2)
	}

	close(release) // let the single install finish
	pollInstallState(t, b, "installed")

	if got := calls.Load(); got != 1 {
		t.Errorf("EnsureInstalled invoked %d times, want exactly 1 (single-flight)", got)
	}
}
