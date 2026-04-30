package team

// Tests for the C5c migration: write-side / destructive paneLifecycle
// methods (ClearAgentPanes, ClearOverflowAgentWindows, KillSession,
// RespawnAgentPane, RespawnChannelPane, CaptureDeadChannelPane). All
// driven through fakeTmuxRunner via setTmuxRunnerForTest — no real
// tmux server required, no time.Sleep in any of them.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPaneLifecycle_ClearAgentPanesKillsHigherIndicesFirst(t *testing.T) {
	// Reverse-order kill matters: tmux renumbers panes after a kill, so
	// killing pane 1 before pane 3 leaves the original pane 3 sitting at
	// index 2 — and the next kill-pane targets the wrong process.
	// ClearAgentPanes sorts descending to avoid that. This test pins the
	// invariant.
	fake := newFakeTmuxRunner()
	fake.outputs["list-panes"] = []byte("0 channel\n1 ceo\n2 fe\n3 be\n")
	setTmuxRunnerForTest(t, fake)

	if err := newPaneLifecycle("wuphf-team").ClearAgentPanes(); err != nil {
		t.Fatalf("ClearAgentPanes err = %v", err)
	}

	kills := fake.callsFor("kill-pane")
	if len(kills) != 3 {
		t.Fatalf("kill-pane calls = %d, want 3 (indices 1,2,3)", len(kills))
	}
	wantTargets := []string{"wuphf-team:team.3", "wuphf-team:team.2", "wuphf-team:team.1"}
	for i, call := range kills {
		// Each call's args are exactly: "kill-pane", "-t", target.
		if len(call) != 3 || call[2] != wantTargets[i] {
			t.Errorf("kill-pane[%d] = %v, want target=%s (descending order)", i, call, wantTargets[i])
		}
	}
}

func TestPaneLifecycle_ClearAgentPanesNoSessionIsNoOp(t *testing.T) {
	fake := newFakeTmuxRunner()
	fake.outputs["list-panes"] = []byte("no server running")
	fake.errors["list-panes"] = fmt.Errorf("exit 1")
	setTmuxRunnerForTest(t, fake)

	if err := newPaneLifecycle("wuphf-team").ClearAgentPanes(); err != nil {
		t.Fatalf("ClearAgentPanes(no session) err = %v, want nil", err)
	}
	if got := fake.callsFor("kill-pane"); len(got) != 0 {
		t.Fatalf("expected zero kill-pane calls when session missing, got %d", len(got))
	}
}

func TestPaneLifecycle_ClearOverflowAgentWindowsFiltersByPrefix(t *testing.T) {
	fake := newFakeTmuxRunner()
	fake.outputs["list-windows"] = []byte("team\nagent-fe\nlogs\nagent-be\n")
	setTmuxRunnerForTest(t, fake)

	newPaneLifecycle("wuphf-team").ClearOverflowAgentWindows()

	kills := fake.callsFor("kill-window")
	if len(kills) != 2 {
		t.Fatalf("kill-window calls = %d, want 2 (agent-fe, agent-be)", len(kills))
	}
	wantTargets := []string{"wuphf-team:agent-fe", "wuphf-team:agent-be"}
	for i, call := range kills {
		if len(call) < 3 || call[2] != wantTargets[i] {
			t.Errorf("kill-window[%d] = %v, want %s", i, call, wantTargets[i])
		}
	}
}

func TestPaneLifecycle_ClearOverflowAgentWindowsListErrorIsNoOp(t *testing.T) {
	fake := newFakeTmuxRunner()
	fake.errors["list-windows"] = fmt.Errorf("no server")
	setTmuxRunnerForTest(t, fake)

	newPaneLifecycle("wuphf-team").ClearOverflowAgentWindows()

	if got := fake.callsFor("kill-window"); len(got) != 0 {
		t.Fatalf("expected zero kill-window calls on list error, got %d", len(got))
	}
}

func TestPaneLifecycle_KillSessionIssuesKillSession(t *testing.T) {
	fake := newFakeTmuxRunner()
	setTmuxRunnerForTest(t, fake)

	newPaneLifecycle("wuphf-team").KillSession()

	calls := fake.callsFor("kill-session")
	if len(calls) != 1 {
		t.Fatalf("kill-session calls = %d, want 1", len(calls))
	}
	want := []string{"kill-session", "-t", "wuphf-team"}
	if !equalStrings(calls[0], want) {
		t.Errorf("kill-session args = %v, want %v", calls[0], want)
	}
}

