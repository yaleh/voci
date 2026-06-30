package session

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteMonitorTaskID_CreatesFile(t *testing.T) {
	dir := t.TempDir()
	if err := WriteMonitorTaskID(dir, "sess-abc", "task-xyz"); err != nil {
		t.Fatalf("WriteMonitorTaskID: %v", err)
	}
	path := filepath.Join(dir, "sess-abc.task")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf(".task file not created: %v", err)
	}
}

func TestReadMonitorTaskID_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	if err := WriteMonitorTaskID(dir, "sess-rt", "task-roundtrip"); err != nil {
		t.Fatalf("WriteMonitorTaskID: %v", err)
	}
	got, err := ReadMonitorTaskID(dir, "sess-rt")
	if err != nil {
		t.Fatalf("ReadMonitorTaskID: %v", err)
	}
	if got != "task-roundtrip" {
		t.Errorf("ReadMonitorTaskID = %q, want %q", got, "task-roundtrip")
	}
}

func TestReadMonitorTaskID_Missing_ReturnsError(t *testing.T) {
	dir := t.TempDir()
	_, err := ReadMonitorTaskID(dir, "nonexistent")
	if err == nil {
		t.Error("expected error for missing .task file, got nil")
	}
}

func TestRemoveMonitorTaskID_DeletesFile(t *testing.T) {
	dir := t.TempDir()
	if err := WriteMonitorTaskID(dir, "sess-del", "task-del"); err != nil {
		t.Fatalf("WriteMonitorTaskID: %v", err)
	}
	if err := RemoveMonitorTaskID(dir, "sess-del"); err != nil {
		t.Fatalf("RemoveMonitorTaskID: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "sess-del.task")); !os.IsNotExist(err) {
		t.Error(".task file should be gone after RemoveMonitorTaskID")
	}
}

func TestRemoveMonitorTaskID_MissingFile_NoError(t *testing.T) {
	dir := t.TempDir()
	if err := RemoveMonitorTaskID(dir, "nonexistent"); err != nil {
		t.Errorf("RemoveMonitorTaskID on missing file should return nil, got: %v", err)
	}
}

// TestSweepStaleTaskFiles_RemovesOrphanedTaskFile verifies that a .task file
// without a corresponding .lock file is removed (the voci serve process exited
// and cleaned up its lock, but the skill didn't get a chance to remove the .task).
func TestSweepStaleTaskFiles_RemovesOrphanedTaskFile(t *testing.T) {
	dir := t.TempDir()
	// Write .task file but no .lock file.
	if err := WriteMonitorTaskID(dir, "orphan-sess", "task-orphan"); err != nil {
		t.Fatalf("WriteMonitorTaskID: %v", err)
	}
	if err := SweepStaleTaskFiles(dir); err != nil {
		t.Fatalf("SweepStaleTaskFiles: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "orphan-sess.task")); !os.IsNotExist(err) {
		t.Error("orphaned .task file should have been removed by sweep")
	}
}

// TestSweepStaleTaskFiles_KeepsTaskFileWithLiveLock verifies that a .task file
// is NOT removed when a corresponding .lock file exists (the session is live).
func TestSweepStaleTaskFiles_KeepsTaskFileWithLiveLock(t *testing.T) {
	dir := t.TempDir()
	if err := WriteLock(dir, "live-sess", os.Getpid(), 9999); err != nil {
		t.Fatalf("WriteLock: %v", err)
	}
	if err := WriteMonitorTaskID(dir, "live-sess", "task-live"); err != nil {
		t.Fatalf("WriteMonitorTaskID: %v", err)
	}
	if err := SweepStaleTaskFiles(dir); err != nil {
		t.Fatalf("SweepStaleTaskFiles: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "live-sess.task")); err != nil {
		t.Error(".task file with live lock should NOT be removed by sweep")
	}
}

func TestSweepStaleTaskFiles_EmptyDir_NoError(t *testing.T) {
	dir := t.TempDir()
	if err := SweepStaleTaskFiles(dir); err != nil {
		t.Errorf("SweepStaleTaskFiles on empty dir: %v", err)
	}
}

// TestWriteMonitorTaskID_OverwritesExisting verifies that writing a new task ID
// for the same session replaces the old one (idempotent for reconnects).
func TestWriteMonitorTaskID_OverwritesExisting(t *testing.T) {
	dir := t.TempDir()
	if err := WriteMonitorTaskID(dir, "sess-ow", "task-old"); err != nil {
		t.Fatalf("first WriteMonitorTaskID: %v", err)
	}
	if err := WriteMonitorTaskID(dir, "sess-ow", "task-new"); err != nil {
		t.Fatalf("second WriteMonitorTaskID: %v", err)
	}
	got, err := ReadMonitorTaskID(dir, "sess-ow")
	if err != nil {
		t.Fatalf("ReadMonitorTaskID: %v", err)
	}
	if got != "task-new" {
		t.Errorf("ReadMonitorTaskID = %q, want task-new", got)
	}
}

func TestWriteMonitorTaskID_ReadOnlyDir(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("chmod ineffective as root")
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o444); err != nil {
		t.Fatal(err)
	}
	err := WriteMonitorTaskID(dir, "sess-ro", "task-1")
	if err == nil {
		t.Fatal("expected error when writing to read-only dir")
	}
}
