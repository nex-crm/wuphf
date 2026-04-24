package team

import (
	"bytes"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/nex-crm/wuphf/internal/gitexec"
)

var prepareTaskWorktree = defaultPrepareTaskWorktree
var cleanupTaskWorktree = defaultCleanupTaskWorktree
var taskWorktreeRootDir = defaultTaskWorktreeRootDir
var verifyTaskWorktreeWritable = defaultVerifyTaskWorktreeWritable

// allowRealTaskWorktree gates access to the real git-worktree codepath. In
// production it stays true; an init() in a *_test.go file in this package
// flips it to false so test runs cannot accidentally register a real worktree
// against the developer's `wuphf` repo. Tests that legitimately need the real
// codepath (always against a tempdir-rooted repo) opt in via
// allowRealTaskWorktreeForTest(t), which scopes the re-enable to one test.
var allowRealTaskWorktree = true

// errRealTaskWorktreeDisabled is returned by defaultPrepareTaskWorktree when
// it is called from a test that has not opted into the real codepath. The
// message names the opt-in helper so the fix is immediate.
var errRealTaskWorktreeDisabled = fmt.Errorf(
	"defaultPrepareTaskWorktree disabled in tests: call allowRealTaskWorktreeForTest(t) " +
		"or monkey-patch prepareTaskWorktree",
)

var overlaySourceWorkspaceSkipExact = map[string]struct{}{
	".playwright-cli": {},
	".playwright-mcp": {},
}

var overlaySourceWorkspaceSkipPrefixes = []string{
	".playwright-cli/",
	".playwright-mcp/",
	".wuphf/",
}

func defaultPrepareTaskWorktree(taskID string) (string, string, error) {
	if !allowRealTaskWorktree {
		return "", "", errRealTaskWorktreeDisabled
	}
	repoRoot, err := gitRepoRoot()
	if err != nil {
		return "", "", err
	}

	branch := worktreeBranchNameForRepo(taskID, repoRoot)
	root := taskWorktreeRootDir(repoRoot)
	if strings.TrimSpace(root) == "" {
		root = filepath.Join(os.TempDir(), "wuphf-task-worktrees")
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", "", fmt.Errorf("prepare task worktree root: %w", err)
	}
	path := filepath.Join(root, "wuphf-task-"+sanitizeWorktreeToken(taskID))
	_ = runGit(repoRoot, "worktree", "prune")
	_ = cleanupTaskWorktreeAtRepoRoot(repoRoot, path, branch)
	_ = clearStaleTaskBranch(repoRoot, branch)
	finish := func(path, branch string) (string, string, error) {
		if err := overlaySourceWorkspace(repoRoot, path); err != nil {
			_ = cleanupTaskWorktreeAtRepoRoot(repoRoot, path, branch)
			return "", "", fmt.Errorf("overlay source workspace: %w", err)
		}
		if err := overlayPersistedTaskWorktrees(path, taskID); err != nil {
			_ = cleanupTaskWorktreeAtRepoRoot(repoRoot, path, branch)
			return "", "", fmt.Errorf("overlay prior task worktrees: %w", err)
		}
		return path, branch, nil
	}
	firstErr := runGit(repoRoot, "worktree", "add", "-b", branch, path, "HEAD")
	if firstErr == nil {
		return finish(path, branch)
	}
	_ = runGit(repoRoot, "worktree", "prune")
	_ = cleanupTaskWorktreeAtRepoRoot(repoRoot, path, branch)
	_ = clearStaleTaskBranch(repoRoot, branch)
	if err := runGit(repoRoot, "worktree", "add", "-b", branch, path, "HEAD"); err == nil {
		return finish(path, branch)
	}
	if err := runGit(repoRoot, "worktree", "add", path, branch); err == nil {
		return finish(path, branch)
	}

	return "", "", fmt.Errorf("create git worktree for %s: %w", taskID, firstErr)
}

func clearStaleTaskBranch(repoRoot, branch string) error {
	branch = strings.TrimSpace(branch)
	if branch == "" || !gitRefExists(repoRoot, "refs/heads/"+branch) {
		return nil
	}
	if err := runGit(repoRoot, "branch", "-D", branch); err == nil {
		return nil
	}
	_ = runGit(repoRoot, "worktree", "prune")
	if !gitRefExists(repoRoot, "refs/heads/"+branch) {
		return nil
	}
	return runGit(repoRoot, "branch", "-D", branch)
}

func defaultCleanupTaskWorktree(path, branch string) error {
	repoRoot, err := gitRepoRoot()
	if err != nil {
		return err
	}
	return cleanupTaskWorktreeAtRepoRoot(repoRoot, path, branch)
}

