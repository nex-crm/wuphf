//go:build linux

package provider

import (
	"os/exec"
	"syscall"
)

func configureClaudeProcess(cmd *exec.Cmd) {
	// Setsid alone gives us everything we need: a new session, the child as
	// session leader, a fresh process group, and no controlling terminal.
	// Adding Setpgid fails with EPERM (cannot change pgid of a session
	// leader); adding Noctty fails with ENOTTY when stdin is a pipe.
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true,
	}
}
