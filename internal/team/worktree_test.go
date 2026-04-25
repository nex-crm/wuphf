package team

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nex-crm/wuphf/internal/gitexec"
)

func TestCleanupPersistedTaskWorktreesRemovesUniqueTrackedWorktrees(t *testing.T) {
	stateDir := t.TempDir()
	statePath := filepath.Join(stateDir, "broker-state.json")

	oldCleanup := cleanupTaskWorktree
	defer func() {
		cleanupTaskWorktree = oldCleanup
	}()

	setBrokerStatePathForTest(t, func() string { return statePath })

	var calls []string
	cleanupTaskWorktree = func(path, branch string) error {
		calls = append(calls, path+"|"+branch)
		return nil
	}

	state := struct {
		Tasks []teamTask `json:"tasks"`
	}{
		Tasks: []teamTask{
			{ID: "task-1", WorktreePath: "/tmp/wuphf-task-1", WorktreeBranch: "wuphf-task-1"},
			{ID: "task-2", WorktreePath: "/tmp/wuphf-task-1", WorktreeBranch: "wuphf-task-1"},
			{ID: "task-3", WorktreePath: "/tmp/wuphf-task-3", WorktreeBranch: "wuphf-task-3"},
		},
	}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("marshal state: %v", err)
	}
	if err := os.WriteFile(statePath, raw, 0o600); err != nil {
		t.Fatalf("write state: %v", err)
	}

	if err := CleanupPersistedTaskWorktrees(); err != nil {
		t.Fatalf("cleanup persisted task worktrees: %v", err)
	}
	if len(calls) != 2 {
		t.Fatalf("expected 2 unique cleanup calls, got %d (%v)", len(calls), calls)
	}
}

func TestCleanupPersistedTaskWorktreesMissingStateIsNoOp(t *testing.T) {
	stateDir := t.TempDir()
	statePath := filepath.Join(stateDir, "broker-state.json")

	setBrokerStatePathForTest(t, func() string { return statePath })

	if err := CleanupPersistedTaskWorktrees(); err != nil {
		t.Fatalf("expected missing state cleanup to succeed, got %v", err)
	}
}

