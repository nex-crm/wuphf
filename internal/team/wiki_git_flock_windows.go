//go:build windows

package team

import "os"

// TODO(wiki): Windows port should implement LockFileEx via golang.org/x/sys/windows
// when Obsidian-on-Windows compatibility is in scope.
func acquireArticleLock(fullPath string) (*os.File, error) { return nil, nil }

func releaseArticleLock(f *os.File) {
	if f != nil {
		_ = f.Close()
	}
}
