//go:build darwin || linux

package workspaces

import "syscall"

// sendSIGTERM sends SIGTERM to pid. Errors are ignored — the caller has a
// SIGKILL escalation path on the same wall-clock budget.
func sendSIGTERM(pid int) {
	if pid <= 0 {
		return
	}
	_ = syscall.Kill(pid, syscall.SIGTERM)
}

// sendSIGKILL sends SIGKILL to pid. Best-effort; errors ignored.
func sendSIGKILL(pid int) {
	if pid <= 0 {
		return
	}
	_ = syscall.Kill(pid, syscall.SIGKILL)
}
