package channelui

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
)

// IsDarwin reports whether the current build is running on macOS.
func IsDarwin() bool { return runtime.GOOS == "darwin" }

// IsLinux reports whether the current build is running on Linux.
func IsLinux() bool { return runtime.GOOS == "linux" }

// IsWindows reports whether the current build is running on Windows.
func IsWindows() bool { return runtime.GOOS == "windows" }

// OpenBrowserURL spawns a detached OS helper to open url in the
// default browser. We use context.Background() because the child must
// outlive this call — the user keeps interacting with the browser
// long after we return — and noctx's "use CommandContext"
// recommendation is satisfied by the background ctx. Cancellation
// isn't meaningful for a fire-and-forget open-in-browser handoff.
// Empty url is a no-op. Returns an error on unsupported platforms or
// if the helper fails to start.
//
// A detached goroutine reaps the child via cmd.Wait() so its OS
// resources (pipes, zombie process slot) are released once the helper
// exits. The reap goroutine intentionally discards Wait's error — the
// helper's exit status doesn't change anything the caller can act on.
func OpenBrowserURL(url string) error {
	ctx := context.Background()
	var cmd *exec.Cmd
	switch {
	case url == "":
		return nil
	case IsDarwin():
		cmd = exec.CommandContext(ctx, "open", url)
	case IsLinux():
		cmd = exec.CommandContext(ctx, "xdg-open", url)
	case IsWindows():
		cmd = exec.CommandContext(ctx, "cmd", "/c", "start", "", url)
	default:
		return fmt.Errorf("unsupported platform")
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
