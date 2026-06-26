package onboarding

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestVerifyRuntimeNotInstalled covers the not_installed branch with a bogus
// runtime name that cannot resolve on any PATH.
func TestVerifyRuntimeNotInstalled(t *testing.T) {
	res := VerifyRuntime(context.Background(), "nonexistent-runtime-xyz-wuphf")
	if res.Status != VerifyStatusNotInstalled {
		t.Fatalf("Status: got %q, want %q", res.Status, VerifyStatusNotInstalled)
	}
	if res.Runtime != "nonexistent-runtime-xyz-wuphf" {
		t.Errorf("Runtime: got %q, want bogus name echoed back", res.Runtime)
	}
	if res.FailedStep == "" {
		t.Error("FailedStep should be set for not_installed")
	}
	if res.Hint == "" {
		t.Error("Hint should be set for not_installed")
	}
	if res.Version != "" {
		t.Errorf("Version should be empty for a missing binary, got %q", res.Version)
	}
}

// TestVerifyRuntimeKnownCLINeverPanics walks every CLI runtime in prereqSpecs
// and asserts the classification stays inside the honest status set. Whether a
// runtime is installed/signed-in on the test machine is environment-dependent,
// so we assert the invariant (a valid status + no panic) rather than a fixed
// outcome.
func TestVerifyRuntimeKnownCLINeverPanics(t *testing.T) {
	valid := map[VerifyStatus]bool{
		VerifyStatusPass:         true,
		VerifyStatusNotInstalled: true,
		VerifyStatusAuthRequired: true,
		VerifyStatusOtherError:   true,
	}
	for _, name := range []string{"claude", "codex", "opencode", "cursor", "windsurf"} {
		res := VerifyRuntime(context.Background(), name)
		if !valid[res.Status] {
			t.Errorf("%s: classified as unexpected status %q", name, res.Status)
		}
		if res.Runtime != name {
			t.Errorf("%s: Runtime echoed as %q", name, res.Runtime)
		}
		switch res.Status {
		case VerifyStatusPass:
			if res.Hint != "" {
				t.Errorf("%s: pass should carry no hint, got %q", name, res.Hint)
			}
			if res.FailedStep != "" {
				t.Errorf("%s: pass should carry no failed step, got %q", name, res.FailedStep)
			}
		case VerifyStatusAuthRequired:
			if res.SignInCommand == "" {
				t.Errorf("%s: auth_required should carry a sign-in command", name)
			}
			if res.Command != res.SignInCommand {
				t.Errorf("%s: auth_required Command (%q) should equal SignInCommand (%q)", name, res.Command, res.SignInCommand)
			}
			if res.FailedStep == "" {
				t.Errorf("%s: auth_required should name the failed step", name)
			}
		case VerifyStatusNotInstalled:
			if res.FailedStep == "" {
				t.Errorf("%s: not_installed should name the failed step", name)
			}
		}
	}
}

// TestInstallStepsForEachCLIRuntime asserts every pickable CLI runtime gets a
// non-empty guided setup whose final step is Verify, and that node/git (the
// prerequisites) and unknown names behave per contract.
func TestInstallStepsForEachCLIRuntime(t *testing.T) {
	tests := []struct {
		name        string
		runtime     string
		wantSteps   bool
		wantVerify  bool
		wantNil     bool
		wantLinkURL string
	}{
		{name: "claude", runtime: "claude", wantSteps: true, wantVerify: true, wantLinkURL: "https://claude.ai/code"},
		{name: "codex", runtime: "codex", wantSteps: true, wantVerify: true, wantLinkURL: "https://github.com/openai/codex"},
		{name: "opencode", runtime: "opencode", wantSteps: true, wantVerify: true, wantLinkURL: "https://opencode.ai"},
		{name: "cursor", runtime: "cursor", wantSteps: true, wantVerify: true, wantLinkURL: "https://cursor.com/"},
		{name: "windsurf", runtime: "windsurf", wantSteps: true, wantVerify: true, wantLinkURL: "https://codeium.com/windsurf"},
		{name: "node prerequisite", runtime: "node", wantSteps: true, wantVerify: false, wantLinkURL: "https://nodejs.org"},
		{name: "git prerequisite", runtime: "git", wantSteps: true, wantVerify: false, wantLinkURL: "https://git-scm.com"},
		{name: "unknown name", runtime: "totally-unknown", wantNil: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			steps := InstallSteps(tt.runtime)
			if tt.wantNil {
				if steps != nil {
					t.Fatalf("InstallSteps(%q): want nil, got %v", tt.runtime, steps)
				}
				return
			}
			if tt.wantSteps && len(steps) == 0 {
				t.Fatalf("InstallSteps(%q): want non-empty steps", tt.runtime)
			}
			for i, s := range steps {
				if s.Title == "" {
					t.Errorf("InstallSteps(%q)[%d]: empty Title", tt.runtime, i)
				}
			}
			if tt.wantVerify {
				last := steps[len(steps)-1]
				if last.Title != "Verify" {
					t.Errorf("InstallSteps(%q): last step Title = %q, want %q", tt.runtime, last.Title, "Verify")
				}
			}
			if tt.wantLinkURL != "" {
				found := false
				for _, s := range steps {
					if s.LinkURL == tt.wantLinkURL {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("InstallSteps(%q): no step linked to %q", tt.runtime, tt.wantLinkURL)
				}
			}
		})
	}
}

