package session_test

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/yaleh/voci/internal/daemon/session"
)

func TestWriteStatus_CreatesFile(t *testing.T) {
	dir := t.TempDir()
	if err := session.WriteStatus(dir, "sess-abc", "http://127.0.0.1:9500", "https://share.example.com", "tok"); err != nil {
		t.Fatal(err)
	}
	s, err := session.ReadStatus(dir, "sess-abc")
	if err != nil {
		t.Fatal(err)
	}
	if s.LocalURL != "http://127.0.0.1:9500" || s.ShareURL != "https://share.example.com" || s.BearerToken != "tok" {
		t.Fatalf("round-trip mismatch: %+v", s)
	}
}

func TestReadStatus_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	session.WriteStatus(dir, "s1", "local", "share", "bearer")
	got, err := session.ReadStatus(dir, "s1")
	if err != nil {
		t.Fatal(err)
	}
	if got.LocalURL != "local" || got.ShareURL != "share" || got.BearerToken != "bearer" {
		t.Fatalf("mismatch: %+v", got)
	}
}

func TestRemoveStatus_DeletesFile(t *testing.T) {
	dir := t.TempDir()
	session.WriteStatus(dir, "s2", "a", "b", "c")
	if err := session.RemoveStatus(dir, "s2"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "s2.status")); !os.IsNotExist(err) {
		t.Fatal("file should be gone")
	}
}

func TestRemoveStatus_MissingFile_NoError(t *testing.T) {
	dir := t.TempDir()
	if err := session.RemoveStatus(dir, "nonexistent"); err != nil {
		t.Fatal(err)
	}
}

func TestWriteStatusAtomic(t *testing.T) {
	dir := t.TempDir()
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			session.WriteStatus(dir, "atomic-sess", "local", "share", "tok")
		}()
	}
	wg.Wait()
	// file must be valid and readable
	if _, err := session.ReadStatus(dir, "atomic-sess"); err != nil {
		t.Fatal(err)
	}
	// no stray .tmp files
	matches, _ := filepath.Glob(filepath.Join(dir, "*.tmp"))
	if len(matches) > 0 {
		t.Fatalf("stray .tmp files: %v", matches)
	}
}

func TestSweepStaleStatuses_RemovesOrphanedStatusFile(t *testing.T) {
	dir := t.TempDir()
	// write orphaned .status with no .lock
	session.WriteStatus(dir, "orphan", "l", "s", "t")
	if err := session.SweepStaleStatuses(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "orphan.status")); !os.IsNotExist(err) {
		t.Fatal("orphaned status should be removed")
	}
}

func TestSweepStaleStatuses_KeepsActiveStatusFile(t *testing.T) {
	dir := t.TempDir()
	session.WriteStatus(dir, "active", "l", "s", "t")
	// write a lock file for the same session with current PID
	session.WriteLock(dir, "active", os.Getpid(), 0)
	if err := session.SweepStaleStatuses(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "active.status")); err != nil {
		t.Fatalf("active status should remain: %v", err)
	}
}

func TestSweepStaleStatuses_DeadPIDLockFile(t *testing.T) {
	dir := t.TempDir()
	// Write a status file with a corresponding lock file containing a dead PID.
	if err := session.WriteStatus(dir, "dead-sess", "http://127.0.0.1:9000", "https://share.example.com", "tok"); err != nil {
		t.Fatal(err)
	}
	// PID 99999999 is guaranteed to not exist.
	if err := session.WriteLock(dir, "dead-sess", 99999999, 0); err != nil {
		t.Fatal(err)
	}
	if err := session.SweepStaleStatuses(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "dead-sess.status")); !os.IsNotExist(err) {
		t.Error("status file with dead PID lock should have been removed")
	}
}

func TestWriteStatus_UnwritableDir_ReturnsError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("chmod ineffective as root")
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o444); err != nil {
		t.Fatal(err)
	}
	err := session.WriteStatus(dir, "sess-ro", "local", "share", "tok")
	if err == nil {
		t.Fatal("expected error when writing to read-only dir")
	}
}

func TestSweepStaleStatuses_DirNotExist(t *testing.T) {
	err := session.SweepStaleStatuses("/nonexistent/path/for/status")
	if err != nil {
		t.Fatalf("expected nil error for nonexistent dir, got: %v", err)
	}
}

func TestReadStatus_CorruptJSON(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(dir, 0o700)
	os.WriteFile(filepath.Join(dir, "corrupt.status"), []byte("not json {{{"), 0o600)
	_, err := session.ReadStatus(dir, "corrupt")
	if err == nil {
		t.Fatal("expected error for corrupt JSON status file")
	}
}

func TestSweepStaleStatuses_ZeroPIDLock(t *testing.T) {
	dir := t.TempDir()
	// Write a status file with a lock file containing pid=0 (considered dead).
	if err := session.WriteStatus(dir, "zero-pid", "http://127.0.0.1:9000", "https://share.example.com", "tok"); err != nil {
		t.Fatal(err)
	}
	if err := session.WriteLock(dir, "zero-pid", 0, 0); err != nil {
		t.Fatal(err)
	}
	if err := session.SweepStaleStatuses(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "zero-pid.status")); !os.IsNotExist(err) {
		t.Error("status file with pid=0 lock should have been removed")
	}
}
