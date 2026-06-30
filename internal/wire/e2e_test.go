//go:build e2e

package wire

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// TestE2E_Share_Port0_CloudflaredGetsRealPort builds the voci binary, installs a
// fake cloudflared stub on PATH that records the --url argument it received, then
// runs "voci serve --share --serve-port 0".  It asserts that the port embedded in
// the --url flag is non-zero — the regression check for the 502 bug where
// cloudflared received "http://127.0.0.1:0".
func TestE2E_Share_Port0_CloudflaredGetsRealPort(t *testing.T) {
	// Build the voci binary into a temp dir.
	binDir := t.TempDir()
	vociBin := binDir + "/voci"
	if out, err := exec.Command("go", "build", "-o", vociBin, "../../cmd/voci").CombinedOutput(); err != nil {
		t.Fatalf("build voci: %v\n%s", err, out)
	}

	// Write a fake cloudflared stub that records its args, emits the fake URL
	// on stderr (so StartTunnel's drainStderr finds it), then blocks until killed.
	stubDir := t.TempDir()
	argsFile := stubDir + "/cloudflared-args.txt"
	stubScript := fmt.Sprintf(`#!/bin/sh
echo "$@" > %q
# Emit a fake trycloudflare.com URL on stderr so StartTunnel's drainStderr unblocks.
echo "https://fake-stub-tunnel.trycloudflare.com" >&2
# Block so voci serve stays alive long enough for us to read the args file.
sleep 60
`, argsFile)
	stubPath := stubDir + "/cloudflared"
	if err := os.WriteFile(stubPath, []byte(stubScript), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}

	// Build PATH: stub dir first so our fake cloudflared wins.
	origPATH := os.Getenv("PATH")
	newPATH := stubDir + ":" + origPATH

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cmd := exec.CommandContext(ctx, vociBin,
		"serve", "--share", "--serve-port=0", "--share-auth=tok")
	cmd.Env = append(os.Environ(),
		"PATH="+newPATH,
		"SILICONFLOW_API_KEY=sk-test",
		"OLLAMA_HOST=http://localhost:11434",
	)
	cmd.Stdout = io.Discard
	stderrPR, stderrPW, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	cmd.Stderr = stderrPW

	if err := cmd.Start(); err != nil {
		t.Fatalf("start voci serve: %v", err)
	}
	stderrPW.Close()

	// Read stderr until we see "voci share URL:" which means the stub was invoked.
	stderrLines := make(chan string, 100)
	go func() {
		buf := make([]byte, 4096)
		var acc string
		for {
			n, readErr := stderrPR.Read(buf)
			if n > 0 {
				acc += string(buf[:n])
				for {
					idx := strings.IndexByte(acc, '\n')
					if idx < 0 {
						break
					}
					stderrLines <- acc[:idx]
					acc = acc[idx+1:]
				}
			}
			if readErr != nil {
				close(stderrLines)
				return
			}
		}
	}()

	// Read stderr lines until "voci share URL:" appears, then cancel the context.
	// Also capture "voci local URL:" for assertion.
	urlSeen := make(chan string, 1)
	var listenAddr, localURL string
	go func() {
		for line := range stderrLines {
			if strings.HasPrefix(line, "voci serve: listening on ") {
				listenAddr = strings.TrimPrefix(line, "voci serve: listening on ")
			}
			if strings.HasPrefix(line, "voci local URL:") {
				localURL = strings.TrimSpace(strings.TrimPrefix(line, "voci local URL:"))
			}
			if strings.HasPrefix(line, "voci share URL:") {
				urlSeen <- line
				cancel()
				return
			}
		}
		close(urlSeen)
	}()

	// Wait for the URL line or a 20s timeout.
	timer := time.NewTimer(20 * time.Second)
	defer timer.Stop()
	select {
	case line, ok := <-urlSeen:
		if !ok {
			t.Fatal("stderr closed without 'voci share URL:' — stub may not have emitted a URL or voci exited early")
		}
		t.Logf("stderr: %s", line)
	case <-timer.C:
		cancel()
		t.Fatal("timed out (20s) waiting for 'voci share URL:'")
	}

	// Now wait for the process to exit after context cancel.
	if err := cmd.Wait(); err != nil && ctx.Err() == nil {
		t.Fatalf("voci serve exited with unexpected error: %v", err)
	}

	// Read the args the stub captured.
	argsData, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("read cloudflared-args.txt: %v (stub may not have been called)", err)
	}
	args := strings.TrimSpace(string(argsData))
	t.Logf("cloudflared invoked with: %s", args)
	t.Logf("voci listening addr: %s", listenAddr)

	// Find the --url argument.
	var urlArg string
	for _, part := range strings.Fields(args) {
		if strings.HasPrefix(part, "http://127.0.0.1:") {
			urlArg = part
			break
		}
	}
	if urlArg == "" {
		t.Fatalf("cloudflared args did not contain http://127.0.0.1:<port>: %q", args)
	}

	// Extract and validate the port.
	_, portStr, splitErr := net.SplitHostPort(strings.TrimPrefix(urlArg, "http://"))
	if splitErr != nil {
		t.Fatalf("SplitHostPort(%q): %v", urlArg, splitErr)
	}
	if portStr == "0" || portStr == "" {
		t.Errorf("cloudflared received port 0 in --url %q — want real OS-assigned port", urlArg)
	} else {
		t.Logf("cloudflared received correct port: %s", portStr)
	}

	// Cross-check: the port in the stub URL matches the port voci actually bound.
	if listenAddr != "" {
		_, listenPort, _ := net.SplitHostPort(listenAddr)
		if portStr != listenPort {
			t.Errorf("cloudflared port %s != voci listen port %s", portStr, listenPort)
		}
	}

	// Assert "voci local URL: http://127.0.0.1:<port>" was emitted to stderr.
	t.Logf("voci local URL line: %q", localURL)
	if localURL == "" {
		t.Error("'voci local URL:' line not found in stderr")
	} else if !strings.HasPrefix(localURL, "http://127.0.0.1:") {
		t.Errorf("local URL %q does not start with http://127.0.0.1:", localURL)
	} else {
		_, localPort, splitErr2 := net.SplitHostPort(strings.TrimPrefix(localURL, "http://"))
		if splitErr2 != nil {
			t.Errorf("SplitHostPort(%q): %v", localURL, splitErr2)
		} else if localPort != portStr {
			t.Errorf("local URL port %s != cloudflared port %s", localPort, portStr)
		}
	}
}
