package daemon

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

// ParseTunnelURL extracts the first trycloudflare.com HTTPS URL from a line of text.
func ParseTunnelURL(line string) string {
	return trycloudflareRe.FindString(line)
}

// StartTunnel starts cloudflared Quick Tunnel for the given port.
// It reads cloudflared's stderr until it finds the public URL or times out (15s).
// Returns the running *exec.Cmd and the public URL.
func StartTunnel(ctx context.Context, port int, logW io.Writer) (*exec.Cmd, string, error) {
	bin, err := exec.LookPath("cloudflared")
	if err != nil {
		return nil, "", fmt.Errorf("cloudflared not found in PATH: %w\nInstall: https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/downloads/", err)
	}

	cmd := exec.CommandContext(ctx, bin, "tunnel", "--url", fmt.Sprintf("http://127.0.0.1:%d", port))
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, "", fmt.Errorf("pipe stderr: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, "", fmt.Errorf("start cloudflared: %w", err)
	}

	urlCh := make(chan string, 1)
	go func() {
		sc := bufio.NewScanner(stderr)
		for sc.Scan() {
			line := sc.Text()
			if logW != nil {
				fmt.Fprintln(logW, line)
			}
			if u := ParseTunnelURL(line); u != "" {
				urlCh <- u
				return
			}
		}
		urlCh <- ""
	}()

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
