//go:build e2e

package daemon

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yalehu/voci/internal/intent"
	"github.com/yalehu/voci/internal/pipeline"
)

func newDeterministicDaemonServer(buf *bytes.Buffer) *Server {
	return &Server{
		TranscribeFn: func(ctx context.Context, key, audioPath, apiURL string) (string, error) {
			return "raw transcript", nil
		},
		HintedFn: func(ctx context.Context, raw, hint string, chatFn pipeline.ChatFn) (string, error) {
			return "hinted transcript", nil
		},
		RewriteFn: func(ctx context.Context, hinted, hint string, chatFn pipeline.ChatFn) (string, error) {
			return "rewritten text", nil
		},
		ClassifyFn: func(ctx context.Context, rewritten, fullContext string, chat pipeline.ChatFn) (intent.ActionProposal, error) {
			return intent.ActionProposal{
				Kind:      intent.KindDirectPrompt,
				Rewritten: rewritten,
			}, nil
		},
		BuildHintFn: func() string { return "test-hint" },
		EventWriter: buf,
	}
}

func TestE2E_Daemon_TranscribeDoesNotEmit(t *testing.T) {
	var buf bytes.Buffer
	srv := newDeterministicDaemonServer(&buf)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/api/voice/transcribe", "application/octet-stream", bytes.NewReader([]byte("fake audio")))
	if err != nil {
		t.Fatalf("POST /transcribe: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var proposal intent.ActionProposal
	if err := json.NewDecoder(resp.Body).Decode(&proposal); err != nil {
		t.Fatalf("decode proposal: %v", err)
	}
	if proposal.Kind != intent.KindDirectPrompt {
		t.Errorf("Kind: got %q, want %q", proposal.Kind, intent.KindDirectPrompt)
	}

	if buf.Len() != 0 {
		t.Errorf("expected EventWriter empty after /transcribe, got %d bytes: %q", buf.Len(), buf.String())
	}
}

func TestE2E_Daemon_EmitWritesOneLine(t *testing.T) {
	var buf bytes.Buffer
	srv := newDeterministicDaemonServer(&buf)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/api/voice/emit", "application/json", strings.NewReader(`{"text":"do X"}`))
	if err != nil {
		t.Fatalf("POST /emit: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", resp.StatusCode)
	}

	if bytes.Count(buf.Bytes(), []byte{'\n'}) != 1 {
		t.Fatalf("expected exactly 1 newline in EventWriter, got %d: %q", bytes.Count(buf.Bytes(), []byte{'\n'}), buf.String())
	}

	scanner := bufio.NewScanner(&buf)
	scanner.Scan()
	var ev Event
	if err := json.Unmarshal(scanner.Bytes(), &ev); err != nil {
		t.Fatalf("unmarshal event line: %v", err)
	}
	if ev.Rewritten != "do X" {
		t.Errorf("ev.Rewritten: got %q, want %q", ev.Rewritten, "do X")
	}
	if ev.Kind != "direct_prompt" {
		t.Errorf("ev.Kind: got %q, want %q", ev.Kind, "direct_prompt")
	}
}

func TestE2E_Daemon_TwoStepContract(t *testing.T) {
	var buf bytes.Buffer
	srv := newDeterministicDaemonServer(&buf)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// Step 1: /transcribe — buffer must remain empty
	resp, err := http.Post(ts.URL+"/api/voice/transcribe", "application/octet-stream", bytes.NewReader([]byte("fake audio")))
	if err != nil {
		t.Fatalf("POST /transcribe: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("/transcribe: expected 200, got %d", resp.StatusCode)
	}
	if buf.Len() != 0 {
		t.Errorf("buffer not empty after /transcribe: %q", buf.String())
	}

	// Step 2: /emit — buffer must have exactly one line
	resp2, err := http.Post(ts.URL+"/api/voice/emit", "application/json", strings.NewReader(`{"text":"run tests"}`))
	if err != nil {
		t.Fatalf("POST /emit: %v", err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusNoContent {
		t.Fatalf("/emit: expected 204, got %d", resp2.StatusCode)
	}
	if bytes.Count(buf.Bytes(), []byte{'\n'}) != 1 {
		t.Fatalf("expected exactly 1 line after /emit, got %d newlines: %q", bytes.Count(buf.Bytes(), []byte{'\n'}), buf.String())
	}
}
