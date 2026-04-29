//go:build windows

package workspaces

import (
	"errors"
	"os"
)

// errFileLockUnavailable is returned by lockFileExclusiveNonBlocking on
// Windows in lieu of EWOULDBLOCK semantics. The Windows fallback in this
// package is best-effort: lock helpers no-op so cross-builds compile.
// Multi-broker concurrency on Windows is out of scope for the v1 multi-
// workspace feature; tracked separately.
var errFileLockUnavailable = errors.New("workspaces: file lock not implemented on windows")

// lockFileExclusive is a no-op on Windows. WUPHF runs as a single
// foreground process there for v1; concurrent broker locks land with
// the Windows port follow-up.
func lockFileExclusive(f *os.File) error { return nil }

// lockFileExclusiveNonBlocking always succeeds on Windows; same caveat as
// lockFileExclusive. We keep the function signature so callers can be
// portable without #ifdef-style branching.
func lockFileExclusiveNonBlocking(f *os.File) error { return nil }

// unlockFile is a no-op on Windows — no lock was acquired.
func unlockFile(f *os.File) error { return nil }

var _ = errFileLockUnavailable // reserved for future windows.LockFileEx wiring
