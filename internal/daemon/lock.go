package daemon

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"syscall"
)

// LockEntry is the JSON payload written to a per-session lock file.
type LockEntry struct {
	SessionID string `json:"session_id"`
	PID       int    `json:"pid"`
	Port      int    `json:"port"`
}

func lockPath(dir, sessionID string) string {
	return filepath.Join(dir, sessionID+".lock")
}

// WriteLock writes a LockEntry to <dir>/<sessionID>.lock as JSON.
func WriteLock(dir, sessionID string, pid, port int) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	entry := LockEntry{SessionID: sessionID, PID: pid, Port: port}
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	return os.WriteFile(lockPath(dir, sessionID), data, 0o600)
}

// ReadLock reads and decodes the lock file for sessionID from dir.
func ReadLock(dir, sessionID string) (LockEntry, error) {
	data, err := os.ReadFile(lockPath(dir, sessionID))
	if err != nil {
		return LockEntry{}, err
	}
	var entry LockEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return LockEntry{}, err
	}
	return entry, nil
}

// RemoveLock deletes the lock file for sessionID. Returns nil if the file is
// already gone.
func RemoveLock(dir, sessionID string) error {
	err := os.Remove(lockPath(dir, sessionID))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// SweepStaleLocks iterates *.lock files in dir, checks liveness of each
// recorded PID via kill(pid, 0), and removes files whose process is gone.
// Live lock files are left untouched. An empty or absent dir is not an error.
func SweepStaleLocks(dir string) error {
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
			// Corrupt file — remove it.
			os.Remove(path)
			continue
		}
		if entry.PID <= 0 {
			os.Remove(path)
			continue
		}
		if err := syscall.Kill(entry.PID, 0); err != nil {
			// Process is gone (or not ours) — safe to remove.
			os.Remove(path)
		}
	}
	return nil
}
