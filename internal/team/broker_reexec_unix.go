//go:build !windows

package team

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

// platformReExecBroker replaces the running process image with a fresh exec of
// the binary that lives at the path we were launched from — picking up
// whatever `npm install -g wuphf@latest` (or any other installer) has just
// written there.
//
// Same PID as before, so the npm shim parent (`npm/bin/wuphf.js`) never sees a
// child exit; it just keeps waiting on the same pid which is now running the
// new binary. Go opens listeners with O_CLOEXEC by default, so the old TCP
// listener is closed by the kernel during exec — the new binary re-binds it
// when its broker comes up.
//
// We deliberately do NOT use os.Executable() here. On Linux it resolves
// /proc/self/exe, which is bound to the inode of the running binary; after
// npm's typical rename-based replacement the original inode is unlinked and
// the readlink returns a path with a " (deleted)" suffix that no longer
// exists on disk. Resolving from os.Args[0] via exec.LookPath gives us the
// path npm/yarn/bun actually wrote the new binary to, which is what we want
// to exec.
//
// On success this function does not return.
func platformReExecBroker() error {
	if len(os.Args) == 0 || os.Args[0] == "" {
		return errors.New("resolve executable: argv[0] is empty")
	}
	exe, err := exec.LookPath(os.Args[0])
	if err != nil {
		return fmt.Errorf("resolve executable from argv[0]: %w", err)
	}
	exe, err = filepath.Abs(exe)
	if err != nil {
		return fmt.Errorf("resolve absolute executable path: %w", err)
	}
	argv := append([]string(nil), os.Args...)
	argv[0] = exe
	if err := syscall.Exec(exe, argv, os.Environ()); err != nil {
		return fmt.Errorf("exec %s: %w", exe, err)
	}
	// Unreachable: syscall.Exec only returns on failure.
	return nil
}
