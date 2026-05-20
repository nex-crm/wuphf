//go:build darwin || linux

package team

import (
	"os"
	"syscall"
)

// acquireArticleLock takes an exclusive POSIX advisory lock on the article
// file so concurrent WUPHF writers (WikiWorker, ObsidianWatcher, a sibling
// `wuphf` CLI process) cannot interleave writes with this commit. It does
// NOT coordinate with Obsidian's editor — Obsidian writes vault files as
// plain disk I/O without acquiring POSIX advisory locks (by design, to
// coexist with Dropbox / Syncthing / git / external scripts). See
// WIKI-OBSIDIAN-COMPATIBILITY.md §6 for the watcher / debounce / sentinel
// layers that handle Obsidian-side concurrency. The file is opened with the
// same permissions as the existing write path (0o600) and returned to the
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
