//go:build linux

package tunnel

import (
	"os/exec"
	"syscall"
)

func applyChildAttrs(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Pdeathsig: syscall.SIGTERM,
	}
}
