//go:build e2e

package session

import (
	"os"
	"path/filepath"
	"testing"
)

// TestE2E_MonitorTaskID_FullLifecycle simulates the full voci-listen lifecycle
// for the .task file:
//  1. voci serve starts → WriteLock is called (OnListening)
//  2. skill arms Monitor → WriteMonitorTaskID is called
//  3. reconnectGuard reads both files to verify the session is live
//  4. voci serve exits → RemoveLock is called (defer)
//  5. SweepStaleTaskFiles removes the orphaned .task file
func TestE2E_MonitorTaskID_FullLifecycle(t *testing.T) {
	dir := t.TempDir()
	sessionID := "e2e-sess-" + NewSessionID()
	taskID := "task-e2e-abc123"

	// Step 1: voci serve starts, writes lock (simulated by WriteLock).
	if err := WriteLock(dir, sessionID, os.Getpid(), 55123); err != nil {
		t.Fatalf("WriteLock: %v", err)
	}

	// Step 2: skill writes task ID after Monitor is armed.
	if err := WriteMonitorTaskID(dir, sessionID, taskID); err != nil {
		t.Fatalf("WriteMonitorTaskID: %v", err)
	}

	// Step 3: reconnectGuard reads lock + task file.
	entry, err := ReadLock(dir, sessionID)
	if err != nil {
		t.Fatalf("ReadLock: %v", err)
	}
	if entry.Port != 55123 {
		t.Errorf("lock Port = %d, want 55123", entry.Port)
	}

	gotTaskID, err := ReadMonitorTaskID(dir, sessionID)
	if err != nil {
		t.Fatalf("ReadMonitorTaskID: %v", err)
	}
	if gotTaskID != taskID {
		t.Errorf("ReadMonitorTaskID = %q, want %q", gotTaskID, taskID)
	}

	// Verify .task file is NOT swept while lock exists.
	if err := SweepStaleTaskFiles(dir); err != nil {
		t.Fatalf("SweepStaleTaskFiles (pre-exit): %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, sessionID+".task")); err != nil {
		t.Error(".task file should still exist while .lock is present")
	}

	// Step 4: voci serve exits, removes lock.
	if err := RemoveLock(dir, sessionID); err != nil {
		t.Fatalf("RemoveLock: %v", err)
	}

	// Step 5: SweepStaleTaskFiles removes the now-orphaned .task file.
	if err := SweepStaleTaskFiles(dir); err != nil {
		t.Fatalf("SweepStaleTaskFiles (post-exit): %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, sessionID+".task")); !os.IsNotExist(err) {
		t.Error("orphaned .task file should be removed after lock is gone")
	}
}

// TestE2E_ReconnectGuard_DetectsLiveSession verifies that when a valid .lock
// and .task file exist for a live PID, a second invocation can detect them.
func TestE2E_ReconnectGuard_DetectsLiveSession(t *testing.T) {
	dir := t.TempDir()
	sessionID := "e2e-reconnect-" + NewSessionID()

	// Simulate first invocation: write lock (live PID) + task file.
	if err := WriteLock(dir, sessionID, os.Getpid(), 44321); err != nil {
		t.Fatalf("WriteLock: %v", err)
	}
	if err := WriteMonitorTaskID(dir, sessionID, "task-live-123"); err != nil {
		t.Fatalf("WriteMonitorTaskID: %v", err)
	}

	// Simulate SweepStaleLocks (second invocation startup) — live PID, so lock survives.
	if err := SweepStaleLocks(dir); err != nil {
		t.Fatalf("SweepStaleLocks: %v", err)
	}
	// Simulate SweepStaleTaskFiles — lock exists, so .task survives.
	if err := SweepStaleTaskFiles(dir); err != nil {
		t.Fatalf("SweepStaleTaskFiles: %v", err)
	}

	// reconnectGuard: enumerate .lock files.
	locks, err := filepath.Glob(filepath.Join(dir, "*.lock"))
	if err != nil || len(locks) == 0 {
		t.Fatal("expected at least one .lock file for live session")
	}

	// For each lock, check for .task file.
	found := false
	for _, lf := range locks {
		base := filepath.Base(lf[:len(lf)-5]) // strip .lock
		taskID, taskErr := ReadMonitorTaskID(dir, base)
		if taskErr != nil {
			continue
		}
		// In real skill: call TaskOutput(taskID) to verify Monitor is running.
		// Here we just assert the task ID is readable.
		if taskID == "task-live-123" {
			found = true
		}
		_ = taskID
	}

	if !found {
		t.Error("reconnectGuard simulation: expected to find task-live-123 in .task files")
	}

	// Assert that the entry from the lock has the correct port.
	entry, err := ReadLock(dir, sessionID)
	if err != nil {
		t.Fatalf("ReadLock: %v", err)
	}
	if entry.Port != 44321 {
		t.Errorf("lock Port = %d, want 44321", entry.Port)
	}
}
