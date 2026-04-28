//go:build !windows

package agent

import (
	"context"
	"os/exec"
)

// newShellCommand builds the bash-tool subprocess on Unix-likes. /bin/sh -lc
// loads the user's login-profile env (PATH, NVM, asdf shims) so agent shell
// commands behave like an interactive shell session. The Windows variant in
// tools_shell_windows.go uses cmd.exe instead.
func newShellCommand(ctx context.Context, command string) *exec.Cmd {
	return exec.CommandContext(ctx, "/bin/sh", "-lc", command)
}
