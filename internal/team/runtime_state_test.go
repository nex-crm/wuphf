package team

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestDetectRuntimeCapabilities(t *testing.T) {
	oldLookPath := lookPathFn
	oldCommandOutput := commandCombinedOutputFn
	oldActionProbe := actionProviderProbe
	defer func() {
		lookPathFn = oldLookPath
		commandCombinedOutputFn = oldCommandOutput
		actionProviderProbe = oldActionProbe
	}()

	lookPathFn = func(name string) (string, error) {
		switch name {
		case "tmux":
			return "/usr/bin/tmux", nil
		case "claude":
			return "/usr/bin/claude", nil
		default:
			return "", errors.New("missing")
		}
	}
	commandCombinedOutputFn = func(name string, args ...string) ([]byte, error) {
		if name != "tmux" {
			return nil, errors.New("unexpected command")
		}
		if len(args) == 1 && args[0] == "-V" {
			return []byte("tmux 3.4a\n"), nil
		}
		if len(args) == 5 && args[0] == "-L" && args[1] == tmuxSocketName && args[2] == "list-sessions" && args[3] == "-F" {
			return []byte("wuphf-team\t2\t4\nscratch\t1\t1\n"), nil
		}
		return nil, errors.New("unexpected tmux probe")
	}
	actionProviderProbe = func() (string, error) {
		return "", errors.New("no configured provider available")
	}

	t.Setenv("TMUX", "/tmp/tmux-1000/default,123,0")
	t.Setenv("WUPHF_NO_NEX", "1")

	got := DetectRuntimeCapabilities()
	ready, warn, info := got.Counts()
	if ready != 2 || warn != 1 || info != 1 {
		t.Fatalf("unexpected capability counts: ready=%d warn=%d info=%d", ready, warn, info)
	}
	if got.Tmux.BinaryPath != "/usr/bin/tmux" {
		t.Fatalf("expected tmux binary path to be recorded, got %q", got.Tmux.BinaryPath)
	}
	if got.Tmux.Version != "tmux 3.4a" {
		t.Fatalf("expected tmux version to be recorded, got %q", got.Tmux.Version)
	}
	if !got.Tmux.ServerRunning {
		t.Fatalf("expected tmux server to be marked running")
	}
	if !got.Tmux.InsideTmux {
		t.Fatalf("expected inside-tmux state to be recorded")
	}
	if len(got.Tmux.Sessions) != 2 {
		t.Fatalf("expected 2 tmux sessions, got %d", len(got.Tmux.Sessions))
	}
	if got.Tmux.Sessions[0].Name != SessionName || got.Tmux.Sessions[0].Attached != 2 || got.Tmux.Sessions[0].Windows != 4 {
		t.Fatalf("unexpected target tmux session: %+v", got.Tmux.Sessions[0])
	}
}

func TestDetectRuntimeCapabilitiesWhenTmuxServerIsMissing(t *testing.T) {
	oldLookPath := lookPathFn
	oldCommandOutput := commandCombinedOutputFn
	oldActionProbe := actionProviderProbe
	defer func() {
		lookPathFn = oldLookPath
		commandCombinedOutputFn = oldCommandOutput
		actionProviderProbe = oldActionProbe
	}()

	lookPathFn = func(name string) (string, error) {
		switch name {
		case "tmux":
			return "/usr/bin/tmux", nil
		case "claude":
			return "/usr/bin/claude", nil
		default:
			return "", errors.New("missing")
		}
	}
	commandCombinedOutputFn = func(name string, args ...string) ([]byte, error) {
		if name != "tmux" {
			return nil, errors.New("unexpected command")
		}
		if len(args) == 1 && args[0] == "-V" {
			return []byte("tmux 3.4a\n"), nil
		}
		if len(args) == 5 && args[0] == "-L" && args[1] == tmuxSocketName && args[2] == "list-sessions" && args[3] == "-F" {
			return []byte("no server running on /tmp/tmux-1000/wuphf\n"), errors.New("exit status 1")
		}
		return nil, errors.New("unexpected tmux probe")
	}
	actionProviderProbe = func() (string, error) {
		return "", errors.New("no configured provider available")
	}

	t.Setenv("WUPHF_NO_NEX", "1")

	got := DetectRuntimeCapabilities()
	ready, warn, info := got.Counts()
	if ready != 1 || warn != 1 || info != 2 {
		t.Fatalf("unexpected capability counts: ready=%d warn=%d info=%d", ready, warn, info)
	}
	if got.Tmux.ServerRunning {
		t.Fatalf("expected tmux server to be marked missing")
	}
	if got.Items[0].Level != CapabilityInfo {
		t.Fatalf("expected tmux capability to be informational when the server is absent, got %s", got.Items[0].Level)
	}
	if !strings.Contains(got.Tmux.ProbeError, "no server running") {
		t.Fatalf("expected tmux probe note to keep the server error, got %q", got.Tmux.ProbeError)
	}
}

func TestBuildRuntimeSnapshotFormatsRecoveryAndCapabilities(t *testing.T) {
	snapshot := BuildRuntimeSnapshot(RuntimeSnapshotInput{
		Channel:     "general",
		SessionMode: SessionModeOneOnOne,
		DirectAgent: "pm",
		Tasks: []RuntimeTask{{
			ID:             "task-1",
			Title:          "Polish launch checklist",
			Owner:          "pm",
			Status:         "in_progress",
			PipelineStage:  "review",
			ExecutionMode:  "local_worktree",
			WorktreePath:   "/tmp/wuphf-task-1",
			WorktreeBranch: "feat/task-1",
		}},
		Requests: []RuntimeRequest{{
			ID:       "req-1",
			Title:    "Approve launch timing",
			From:     "ceo",
			Status:   "pending",
			Blocking: true,
		}},
		Recent: []RuntimeMessage{{
			ID:      "msg-1",
			From:    "ceo",
			Content: "We need a final timing call before tomorrow.",
		}},
		Capabilities: RuntimeCapabilities{
			Tmux: TmuxCapability{
				BinaryPath:    "/usr/bin/tmux",
				Version:       "tmux 3.4a",
				SocketName:    tmuxSocketName,
				SessionName:   SessionName,
				InsideTmux:    true,
				InsideTmuxEnv: "/tmp/tmux-1000/default,123,0",
				ServerRunning: true,
				Sessions: []TmuxSessionStatus{
					{Name: SessionName, Attached: 2, Windows: 4},
					{Name: "scratch", Attached: 1, Windows: 1},
				},
			},
			Items: []CapabilityStatus{{
				Name:   "tmux",
				Level:  CapabilityReady,
				Detail: "tmux 3.4a on socket wuphf is running with session wuphf-team (2 attached, 4 windows).",
			}},
		},
		Now: time.Unix(100, 0),
	})

	text := snapshot.FormatText()
	for _, want := range []string{
		"Runtime state for #general",
		"Session mode: 1:1 with @pm",
		"Pending human requests: 1",
		"Approve launch timing from @ceo.",
		"Use working_directory /tmp/wuphf-task-1",
		"Recent highlights:",
		"Tmux runtime:",
		"Binary: /usr/bin/tmux",
		"Version: tmux 3.4a",
		"Inside tmux: yes",
		"WUPHF session: running (2 attached, 4 windows)",
		"scratch: 1 attached, 1 windows",
		"Runtime capabilities:",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected %q in %q", want, text)
		}
	}
}
