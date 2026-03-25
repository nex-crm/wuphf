//go:build linux

package provider

import (
	"os/exec"
	"syscall"
)

func configureClaudeProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid:  true,
		Setpgid: true,
		Noctty:  true,
	}
}
