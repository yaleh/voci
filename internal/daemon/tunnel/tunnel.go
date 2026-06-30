package tunnel

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"time"
)


var trycloudflareRe = regexp.MustCompile(`https://[^\s]+\.trycloudflare\.com`)

// WatchTunnel calls cancel when cmd exits, allowing callers to react to an
// unexpected cloudflared exit without polling. It starts a background goroutine
// and returns immediately.
func WatchTunnel(cmd *exec.Cmd, cancel context.CancelFunc) {
	go func() {
		cmd.Wait() //nolint:errcheck — we only care that it exited
		cancel()
	}()
}

// ParseTunnelURL extracts the first trycloudflare.com HTTPS URL from a line of text.
func ParseTunnelURL(line string) string {
	return trycloudflareRe.FindString(line)
}

// drainStderr reads r line by line, writing every line to logW (if non-nil),
// and sends the first trycloudflare.com URL found to urlCh. It keeps reading
// until EOF so that all post-URL cloudflared output is preserved in logW.
// Sends "" to urlCh if r closes without a URL.
func drainStderr(r io.Reader, logW io.Writer, urlCh chan<- string) {
	sc := bufio.NewScanner(r)
	urlSent := false
	for sc.Scan() {
		line := sc.Text()
		if logW != nil {
			fmt.Fprintln(logW, line)
		}
		if !urlSent {
			if u := ParseTunnelURL(line); u != "" {
				urlCh <- u
				urlSent = true
			}
		}
	}
	if !urlSent {
		urlCh <- ""
	}
}

// StartTunnel starts cloudflared Quick Tunnel for the given port.
// It reads cloudflared's stderr until it finds the public URL or times out (15s).
// After the URL is found the drain goroutine keeps running, writing all subsequent
// cloudflared output to logW so crash diagnostics are not lost.
// Returns the running *exec.Cmd and the public URL.
func StartTunnel(ctx context.Context, port int, logW io.Writer) (*exec.Cmd, string, error) {
	bin, err := exec.LookPath("cloudflared")
	if err != nil {
		return nil, "", fmt.Errorf("cloudflared not found in PATH: %w\nInstall: https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/downloads/", err)
	}

	cmd := exec.CommandContext(ctx, bin, "tunnel", "--url", fmt.Sprintf("http://127.0.0.1:%d", port))
	applyChildAttrs(cmd)
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, "", fmt.Errorf("pipe stderr: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, "", fmt.Errorf("start cloudflared: %w", err)
	}

	urlCh := make(chan string, 1)
	go drainStderr(stderr, logW, urlCh)

	select {
	case u := <-urlCh:
		if u == "" {
			cmd.Process.Kill()
			return nil, "", fmt.Errorf("cloudflared did not emit a URL")
		}
		return cmd, u, nil
	case <-time.After(15 * time.Second):
		cmd.Process.Kill()
		return nil, "", fmt.Errorf("cloudflared URL timeout after 15s")
	}
}
