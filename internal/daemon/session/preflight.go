package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
)

// PreflightResult holds the result of a voci-listen preflight check.
type PreflightResult struct {
	Decision  string // "stopped", "reconnect", or "coldstart"
	SessionID string
	LocalURL  string
	ShareURL  string
	Token     string
}

// SweepOrphanLocks removes *.lock files whose PID is alive but lacks a claude
// ancestor in its process tree. These are orphaned voci serve processes that
// are no longer controlled by a claude harness.
func SweepOrphanLocks(dir string, read ProcAncestryReader) error {
	entries, err := filepath.Glob(filepath.Join(dir, "*.lock"))
	if err != nil {
		return err
	}
	for _, path := range entries {
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			continue
		}
		var entry LockEntry
		if jsonErr := json.Unmarshal(data, &entry); jsonErr != nil {
			continue
		}
		if !isProcessAlive(entry.PID) {
			// Leave dead-PID locks for SweepStaleLocks.
			continue
		}
		if !HasClaudeAncestor(entry.PID, read) {
			os.Remove(path) //nolint:errcheck
		}
	}
	return nil
}

// Preflight performs all pre-arm checks for voci-listen:
//  1. Sweeps stale and orphaned locks and statuses.
//  2. Checks for stop sentinel.
//  3. Resolves session ID from claude ancestry.
//  4. Checks for existing live lock → reconnect.
//  5. Otherwise → coldstart.
func Preflight(dir string, selfPID int, read ProcAncestryReader) (PreflightResult, error) {
	if err := SweepStaleLocks(dir); err != nil {
		return PreflightResult{}, err
	}
	if err := SweepStaleStatuses(dir); err != nil {
		return PreflightResult{}, err
	}
	if err := SweepOrphanLocks(dir, read); err != nil {
		return PreflightResult{}, err
	}

	// Check stop sentinel.
	if _, err := os.Stat(filepath.Join(dir, ".listen-stop")); err == nil {
		return PreflightResult{Decision: "stopped"}, nil
	}

	// Resolve session ID.
	sid, ok := ResolveSessionID(selfPID, read)
	if !ok {
		sid = NewSessionID()
	}

	// Check for existing live lock → reconnect.
	lockData, lockErr := os.ReadFile(filepath.Join(dir, sid+".lock"))
	if lockErr == nil {
		var entry LockEntry
		if jsonErr := json.Unmarshal(lockData, &entry); jsonErr == nil && isProcessAlive(entry.PID) {
			// Read status for reconnect metadata.
			res := PreflightResult{
				Decision:  "reconnect",
				SessionID: sid,
				LocalURL:  "http://127.0.0.1:" + strconv.Itoa(entry.Port),
			}
			if st, err := ReadStatus(dir, sid); err == nil {
				res.LocalURL = st.LocalURL
				res.ShareURL = st.ShareURL
				res.Token = st.BearerToken
			}
			return res, nil
		}
	}

	return PreflightResult{Decision: "coldstart", SessionID: sid}, nil
}
