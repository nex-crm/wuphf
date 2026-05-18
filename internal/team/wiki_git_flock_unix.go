//go:build darwin || linux

package team

import (
	"os"
	"syscall"
)

// acquireArticleLock takes an exclusive POSIX advisory lock on the article
// file so an Obsidian editor (which respects flock on macOS / Linux) cannot
// interleave writes with the worker's atomic commit. The file is opened with
// the same permissions as the existing write path (0o600) and returned to the
// caller so releaseArticleLock can both Flock(LOCK_UN) and Close in defer.
func acquireArticleLock(fullPath string) (*os.File, error) {
	f, err := os.OpenFile(fullPath, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		_ = f.Close()
		return nil, err
	}
	return f, nil
}

func releaseArticleLock(f *os.File) {
	if f == nil {
		return
	}
	_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	_ = f.Close()
}
