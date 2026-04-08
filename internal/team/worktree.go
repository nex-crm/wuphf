package team

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

var prepareTaskWorktree = defaultPrepareTaskWorktree
var cleanupTaskWorktree = defaultCleanupTaskWorktree

func defaultPrepareTaskWorktree(taskID string) (string, string, error) {
	repoRoot, err := gitRepoRoot()
	if err != nil {
		return "", "", err
	}

	branch := worktreeBranchName(taskID)
	path := filepath.Join(os.TempDir(), "wuphf-task-"+sanitizeWorktreeToken(taskID))
	if _, err := os.Stat(path); err == nil {
		return path, branch, nil
	}

	if err := runGit(repoRoot, "worktree", "add", "-b", branch, path, "HEAD"); err == nil {
		return path, branch, nil
	}
	if err := runGit(repoRoot, "worktree", "add", path, branch); err == nil {
		return path, branch, nil
	}

	return "", "", fmt.Errorf("create git worktree for %s", taskID)
}

func defaultCleanupTaskWorktree(path, branch string) error {
	repoRoot, err := gitRepoRoot()
	if err != nil {
		return err
	}

	if strings.TrimSpace(path) != "" {
		if err := runGit(repoRoot, "worktree", "remove", "--force", path); err != nil {
			if _, statErr := os.Stat(path); statErr == nil {
				return err
			}
		}
	}
	if strings.TrimSpace(branch) != "" {
		_ = runGit(repoRoot, "branch", "-D", branch)
	}
	return nil
}

func gitRepoRoot() (string, error) {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("resolve repo root: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(stdout.String()), nil
}

func runGit(dir string, args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

func worktreeBranchName(taskID string) string {
	return "wuphf-" + sanitizeWorktreeToken(taskID)
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