func TestDefaultPrepareTaskWorktreeOverlaysDirtyWorkspace(t *testing.T) {
	allowRealTaskWorktreeForTest(t)
	repoDir := t.TempDir()
	worktreeRoot := filepath.Join(t.TempDir(), "task-worktrees")
	oldCwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer func() { _ = os.Chdir(oldCwd) }()
	oldTaskRoot := taskWorktreeRootDir
	defer func() { taskWorktreeRootDir = oldTaskRoot }()
	taskWorktreeRootDir = func(repoRoot string) string {
		return filepath.Join(worktreeRoot, sanitizeWorktreeToken(filepath.Base(repoRoot)))
	}

	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = repoDir
		cmd.Env = gitexec.CleanEnv()
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%s failed: %v\n%s", strings.Join(args, " "), err, out)
		}
	}

	run("git", "init", "-b", "main")
	run("git", "config", "user.name", "WUPHF Test")
	run("git", "config", "user.email", "wuphf@example.com")
	if err := os.WriteFile(filepath.Join(repoDir, "tracked.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatalf("write tracked baseline: %v", err)
	}
	run("git", "add", "tracked.txt")
	run("git", "commit", "-m", "base")

	if err := os.WriteFile(filepath.Join(repoDir, "tracked.txt"), []byte("modified\n"), 0o644); err != nil {
		t.Fatalf("write tracked modification: %v", err)
	}
	untrackedPath := filepath.Join(repoDir, "docs", "youtube-factory", "episode-launch-packets", "vid_01-inbox-operator.yaml")
	if err := os.MkdirAll(filepath.Dir(untrackedPath), 0o755); err != nil {
		t.Fatalf("mkdir untracked parent: %v", err)
	}
	if err := os.WriteFile(untrackedPath, []byte("id: vid_01\n"), 0o644); err != nil {
		t.Fatalf("write untracked file: %v", err)
	}
	skippedCachePath := filepath.Join(repoDir, ".wuphf", "cache", "go-build", "ceo", "trim.txt")
	if err := os.MkdirAll(filepath.Dir(skippedCachePath), 0o755); err != nil {
		t.Fatalf("mkdir skipped cache parent: %v", err)
	}
	if err := os.WriteFile(skippedCachePath, []byte("skip me\n"), 0o644); err != nil {
		t.Fatalf("write skipped cache file: %v", err)
	}
	skippedStatePath := filepath.Join(repoDir, ".wuphf", "browser-home-test", ".wuphf", "team", "broker-state.json")
	if err := os.MkdirAll(filepath.Dir(skippedStatePath), 0o755); err != nil {
		t.Fatalf("mkdir skipped state parent: %v", err)
	}
	if err := os.WriteFile(skippedStatePath, []byte("{\"tasks\":[]}"), 0o644); err != nil {
		t.Fatalf("write skipped state file: %v", err)
	}
	skippedPlaywrightPath := filepath.Join(repoDir, ".playwright-cli", "console.log")
	if err := os.MkdirAll(filepath.Dir(skippedPlaywrightPath), 0o755); err != nil {
		t.Fatalf("mkdir skipped playwright parent: %v", err)
	}
	if err := os.WriteFile(skippedPlaywrightPath, []byte("skip me too\n"), 0o644); err != nil {
		t.Fatalf("write skipped playwright file: %v", err)
	}

	if err := os.Chdir(repoDir); err != nil {
		t.Fatalf("chdir repo: %v", err)
	}

	path, branch, err := defaultPrepareTaskWorktree("task-overlay")
	if err != nil {
		t.Fatalf("defaultPrepareTaskWorktree: %v", err)
	}
	defer func() {
		if err := defaultCleanupTaskWorktree(path, branch); err != nil {
			t.Fatalf("cleanup task worktree: %v", err)
		}
	}()
	if wantRoot := taskWorktreeRootDir(repoDir); !strings.HasPrefix(path, wantRoot+string(os.PathSeparator)) {
		t.Fatalf("expected worktree under managed root %q, got %q", wantRoot, path)
	}

	trackedRaw, err := os.ReadFile(filepath.Join(path, "tracked.txt"))
	if err != nil {
		t.Fatalf("read tracked file from worktree: %v", err)
	}
	if got := string(trackedRaw); got != "modified\n" {
		t.Fatalf("expected tracked overlay in worktree, got %q", got)
	}

	untrackedRaw, err := os.ReadFile(filepath.Join(path, "docs", "youtube-factory", "episode-launch-packets", "vid_01-inbox-operator.yaml"))
	if err != nil {
		t.Fatalf("read untracked file from worktree: %v", err)
	}
	if got := string(untrackedRaw); got != "id: vid_01\n" {
		t.Fatalf("expected untracked overlay in worktree, got %q", got)
	}

	if _, err := os.Stat(filepath.Join(path, ".wuphf", "cache", "go-build", "ceo", "trim.txt")); !os.IsNotExist(err) {
		t.Fatalf("expected generated cache file to be skipped, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(path, ".wuphf", "browser-home-test", ".wuphf", "team", "broker-state.json")); !os.IsNotExist(err) {
		t.Fatalf("expected runtime state under .wuphf to be skipped, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(path, ".playwright-cli", "console.log")); !os.IsNotExist(err) {
		t.Fatalf("expected playwright log to be skipped, stat err=%v", err)
	}
}

func TestDefaultPrepareTaskWorktreeOverlaysCompletedSiblingTaskWorkspace(t *testing.T) {
	allowRealTaskWorktreeForTest(t)
	repoDir := t.TempDir()
	worktreeRoot := filepath.Join(t.TempDir(), "task-worktrees")
	stateDir := t.TempDir()
	statePath := filepath.Join(stateDir, "broker-state.json")

	oldCwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer func() { _ = os.Chdir(oldCwd) }()
	oldTaskRoot := taskWorktreeRootDir
	defer func() {
		taskWorktreeRootDir = oldTaskRoot
	}()
	taskWorktreeRootDir = func(repoRoot string) string {
		return filepath.Join(worktreeRoot, sanitizeWorktreeToken(filepath.Base(repoRoot)))
	}
	setBrokerStatePathForTest(t, func() string { return statePath })

	run := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		cmd.Env = gitexec.CleanEnv()
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%s failed: %v\n%s", strings.Join(args, " "), err, out)
		}
	}

	run(repoDir, "git", "init", "-b", "main")
	run(repoDir, "git", "config", "user.name", "WUPHF Test")
	run(repoDir, "git", "config", "user.email", "wuphf@example.com")
	if err := os.WriteFile(filepath.Join(repoDir, "tracked.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatalf("write tracked baseline: %v", err)
	}
	run(repoDir, "git", "add", "tracked.txt")
	run(repoDir, "git", "commit", "-m", "base")

	if err := os.Chdir(repoDir); err != nil {
		t.Fatalf("chdir repo: %v", err)
	}

	firstPath, firstBranch, err := defaultPrepareTaskWorktree("task-3")
	if err != nil {
		t.Fatalf("prepare first task worktree: %v", err)
	}
	defer func() {
		if err := defaultCleanupTaskWorktree(firstPath, firstBranch); err != nil {
			t.Fatalf("cleanup first task worktree: %v", err)
		}
	}()

	if err := os.MkdirAll(filepath.Join(firstPath, "cmd", "topicpack"), 0o755); err != nil {
		t.Fatalf("mkdir topicpack: %v", err)
	}
	if err := os.WriteFile(filepath.Join(firstPath, "cmd", "topicpack", "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("write sibling task file: %v", err)
	}

	state := struct {
		Tasks []teamTask `json:"tasks"`
	}{
		Tasks: []teamTask{
			{
				ID:             "task-3",
				Status:         "done",
				ExecutionMode:  "local_worktree",
				WorktreePath:   firstPath,
				WorktreeBranch: firstBranch,
			},
		},
	}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("marshal state: %v", err)
	}
	if err := os.WriteFile(statePath, raw, 0o600); err != nil {
		t.Fatalf("write state: %v", err)
	}

	secondPath, secondBranch, err := defaultPrepareTaskWorktree("task-14")
	if err != nil {
		t.Fatalf("prepare second task worktree: %v", err)
	}
	defer func() {
		if err := defaultCleanupTaskWorktree(secondPath, secondBranch); err != nil {
			t.Fatalf("cleanup second task worktree: %v", err)
		}
	}()

	secondRaw, err := os.ReadFile(filepath.Join(secondPath, "cmd", "topicpack", "main.go"))
	if err != nil {
		t.Fatalf("read overlaid sibling task file: %v", err)
	}
	if got := string(secondRaw); got != "package main\n" {
		t.Fatalf("expected sibling task overlay in new worktree, got %q", got)
	}
}

func TestDefaultPrepareTaskWorktreeSkipsDuplicateAndMissingCompletedSiblingSources(t *testing.T) {
	allowRealTaskWorktreeForTest(t)
	repoDir := t.TempDir()
	worktreeRoot := filepath.Join(t.TempDir(), "task-worktrees")
	stateDir := t.TempDir()
	statePath := filepath.Join(stateDir, "broker-state.json")

	oldCwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer func() { _ = os.Chdir(oldCwd) }()
	oldTaskRoot := taskWorktreeRootDir
	defer func() {
		taskWorktreeRootDir = oldTaskRoot
	}()
	taskWorktreeRootDir = func(repoRoot string) string {
		return filepath.Join(worktreeRoot, sanitizeWorktreeToken(filepath.Base(repoRoot)))
	}
	setBrokerStatePathForTest(t, func() string { return statePath })

	run := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		cmd.Env = gitexec.CleanEnv()
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%s failed: %v\n%s", strings.Join(args, " "), err, out)
		}
	}

	run(repoDir, "git", "init", "-b", "main")
	run(repoDir, "git", "config", "user.name", "WUPHF Test")
	run(repoDir, "git", "config", "user.email", "wuphf@example.com")
	if err := os.WriteFile(filepath.Join(repoDir, "tracked.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatalf("write tracked baseline: %v", err)
	}
	run(repoDir, "git", "add", "tracked.txt")
	run(repoDir, "git", "commit", "-m", "base")

	if err := os.Chdir(repoDir); err != nil {
		t.Fatalf("chdir repo: %v", err)
	}

	firstPath, firstBranch, err := defaultPrepareTaskWorktree("task-2")
	if err != nil {
		t.Fatalf("prepare first task worktree: %v", err)
	}
	defer func() {
		if err := defaultCleanupTaskWorktree(firstPath, firstBranch); err != nil {
			t.Fatalf("cleanup first task worktree: %v", err)
		}
	}()

	if err := os.MkdirAll(filepath.Join(firstPath, "cmd", "approvalrunner"), 0o755); err != nil {
		t.Fatalf("mkdir approvalrunner: %v", err)
	}
	if err := os.WriteFile(filepath.Join(firstPath, "cmd", "approvalrunner", "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("write sibling task file: %v", err)
	}
	// Stage the file so it appears in `git diff --name-only HEAD` (the primary
	// overlay detection path) rather than relying solely on `git ls-files --others`
	// which can be unreliable in CI worktree environments.
	run(firstPath, "git", "add", "cmd/approvalrunner/main.go")

	state := struct {
		Tasks []teamTask `json:"tasks"`
	}{
		Tasks: []teamTask{
			{
				ID:             "task-2",
				Status:         "done",
				ExecutionMode:  "local_worktree",
				WorktreePath:   firstPath,
				WorktreeBranch: firstBranch,
			},
			{
				ID:             "task-11",
				Status:         "done",
				ExecutionMode:  "local_worktree",
				WorktreePath:   firstPath,
				WorktreeBranch: firstBranch,
			},
			{
				ID:             "task-12",
				Status:         "review",
				ExecutionMode:  "local_worktree",
				WorktreePath:   filepath.Join(t.TempDir(), "missing-worktree"),
				WorktreeBranch: "wuphf-missing-task-12",
			},
		},
	}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("marshal state: %v", err)
	}
	if err := os.WriteFile(statePath, raw, 0o600); err != nil {
		t.Fatalf("write state: %v", err)
	}

	secondPath, secondBranch, err := defaultPrepareTaskWorktree("task-21")
	if err != nil {
		t.Fatalf("prepare second task worktree: %v", err)
	}
	defer func() {
		if err := defaultCleanupTaskWorktree(secondPath, secondBranch); err != nil {
			t.Fatalf("cleanup second task worktree: %v", err)
		}
	}()

	secondRaw, err := os.ReadFile(filepath.Join(secondPath, "cmd", "approvalrunner", "main.go"))
	if err != nil {
		t.Fatalf("read overlaid sibling task file: %v", err)
	}
	if got := string(secondRaw); got != "package main\n" {
		t.Fatalf("expected sibling task overlay in new worktree, got %q", got)
	}
}

func TestWorktreePathLooksSafeAllowsManagedAndLegacyRoots(t *testing.T) {
	oldTaskRoot := taskWorktreeRootDir
	defer func() { taskWorktreeRootDir = oldTaskRoot }()
	taskWorktreeRootDir = func(string) string {
		return filepath.Join(t.TempDir(), "task-worktrees", "repo")
	}

	managed := filepath.Join(taskWorktreeRootDir("repo"), "wuphf-task-task-1")
	if !worktreePathLooksSafe(managed) {
		t.Fatalf("expected managed worktree path to be safe: %q", managed)
	}

	legacy := filepath.Join(os.TempDir(), "wuphf-task-task-legacy")
	if !worktreePathLooksSafe(legacy) {
		t.Fatalf("expected legacy temp worktree path to remain safe: %q", legacy)
	}

	unsafe := filepath.Join(t.TempDir(), "somewhere-else", "task-1")
	if worktreePathLooksSafe(unsafe) {
		t.Fatalf("expected non-worktree path to be unsafe: %q", unsafe)
	}
}
