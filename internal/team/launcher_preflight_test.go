package team

import (
	"os"
	"testing"
)

// TestCheckGHCapabilityReportsMissingBinary pins the "gh not installed"
// branch. We blank PATH so exec.LookPath("gh") fails deterministically.
//
// The authed-vs-unauthed branches shell out to a real `gh` and depend on the
// host's credential store — they aren't deterministic in unit tests, so we
// don't pin them here. The regression we DO care about (false-positive
// "not authenticated" on every boot, #942 B-12) is structural: we no longer
// rely on `gh auth status` exit codes, and the bare `gh auth token` token
// check is exercised by hand-verification documented in the PR body.
func TestCheckGHCapabilityReportsMissingBinary(t *testing.T) {
	oldPath := os.Getenv("PATH")
	t.Cleanup(func() { _ = os.Setenv("PATH", oldPath) })
	if err := os.Setenv("PATH", ""); err != nil {
		t.Fatalf("set PATH: %v", err)
	}
	installed, authed, note := checkGHCapability()
	if installed {
		t.Fatalf("expected installed=false with empty PATH; got true")
	}
	if authed {
		t.Fatalf("expected authed=false with empty PATH; got true")
	}
	if note == "" {
		t.Fatalf("expected an install advisory note; got empty string")
	}
}
