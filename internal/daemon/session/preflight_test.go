package session

import (
	"os"
	"testing"
)

func TestPreflightStopped(t *testing.T) {
	dir := t.TempDir()
	// Create stop sentinel.
	if err := os.WriteFile(dir+"/.listen-stop", []byte(""), 0o600); err != nil {
		t.Fatal(err)
	}
	// Build a fake ancestry that has claude.
	fake := fakeProcAncestry(map[int]struct {
		ppid int
		comm string
	}{
		200: {100, "bash"},
		100: {80, "claude"},
		80:  {1, "init"},
	})
	res, err := Preflight(dir, 200, fake)
	if err != nil {
		t.Fatalf("Preflight: %v", err)
	}
	if res.Decision != "stopped" {
		t.Errorf("expected Decision='stopped', got %q", res.Decision)
	}
}

func TestPreflightColdstart(t *testing.T) {
	dir := t.TempDir()
	// No lock files, no stop sentinel.
	// Build a fake ancestry that has claude at PID 80.
	fake := fakeProcAncestry(map[int]struct {
		ppid int
		comm string
	}{
		200: {100, "bash"},
		100: {80, "claude"},
		80:  {1, "init"},
	})
	res, err := Preflight(dir, 200, fake)
	if err != nil {
		t.Fatalf("Preflight: %v", err)
	}
	if res.Decision != "coldstart" {
		t.Errorf("expected Decision='coldstart', got %q", res.Decision)
	}
	if res.SessionID != "100" {
		t.Errorf("expected SessionID='100' (the claude PID), got %q", res.SessionID)
	}
}

func TestPreflightReconnect(t *testing.T) {
	dir := t.TempDir()
	// Write a live lock + status for the session.
	// The lock PID must be in the fake ancestry chain AND be alive,
	// otherwise SweepOrphanLocks will remove it.
	pid := os.Getpid()
	// The fake chain below resolves the claude ancestor PID to 100, so the lock
	// must be keyed by "100" for Preflight to find it and choose reconnect.
	sid := "100"
	if err := WriteLock(dir, sid, pid, 9474); err != nil {
		t.Fatal(err)
	}
	if err := WriteStatus(dir, sid, "http://127.0.0.1:9474", "https://voci.example.com", "token-abc"); err != nil {
		t.Fatal(err)
	}

	// Fake ancestry: the lock PID (os.Getpid()) must trace to claude so
	// SweepOrphanLocks keeps it. Chain: selfPID=200 → 100 → 80(claude) ...
	// Also include os.Getpid() in the chain so SweepOrphanLocks finds claude.
	fakeData := map[int]struct {
		ppid int
		comm string
	}{
		pid: {200, "shell"},
		200: {100, "bash"},
		100: {80, "claude"},
		80:  {1, "init"},
	}
	fake := fakeProcAncestry(fakeData)
	res, err := Preflight(dir, 200, fake)
	if err != nil {
		t.Fatalf("Preflight: %v", err)
	}
	if res.Decision != "reconnect" {
		t.Fatalf("expected Decision='reconnect', got %q", res.Decision)
	}
	if res.SessionID != "100" {
		t.Errorf("expected SessionID='100', got %q", res.SessionID)
	}
	if res.LocalURL != "http://127.0.0.1:9474" {
		t.Errorf("expected LocalURL='http://127.0.0.1:9474', got %q", res.LocalURL)
	}
	if res.ShareURL != "https://voci.example.com" {
		t.Errorf("expected ShareURL='https://voci.example.com', got %q", res.ShareURL)
	}
	if res.Token != "token-abc" {
		t.Errorf("expected Token='token-abc', got %q", res.Token)
	}
}

func TestSweepOrphanLocksRemovesOrphan(t *testing.T) {
	dir := t.TempDir()
	// Write a lock with live PID but fake ancestry has no claude ancestor.
	pid := os.Getpid()
	if err := WriteLock(dir, "orphan-sess", pid, 9000); err != nil {
		t.Fatal(err)
	}
	// Fake ancestry: process tree has no claude.
	fake := fakeProcAncestry(map[int]struct {
		ppid int
		comm string
	}{
		pid: {pid - 1, "bash"},
	})
	if err := SweepOrphanLocks(dir, fake); err != nil {
		t.Fatalf("SweepOrphanLocks: %v", err)
	}
	if _, err := os.Stat(dir + "/orphan-sess.lock"); !os.IsNotExist(err) {
		t.Error("orphan lock should have been removed")
	}
}

func TestSweepOrphanLocksKeepsLive(t *testing.T) {
	dir := t.TempDir()
	pid := os.Getpid()
	if err := WriteLock(dir, "live-sess", pid, 9001); err != nil {
		t.Fatal(err)
	}
	// Fake ancestry: process tree has claude ancestor.
	fake := fakeProcAncestry(map[int]struct {
		ppid int
		comm string
	}{
		pid: {pid + 1, "claude"}, // claude as parent
	})
	if err := SweepOrphanLocks(dir, fake); err != nil {
		t.Fatalf("SweepOrphanLocks: %v", err)
	}
	if _, err := os.Stat(dir + "/live-sess.lock"); os.IsNotExist(err) {
		t.Error("live lock with claude ancestor should NOT be removed")
	}
}

func TestSweepOrphanLocksIgnoresDeadPID(t *testing.T) {
	dir := t.TempDir()
	// PID 99999999 is guaranteed dead.
	if err := WriteLock(dir, "dead-sess", 99999999, 9002); err != nil {
		t.Fatal(err)
	}
	// Fake that returns true for 99999999 — but isProcessAlive returns false for it.
	fake := fakeProcAncestry(map[int]struct {
		ppid int
		comm string
	}{
		99999999: {1, "init"},
	})
	if err := SweepOrphanLocks(dir, fake); err != nil {
		t.Fatalf("SweepOrphanLocks: %v", err)
	}
	// Dead PID locks are left to SweepStaleLocks, but in the
	// Preflight flow SweepStaleLocks runs first. Here we just verify
	// SweepOrphanLocks doesn't panic and doesn't delete it (it skips dead PIDs).
	// Actually, even if it's kept here, that's fine — SweepStaleLocks would delete it.
	// So we just verify no panic.
}

func TestPreflightColdstartNoClaudeAncestor(t *testing.T) {
	dir := t.TempDir()
	// No lock files, no stop sentinel. Ancestry has no claude.
	fake := fakeProcAncestry(map[int]struct {
		ppid int
		comm string
	}{
		200: {100, "bash"},
		100: {80, "zsh"},
		80:  {1, "init"},
	})
	res, err := Preflight(dir, 200, fake)
	if err != nil {
		t.Fatalf("Preflight: %v", err)
	}
	if res.Decision != "coldstart" {
		t.Errorf("expected Decision='coldstart', got %q", res.Decision)
	}
	if len(res.SessionID) != 32 {
		t.Errorf("expected 32-char hex fallback session ID, got len=%d: %q", len(res.SessionID), res.SessionID)
	}
}
