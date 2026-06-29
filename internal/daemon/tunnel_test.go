package daemon

import (
	"context"
	"os/exec"
	"testing"
	"time"
)

func TestParseTunnelURL_ExtractsHTTPS(t *testing.T) {
	input := "2024-01-01T00:00:00Z INF |  https://abc123.trycloudflare.com  |\n"
	got := ParseTunnelURL(input)
	if got != "https://abc123.trycloudflare.com" {
		t.Errorf("want https://abc123.trycloudflare.com, got %q", got)
	}
}

func TestParseTunnelURL_ReturnsEmptyWhenNoMatch(t *testing.T) {
	got := ParseTunnelURL("no url here")
	if got != "" {
		t.Errorf("want empty, got %q", got)
	}
}

// TestWatchTunnel_CancelsContextOnExit verifies that WatchTunnel cancels the
// provided context when the watched process exits.
func TestWatchTunnel_CancelsContextOnExit(t *testing.T) {
	cmd := exec.Command("true") // exits immediately with code 0
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	WatchTunnel(cmd, cancel)

	select {
	case <-ctx.Done():
		// good: context cancelled when process exited
	case <-time.After(3 * time.Second):
		t.Fatal("context was not cancelled within 3s after process exit")
	}
}

// TestWatchTunnel_NoEarlyCancel verifies that WatchTunnel does NOT cancel the
// context while the process is still running.
func TestWatchTunnel_NoEarlyCancel(t *testing.T) {
	cmd := exec.Command("sleep", "60")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer cmd.Process.Kill()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	WatchTunnel(cmd, cancel)

	select {
	case <-ctx.Done():
		t.Fatal("context cancelled while process was still running")
	case <-time.After(200 * time.Millisecond):
		// good: still running
	}
}
