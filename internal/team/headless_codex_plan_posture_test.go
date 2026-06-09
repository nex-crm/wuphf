package team

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nex-crm/wuphf/internal/agent"
)

// TestRunHeadlessCodexTurnPlanPostureReadOnly is the Codex half of the native
// plan-mode guarantee: a turn whose task is in LifecycleStatePlanning runs in
// Codex's read-only sandbox (-s read-only), never workspace-write and never the
// dangerous bypass — so the planning turn cannot change the repo before
// "Approve & Start".
func TestRunHeadlessCodexTurnPlanPostureReadOnly(t *testing.T) {
	recordFile := filepath.Join(t.TempDir(), "headless-codex-record.jsonl")
	oldLookPath := headlessCodexLookPath
	oldExecutablePath := headlessCodexExecutablePath
	oldCommandContext := headlessCodexCommandContext
	headlessCodexLookPath = func(file string) (string, error) {
		switch file {
		case "codex":
			return "/usr/bin/codex", nil
		case "nex-mcp":
			return "/usr/bin/nex-mcp", nil
		default:
			return "", exec.ErrNotFound
		}
	}
	headlessCodexExecutablePath = func() (string, error) { return "/tmp/wuphf", nil }
	headlessCodexCommandContext = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		cmdArgs := []string{"-test.run=TestHeadlessCodexHelperProcess", "--"}
		cmdArgs = append(cmdArgs, args...)
		return exec.CommandContext(ctx, os.Args[0], cmdArgs...)
	}
	defer func() {
		headlessCodexLookPath = oldLookPath
		headlessCodexExecutablePath = oldExecutablePath
		headlessCodexCommandContext = oldCommandContext
	}()

	t.Setenv("GO_WANT_HEADLESS_CODEX_HELPER_PROCESS", "1")
	t.Setenv("HEADLESS_CODEX_RECORD_FILE", recordFile)
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("WUPHF_RUNTIME_HOME", tmpHome)
	t.Setenv("WUPHF_API_KEY", "nex-secret-key")
	t.Setenv("WUPHF_OPENAI_API_KEY", "openai-secret-key")

	b := newTestBroker(t)
	seedTaskInState(t, b, "task-plan", LifecycleStatePlanning)
	l := &Launcher{
		pack:     agent.GetPack("founding-team"),
		cwd:      t.TempDir(),
		broker:   b,
		headless: headlessWorkerPool{ctx: t.Context()},
	}

	ctx := withHeadlessTurnTaskID(t.Context(), "task-plan")
	if err := l.runHeadlessCodexTurn(ctx, "ceo", "Plan this work."); err != nil {
		t.Fatalf("runHeadlessCodexTurn: %v", err)
	}

	record := readHeadlessCodexRecord(t, recordFile)
	joinedArgs := strings.Join(record.Args, " ")
	if !strings.Contains(joinedArgs, "-a never") || !strings.Contains(joinedArgs, "-s read-only") {
		t.Fatalf("expected read-only sandbox for planning turn, got %#v", record.Args)
	}
	if strings.Contains(joinedArgs, "-s workspace-write") {
		t.Fatalf("planning turn must not be workspace-write, got %#v", record.Args)
	}
	if strings.Contains(joinedArgs, "--dangerously-bypass-approvals-and-sandbox") {
		t.Fatalf("planning turn must not bypass the sandbox, got %#v", record.Args)
	}
}
