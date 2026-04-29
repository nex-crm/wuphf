package team

// Tests for the C5d migration: spawn-side tmux helpers added to
// paneLifecycle (SplitFirstAgent, SplitAdditionalAgent, NewOverflowWindow,
// NewSession, SetSessionOption, SetTeamWindowOption,
// ApplyMainVerticalLayout, SetPaneTitle, SelectTeamWindow, FocusPane,
// IsPaneDead, CapturePaneHistory, SendEnter). High-level spawn
// orchestration (spawnVisibleAgents/etc.) is not covered here — those
// methods need callbacks for claudeCommand/buildPrompt/broker access
// that come in a follow-up ownership PR. The helper tests pin the exact
// tmux args every spawn path emits, which is the value those tests
// would have provided anyway.

import (
	"fmt"
	"testing"
)

func TestPaneLifecycle_SplitFirstAgentArgs(t *testing.T) {
	fake := newFakeTmuxRunner()
	fake.outputs["split-window"] = []byte("ok")
	setTmuxRunnerForTest(t, fake)

	out, err := newPaneLifecycle("wuphf-team").SplitFirstAgent("/repo", "claude --print")
	if err != nil {
		t.Fatalf("SplitFirstAgent err = %v", err)
	}
	if string(out) != "ok" {
		t.Errorf("output = %q, want ok", out)
	}
	calls := fake.callsFor("split-window")
	if len(calls) != 1 {
		t.Fatalf("split-window calls = %d, want 1", len(calls))
	}
	want := []string{"split-window", "-h", "-t", "wuphf-team:team", "-p", "65", "-c", "/repo", "claude --print"}
	if !equalStrings(calls[0], want) {
		t.Errorf("split-window args = %v, want %v", calls[0], want)
	}
}

func TestPaneLifecycle_SplitAdditionalAgentArgs(t *testing.T) {
	fake := newFakeTmuxRunner()
	setTmuxRunnerForTest(t, fake)

	if _, err := newPaneLifecycle("wuphf-team").SplitAdditionalAgent("/repo", "claude"); err != nil {
		t.Fatalf("SplitAdditionalAgent err = %v", err)
	}
	calls := fake.callsFor("split-window")
	if len(calls) != 1 {
		t.Fatalf("split-window calls = %d, want 1", len(calls))
	}
	// Note: no -h flag — additional agents tile vertically on the right.
	// Note: no -p flag — additional agents take whatever default split
	// percentage tmux gives them, then ApplyMainVerticalLayout normalizes.
	want := []string{"split-window", "-t", "wuphf-team:team.1", "-c", "/repo", "claude"}
	if !equalStrings(calls[0], want) {
		t.Errorf("args = %v, want %v", calls[0], want)
	}
}

func TestPaneLifecycle_NewOverflowWindowArgs(t *testing.T) {
	fake := newFakeTmuxRunner()
	setTmuxRunnerForTest(t, fake)

	if _, err := newPaneLifecycle("wuphf-team").NewOverflowWindow("agent-fe", "/repo", "claude"); err != nil {
		t.Fatalf("NewOverflowWindow err = %v", err)
	}
	calls := fake.callsFor("new-window")
	if len(calls) != 1 {
		t.Fatalf("new-window calls = %d, want 1", len(calls))
	}
	want := []string{"new-window", "-d", "-t", "wuphf-team", "-n", "agent-fe", "-c", "/repo", "claude"}
	if !equalStrings(calls[0], want) {
		t.Errorf("args = %v, want %v", calls[0], want)
	}
}

func TestPaneLifecycle_NewSessionArgs(t *testing.T) {
	fake := newFakeTmuxRunner()
	setTmuxRunnerForTest(t, fake)

	if err := newPaneLifecycle("wuphf-team").NewSession("/repo", "sleep infinity"); err != nil {
		t.Fatalf("NewSession err = %v", err)
	}
	calls := fake.callsFor("new-session")
	if len(calls) != 1 {
		t.Fatalf("new-session calls = %d, want 1", len(calls))
	}
	want := []string{"new-session", "-d", "-s", "wuphf-team", "-n", "team", "-c", "/repo", "sleep infinity"}
	if !equalStrings(calls[0], want) {
		t.Errorf("args = %v, want %v", calls[0], want)
	}
}

func TestPaneLifecycle_NewSessionSurfacesError(t *testing.T) {
	fake := newFakeTmuxRunner()
	fake.errors["new-session"] = fmt.Errorf("session exists")
	setTmuxRunnerForTest(t, fake)

	if err := newPaneLifecycle("wuphf-team").NewSession("/repo", "sleep infinity"); err == nil {
		t.Fatal("NewSession err = nil, want non-nil")
	}
}

func TestPaneLifecycle_SetSessionOptionArgs(t *testing.T) {
	fake := newFakeTmuxRunner()
	setTmuxRunnerForTest(t, fake)

	newPaneLifecycle("wuphf-team").SetSessionOption("mouse", "off")
	calls := fake.callsFor("set-option")
	if len(calls) != 1 {
		t.Fatalf("set-option calls = %d, want 1", len(calls))
	}
	want := []string{"set-option", "-t", "wuphf-team", "mouse", "off"}
	if !equalStrings(calls[0], want) {
		t.Errorf("args = %v, want %v", calls[0], want)
	}
}

func TestPaneLifecycle_SetTeamWindowOptionArgs(t *testing.T) {
	fake := newFakeTmuxRunner()
	setTmuxRunnerForTest(t, fake)

	newPaneLifecycle("wuphf-team").SetTeamWindowOption("remain-on-exit", "on")
	calls := fake.callsFor("set-window-option")
	if len(calls) != 1 {
		t.Fatalf("set-window-option calls = %d, want 1", len(calls))
	}
	want := []string{"set-window-option", "-t", "wuphf-team:team", "remain-on-exit", "on"}
	if !equalStrings(calls[0], want) {
		t.Errorf("args = %v, want %v", calls[0], want)
	}
}

