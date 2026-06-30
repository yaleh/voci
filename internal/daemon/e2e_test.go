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

	"github.com/yaleh/voci/internal/daemon/session"
	"github.com/yaleh/voci/internal/intent/model"
	"github.com/yaleh/voci/internal/pipeline"
)

func newDeterministicDaemonServer(buf *bytes.Buffer) *Server {
	return &Server{
		TranscribeFn: func(ctx context.Context, key, audioPath, apiURL, language string, entities []string) string {
			return "raw transcript"
		},
		HintedFn: func(ctx context.Context, raw, hint string, chatFn pipeline.ChatFn) (string, error) {
			return "hinted transcript", nil
		},
		RewriteFn: func(ctx context.Context, hinted, hint string, chatFn pipeline.ChatFn) (string, error) {
			return "rewritten text", nil
		},
		ClassifyFn: func(ctx context.Context, rewritten, fullContext string, chat pipeline.ChatFn) (model.ActionProposal, error) {
			return model.ActionProposal{
				Kind:      model.KindDirectPrompt,
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

	var proposal model.ActionProposal
	if err := json.NewDecoder(resp.Body).Decode(&proposal); err != nil {
		t.Fatalf("decode proposal: %v", err)
	}
	if proposal.Kind != model.KindDirectPrompt {
		t.Errorf("Kind: got %q, want %q", proposal.Kind, model.KindDirectPrompt)
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
	var ev session.Event
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

// TestE2E_Emit_EventWriterJSONMatchesGrepFilter is the regression test for the
// Monitor silence bug: /api/voice/emit must write a JSON line whose key is
// "rewritten" (lowercase) so the voci-listen grep pattern '"rewritten"' matches.
func TestE2E_Emit_EventWriterJSONMatchesGrepFilter(t *testing.T) {
	var buf bytes.Buffer
	srv := makeEmitServerWithWriter(t, &buf)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/api/voice/emit", "application/json",
		strings.NewReader(`{"text":"pwd","kind":"direct_prompt"}`))
	if err != nil {
		t.Fatalf("POST /api/voice/emit: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", resp.StatusCode)
	}

	line := strings.TrimSpace(buf.String())

	// Primary assertion: the grep pattern '"rewritten"' must match this line.
	if !strings.Contains(line, `"rewritten"`) {
		t.Errorf(`EventWriter output missing key "rewritten"; voci-listen grep pattern '"rewritten"' will not fire.\nGot: %s`, line)
	}
	// Inverse guard: must NOT have uppercase key (which would indicate a tag regression).
	if strings.Contains(line, `"Rewritten"`) {
		t.Errorf(`EventWriter output contains "Rewritten" (uppercase); json tag on Event struct has regressed.\nGot: %s`, line)
	}

	// Roundtrip sanity: value is preserved.
	var ev session.Event
	if err := json.Unmarshal([]byte(line), &ev); err != nil {
		t.Fatalf("unmarshal emitted event: %v", err)
	}
	if ev.Rewritten != "pwd" {
		t.Errorf("ev.Rewritten: want %q, got %q", "pwd", ev.Rewritten)
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
