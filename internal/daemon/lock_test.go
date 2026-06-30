package daemon

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteLock_CreatesFile(t *testing.T) {
	dir := t.TempDir()
	if err := WriteLock(dir, "sess-abc", 1234, 9500); err != nil {
		t.Fatalf("WriteLock: %v", err)
	}
	path := filepath.Join(dir, "sess-abc.lock")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("lock file not created: %v", err)
	}
	entry, err := ReadLock(dir, "sess-abc")
	if err != nil {
		t.Fatalf("ReadLock: %v", err)
	}
	if entry.SessionID != "sess-abc" {
		t.Errorf("SessionID = %q, want sess-abc", entry.SessionID)
	}
	if entry.PID != 1234 {
		t.Errorf("PID = %d, want 1234", entry.PID)
	}
	if entry.Port != 9500 {
		t.Errorf("Port = %d, want 9500", entry.Port)
	}
}

func TestReadLock_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	if err := WriteLock(dir, "sess-rt", 5678, 8080); err != nil {
		t.Fatalf("WriteLock: %v", err)
	}
	got, err := ReadLock(dir, "sess-rt")
	if err != nil {
		t.Fatalf("ReadLock: %v", err)
	}
	if got.SessionID != "sess-rt" || got.PID != 5678 || got.Port != 8080 {
		t.Errorf("round-trip mismatch: %+v", got)
	}
}

func TestRemoveLock_DeletesFile(t *testing.T) {
	dir := t.TempDir()
	if err := WriteLock(dir, "sess-del", 999, 9000); err != nil {
		t.Fatalf("WriteLock: %v", err)
	}
	if err := RemoveLock(dir, "sess-del"); err != nil {
		t.Fatalf("RemoveLock: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "sess-del.lock")); !os.IsNotExist(err) {
		t.Error("lock file should be gone after RemoveLock")
	}
}

func TestRemoveLock_MissingFile_NoError(t *testing.T) {
	dir := t.TempDir()
	if err := RemoveLock(dir, "nonexistent"); err != nil {
		t.Errorf("RemoveLock on missing file should return nil, got: %v", err)
	}
}

func TestSweepStaleLocks_RemovesDeadPID(t *testing.T) {
	dir := t.TempDir()
	// PID 99999999 is guaranteed to not exist.
	if err := WriteLock(dir, "dead-sess", 99999999, 9001); err != nil {
		t.Fatalf("WriteLock: %v", err)
	}
	if err := SweepStaleLocks(dir); err != nil {
		t.Fatalf("SweepStaleLocks: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "dead-sess.lock")); !os.IsNotExist(err) {
		t.Error("dead lock file should have been removed by sweep")
	}
}

func TestSweepStaleLocks_KeepsLivePID(t *testing.T) {
	dir := t.TempDir()
	pid := os.Getpid()
	if err := WriteLock(dir, "live-sess", pid, 9002); err != nil {
		t.Fatalf("WriteLock: %v", err)
	}
	if err := SweepStaleLocks(dir); err != nil {
		t.Fatalf("SweepStaleLocks: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "live-sess.lock")); err != nil {
		t.Error("live lock file should NOT have been removed by sweep")
	}
}

func TestSweepStaleLocks_EmptyDir_NoError(t *testing.T) {
	dir := t.TempDir()
	if err := SweepStaleLocks(dir); err != nil {
		t.Errorf("SweepStaleLocks on empty dir: %v", err)
	}
}
