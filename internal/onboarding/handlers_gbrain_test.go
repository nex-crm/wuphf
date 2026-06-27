package onboarding

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

// TestEnsureGBrainBrainCallsEnsureWhenInstalled verifies the completion hook
// invokes gbrain.EnsureBrain (via the seam) when gbrain is installed.
func TestEnsureGBrainBrainCallsEnsureWhenInstalled(t *testing.T) {
	origInstalled, origEnsure := gbrainIsInstalled, gbrainEnsureBrain
	t.Cleanup(func() { gbrainIsInstalled, gbrainEnsureBrain = origInstalled, origEnsure })

	called := false
	gbrainIsInstalled = func() bool { return true }
	gbrainEnsureBrain = func(_ context.Context) (string, error) {
		called = true
		return "openai:text-embedding-3-large", nil
	}

	ensureGBrainBrain()

	if !called {
		t.Fatal("EnsureBrain seam was not invoked when gbrain installed")
	}
}

// TestEnsureGBrainBrainNoopWhenAbsent verifies the hook never calls EnsureBrain
// when gbrain is not installed — onboarding must not depend on gbrain.
func TestEnsureGBrainBrainNoopWhenAbsent(t *testing.T) {
	origInstalled, origEnsure := gbrainIsInstalled, gbrainEnsureBrain
	t.Cleanup(func() { gbrainIsInstalled, gbrainEnsureBrain = origInstalled, origEnsure })

	called := false
	gbrainIsInstalled = func() bool { return false }
	gbrainEnsureBrain = func(_ context.Context) (string, error) {
		called = true
		return "", nil
	}

	ensureGBrainBrain()

	if called {
		t.Fatal("EnsureBrain must not be invoked when gbrain absent")
	}
}

// TestHandleCompleteFiresEnsureBrainHook verifies the onboarding-complete HTTP
// path launches the brain-ensure hook after a successful completion, and that
// the response is unaffected (best-effort, non-blocking).
func TestHandleCompleteFiresEnsureBrainHook(t *testing.T) {
	withTempHome(t, func(home string) {
		t.Setenv("WUPHF_CONFIG_PATH", filepath.Join(home, ".wuphf", "config.json"))

		origHook := ensureBrainHook
		t.Cleanup(func() { ensureBrainHook = origHook })
		fired := make(chan struct{}, 1)
		ensureBrainHook = func() { fired <- struct{}{} }

		body := map[string]any{"task": "ship the thing", "skip_task": false}
		data, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, "/onboarding/complete", bytes.NewReader(data))
		w := httptest.NewRecorder()
		HandleComplete(w, req, nil)

		if w.Code != http.StatusOK {
			t.Fatalf("status: got %d, want 200\nbody: %s", w.Code, w.Body.String())
		}
		var resp map[string]any
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if resp["ok"] != true {
			t.Errorf("expected ok=true, got: %v", resp)
		}

		select {
		case <-fired:
		case <-time.After(2 * time.Second):
			t.Fatal("ensureBrainHook was not invoked after successful completion")
		}
	})
}

// TestHandleCompleteSucceedsAndPersistsWithHook guards the no-regression
// invariant: onboarding completes successfully and persists onboarded state
// while the best-effort brain hook fires. The hook is stubbed (signal + wait)
// so the goroutine is joined before the test ends — this keeps the test
// race-free and avoids invoking the real gbrain CLI as a side effect.
func TestHandleCompleteSucceedsAndPersistsWithHook(t *testing.T) {
	withTempHome(t, func(home string) {
		t.Setenv("WUPHF_CONFIG_PATH", filepath.Join(home, ".wuphf", "config.json"))

		origHook := ensureBrainHook
		t.Cleanup(func() { ensureBrainHook = origHook })
		done := make(chan struct{}, 1)
		ensureBrainHook = func() { done <- struct{}{} }

		body := map[string]any{"task": "", "skip_task": true}
		data, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, "/onboarding/complete", bytes.NewReader(data))
		w := httptest.NewRecorder()
		HandleComplete(w, req, nil)

		if w.Code != http.StatusOK {
			t.Fatalf("status: got %d, want 200\nbody: %s", w.Code, w.Body.String())
		}
		s, err := Load()
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if !s.Onboarded() {
			t.Error("state should be onboarded after HandleComplete")
		}

		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("ensureBrainHook was not invoked")
		}
	})
}