func defaultVerifyTaskWorktreeWritable(path string) error {
	path = filepath.Clean(strings.TrimSpace(path))
	if path == "" {
		return fmt.Errorf("task worktree path required")
	}
	if !worktreePathLooksSafe(path) {
		return fmt.Errorf("unsafe task worktree path %q", path)
	}
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat task worktree: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("task worktree is not a directory: %q", path)
	}
	probe, err := os.CreateTemp(path, ".wuphf-write-probe-*")
	if err != nil {
		return fmt.Errorf("write probe failed: %w", err)
	}
	probePath := probe.Name()
	if closeErr := probe.Close(); closeErr != nil {
		_ = os.Remove(probePath)
		return fmt.Errorf("close write probe: %w", closeErr)
	}
	if err := os.Remove(probePath); err != nil {
		return fmt.Errorf("cleanup write probe: %w", err)
	}
	return nil
}

func cleanupTaskWorktreeAtRepoRoot(repoRoot, path, branch string) error {
	var failures []string
	if strings.TrimSpace(path) != "" {
		if err := runGit(repoRoot, "worktree", "remove", "--force", path); err != nil {
			if _, statErr := os.Stat(path); statErr == nil {
				if worktreePathLooksSafe(path) {
					if rmErr := os.RemoveAll(path); rmErr != nil {
						failures = append(failures, rmErr.Error())
					}
				} else {
					failures = append(failures, err.Error())
				}
			}
		}
	}
	if strings.TrimSpace(branch) != "" {
		if gitRefExists(repoRoot, "refs/heads/"+branch) {
			if err := runGit(repoRoot, "branch", "-D", branch); err != nil {
				failures = append(failures, err.Error())
			}
		}
	}
	if len(failures) > 0 {
		return fmt.Errorf("%s", strings.Join(failures, "; "))
	}
	return nil
}

func gitRepoRoot() (string, error) {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	cmd.Env = gitexec.CleanEnv()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("resolve repo root: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(stdout.String()), nil
}

// GitCleanEnv is a thin backwards-compatibility shim that delegates to
// gitexec.CleanEnv. The canonical implementation (and the full godoc
// describing the GIT_DIR / GIT_CONFIG_* strip policy) now lives in
// internal/gitexec. Kept exported for one release as a safety net for any
// out-of-tree callers; new code should import gitexec directly.
//
// Deprecated: use gitexec.CleanEnv directly.
func GitCleanEnv() []string {
	return gitexec.CleanEnv()
}

func defaultTaskWorktreeRootDir(repoRoot string) string {
	repoToken := sanitizeWorktreeToken(filepath.Base(strings.TrimSpace(repoRoot)))
	if repoToken == "" {
		repoToken = "workspace"
	}

	if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
		return filepath.Join(home, ".wuphf", "task-worktrees", repoToken)
	}
	return filepath.Join(os.TempDir(), "wuphf-task-worktrees", repoToken)
}

func runGit(dir string, args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = gitexec.CleanEnv()
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

func runGitOutput(dir string, args ...string) ([]byte, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = gitexec.CleanEnv()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

func overlaySourceWorkspace(repoRoot, worktreePath string) error {
	return overlayWorkspaceChanges(repoRoot, worktreePath)
}

func overlayWorkspaceChanges(sourceRoot, worktreePath string) error {
	changed, err := runGitOutput(sourceRoot, "diff", "--name-only", "-z", "HEAD", "--")
	if err != nil {
		return err
	}
	untracked, err := runGitOutput(sourceRoot, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return err
	}
	seen := map[string]struct{}{}
	for _, raw := range append(bytes.Split(changed, []byte{0}), bytes.Split(untracked, []byte{0})...) {
		rel := strings.TrimSpace(string(raw))
		if rel == "" {
			continue
		}
		if !shouldOverlaySourceWorkspacePath(rel) {
			continue
		}
		if _, ok := seen[rel]; ok {
			continue
		}
		seen[rel] = struct{}{}
		src := filepath.Join(sourceRoot, filepath.FromSlash(rel))
		dst := filepath.Join(worktreePath, filepath.FromSlash(rel))
		info, statErr := os.Lstat(src)
		if statErr != nil {
			if os.IsNotExist(statErr) {
				if err := os.RemoveAll(dst); err != nil && !os.IsNotExist(err) {
					return err
				}
				continue
			}
			return statErr
		}
		if err := copyWorkspacePath(src, dst, info); err != nil {
			return err
		}
	}
	return nil
}

func overlayPersistedTaskWorktrees(worktreePath string, currentTaskID string) error {
	raw, err := os.ReadFile(brokerStatePath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	var state struct {
		Tasks []teamTask `json:"tasks"`
	}
	if err := json.Unmarshal(raw, &state); err != nil {
		return err
	}

	seenSources := make(map[string]struct{})
	for _, task := range state.Tasks {
		if strings.TrimSpace(task.ID) == strings.TrimSpace(currentTaskID) {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(task.ExecutionMode), "local_worktree") {
			continue
		}
		status := strings.ToLower(strings.TrimSpace(task.Status))
		if status != "done" && status != "review" {
			continue
		}
		sourcePath := strings.TrimSpace(task.WorktreePath)
		if sourcePath == "" {
			continue
		}
		if sameCleanPath(sourcePath, worktreePath) {
			continue
		}
		sourceKey := filepath.Clean(sourcePath)
		if _, seen := seenSources[sourceKey]; seen {
			continue
		}
		seenSources[sourceKey] = struct{}{}
		if !taskWorktreeSourceLooksUsable(sourcePath) {
			continue
		}
		if err := overlayWorkspaceChanges(sourcePath, worktreePath); err != nil {
			return fmt.Errorf("%s: %w", task.ID, err)
		}
	}
	return nil
}

func taskWorktreeSourceLooksUsable(path string) bool {
	path = filepath.Clean(strings.TrimSpace(path))
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return false
	}
	out, err := runGitOutput(path, "rev-parse", "--is-inside-work-tree")
	if err != nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(string(out)), "true")
}

func shouldOverlaySourceWorkspacePath(rel string) bool {
	rel = filepath.ToSlash(strings.TrimSpace(rel))
	if rel == "" {
		return false
	}
	if _, skip := overlaySourceWorkspaceSkipExact[rel]; skip {
		return false
	}
	for _, prefix := range overlaySourceWorkspaceSkipPrefixes {
		if strings.HasPrefix(rel, prefix) {
			return false
		}
	}
	return true
}

func copyWorkspacePath(src, dst string, info os.FileInfo) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(src)
		if err != nil {
			return err
		}
		_ = os.RemoveAll(dst)
		return os.Symlink(target, dst)
	}
	if !info.Mode().IsRegular() {
		return nil
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.WriteFile(dst, data, info.Mode().Perm()); err != nil {
		return err
	}
	return nil
}

