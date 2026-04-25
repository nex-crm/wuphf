package team

import (
	"os"
	"strings"
	"testing"
)

// TestClaudeCommand_UsesFileForSystemPrompt is a regression test for the tmux
// "command too long" failure introduced in PR #139. The old implementation
// inlined the full system prompt (5-10KB) in the shell command passed to tmux
// split-window. Tmux has an internal command-parse buffer; oversize args are
// rejected before sh -c ever runs.
//
// The fix writes the prompt to a temp file and uses Claude Code's native
// --append-system-prompt-file flag. This keeps the tmux command bounded.
func TestClaudeCommand_UsesFileForSystemPrompt(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	// Pair HOME with WUPHF_RUNTIME_HOME so resetManifestToPack writes into
	// this test's tmpdir, not the process-level leaked runtime home from
	// worktree_guard_test's init.
	t.Setenv("WUPHF_RUNTIME_HOME", home)
	t.Setenv("WUPHF_START_FROM_SCRATCH", "1")

	l, err := NewLauncher("from-scratch")
	if err != nil {
		t.Fatalf("NewLauncher: %v", err)
	}

	members := l.officeMembersSnapshot()
	if len(members) == 0 {
		t.Fatal("expected office members, got none")
	}
	slug := members[0].Slug

	cmd, err := l.claudeCommand(slug, l.buildPrompt(slug))
	if err != nil {
		t.Fatalf("claudeCommand: %v", err)
	}

	if !strings.Contains(cmd, "--append-system-prompt-file ") {
		t.Errorf("command should use --append-system-prompt-file, got:\n%s", cmd)
	}
	if strings.Contains(cmd, "--append-system-prompt '") {
		t.Errorf("command should not inline --append-system-prompt '...' (triggers tmux command-too-long), got:\n%s", cmd)
	}
}

// TestClaudeCommand_StaysUnderTmuxLimit is the direct regression test for the
// tmux "command too long" bug from PR #139. The test constructs the command
// string for every visible office member and asserts the length is comfortably
// below any plausible tmux command-parse buffer. 4096 bytes is a safety margin
// well below historical tmux limits. If this test fails, pane-backed spawn
// will regress and the opt-in WUPHF_AGENT_MODE=panes path falls back to the
// default headless claude --print dispatch.
func TestClaudeCommand_StaysUnderTmuxLimit(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	// Pair HOME with WUPHF_RUNTIME_HOME so resetManifestToPack writes into
	// this test's tmpdir, not the process-level leaked runtime home from
	// worktree_guard_test's init.
	t.Setenv("WUPHF_RUNTIME_HOME", home)
	t.Setenv("WUPHF_START_FROM_SCRATCH", "1")

	l, err := NewLauncher("from-scratch")
	if err != nil {
		t.Fatalf("NewLauncher: %v", err)
	}

	const tmuxSafetyLimit = 4096
	members := l.officeMembersSnapshot()
	if len(members) == 0 {
		t.Fatal("expected office members, got none")
	}
	for _, member := range members {
		cmd, err := l.claudeCommand(member.Slug, l.buildPrompt(member.Slug))
		if err != nil {
			t.Fatalf("claudeCommand(%s): %v", member.Slug, err)
		}
		if got := len(cmd); got >= tmuxSafetyLimit {
			t.Errorf("command for %s is %d bytes, want < %d (tmux command-parse buffer)", member.Slug, got, tmuxSafetyLimit)
		}
	}
}

// TestClaudeCommand_WritesPromptFileWithCorrectContent verifies that the
// prompt file referenced by --append-system-prompt-file contains the full
// buildPrompt output with the correct permissions. If the file is missing,
// empty, or truncated, the agent launches with no team context and acts
// off-script — a silent correctness regression that is worse than the tmux
// command-too-long failure (which at least fails loudly).
func TestClaudeCommand_WritesPromptFileWithCorrectContent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	// Pair HOME with WUPHF_RUNTIME_HOME so resetManifestToPack writes into
	// this test's tmpdir, not the process-level leaked runtime home from
	// worktree_guard_test's init.
	t.Setenv("WUPHF_RUNTIME_HOME", home)
	t.Setenv("WUPHF_START_FROM_SCRATCH", "1")

	l, err := NewLauncher("from-scratch")
	if err != nil {
		t.Fatalf("NewLauncher: %v", err)
	}

	members := l.officeMembersSnapshot()
	if len(members) == 0 {
		t.Fatal("expected office members, got none")
	}
	slug := members[0].Slug
	prompt := l.buildPrompt(slug)

	cmd, err := l.claudeCommand(slug, prompt)
	if err != nil {
		t.Fatalf("claudeCommand: %v", err)
	}

	// Extract file path: --append-system-prompt-file '<path>' or --append-system-prompt-file <path>
	const marker = "--append-system-prompt-file "
	idx := strings.Index(cmd, marker)
	if idx < 0 {
		t.Fatalf("no %q in command:\n%s", marker, cmd)
	}
	rest := cmd[idx+len(marker):]
	// Path may be single-quoted for shell safety; tolerate both forms.
	var path string
	if strings.HasPrefix(rest, "'") {
		end := strings.Index(rest[1:], "'")
		if end < 0 {
			t.Fatalf("unterminated quoted path in command:\n%s", cmd)
		}
		path = rest[1 : 1+end]
	} else {
		path = strings.SplitN(rest, " ", 2)[0]
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read prompt file %q: %v", path, err)
	}
	if string(data) != prompt {
		t.Errorf("prompt file content differs from buildPrompt output\nfile: %d bytes\nprompt: %d bytes", len(data), len(prompt))
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %q: %v", path, err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("prompt file perms = %v, want 0600", got)
	}
}

