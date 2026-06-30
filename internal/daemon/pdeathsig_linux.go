//go:build linux

package daemon

import (
	"os/exec"
	"syscall"
)

func applyChildAttrs(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Pdeathsig: syscall.SIGTERM,
	}
}
