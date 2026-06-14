//go:build !windows

package team

import (
	"context"
	"os/exec"
)

// verificationShellCommand builds the platform shell invocation for a
// command-kind definition-of-done check (task_verification.go).
func verificationShellCommand(ctx context.Context, spec string) *exec.Cmd {
	return exec.CommandContext(ctx, "sh", "-c", spec)
}
