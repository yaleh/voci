package session

import (
	"os"
	"strconv"
	"strings"
)

// ProcAncestryReader returns the parent PID and command name for a given PID.
// ok is false when the process cannot be read (e.g., it no longer exists).
type ProcAncestryReader func(pid int) (ppid int, comm string, ok bool)

// ResolveSessionID walks up the /proc ancestry from startPID looking for an
// ancestor process with comm=="claude". Returns that process's PID as a decimal
// string. Returns ("", false) if no claude ancestor is found or the chain
// cannot be traversed. Stops after at most 64 hops to prevent infinite loops.
func ResolveSessionID(startPID int, read ProcAncestryReader) (string, bool) {
	const maxHops = 64
	pid := startPID
	for i := 0; i < maxHops; i++ {
		ppid, comm, ok := read(pid)
		if !ok || ppid <= 1 {
			return "", false
		}
		if comm == "claude" {
			return strconv.Itoa(ppid), true
		}
		pid = ppid
	}
	return "", false
}

// HasClaudeAncestor returns true if the process tree rooted at pid contains
// an ancestor with comm=="claude".
func HasClaudeAncestor(pid int, read ProcAncestryReader) bool {
	_, ok := ResolveSessionID(pid, read)
	return ok
}

// SessionIDOrFallback resolves the claude ancestor PID as the session ID.
// If no claude ancestor is found, falls back to a random 32-char hex string.
func SessionIDOrFallback(read ProcAncestryReader) string {
	sid, ok := ResolveSessionID(os.Getpid(), read)
	if !ok {
		return NewSessionID()
	}
	return sid
}

// ProcAncestry is the production implementation of ProcAncestryReader.
// It reads /proc/<pid>/stat and extracts the PPID (4th field) and comm
// (2nd field, in parentheses).
func ProcAncestry(pid int) (int, string, bool) {
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return 0, "", false
	}
	// stat format: pid (comm) state ppid ...  (see proc(5))
	// comm may contain spaces and parentheses; the format is:
	// "%d (%[^)]) %c %d" — but simpler: find the last ')' to split comm.
	stat := strings.TrimSpace(string(data))

	// Find the closing ')': comm is everything between '(' and the last ')'.
	closeParen := strings.LastIndex(stat, ")")
	if closeParen < 0 {
		return 0, "", false
	}
	openParen := strings.Index(stat, "(")
	if openParen < 0 || openParen >= closeParen {
		return 0, "", false
	}
	comm := stat[openParen+1 : closeParen]

	// After ") " comes the state char and then PPID.
	rest := strings.TrimSpace(stat[closeParen+1:])
	fields := strings.Fields(rest)
	if len(fields) < 2 {
		return 0, "", false
	}
	// fields[0] = state, fields[1] = ppid
	ppid, err := strconv.Atoi(fields[1])
	if err != nil {
		return 0, "", false
	}
	return ppid, comm, true
}
