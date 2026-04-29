package team

// tmux_runner.go owns the tmux command seam (PLAN.md §3) used by
// paneLifecycle and (in a follow-up) paneDispatcher to invoke the local
// tmux binary. The interface is narrow on purpose — three call shapes
// (fire-and-forget, stdout-only, combined-stdout-and-stderr) cover every
// existing caller in launcher.go without copying flags around. Production
// uses realTmuxRunner, which hardcodes the `-L tmuxSocketName` prefix and
// exec.CommandContext(context.Background()) so callers don't have to.
//
// The override pattern follows the existing
// launcherSendNotificationToPaneOverride seam: an atomic.Pointer that
// tests load through setTmuxRunnerForTest. Production never touches it,
// so the read on the hot path is a single nil check.

import (
	"context"
	"os/exec"
	"sync/atomic"
	"testing"
)

// tmuxRunner is the seam for invoking the local tmux binary. Every method
// receives the post-`-L socketname` arguments — the runner is responsible
// for prepending the socket prefix.
type tmuxRunner interface {
	// Run invokes tmux fire-and-forget; the return value is the exec error
	// (nil on success). Stdout/stderr are discarded.
	Run(args ...string) error
	// Output captures stdout only; stderr is discarded. Mirrors
	// exec.Cmd.Output's contract.
	Output(args ...string) ([]byte, error)
	// Combined captures stdout and stderr together. Mirrors
	// exec.Cmd.CombinedOutput's contract — used by callers that surface
	// tmux error text from stderr in their own error messages.
	Combined(args ...string) ([]byte, error)
}

// realTmuxRunner is the production implementation. It prepends
// `-L tmuxSocketName` to every invocation and uses
// exec.CommandContext(context.Background()) — matching what every
// existing call site in launcher.go did before extraction.
type realTmuxRunner struct{}

func (realTmuxRunner) cmd(args ...string) *exec.Cmd {
	full := make([]string, 0, len(args)+2)
	full = append(full, "-L", tmuxSocketName)
	full = append(full, args...)
	return exec.CommandContext(context.Background(), "tmux", full...)
}

func (r realTmuxRunner) Run(args ...string) error {
	return r.cmd(args...).Run()
}

func (r realTmuxRunner) Output(args ...string) ([]byte, error) {
	return r.cmd(args...).Output()
}

func (r realTmuxRunner) Combined(args ...string) ([]byte, error) {
	return r.cmd(args...).CombinedOutput()
}

// tmuxRunnerOverride is the test seam. Production code never writes to
// it; tests install a fake via setTmuxRunnerForTest and rely on
// t.Cleanup to restore the prior value. The pointer-of-pointer dance
// matches the existing launcherSendNotificationToPaneOverride pattern
// (PLAN.md §3) so the existing test conventions transfer wholesale.
var tmuxRunnerOverride atomic.Pointer[tmuxRunner]

// newTmuxRunner returns the override if one is installed, otherwise the
// production realTmuxRunner. Called from constructors at type-creation
// time, not on the hot path.
func newTmuxRunner() tmuxRunner {
	if p := tmuxRunnerOverride.Load(); p != nil {
		return *p
	}
	return realTmuxRunner{}
}

// setTmuxRunnerForTest installs r as the package-level tmux runner for
// the duration of the test, restoring the prior value via t.Cleanup. As
// with every other *Override seam in this package: do not combine with
// t.Parallel() — the override is package-global. PLAN.md §5 trap 8
// covers the rationale for keeping it global rather than per-Launcher.
func setTmuxRunnerForTest(t *testing.T, r tmuxRunner) {
	t.Helper()
	prior := tmuxRunnerOverride.Load()
	tmuxRunnerOverride.Store(&r)
	t.Cleanup(func() { tmuxRunnerOverride.Store(prior) })
}
