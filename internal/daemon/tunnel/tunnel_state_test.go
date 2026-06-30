package tunnel

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// withTunnelStateDir temporarily overrides the home dir to an isolated temp dir.
// It patches os.UserHomeDir by writing a fake state file into t.TempDir().
func tunnelStatePath(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	vociDir := filepath.Join(home, ".voci")
	return filepath.Join(vociDir, "active-tunnel.json")
}

// writeFakeState writes a TunnelState to a known path by temporarily redirecting
// WriteActiveTunnel via HOME env var.
func withFakeHome(t *testing.T, fn func(home string)) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	fn(home)
}

func TestTunnelState_RoundTrip(t *testing.T) {
	withFakeHome(t, func(home string) {
		want := &TunnelState{
			TunnelID:    "tun-abc",
			Token:       "tok-xyz",
			PublicURL:   "https://voci-abc.example.com",
			DNSRecordID: "dns-def",
			CreatedAt:   time.Now().Truncate(time.Second),
		}
		if err := WriteActiveTunnel(want); err != nil {
			t.Fatalf("WriteActiveTunnel: %v", err)
		}
		got, err := ReadActiveTunnel()
		if err != nil {
			t.Fatalf("ReadActiveTunnel: %v", err)
		}
		if got == nil {
			t.Fatal("ReadActiveTunnel returned nil")
		}
		if got.TunnelID != want.TunnelID {
			t.Errorf("TunnelID = %q, want %q", got.TunnelID, want.TunnelID)
		}
		if got.Token != want.Token {
			t.Errorf("Token = %q, want %q", got.Token, want.Token)
		}
		if got.PublicURL != want.PublicURL {
			t.Errorf("PublicURL = %q, want %q", got.PublicURL, want.PublicURL)
		}
		if got.DNSRecordID != want.DNSRecordID {
			t.Errorf("DNSRecordID = %q, want %q", got.DNSRecordID, want.DNSRecordID)
		}
		if !got.CreatedAt.Equal(want.CreatedAt) {
			t.Errorf("CreatedAt = %v, want %v", got.CreatedAt, want.CreatedAt)
		}
	})
}

func TestTunnelState_MissingFile(t *testing.T) {
	withFakeHome(t, func(home string) {
		s, err := ReadActiveTunnel()
		if err != nil {
			t.Fatalf("expected nil error for missing file, got: %v", err)
		}
		if s != nil {
			t.Errorf("expected nil state for missing file, got: %+v", s)
		}
	})
}

func TestTunnelState_ExpiredTTL(t *testing.T) {
	withFakeHome(t, func(home string) {
		s := &TunnelState{
			TunnelID:  "tun-old",
			CreatedAt: time.Now().Add(-25 * time.Hour),
		}
		if err := WriteActiveTunnel(s); err != nil {
			t.Fatalf("WriteActiveTunnel: %v", err)
		}
		got, err := ReadActiveTunnel()
		if err != nil {
			t.Fatalf("ReadActiveTunnel: %v", err)
		}
		if got.IsWithinTTL(20 * time.Hour) {
			t.Error("expected state to be expired (25h old vs 20h TTL), but IsWithinTTL returned true")
		}
	})
}

func TestTunnelState_ValidTTL(t *testing.T) {
	withFakeHome(t, func(home string) {
		s := &TunnelState{
			TunnelID:  "tun-fresh",
			CreatedAt: time.Now().Add(-1 * time.Hour),
		}
		if err := WriteActiveTunnel(s); err != nil {
			t.Fatalf("WriteActiveTunnel: %v", err)
		}
		got, err := ReadActiveTunnel()
		if err != nil {
			t.Fatalf("ReadActiveTunnel: %v", err)
		}
		if !got.IsWithinTTL(20 * time.Hour) {
			t.Error("expected state to be valid (1h old vs 20h TTL), but IsWithinTTL returned false")
		}
	})
}

func TestTunnelState_CorruptJSON(t *testing.T) {
	withFakeHome(t, func(home string) {
		stateDir := filepath.Join(home, ".voci")
		os.MkdirAll(stateDir, 0o700)
		statePath := filepath.Join(stateDir, "active-tunnel.json")
		os.WriteFile(statePath, []byte("not json {{{{"), 0o600)

		_, err := ReadActiveTunnel()
		if err == nil {
			t.Fatal("expected error for corrupt JSON, got nil")
		}
	})
}

func TestTunnelState_DefaultPath(t *testing.T) {
	withFakeHome(t, func(home string) {
		p, err := ActiveTunnelPath()
		if err != nil {
			t.Fatalf("ActiveTunnelPath: %v", err)
		}
		if !strings.Contains(p, ".voci") {
			t.Errorf("expected path under .voci, got: %q", p)
		}
		if !strings.HasSuffix(p, "active-tunnel.json") {
			t.Errorf("expected path to end with active-tunnel.json, got: %q", p)
		}
	})
}

func TestWriteActiveTunnel_CreatesDir(t *testing.T) {
	withFakeHome(t, func(home string) {
		// ~/.voci must not exist yet
		vociDir := filepath.Join(home, ".voci")
		if _, err := os.Stat(vociDir); err == nil {
			t.Skip(".voci already exists")
		}
		if err := WriteActiveTunnel(&TunnelState{TunnelID: "x"}); err != nil {
			t.Fatalf("WriteActiveTunnel: %v", err)
		}
		info, err := os.Stat(vociDir)
		if err != nil {
			t.Fatalf(".voci not created: %v", err)
		}
		if info.Mode().Perm() != 0o700 {
			t.Errorf(".voci perm = %o, want 0700", info.Mode().Perm())
		}
	})
}

func TestWriteActiveTunnel_ReadOnlyParent(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("chmod ineffective as root")
	}
	withFakeHome(t, func(home string) {
		// Create .voci dir and chmod to read-only so WriteActiveTunnel's MkdirAll fails
		vociDir := filepath.Join(home, ".voci")
		if err := os.MkdirAll(vociDir, 0o444); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		if err := os.Chmod(vociDir, 0o444); err != nil {
			t.Fatalf("Chmod: %v", err)
		}
		// WriteActiveTunnel calls MkdirAll on an existing read-only dir → should succeed,
		// but WriteFile inside will fail because the dir is read-only.
		err := WriteActiveTunnel(&TunnelState{TunnelID: "ro-test"})
		if err == nil {
			t.Fatal("expected error when writing to read-only dir")
		}
	})
}
