//go:build linux

package tunnel

import (
	"os/exec"
	"syscall"
	"testing"
)

func TestPdeathsigSet_applyChildAttrs(t *testing.T) {
	cmd := exec.Command("true")
	applyChildAttrs(cmd)
	if cmd.SysProcAttr == nil {
		t.Fatal("expected SysProcAttr to be set, got nil")
	}
	if cmd.SysProcAttr.Pdeathsig != syscall.SIGTERM {
		t.Errorf("Pdeathsig = %v, want SIGTERM", cmd.SysProcAttr.Pdeathsig)
	}
}
