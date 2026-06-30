package session

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

func taskPath(dir, sessionID string) string {
	return filepath.Join(dir, sessionID+".task")
}

// WriteMonitorTaskID writes the harness Monitor task ID for sessionID to
// <dir>/<sessionID>.task. Overwrites any existing file atomically.
func WriteMonitorTaskID(dir, sessionID, taskID string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	final := taskPath(dir, sessionID)
	tmp := final + ".tmp"
	if err := os.WriteFile(tmp, []byte(taskID), 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, final); err != nil {
		os.Remove(tmp) //nolint:errcheck
		return err
	}
	return nil
}

// ReadMonitorTaskID reads the harness Monitor task ID for sessionID from dir.
// Returns an error if the file does not exist.
func ReadMonitorTaskID(dir, sessionID string) (string, error) {
	data, err := os.ReadFile(taskPath(dir, sessionID))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

// RemoveMonitorTaskID deletes the .task file for sessionID. Returns nil if the
// file is already gone.
func RemoveMonitorTaskID(dir, sessionID string) error {
	err := os.Remove(taskPath(dir, sessionID))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// SweepStaleTaskFiles removes .task files that have no corresponding .lock file.
// When voci serve exits it removes its own .lock; any leftover .task files are
// orphaned and safe to delete so they don't block future reconnectGuard checks.
func SweepStaleTaskFiles(dir string) error {
	entries, err := filepath.Glob(filepath.Join(dir, "*.task"))
	if err != nil {
		return err
	}
	for _, taskFile := range entries {
		base := strings.TrimSuffix(filepath.Base(taskFile), ".task")
		lockFile := filepath.Join(dir, base+".lock")
		if _, err := os.Stat(lockFile); os.IsNotExist(err) {
			os.Remove(taskFile) //nolint:errcheck
		}
	}
	return nil
}
