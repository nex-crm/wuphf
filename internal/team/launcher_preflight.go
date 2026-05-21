package team

// launcher_preflight.go owns the pre-launch capability check
// (PLAN.md §C17). Preflight verifies that the runtime binaries the
// active provider needs are present on PATH (claude / codex /
// opencode) and surfaces a one-line gh-cli installation note when
// applicable. PreflightWeb (in launcher_web.go) is the
// browser-mode equivalent that runs in the same shape but skips
// tmux/auth checks during onboarding.

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/nex-crm/wuphf/internal/runtimebin"
)

// Preflight checks that required tools are available.
//
// The gh capability advisory runs after every successful runtime check
// (codex, opencode, claude+tmux), not just the claude branch. Pre-fix,
// codex/opencode launches missed the "gh CLI not found / not authed"
// note, leaving operators puzzled when their agents couldn't open PRs.
func (l *Launcher) Preflight() error {
	if l.usesCodexRuntime() {
		if l.usesOpencodeRuntime() {
			if _, err := runtimebin.LookPath("opencode"); err != nil {
				return fmt.Errorf("opencode not found. Install Opencode CLI (https://opencode.ai) and configure your provider credentials")
			}
			emitGHCapabilityNote()
			return nil
		}
		if _, err := exec.LookPath("codex"); err != nil {
			return fmt.Errorf("codex not found. Install Codex CLI and run `codex login`")
		}
		emitGHCapabilityNote()
		return nil
	}
	if _, err := exec.LookPath("tmux"); err != nil {
		return fmt.Errorf("tmux not found. Install: brew install tmux")
	}
	if _, err := exec.LookPath("claude"); err != nil {
		return fmt.Errorf("claude not found. Install: npm install -g @anthropic-ai/claude-code")
	}
	emitGHCapabilityNote()
	return nil
}

// emitGHCapabilityNote prints checkGHCapability's advisory to stderr
// when one is set. Wrapped because the same emit pattern fires from
// every runtime branch in Preflight.
func emitGHCapabilityNote() {
	if _, _, note := checkGHCapability(); note != "" {
		fmt.Fprintf(os.Stderr, "note: %s\n", note)
	}
}

// checkGHCapability checks whether the gh CLI is installed and authenticated.
// It returns a soft-warning note when either condition is not met; callers
// should print the note but must NOT treat it as a fatal error — agents can
// still work locally without gh. Only PR-opening will be unavailable.
//
// Authentication is probed with `gh auth token`, not `gh auth status`. The
// former is cheap, deterministic, and exits non-zero with an unambiguous
// "no oauth token found" only when the user is truly unauthenticated.
// `gh auth status` was previously used but its exit code conflates a clean
// unauth state with transient failures (keychain prompts, timeout under load,
// hostname-list quirks where one host is authed and another is not). The
// effect (#942 B-12): the warning fired on every boot for users with a valid
// gho_* token, because gh auth status occasionally failed for unrelated
// reasons inside the launcher's subprocess context. `gh auth token` returns
// the active host's token straight from the config and skips that ambiguity.
//
// A 5s deadline is generous for the happy path; on timeout we suppress the
// warning entirely rather than print a misleading "not authenticated" note.
// A real unauth state surfaces immediately when an agent actually tries to
// open a PR — printing a false positive on every boot is worse than missing
// a true positive on a flaky network.
func checkGHCapability() (installed bool, authed bool, note string) {
	if _, err := exec.LookPath("gh"); err != nil {
		return false, false, "gh CLI not found in PATH; agents won't be able to open real PRs. Install from https://cli.github.com."
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "gh", "auth", "token")
	out, err := cmd.CombinedOutput()
	if err == nil && len(strings.TrimSpace(string(out))) > 0 {
		return true, true, ""
	}
	if ctx.Err() != nil {
		// Timeout: don't claim unauth — it might be a stalled keychain prompt
		// on a perfectly authenticated machine. Treat as "we don't know".
		return true, false, ""
	}
	// gh prints messages like "no oauth token" / "you are not logged into any
	// hosts" to stderr and exits non-zero. Anything else (binary corrupted,
	// transient failure) we also surface, because gh auth token's
	// non-timeout failure space is narrow.
	return true, false, "gh installed but not authenticated; run `gh auth login` so agents can open real PRs."
}
