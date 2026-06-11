package team

// headless_workspace.go — working-directory resolution for headless agent
// turns (ICP-eval v3 fix family #3, V3-N5).
//
// The v3 live run had agents executing in the broker process's launch cwd:
// the founder launched the office from their host git worktree, a chat-turn
// CEO wrote landing/index.html into that repo ([19:53]), and a later
// "revert" ran `git checkout HEAD` there and destroyed the session's
// deliverable ([20:03]). The root cause was the per-runner fallback
// `cmd.Dir = l.cwd` for any turn without a task worktree.
//
// Contract after this file: every HEADLESS turn gets a working directory
// INSIDE the office workspace —
//   - task turns with a worktree run in that worktree (unchanged);
//   - everything else (chat turns, office-mode task turns) runs in a
//     per-agent scratch directory under the runtime home,
//     <WUPHF_RUNTIME_HOME>/.wuphf/agent-scratch/<slug>/, created on demand.
//
// NOTHING falls back to the broker process cwd. The interactive pane/TUI
// path (launcher_boot.go / pane_*) is deliberately NOT covered: there the
// human launched wuphf in their own directory on purpose.

import (
	"os"
	"path/filepath"

	"github.com/nex-crm/wuphf/internal/config"
)

// agentScratchDir returns the per-agent scratch working directory for
// headless turns that have no task worktree, creating it on demand. The
// directory lives under the office runtime home
// (<runtime_home>/.wuphf/agent-scratch/<slug>); when no runtime home
// resolves, it falls back to a tempdir-rooted path — never the broker
// process cwd.
func agentScratchDir(slug string) string {
	token := sanitizeWorktreeToken(slug)
	if token == "" {
		token = "agent"
	}
	root := ""
	if home := config.RuntimeHomeDir(); home != "" {
		root = filepath.Join(home, ".wuphf", "agent-scratch")
	} else {
		root = filepath.Join(os.TempDir(), "wuphf-agent-scratch")
	}
	dir := filepath.Join(root, token)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fallback := filepath.Join(os.TempDir(), "wuphf-agent-scratch", token)
		if fallback != dir {
			if err2 := os.MkdirAll(fallback, 0o755); err2 == nil {
				return fallback
			}
		}
		// Last resort: the system temp dir itself. Still never the
		// broker process cwd.
		return os.TempDir()
	}
	return dir
}

// headlessTurnWorkspace resolves the working directory for a headless turn:
// the task worktree when this turn's task has one (or can prepare one), else
// the agent's scratch dir. The second return reports whether the directory
// is a real task worktree (callers use it to decide whether to advertise
// WUPHF_WORKTREE_PATH to the child process).
func (l *Launcher) headlessTurnWorkspace(slug, taskID string) (string, bool) {
	if dir := normalizeHeadlessWorkspaceDir(l.headlessTaskWorkspaceDir(slug, taskID)); dir != "" {
		return dir, true
	}
	return normalizeHeadlessWorkspaceDir(agentScratchDir(slug)), false
}
