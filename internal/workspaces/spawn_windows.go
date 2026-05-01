//go:build windows

package workspaces

import "errors"

// errSpawnNotSupportedOnWindows is returned by Spawn on Windows. Multi-
// workspace broker spawning relies on POSIX detached process groups
// (Setpgid + flock-coordinated PID files); the Windows port is tracked as
// a follow-up. Returning a typed error lets the orchestrator surface a
// clean message instead of a Windows cross-build failure.
var errSpawnNotSupportedOnWindows = errors.New(
	"workspaces: multi-workspace broker spawning is not yet supported on Windows",
)

// Spawn returns an error on Windows. The package compiles cleanly so other
// orchestrator paths (List, Switch, Pause, Doctor) remain available — but
// Create/Resume that depend on spawning will fail fast with this message
// rather than producing an unbound broker.
func Spawn(name string, runtimeHome string, brokerPort, webPort int) error {
	return errSpawnNotSupportedOnWindows
}
