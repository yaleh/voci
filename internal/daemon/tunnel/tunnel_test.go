package tunnel

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

// TestDrainStderr_ContinuesAfterURL verifies that drainStderr keeps writing to
// logW even after the trycloudflare URL has been sent, and does not drop lines.
func TestDrainStderr_ContinuesAfterURL(t *testing.T) {
	input := strings.Join([]string{
		"INF starting tunnel",
		"INF | https://abc123.trycloudflare.com |",
		"INF post-url line 1",
		"INF post-url line 2",
	}, "\n") + "\n"

	var logBuf bytes.Buffer
	urlCh := make(chan string, 1)
	drainStderr(strings.NewReader(input), &logBuf, urlCh)

	got := <-urlCh
	if got != "https://abc123.trycloudflare.com" {
		t.Errorf("url: want https://abc123.trycloudflare.com, got %q", got)
	}
	log := logBuf.String()
	for _, want := range []string{"INF starting tunnel", "INF | https://abc123.trycloudflare.com |", "INF post-url line 1", "INF post-url line 2"} {
		if !strings.Contains(log, want) {
			t.Errorf("logW missing %q; got:\n%s", want, log)
		}
	}
}

// TestDrainStderr_NoURL verifies that drainStderr sends empty string when
// the reader closes without emitting a trycloudflare URL.
func TestDrainStderr_NoURL(t *testing.T) {
	urlCh := make(chan string, 1)
	drainStderr(strings.NewReader("no url here\n"), nil, urlCh)
	if got := <-urlCh; got != "" {
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

func TestStartTunnel_BinaryNotFound(t *testing.T) {
	// Override PATH to empty so LookPath fails.
	t.Setenv("PATH", "")
	_, _, err := StartTunnel(context.Background(), 8080, nil)
	if err == nil {
		t.Fatal("expected error when cloudflared not in PATH")
	}
	if !strings.Contains(err.Error(), "cloudflared not found") {
		t.Errorf("error should mention 'cloudflared not found', got: %v", err)
	}
}

func TestStartTunnel_NoURLEmitted(t *testing.T) {
	// Create a fake cloudflared binary that writes no URL and exits 0.
	dir := t.TempDir()
	fakeBin := filepath.Join(dir, "cloudflared")
	script := "#!/bin/sh\necho 'no url here' >&2\nexit 0\n"
	if err := os.WriteFile(fakeBin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)

	_, _, err := StartTunnel(context.Background(), 8080, nil)
	if err == nil {
		t.Fatal("expected error when cloudflared emits no URL")
	}
	if !strings.Contains(err.Error(), "did not emit a URL") {
		t.Errorf("error should mention 'did not emit a URL', got: %v", err)
	}
}
