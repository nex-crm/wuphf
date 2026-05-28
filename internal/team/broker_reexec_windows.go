//go:build windows

package team

import "fmt"

// platformReExecBroker is a no-op on Windows: there is no `execve` equivalent
// that keeps the same PID, and the npm shim parent watches our PID, so
// spawning a detached child and exiting would orphan the new binary and lose
// the user's terminal session.
//
// Returning an error here causes handleWebBrokerRestart to fall back to the
// in-process listener restart, which at least lets the SSE client reconnect.
// The user will need to relaunch the CLI from a terminal for the new binary
// to take effect.
func platformReExecBroker() error {
	return fmt.Errorf("re-exec not supported on windows; relaunch the CLI to pick up the new binary")
}
