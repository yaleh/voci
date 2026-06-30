package session

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// StatusEntry holds startup metadata written by voci serve.
type StatusEntry struct {
	LocalURL    string `json:"local_url"`
	ShareURL    string `json:"share_url"`
	BearerToken string `json:"bearer_token"`
}

func statusPath(dir, sessionID string) string {
	return filepath.Join(dir, sessionID+".status")
}

// WriteStatus atomically writes a StatusEntry for the given session.
func WriteStatus(dir, sessionID, localURL, shareURL, bearerToken string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	data, err := json.Marshal(StatusEntry{LocalURL: localURL, ShareURL: shareURL, BearerToken: bearerToken})
	if err != nil {
		return err
	}
	tmp := statusPath(dir, sessionID) + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, statusPath(dir, sessionID))
}

// ReadStatus reads the StatusEntry for the given session.
func ReadStatus(dir, sessionID string) (StatusEntry, error) {
	data, err := os.ReadFile(statusPath(dir, sessionID))
	if err != nil {
		return StatusEntry{}, err
	}
	var e StatusEntry
	return e, json.Unmarshal(data, &e)
}

// RemoveStatus deletes the status file for the given session.
func RemoveStatus(dir, sessionID string) error {
	err := os.Remove(statusPath(dir, sessionID))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// isProcessAlive returns true if the process with the given PID is running.
func isProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	return syscall.Kill(pid, 0) == nil
}

// SweepStaleStatuses removes .status files whose corresponding .lock is absent or dead.
func SweepStaleStatuses(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".status") {
			continue
		}
		sessID := strings.TrimSuffix(e.Name(), ".status")
		lockFile := filepath.Join(dir, fmt.Sprintf("%s.lock", sessID))
		if _, err := os.Stat(lockFile); os.IsNotExist(err) {
			os.Remove(filepath.Join(dir, e.Name())) //nolint:errcheck
			continue
		}
		// check liveness via PID in lock
		data, err := os.ReadFile(lockFile)
		if err != nil {
			continue
		}
		var lock LockEntry
		if err := json.Unmarshal(data, &lock); err != nil {
			continue
		}
		if !isProcessAlive(lock.PID) {
			os.Remove(filepath.Join(dir, e.Name())) //nolint:errcheck
		}
	}
	return nil
}
