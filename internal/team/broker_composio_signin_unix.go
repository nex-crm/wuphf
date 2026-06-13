//go:build !windows

package team

import (
	"context"
	"os/exec"
)

// defaultComposioInstaller runs the official Composio installer, which is a
// `curl | bash` pipeline. This lives in a Unix-only file because it shells out
// to bash, which does not exist on Windows — the Windows build supplies a stub
// (broker_composio_signin_windows.go) that reports not-supported so the sign-in
// flow falls back to surfacing the manual install command. It runs ONLY after
// the human explicitly chose "Sign in with Composio".
func defaultComposioInstaller(ctx context.Context) error {
	return exec.CommandContext(ctx, "bash", "-c", composioInstallCommand).Run()
}
