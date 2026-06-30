package tunnel

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

// TunnelState persists the active Named Tunnel credentials across voci restarts.
// It is stored at ~/.voci/active-tunnel.json.
type TunnelState struct {
	TunnelID    string    `json:"tunnel_id"`
	Token       string    `json:"token"`
	PublicURL   string    `json:"public_url"`
	DNSRecordID string    `json:"dns_record_id"`
	CreatedAt   time.Time `json:"created_at"`
}

// ActiveTunnelPath returns the canonical path for the active tunnel state file.
func ActiveTunnelPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".voci", "active-tunnel.json"), nil
}

// ReadActiveTunnel reads the state file. Returns nil, nil if the file does not exist.
func ReadActiveTunnel() (*TunnelState, error) {
	p, err := ActiveTunnelPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var s TunnelState
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// WriteActiveTunnel writes the state to ~/.voci/active-tunnel.json,
// creating ~/.voci/ with 0700 permissions if it does not exist.
func WriteActiveTunnel(s *TunnelState) error {
	p, err := ActiveTunnelPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o600)
}

// IsWithinTTL reports whether the state was created within the given TTL duration.
func (s *TunnelState) IsWithinTTL(ttl time.Duration) bool {
	return time.Since(s.CreatedAt) < ttl
}