func TestPaneLifecycle_RespawnAgentPaneSurfacesTmuxOutput(t *testing.T) {
	fake := newFakeTmuxRunner()
	fake.outputs["respawn-pane"] = []byte("tmux: pane locked\n")
	fake.errors["respawn-pane"] = fmt.Errorf("exit 1")
	setTmuxRunnerForTest(t, fake)

	out, err := newPaneLifecycle("wuphf-team").RespawnAgentPane(2, "/tmp/cwd", "claude --print")
	if err == nil {
		t.Fatalf("RespawnAgentPane err = nil, want non-nil")
	}
	if string(out) != "tmux: pane locked\n" {
		t.Errorf("RespawnAgentPane out = %q, want tmux stderr text", out)
	}
	calls := fake.callsFor("respawn-pane")
	if len(calls) != 1 {
		t.Fatalf("respawn-pane calls = %d, want 1", len(calls))
	}
	want := []string{"respawn-pane", "-k", "-t", "wuphf-team:team.2", "-c", "/tmp/cwd", "claude --print"}
	if !equalStrings(calls[0], want) {
		t.Errorf("respawn-pane args = %v, want %v", calls[0], want)
	}
}

func TestPaneLifecycle_RespawnChannelPaneIssuesBothCommands(t *testing.T) {
	fake := newFakeTmuxRunner()
	setTmuxRunnerForTest(t, fake)

	newPaneLifecycle("wuphf-team").RespawnChannelPane("wuphf channel", "/tmp/cwd")

	respawn := fake.callsFor("respawn-pane")
	if len(respawn) != 1 {
		t.Fatalf("respawn-pane calls = %d, want 1", len(respawn))
	}
	wantRespawn := []string{"respawn-pane", "-k", "-t", "wuphf-team:team.0", "-c", "/tmp/cwd", "wuphf channel"}
	if !equalStrings(respawn[0], wantRespawn) {
		t.Errorf("respawn-pane args = %v, want %v", respawn[0], wantRespawn)
	}

	selectPane := fake.callsFor("select-pane")
	if len(selectPane) != 1 {
		t.Fatalf("select-pane calls = %d, want 1", len(selectPane))
	}
	// Just confirm the title and target are right; the emoji string is
	// brittle so we don't pin its exact UTF-8 bytes.
	if selectPane[0][2] != "wuphf-team:team.0" {
		t.Errorf("select-pane target = %q, want wuphf-team:team.0", selectPane[0][2])
	}
	if !strings.Contains(selectPane[0][len(selectPane[0])-1], "channel") {
		t.Errorf("select-pane title %q should contain 'channel'", selectPane[0][len(selectPane[0])-1])
	}
}

func TestPaneLifecycle_CaptureDeadChannelPaneWritesSnapshot(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("WUPHF_RUNTIME_HOME", tmp)

	fake := newFakeTmuxRunner()
	fake.outputs["capture-pane"] = []byte("agent: ready\nagent: dead\n")
	setTmuxRunnerForTest(t, fake)

	if err := newPaneLifecycle("wuphf-team").CaptureDeadChannelPane("1 0 claude"); err != nil {
		t.Fatalf("CaptureDeadChannelPane err = %v", err)
	}

	path := filepath.Join(tmp, ".wuphf", "logs", "channel-pane.log")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read snapshot file %s: %v", path, err)
	}
	got := string(raw)
	if !strings.Contains(got, "status=1 0 claude") {
		t.Errorf("snapshot missing status header: %q", got)
	}
	if !strings.Contains(got, "agent: ready") || !strings.Contains(got, "agent: dead") {
		t.Errorf("snapshot missing capture-pane content: %q", got)
	}
}

func TestPaneLifecycle_CaptureDeadChannelPaneCaptureFailureWritesPlaceholder(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("WUPHF_RUNTIME_HOME", tmp)

	fake := newFakeTmuxRunner()
	fake.errors["capture-pane"] = fmt.Errorf("pane vanished")
	setTmuxRunnerForTest(t, fake)

	if err := newPaneLifecycle("wuphf-team").CaptureDeadChannelPane("1 0 claude"); err != nil {
		t.Fatalf("CaptureDeadChannelPane err = %v, want nil even on capture failure", err)
	}

	path := filepath.Join(tmp, ".wuphf", "logs", "channel-pane.log")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read snapshot file: %v", err)
	}
	if !strings.Contains(string(raw), "<capture failed: pane vanished>") {
		t.Errorf("snapshot missing capture-failed placeholder: %q", raw)
	}
}
