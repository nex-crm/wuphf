package team

import (
	"os/exec"
	"testing"

	"github.com/nex-crm/wuphf/internal/onboarding"
)

// TestPreflightWebSkipsProviderCheckWhenNotOnboarded verifies that the web
// preflight does not fail on a missing claude/codex binary when the user
// has not yet completed onboarding. This is the whole reason `wuphf` should
// be launchable on a clean machine — the onboarding wizard is what installs
// and selects the runtime, so preflight cannot demand it first.
func TestPreflightWebSkipsProviderCheckWhenNotOnboarded(t *testing.T) {
	// Isolate onboarding state to a fresh per-test runtime home so we do
	// not touch the developer's real ~/.wuphf/onboarded.json.
	t.Setenv("WUPHF_RUNTIME_HOME", t.TempDir())

	l := &Launcher{provider: "claude-code"}

	if err := l.PreflightWeb(); err != nil {
		t.Fatalf("PreflightWeb on fresh install: got error %v, want nil", err)
	}
}

// TestPreflightWebRequiresProviderWhenOnboarded verifies that once the user
// has committed a runtime choice, the binary check is enforced again — this
// catches the "you picked claude-code but never installed it" case with a
// clear message instead of mysteriously erroring mid-turn.
func TestPreflightWebRequiresProviderWhenOnboarded(t *testing.T) {
	if _, err := exec.LookPath("claude"); err == nil {
		t.Skip("claude is on PATH; cannot test the missing-binary branch")
	}

	home := t.TempDir()
	t.Setenv("WUPHF_RUNTIME_HOME", home)

	// Mark onboarding complete. We go through Save so state is written in
	// the same format Load() expects.
	s, err := onboarding.Load()
	if err != nil {
		t.Fatalf("load onboarding state: %v", err)
	}
	s.CompletedAt = "2026-04-17T00:00:00Z"
	if err := onboarding.Save(s); err != nil {
		t.Fatalf("save onboarding state: %v", err)
	}

	l := &Launcher{provider: "claude-code"}
	err = l.PreflightWeb()
	if err == nil {
		t.Fatalf("PreflightWeb after onboarding with no claude: got nil, want error")
	}
}
