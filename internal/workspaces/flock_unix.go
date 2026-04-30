//go:build darwin || linux

package workspaces

import (
	"os"
	"syscall"
)

// lockFileExclusive blocks until an exclusive POSIX advisory lock is held on
// f. Caller must release via unlockFile.
func lockFileExclusive(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_EX)
}

// lockFileExclusiveNonBlocking attempts to acquire an exclusive lock without
// blocking. Returns the underlying error (typically EWOULDBLOCK) when the
// lock is already held.
func lockFileExclusiveNonBlocking(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
}

// unlockFile releases the lock acquired by lockFileExclusive[NonBlocking].
func unlockFile(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
}
