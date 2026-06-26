package team

// headless_workspace_test.go — V3-N5 workspace isolation: headless turns
// never execute in the broker process launch cwd. Task turns use their task
// worktree; everything else gets the per-agent scratch dir under the office
// runtime home.

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestAgentScratchDir_UnderRuntimeHomeCreatedOnDemand pins the scratch-dir
// layout: <WUPHF_RUNTIME_HOME>/.wuphf/agent-scratch/<slug>, created on
// demand, never the process cwd.
func TestAgentScratchDir_UnderRuntimeHomeCreatedOnDemand(t *testing.T) {
	home := t.TempDir()
	t.Setenv("WUPHF_RUNTIME_HOME", home)

	got := agentScratchDir("eng")
	want := filepath.Join(home, ".wuphf", "agent-scratch", "eng")
	if got != want {
		t.Fatalf("agentScratchDir(eng) = %q, want %q", got, want)
	}
	info, err := os.Stat(got)
	if err != nil || !info.IsDir() {
		t.Fatalf("scratch dir must be created on demand: stat err=%v", err)
	}
	cwd, _ := os.Getwd()
	if filepath.Clean(got) == filepath.Clean(cwd) {
		t.Fatalf("scratch dir must never be the process cwd %q", cwd)
	}

	// Empty/odd slugs still resolve to a safe token, never an empty path.
	if got := agentScratchDir(""); !strings.HasPrefix(got, filepath.Join(home, ".wuphf", "agent-scratch")) {
		t.Fatalf("empty slug must stay under the scratch root, got %q", got)
	}
}

// TestHeadlessTurnWorkspace_WorktreeElseScratch pins the resolution order:
// a local_worktree task with an assigned path wins (isTaskWorktree=true);
// everything else falls back to the agent scratch dir (false) — never the
// launcher cwd.
func TestHeadlessTurnWorkspace_WorktreeElseScratch(t *testing.T) {
	home := t.TempDir()
	t.Setenv("WUPHF_RUNTIME_HOME", home)
	worktree := t.TempDir()

	b := newTestBroker(t)
	b.mu.Lock()
	b.members = []officeMember{{Slug: "eng", Name: "Engineer"}}
	b.channels = []teamChannel{{Slug: "general", Name: "general", Members: []string{"human", "eng"}}}
	b.tasks = []teamTask{{
		ID: "task-wt-1", Channel: "general", Title: "worktree task", Owner: "eng",
		status: "in_progress", ExecutionMode: "local_worktree", WorktreePath: worktree,
	}}
	b.mu.Unlock()
	l := minimalLauncher(false)
	l.broker = b
	l.cwd = t.TempDir() // the "host launch cwd" that must never leak through

	dir, isWorktree := l.headlessTurnWorkspace("eng", "task-wt-1")
	if !isWorktree || !samePath(dir, worktree) {
		t.Fatalf("worktree task: got (%q, %v), want (%q, true)", dir, isWorktree, worktree)
	}

	// A chat turn while the agent has an active worktree task keeps the
	// legacy single-task fallback (agentActiveTask) — still inside the
	// office workspace. An agent with NO active task gets the scratch dir.
	dir, isWorktree = l.headlessTurnWorkspace("ceo", "")
	wantScratch := filepath.Join(home, ".wuphf", "agent-scratch", "ceo")
	if isWorktree || !samePath(dir, wantScratch) {
		t.Fatalf("chat turn: got (%q, %v), want (%q, false)", dir, isWorktree, wantScratch)
	}
	if samePath(dir, l.cwd) {
		t.Fatalf("chat turn must never resolve to the launcher cwd %q", l.cwd)
	}
}

// TestRunHeadlessClaudeTurn_NoWorktreeRunsInAgentScratch pins the claude
// runner's cmd.Dir for a turn without a task worktree: the agent scratch dir
// under the runtime home — never l.cwd (the V3-N5 bug: the CEO's chat turns
// wrote landing/index.html into the founder's host repo).
func TestRunHeadlessClaudeTurn_NoWorktreeRunsInAgentScratch(t *testing.T) {
	home := t.TempDir()
	t.Setenv("WUPHF_RUNTIME_HOME", home)
	tmpDir := t.TempDir()

	origCommandContext := headlessClaudeCommandContext
	origLookPath := headlessClaudeLookPath
	defer func() {
		headlessClaudeCommandContext = origCommandContext
		headlessClaudeLookPath = origLookPath
	}()
	headlessClaudeLookPath = func(string) (string, error) { return "/bin/true", nil }
	var captured *exec.Cmd
	headlessClaudeCommandContext = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		captured = exec.CommandContext(ctx, "/bin/true")
		return captured
	}

	b := NewBrokerAt(filepath.Join(tmpDir, "broker-state.json"))
	l := minimalLauncher(false)
	l.broker = b
	l.cwd = tmpDir
	mcpPath := filepath.Join(tmpDir, "mcp.json")
	_ = os.WriteFile(mcpPath, []byte(`{"mcpServers":{}}`), 0o600)
	l.mcpConfig = mcpPath

	// Parse error from /bin/true's empty output is expected; cmd.Dir is set
	// before Start so the captured command carries the resolved dir.
	_ = l.runHeadlessClaudeTurn(t.Context(), "ceo", "answer the human in #general")

	if captured == nil {
		t.Fatal("headlessClaudeCommandContext hook was not called")
	}
	wantScratch := filepath.Join(home, ".wuphf", "agent-scratch", "ceo")
	if !samePath(captured.Dir, wantScratch) {
		t.Fatalf("claude cmd.Dir = %q, want agent scratch %q", captured.Dir, wantScratch)
	}
	if samePath(captured.Dir, l.cwd) {
		t.Fatalf("claude cmd.Dir must never be the broker launch cwd %q", l.cwd)
	}
	for _, kv := range captured.Env {
		if strings.HasPrefix(kv, "WUPHF_WORKTREE_PATH=") {
			t.Fatalf("scratch-dir turn must not advertise WUPHF_WORKTREE_PATH; env %q", kv)
		}
	}
}
