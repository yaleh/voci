//go:build e2e

package daemon

import (
	"context"
	"fmt"
	"os/exec"
	"syscall"
	"testing"
	"time"
)

// TestE2E_ContextCancel_KillsChild verifies that cancelling the context passed to
// exec.CommandContext causes the child process to be killed.
func TestE2E_ContextCancel_KillsChild(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	cmd := exec.CommandContext(ctx, "sleep", "60")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start sleep: %v", err)
	}

	// Cancel context — exec.CommandContext sends SIGKILL to the child.
	cancel()

	// Wait for the process to be reaped (cmd.Wait returns when the process exits
	// and its resources are released). Zombie processes are NOT reaped yet so
	// kill -0 returns success — use Wait instead.
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	select {
	case <-done:
		// Process reaped — success.
	case <-time.After(2 * time.Second):
		t.Errorf("child process still alive 2s after context cancel")
		cmd.Process.Kill()
	}
}

// TestE2E_Pdeathsig_KillsChildOnParentExit verifies that when a parent process
// exits, a child started with Pdeathsig=SIGTERM is also killed (Linux only).
func TestE2E_Pdeathsig_KillsChildOnParentExit(t *testing.T) {
	// Start a helper: sh -c "sleep 60 & echo $! ; wait"
	// The helper starts sleep as its child, prints sleep's PID, then waits.
	// We kill the helper; Pdeathsig causes sleep to receive SIGTERM.
	helper := exec.Command("sh", "-c", "sleep 60 & echo $!; wait")
	applyChildAttrs(helper)
	out, err := helper.StdoutPipe()
	if err != nil {
		t.Fatalf("StdoutPipe: %v", err)
	}
	if err := helper.Start(); err != nil {
		t.Fatalf("start helper: %v", err)
	}

	// Read the grandchild PID from helper stdout.
	var grandchildPID int
	if _, err := fmt.Fscan(out, &grandchildPID); err != nil {
		helper.Process.Kill()
		t.Fatalf("read grandchild PID: %v", err)
	}

	// Kill the helper (parent). Pdeathsig sends SIGTERM to grandchild.
	if err := helper.Process.Kill(); err != nil {
		t.Fatalf("kill helper: %v", err)
	}

	// Verify grandchild dies within 3s.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(grandchildPID, 0); err != nil {
			return // grandchild gone — success
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Errorf("grandchild process %d still alive 3s after parent killed", grandchildPID)
	syscall.Kill(grandchildPID, syscall.SIGKILL)
}
