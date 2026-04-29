//go:build windows

package workspaces

import "os"

// sendSIGTERM on Windows falls back to os.Process.Kill since Windows has no
// graceful signal equivalent. The pause/escalation schedule still works —
// SIGTERM-at-60s and SIGKILL-at-75s collapse into the same Kill call.
func sendSIGTERM(pid int) {
	if pid <= 0 {
		return
	}
	if proc, err := os.FindProcess(pid); err == nil {
		_ = proc.Kill()
	}
}

// sendSIGKILL on Windows is os.Process.Kill — same surface as sendSIGTERM.
func sendSIGKILL(pid int) {
	if pid <= 0 {
		return
	}
	if proc, err := os.FindProcess(pid); err == nil {
		_ = proc.Kill()
	}
}
