//go:build !windows

package team

import (
	"fmt"
	"os"
	"syscall"
)

// platformReExecBroker replaces the running process image with a fresh exec of
// the binary at os.Executable() — picking up whatever `npm install -g
// wuphf@latest` (or any other installer) has written to that path.
//
// Same PID as before, so the npm shim parent (`npm/bin/wuphf.js`) never sees a
// child exit; it just keeps waiting on the same pid which is now running the
// new binary. Go opens listeners with O_CLOEXEC by default, so the old TCP
// listener is closed by the kernel during exec — the new binary re-binds it
// when its broker comes up.
//
// On success this function does not return.
func platformReExecBroker() error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable: %w", err)
	}
	argv := append([]string(nil), os.Args...)
	if len(argv) == 0 {
		argv = []string{exe}
	} else {
		argv[0] = exe
	}
	if err := syscall.Exec(exe, argv, os.Environ()); err != nil {
		return fmt.Errorf("exec %s: %w", exe, err)
	}
	// Unreachable: syscall.Exec only returns on failure.
	return nil
}