// TestHandleVerifyClassifiesBogusRuntime exercises the HTTP handler end to end
// for a runtime that cannot exist, asserting a 200 with not_installed.
func TestHandleVerifyClassifiesBogusRuntime(t *testing.T) {
	tests := []struct {
		name string
		// body and query feed the two accepted input forms.
		body  string
		query string
	}{
		{name: "json body", body: `{"runtime":"nonexistent-runtime-xyz-wuphf"}`},
		{name: "query param", query: "runtime=nonexistent-runtime-xyz-wuphf"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url := "/onboarding/verify"
			if tt.query != "" {
				url += "?" + tt.query
			}
			var req *http.Request
			if tt.body != "" {
				req = httptest.NewRequest(http.MethodPost, url, strings.NewReader(tt.body))
			} else {
				req = httptest.NewRequest(http.MethodPost, url, nil)
			}
			w := httptest.NewRecorder()
			HandleVerify(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("status: got %d, want %d body=%s", w.Code, http.StatusOK, w.Body.String())
			}
			var res VerifyResult
			if err := json.NewDecoder(w.Body).Decode(&res); err != nil {
				t.Fatalf("response is not valid VerifyResult JSON: %v\nbody: %s", err, w.Body.String())
			}
			if res.Status != VerifyStatusNotInstalled {
				t.Errorf("Status: got %q, want %q", res.Status, VerifyStatusNotInstalled)
			}
		})
	}
}

// TestHandleVerifyRejectsMissingRuntime asserts a 400 when no runtime is given.
func TestHandleVerifyRejectsMissingRuntime(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/onboarding/verify", strings.NewReader(`{}`))
	w := httptest.NewRecorder()
	HandleVerify(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want %d", w.Code, http.StatusBadRequest)
	}
}

// TestHandleVerifyMethodNotAllowed asserts GET is rejected.
func TestHandleVerifyMethodNotAllowed(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/onboarding/verify", nil)
	w := httptest.NewRecorder()
	HandleVerify(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status: got %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

// TestHandleInstallStepsReturnsSteps asserts the GET endpoint returns the
// guided steps for a known runtime and rejects a missing runtime param.
func TestHandleInstallStepsReturnsSteps(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/onboarding/install-steps?runtime=claude", nil)
	w := httptest.NewRecorder()
	HandleInstallSteps(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	var out struct {
		Runtime string        `json:"runtime"`
		Steps   []InstallStep `json:"steps"`
	}
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("invalid JSON: %v\nbody: %s", err, w.Body.String())
	}
	if out.Runtime != "claude" {
		t.Errorf("Runtime: got %q, want %q", out.Runtime, "claude")
	}
	if len(out.Steps) == 0 {
		t.Error("Steps should be non-empty for claude")
	}
}

func TestHandleInstallStepsRejectsMissingRuntime(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/onboarding/install-steps", nil)
	w := httptest.NewRecorder()
	HandleInstallSteps(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleInstallStepsMethodNotAllowed(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/onboarding/install-steps?runtime=claude", nil)
	w := httptest.NewRecorder()
	HandleInstallSteps(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status: got %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}
