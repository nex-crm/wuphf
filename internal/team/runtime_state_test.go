package team

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/nex-crm/wuphf/internal/action"
)

func TestDetectRuntimeCapabilities(t *testing.T) {
	oldLookPath := lookPathFn
	oldCommandOutput := commandCombinedOutputFn
	oldActionProviderForCapability := actionProviderForCapabilityFn
	oldActionProviders := actionProvidersFn
	defer func() {
		lookPathFn = oldLookPath
		commandCombinedOutputFn = oldCommandOutput
		actionProviderForCapabilityFn = oldActionProviderForCapability
		actionProvidersFn = oldActionProviders
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
	actionProviderForCapabilityFn = func(action.Capability) (action.Provider, error) {
		return nil, errors.New("no configured provider available")
	}
	actionProvidersFn = func() []action.Provider { return nil }

	t.Setenv("TMUX", "/tmp/tmux-1000/default,123,0")
	t.Setenv("WUPHF_NO_NEX", "1")

	got := DetectRuntimeCapabilities()
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
	if office, ok := got.Registry.Entry(CapabilityKeyOfficeRuntime); !ok || office.Level != CapabilityReady {
		t.Fatalf("expected office runtime to be ready, got %+v", office)
	}
	if nex, ok := got.Registry.Entry(CapabilityKeyNex); !ok || nex.Lifecycle != CapabilityLifecycleDisabled {
		t.Fatalf("expected Nex to be disabled in --no-nex mode, got %+v", nex)
	}
}

func TestDetectRuntimeCapabilitiesWhenTmuxServerIsMissing(t *testing.T) {
	oldLookPath := lookPathFn
	oldCommandOutput := commandCombinedOutputFn
	oldActionProviderForCapability := actionProviderForCapabilityFn
	oldActionProviders := actionProvidersFn
	defer func() {
		lookPathFn = oldLookPath
		commandCombinedOutputFn = oldCommandOutput
		actionProviderForCapabilityFn = oldActionProviderForCapability
		actionProvidersFn = oldActionProviders
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
	actionProviderForCapabilityFn = func(action.Capability) (action.Provider, error) {
		return nil, errors.New("no configured provider available")
	}
	actionProvidersFn = func() []action.Provider { return nil }

	t.Setenv("WUPHF_NO_NEX", "1")

	got := DetectRuntimeCapabilities()
	if got.Tmux.ServerRunning {
		t.Fatalf("expected tmux server to be marked missing")
	}
	if !strings.Contains(got.Tmux.ProbeError, "no server running") {
		t.Fatalf("expected tmux probe note to keep the server error, got %q", got.Tmux.ProbeError)
	}
	if tmux, ok := got.Registry.Entry(CapabilityKeyTmux); !ok || tmux.Level != CapabilityInfo {
		t.Fatalf("expected tmux capability to be informational when the server is absent, got %+v", tmux)
	}
	if office, ok := got.Registry.Entry(CapabilityKeyOfficeRuntime); !ok || office.Level != CapabilityWarn {
		t.Fatalf("expected office runtime to stay warning-level when tmux session is missing, got %+v", office)
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
		Artifacts: []RuntimeArtifact{
			{
				ID:            "task-1",
				Kind:          RuntimeArtifactTask,
				Title:         "Polish launch checklist",
				Summary:       "This task is retained as a live execution artifact with its current runtime context.",
				State:         "review",
				Progress:      "Stage: review · Review: pending review · Execution: local worktree",
				PartialOutput: "Latest task output retained for review.",
				Path:          "/tmp/wuphf-task-1/output.log",
				Worktree:      "/tmp/wuphf-task-1",
				ResumeHint:    "Resume in /tmp/wuphf-task-1 or reopen the task thread.",
				ReviewHint:    "Review pending review.",
			},
			{
				ID:         "req-1",
				Kind:       RuntimeArtifactRequest,
				Title:      "Approve launch timing",
				Summary:    "Blocking approval before tomorrow.",
				State:      "pending",
				ResumeHint: "Answer the request or reopen it from Recovery.",
			},
		},
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
		Registry: CapabilityRegistry{
			Entries: []CapabilityDescriptor{{
				Key:      "workflow_execute",
				Label:    "Workflow execution",
				Category: CapabilityCategoryWorkflow,
				Level:    CapabilityReady,
				Detail:   "Workflow execution is available via one.",
			}},
		},
		Now: time.Unix(100, 0),
	})

	text := snapshot.FormatText()
	for _, want := range []string{
		"Runtime state for #general",
		"Session mode: 1:1 with @pm",
		"Pending human requests: 1",
		"Retained execution artifacts: 2",
		"Approve launch timing from @ceo.",
		"Use working_directory /tmp/wuphf-task-1",
		"Execution artifacts:",
		"Polish launch checklist [task] review: This task is retained as a live execution artifact with its current runtime context.",
		"Approve launch timing [request] pending: Blocking approval before tomorrow.",
		"Recent highlights:",
		"Tmux runtime:",
		"Binary: /usr/bin/tmux",
		"Version: tmux 3.4a",
		"Inside tmux: yes",
		"WUPHF session: running (2 attached, 4 windows)",
		"scratch: 1 attached, 1 windows",
		"Runtime capabilities:",
		"Capability registry:",
		"Workflow execution (workflow) [ready]: Workflow execution is available via one.",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected %q in %q", want, text)
		}
	}
	if len(snapshot.Memory.Tasks) != 1 {
		t.Fatalf("expected task memory summary, got %+v", snapshot.Memory.Tasks)
	}
	restore := snapshot.Memory.RestorationContext()
	if len(restore.ActiveTaskIDs) != 1 || restore.ActiveTaskIDs[0] != "task-1" {
		t.Fatalf("expected restore context to keep active task, got %+v", restore)
	}
	if len(restore.PendingRequestIDs) != 1 || restore.PendingRequestIDs[0] != "req-1" {
		t.Fatalf("expected restore context to keep pending request, got %+v", restore)
	}
}
