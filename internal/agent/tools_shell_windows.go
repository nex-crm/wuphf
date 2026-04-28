package agent

import (
	"context"
	"os/exec"
)

// newShellCommand builds the bash-tool subprocess on Windows. cmd.exe /c is
// the most-portable shell available everywhere (PowerShell would be richer
// but isn't always on PATH on Server SKUs). Syntax obviously differs from
// bash, so agents that rely on POSIX features should use the dedicated
// PowerShell-aware tools rather than the cross-platform `bash` tool.
func newShellCommand(ctx context.Context, command string) *exec.Cmd {
	return exec.CommandContext(ctx, "cmd", "/c", command)
}
