package daemon

import (
	"os"
	"path/filepath"
	"regexp"
	"sync"
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

func TestWriteLockAtomic(t *testing.T) {
	dir := t.TempDir()
	const sessID = "atomic-sess"

	// Write the lock from two goroutines concurrently; assert exactly one clean
	// JSON file exists afterwards and no stray .tmp file is left behind.
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(pid int) {
			defer wg.Done()
			_ = WriteLock(dir, sessID, pid, 9000+pid)
		}(i + 1)
	}
	wg.Wait()

	// The final lock file must be readable (one writer wins the rename).
	entry, err := ReadLock(dir, sessID)
	if err != nil {
		t.Fatalf("ReadLock after concurrent WriteLock: %v", err)
	}
	if entry.SessionID != sessID {
		t.Errorf("SessionID = %q, want %q", entry.SessionID, sessID)
	}

	// No stray .tmp file should remain.
	tmpPath := filepath.Join(dir, sessID+".lock.tmp")
	if _, err := os.Stat(tmpPath); err == nil {
		t.Errorf("stray .tmp file left after WriteLock: %s", tmpPath)
	}
}

func TestNewSessionID(t *testing.T) {
	hexRe := regexp.MustCompile(`^[0-9a-f]{32}$`)
	seen := make(map[string]bool)
	for i := 0; i < 10; i++ {
		id := NewSessionID()
		if id == "" {
			t.Fatal("NewSessionID returned empty string")
		}
		if !hexRe.MatchString(id) {
			t.Errorf("NewSessionID returned %q, want 32 lowercase hex chars", id)
		}
		if seen[id] {
			t.Errorf("NewSessionID returned duplicate ID %q", id)
		}
		seen[id] = true
	}
}
