//go:build playwright

package daemon

import (
	"bytes"
	"context"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/yaleh/voci/internal/intent/model"
	"github.com/yaleh/voci/internal/pipeline"
)

// TestPlaywrightSetup starts a real httptest.Server for Playwright e2e tests.
// It writes the server URL to PLAYWRIGHT_URL_FILE, then waits for
// PLAYWRIGHT_DONE_FILE to appear before exiting.
func TestPlaywrightSetup(t *testing.T) {
	urlFile := os.Getenv("PLAYWRIGHT_URL_FILE")
	doneFile := os.Getenv("PLAYWRIGHT_DONE_FILE")
	if urlFile == "" || doneFile == "" {
		t.Skip("PLAYWRIGHT_URL_FILE or PLAYWRIGHT_DONE_FILE not set; skipping")
	}

	var buf bytes.Buffer
	srv := &Server{
		TranscribeFn: func(ctx context.Context, key, audioPath, apiURL, language string, entities []string) string {
			return "raw transcript"
		},
		HintedFn: func(ctx context.Context, raw, hint string, chatFn pipeline.ChatFn) (string, error) {
			return "hinted transcript", nil
		},
		RewriteFn: func(ctx context.Context, hinted, hint string, chatFn pipeline.ChatFn) (string, error) {
			return "rewritten text", nil
		},

		BuildHintFn: func() string {
			return "## Known Entities\nAlice, Bob\n## Active Tasks\ntask-1\n## Recent Dialogue\nU: hello\nA: world\n## Claude Code Session\nsession info"
		},
		HintFn: func(ctx context.Context) (string, error) {
			return "## Known Entities\nAlice, Bob\n## Active Tasks\ntask-1\n## Recent Dialogue\nU: hello\nA: world\n## Claude Code Session\nsession info", nil
		},
		EventWriter: &buf,
	}

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// Write the server URL so globalSetup.ts can read it
	if err := os.WriteFile(urlFile, []byte(ts.URL), 0644); err != nil {
		t.Fatalf("failed to write url file: %v", err)
	}

	t.Logf("Playwright server started at %s", ts.URL)
	t.Logf("Waiting for done signal at %s ...", doneFile)

	// Block until teardown signals done
	deadline := time.Now().Add(10 * time.Minute)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(doneFile); err == nil {
			t.Log("Done signal received, shutting down")
			return
		}
		time.Sleep(200 * time.Millisecond)
	}

	t.Error("timed out waiting for Playwright done signal")
}
