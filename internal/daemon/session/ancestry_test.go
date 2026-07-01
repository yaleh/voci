package session

import (
	"os"
	"testing"
)

// fakeProcAncestry builds a ProcAncestryReader from a map of pid → (ppid, comm).
// When a pid is not in the map, it returns (0, "", false).
func fakeProcAncestry(data map[int]struct {
	ppid int
	comm string
}) ProcAncestryReader {
	return func(pid int) (int, string, bool) {
		v, ok := data[pid]
		if !ok {
			return 0, "", false
		}
		return v.ppid, v.comm, true
	}
}

func TestResolveSessionIDFindsClaude(t *testing.T) {
	// Ancestry: 100 → (90, "bash") → (80, "claude") → (1, "init")
	fake := fakeProcAncestry(map[int]struct {
		ppid int
		comm string
	}{
		100: {90, "bash"},
		90:  {80, "claude"},
		80:  {1, "init"},
	})
	sid, ok := ResolveSessionID(100, fake)
	if !ok {
		t.Fatal("expected to find claude ancestor")
	}
	if sid != "80" {
		t.Errorf("expected session ID '80', got %q", sid)
	}
}

func TestResolveSessionIDNotFound(t *testing.T) {
	// Ancestry: 100 → (90, "bash") → (80, "screen") → (1, "init") — no claude
	fake := fakeProcAncestry(map[int]struct {
		ppid int
		comm string
	}{
		100: {90, "bash"},
		90:  {80, "screen"},
		80:  {1, "init"},
	})
	sid, ok := ResolveSessionID(100, fake)
	if ok {
		t.Fatalf("expected no claude ancestor, but got session ID %q", sid)
	}
	if sid != "" {
		t.Errorf("expected empty session ID when not found, got %q", sid)
	}
}

func TestResolveSessionIDStopsAtPID1(t *testing.T) {
	// Chain goes to PID 1 which has no recorded data → returns (0,"", false)
	fake := fakeProcAncestry(map[int]struct {
		ppid int
		comm string
	}{
		100: {50, "bash"},
		50:  {25, "shell"},
		25:  {1, "init"},
		// PID 1 is not in the map → will return (0, "", false) → halt
	})
	sid, ok := ResolveSessionID(100, fake)
	if ok {
		t.Fatalf("expected no claude ancestor in chain without claude, got %q", sid)
	}
}

func TestHasClaudeAncestorTrue(t *testing.T) {
	fake := fakeProcAncestry(map[int]struct {
		ppid int
		comm string
	}{
		100: {90, "bash"},
		90:  {80, "claude"},
		80:  {1, "init"},
	})
	if !HasClaudeAncestor(100, fake) {
		t.Error("expected HasClaudeAncestor to return true")
	}
}

func TestHasClaudeAncestorFalse(t *testing.T) {
	fake := fakeProcAncestry(map[int]struct {
		ppid int
		comm string
	}{
		100: {90, "bash"},
		90:  {80, "zsh"},
		80:  {1, "init"},
	})
	if HasClaudeAncestor(100, fake) {
		t.Error("expected HasClaudeAncestor to return false")
	}
}

func TestNewSessionIDFallbackHex(t *testing.T) {
	// When ResolveSessionID returns false, fallback to 32-char hex.
	// Use a fake that has no claude ancestor.
	fake := fakeProcAncestry(map[int]struct {
		ppid int
		comm string
	}{
		100: {90, "bash"},
		90:  {80, "zsh"},
		80:  {1, "init"},
	})

	_, ok := ResolveSessionID(100, fake)
	if ok {
		t.Fatal("fake has no claude, should return false")
	}

	// Now verify the fallback itself returns a valid 32-char hex.
	fallback := NewSessionID()
	if len(fallback) != 32 {
		t.Errorf("expected 32-char hex fallback, got len=%d: %q", len(fallback), fallback)
	}
}

func TestSessionIDOrFallback_FindsClaude(t *testing.T) {
	// Use a fake ancestry where os.Getpid()→100(bash)→80(claude)→2(init).
	// SessionIDOrFallback calls os.Getpid() internally; we pass the fake reader.
	fake := fakeProcAncestry(map[int]struct {
		ppid int
		comm string
	}{
		os.Getpid(): {100, "bash"},
		100:         {80, "claude"},
		80:          {2, "init"},
	})
	sid := SessionIDOrFallback(fake)
	// ResolveSessionID chains: os.Getpid()→100(bash) then 100→80(claude)
	// → returns ppid of comm=="claude", which is 80.
	if sid != "80" {
		t.Errorf("expected session ID '80' from fake claude ancestor, got %q", sid)
	}
}

func TestSessionIDOrFallback_NoClaude(t *testing.T) {
	// Use a fake ancestry where our PID has no claude ancestor.
	fake := fakeProcAncestry(map[int]struct {
		ppid int
		comm string
	}{
		os.Getpid(): {99999, "bash"},
	})
	sid := SessionIDOrFallback(fake)
	// Should fall back to 32-char hex.
	if len(sid) != 32 {
		t.Errorf("expected 32-char hex fallback, got len=%d: %q", len(sid), sid)
	}
}

func TestProcAncestry_Self(t *testing.T) {
	// Read /proc/self/stat — must work on Linux.
	ppid, comm, ok := ProcAncestry(os.Getpid())
	if !ok {
		t.Fatal("ProcAncestry should succeed on self")
	}
	if ppid <= 0 {
		t.Errorf("expected positive ppid, got %d", ppid)
	}
	if comm == "" {
		t.Error("expected non-empty comm for self")
	}
}

func TestResolveSessionIDMaxHops(t *testing.T) {
	// Build a fake ancestry chain longer than 64 hops with no claude.
	// Should stop and return ("", false) without infinite loop.
	data := make(map[int]struct {
		ppid int
		comm string
	})
	// Chain: 5000 → 4999 → ... → 4935 (66 hops), all "bash".
	for i := 5000; i >= 4935; i-- {
		data[i] = struct {
			ppid int
			comm string
		}{i - 1, "bash"}
	}
	fake := fakeProcAncestry(data)
	sid, ok := ResolveSessionID(5000, fake)
	if ok {
		t.Errorf("expected no claude ancestor in long chain, got %q", sid)
	}
}

func TestProcAncestry_BadPID(t *testing.T) {
	_, _, ok := ProcAncestry(99999999)
	if ok {
		t.Error("expected ProcAncestry to fail on non-existent PID")
	}
}