func gitRefExists(dir, ref string) bool {
	cmd := exec.Command("git", "show-ref", "--verify", "--quiet", ref)
	cmd.Dir = dir
	cmd.Env = gitexec.CleanEnv()
	return cmd.Run() == nil
}

func worktreeBranchName(taskID string) string {
	repoRoot, err := gitRepoRoot()
	if err != nil {
		return "wuphf-" + sanitizeWorktreeToken(taskID)
	}
	return worktreeBranchNameForRepo(taskID, repoRoot)
}

func worktreeBranchNameForRepo(taskID string, repoRoot string) string {
	taskToken := sanitizeWorktreeToken(taskID)
	if taskToken == "" {
		taskToken = "task"
	}
	if namespace := worktreeNamespaceToken(repoRoot); namespace != "" {
		return "wuphf-" + namespace + "-" + taskToken
	}
	return "wuphf-" + taskToken
}

func worktreeNamespaceToken(repoRoot string) string {
	root := strings.TrimSpace(taskWorktreeRootDir(repoRoot))
	if root == "" {
		root = strings.TrimSpace(repoRoot)
	}
	if root == "" {
		return ""
	}
	sum := fnv.New32a()
	_, _ = sum.Write([]byte(filepath.Clean(root)))
	return fmt.Sprintf("%08x", sum.Sum32())
}

func CleanupPersistedTaskWorktrees() error {
	path := brokerStatePath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	var state struct {
		Tasks []teamTask `json:"tasks"`
	}
	if err := json.Unmarshal(data, &state); err != nil {
		return err
	}

	seen := make(map[string]struct{})
	var firstErr error
	for _, task := range state.Tasks {
		worktreePath := strings.TrimSpace(task.WorktreePath)
		worktreeBranch := strings.TrimSpace(task.WorktreeBranch)
		if worktreePath == "" && worktreeBranch == "" {
			continue
		}
		key := worktreePath + "\x00" + worktreeBranch
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		if err := cleanupTaskWorktree(worktreePath, worktreeBranch); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func worktreePathLooksSafe(path string) bool {
	path = filepath.Clean(strings.TrimSpace(path))
	if path == "" {
		return false
	}
	if !strings.Contains(filepath.Base(path), "wuphf-task-") {
		return false
	}
	for _, root := range managedWorktreeRoots() {
		if pathWithinRoot(path, root) {
			return true
		}
	}
	tempRoot := filepath.Clean(os.TempDir())
	return pathWithinRoot(path, tempRoot)
}

func managedWorktreeRoots() []string {
	roots := make([]string, 0, 2)
	if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
		roots = append(roots, filepath.Join(home, ".wuphf", "task-worktrees"))
	}
	roots = append(roots, filepath.Join(os.TempDir(), "wuphf-task-worktrees"))
	return roots
}

func pathWithinRoot(path string, root string) bool {
	path = filepath.Clean(strings.TrimSpace(path))
	root = filepath.Clean(strings.TrimSpace(root))
	if path == "" || root == "" || root == "." {
		return false
	}
	return path == root || strings.HasPrefix(path, root+string(os.PathSeparator))
}

func sameCleanPath(a string, b string) bool {
	a = filepath.Clean(strings.TrimSpace(a))
	b = filepath.Clean(strings.TrimSpace(b))
	if a == "" || b == "" {
		return false
	}
	return a == b
}

func sanitizeWorktreeToken(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return "task"
	}
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	return strings.Trim(strings.ReplaceAll(b.String(), "--", "-"), "-")
}