func TestPaneLifecycle_ApplyMainVerticalLayoutArgs(t *testing.T) {
	fake := newFakeTmuxRunner()
	setTmuxRunnerForTest(t, fake)

	newPaneLifecycle("wuphf-team").ApplyMainVerticalLayout()
	calls := fake.callsFor("select-layout")
	if len(calls) != 1 {
		t.Fatalf("select-layout calls = %d, want 1", len(calls))
	}
	want := []string{"select-layout", "-t", "wuphf-team:team", "main-vertical"}
	if !equalStrings(calls[0], want) {
		t.Errorf("args = %v, want %v", calls[0], want)
	}
}

func TestPaneLifecycle_SetPaneTitleArgs(t *testing.T) {
	fake := newFakeTmuxRunner()
	setTmuxRunnerForTest(t, fake)

	newPaneLifecycle("wuphf-team").SetPaneTitle("wuphf-team:team.2", "🤖 ceo (@ceo)")
	calls := fake.callsFor("select-pane")
	if len(calls) != 1 {
		t.Fatalf("select-pane calls = %d, want 1", len(calls))
	}
	want := []string{"select-pane", "-t", "wuphf-team:team.2", "-T", "🤖 ceo (@ceo)"}
	if !equalStrings(calls[0], want) {
		t.Errorf("args = %v, want %v", calls[0], want)
	}
}

func TestPaneLifecycle_SelectTeamWindowAndFocusPane(t *testing.T) {
	fake := newFakeTmuxRunner()
	setTmuxRunnerForTest(t, fake)

	pl := newPaneLifecycle("wuphf-team")
	pl.SelectTeamWindow()
	pl.FocusPane("wuphf-team:team.0")

	wins := fake.callsFor("select-window")
	if len(wins) != 1 {
		t.Fatalf("select-window calls = %d, want 1", len(wins))
	}
	if !equalStrings(wins[0], []string{"select-window", "-t", "wuphf-team:team"}) {
		t.Errorf("select-window args = %v", wins[0])
	}

	panes := fake.callsFor("select-pane")
	if len(panes) != 1 {
		t.Fatalf("select-pane calls = %d, want 1", len(panes))
	}
	// FocusPane has no -T flag (vs SetPaneTitle)
	if !equalStrings(panes[0], []string{"select-pane", "-t", "wuphf-team:team.0"}) {
		t.Errorf("select-pane (focus) args = %v", panes[0])
	}
}

func TestPaneLifecycle_IsPaneDeadParsesOutput(t *testing.T) {
	fake := newFakeTmuxRunner()
	fake.outputs["display-message"] = []byte("1\n")
	setTmuxRunnerForTest(t, fake)

	dead, err := newPaneLifecycle("wuphf-team").IsPaneDead("wuphf-team:team.2")
	if err != nil {
		t.Fatalf("IsPaneDead err = %v", err)
	}
	if !dead {
		t.Errorf("IsPaneDead = false, want true")
	}

	calls := fake.callsFor("display-message")
	if len(calls) != 1 {
		t.Fatalf("display-message calls = %d, want 1", len(calls))
	}
	want := []string{"display-message", "-t", "wuphf-team:team.2", "-p", "#{pane_dead}"}
	if !equalStrings(calls[0], want) {
		t.Errorf("args = %v, want %v", calls[0], want)
	}
}

func TestPaneLifecycle_IsPaneDeadFalseWhenZero(t *testing.T) {
	fake := newFakeTmuxRunner()
	fake.outputs["display-message"] = []byte("0\n")
	setTmuxRunnerForTest(t, fake)

	dead, err := newPaneLifecycle("wuphf-team").IsPaneDead("wuphf-team:team.2")
	if err != nil {
		t.Fatalf("IsPaneDead err = %v", err)
	}
	if dead {
		t.Errorf("IsPaneDead = true, want false")
	}
}

func TestPaneLifecycle_CapturePaneHistoryArgs(t *testing.T) {
	fake := newFakeTmuxRunner()
	fake.outputs["capture-pane"] = []byte("last 200 lines\n")
	setTmuxRunnerForTest(t, fake)

	out, err := newPaneLifecycle("wuphf-team").CapturePaneHistory("wuphf-team:team.2", 200)
	if err != nil {
		t.Fatalf("CapturePaneHistory err = %v", err)
	}
	if out != "last 200 lines\n" {
		t.Errorf("output = %q, want %q", out, "last 200 lines\n")
	}
	calls := fake.callsFor("capture-pane")
	if len(calls) != 1 {
		t.Fatalf("capture-pane calls = %d, want 1", len(calls))
	}
	want := []string{"capture-pane", "-t", "wuphf-team:team.2", "-p", "-J", "-S", "-200"}
	if !equalStrings(calls[0], want) {
		t.Errorf("args = %v, want %v", calls[0], want)
	}
}

func TestPaneLifecycle_SendEnterArgs(t *testing.T) {
	fake := newFakeTmuxRunner()
	setTmuxRunnerForTest(t, fake)

	newPaneLifecycle("wuphf-team").SendEnter("wuphf-team:team.2")

	calls := fake.callsFor("send-keys")
	if len(calls) != 1 {
		t.Fatalf("send-keys calls = %d, want 1", len(calls))
	}
	want := []string{"send-keys", "-t", "wuphf-team:team.2", "Enter"}
	if !equalStrings(calls[0], want) {
		t.Errorf("args = %v, want %v", calls[0], want)
	}
}
