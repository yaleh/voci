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