// TestClaudeCommand_ErrorSurfaces verifies that a write failure in the prompt
// file path propagates as an error rather than silently launching an agent
// with no system prompt. Reproduce the failure by setting TMPDIR to a path
// that is not writable for our user.
func TestClaudeCommand_ErrorSurfaces(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	// Pair HOME with WUPHF_RUNTIME_HOME so resetManifestToPack writes into
	// this test's tmpdir, not the process-level leaked runtime home from
	// worktree_guard_test's init.
	t.Setenv("WUPHF_RUNTIME_HOME", home)
	t.Setenv("WUPHF_START_FROM_SCRATCH", "1")

	l, err := NewLauncher("from-scratch")
	if err != nil {
		t.Fatalf("NewLauncher: %v", err)
	}

	// Read-only temp dir forces os.WriteFile to fail.
	ro := t.TempDir()
	if err := os.Chmod(ro, 0o500); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(ro, 0o700) })
	t.Setenv("TMPDIR", ro)

	members := l.officeMembersSnapshot()
	if len(members) == 0 {
		t.Fatal("expected office members, got none")
	}
	slug := members[0].Slug

	_, err = l.claudeCommand(slug, l.buildPrompt(slug))
	if err == nil {
		t.Fatal("expected error when prompt file write fails, got nil")
	}
}

// TestPaneFallbackMessages_TmuxMissingVsSpawnFailure verifies the fallback
// message distinguishes between two visibly different failures:
//
//   - tmux is not installed (correct advice: install tmux)
//   - tmux IS installed but rejected the command (must NOT tell the user to
//     install tmux; that advice is wrong and confusing)
//
// Before the fix, both cases emitted identical "Install tmux" advice.
func TestPaneFallbackMessages_TmuxMissingVsSpawnFailure(t *testing.T) {
	missingStderr, missingBroker := paneFallbackMessages(false, "tmux not found on PATH")
	if !strings.Contains(strings.ToLower(missingStderr), "install tmux") {
		t.Errorf("tmux-missing stderr should mention installing tmux, got:\n%s", missingStderr)
	}
	if !strings.Contains(strings.ToLower(missingBroker), "install tmux") {
		t.Errorf("tmux-missing broker message should mention installing tmux, got:\n%s", missingBroker)
	}

	rejectedStderr, rejectedBroker := paneFallbackMessages(true, "spawn visible agents failed: tmux: command too long")
	if strings.Contains(strings.ToLower(rejectedStderr), "install tmux") {
		t.Errorf("tmux-installed-but-rejected stderr must NOT say 'install tmux' (tmux IS installed), got:\n%s", rejectedStderr)
	}
	if strings.Contains(strings.ToLower(rejectedBroker), "install tmux") {
		t.Errorf("tmux-installed-but-rejected broker message must NOT say 'install tmux', got:\n%s", rejectedBroker)
	}
	// And it should still carry the failure detail forward so the user can
	// file a bug with enough info to reproduce.
	if !strings.Contains(rejectedStderr, "spawn visible agents failed") {
		t.Errorf("tmux-rejected stderr should carry failure detail, got:\n%s", rejectedStderr)
	}
}

// TestLauncherShutdown_CleansAgentTempFiles verifies that Launcher.Shutdown()
// removes the per-agent temp files (MCP config + system prompt) written
// during launch. These files contain the broker token and full system prompt
// and should not outlive the session.
func TestLauncherShutdown_CleansAgentTempFiles(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	// Pair HOME with WUPHF_RUNTIME_HOME so resetManifestToPack writes into
	// this test's tmpdir, not the process-level leaked runtime home from
	// worktree_guard_test's init.
	t.Setenv("WUPHF_RUNTIME_HOME", home)
	t.Setenv("WUPHF_START_FROM_SCRATCH", "1")

	l, err := NewLauncher("from-scratch")
	if err != nil {
		t.Fatalf("NewLauncher: %v", err)
	}

	members := l.officeMembersSnapshot()
	if len(members) == 0 {
		t.Fatal("expected office members, got none")
	}

	// Force generation of both temp files per agent.
	var promptFiles, mcpFiles []string
	for _, m := range members {
		slug := m.Slug
		mcpPath, err := l.ensureAgentMCPConfig(slug)
		if err != nil {
			t.Fatalf("ensureAgentMCPConfig(%s): %v", slug, err)
		}
		mcpFiles = append(mcpFiles, mcpPath)

		promptPath, err := l.writeAgentPromptFile(slug, l.buildPrompt(slug))
		if err != nil {
			t.Fatalf("writeAgentPromptFile(%s): %v", slug, err)
		}
		promptFiles = append(promptFiles, promptPath)
	}

	// Sanity: files exist before shutdown.
	for _, p := range append(promptFiles, mcpFiles...) {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("expected file %s before shutdown: %v", p, err)
		}
	}

	l.cleanupAgentTempFiles()

	for _, p := range append(promptFiles, mcpFiles...) {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("expected file %s removed after shutdown, err=%v", p, err)
		}
	}
}
