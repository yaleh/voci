//go:build e2e

package mcp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/yaleh/voci/internal/intent/model"
	"github.com/yaleh/voci/internal/ollama"
	"github.com/yaleh/voci/internal/pipeline"
)

// TestE2E_HappyPath_FullPipeline tests the full pipeline end-to-end via HTTP.
func TestE2E_HappyPath_FullPipeline(t *testing.T) {
	const rawASR = "raw-asr-output"
	const rewritten = "rewritten-output"

	srv := newDeterministicServer(rawASR, rewritten)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"mcp__voci__transcribe","arguments":{"audio_path":"/tmp/test.wav"}}}`
	resp := postJSON(t, ts, body)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d", resp.StatusCode)
	}

	r := decodeResponse(t, resp)
	if r.Error != nil {
		t.Fatalf("unexpected JSON-RPC error: %v", r.Error)
	}

	proposal := decodeProposal(t, r)

	if proposal.RawTranscript != rawASR {
		t.Errorf("RawTranscript: expected %q, got %q", rawASR, proposal.RawTranscript)
	}
	if proposal.Kind != model.KindDirectPrompt {
		t.Errorf("Kind: expected %q, got %q", model.KindDirectPrompt, proposal.Kind)
	}
	if proposal.Rewritten != rewritten {
		t.Errorf("Rewritten: expected %q, got %q", rewritten, proposal.Rewritten)
	}
}

// TestE2E_ASRError_Returns32603 tests that an ASR failure returns JSON-RPC error code -32603.
func TestE2E_ASRError_Returns32603(t *testing.T) {
	srv := NewServer(
		func(ctx context.Context, key, audioPath, apiURL string) (string, error) {
			return "", errors.New("asr failed")
		},
		func(ctx context.Context, raw, hint string, chatFn pipeline.ChatFn) (string, error) {
			return "hinted", nil
		},
		func(ctx context.Context, hinted, hint string, chatFn pipeline.ChatFn) (string, error) {
			return "rewritten", nil
		},
		func(ctx context.Context, r, fullContext string, chat pipeline.ChatFn) (model.ActionProposal, error) {
			return model.ActionProposal{}, nil
		},
		"test-key",
		func(ctx context.Context, messages []ollama.Message) (string, error) {
			return "ok", nil
		},
		"",
	)

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	body := `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"mcp__voci__transcribe","arguments":{"audio_path":"/tmp/test.wav"}}}`
	resp := postJSON(t, ts, body)
	defer resp.Body.Close()

	r := decodeResponse(t, resp)
	if r.Error == nil {
		t.Fatal("expected JSON-RPC error when ASR fails")
	}
	if r.Error.Code != -32603 {
		t.Errorf("expected error code -32603, got %d", r.Error.Code)
	}
}

// TestE2E_MissingAudioPath_Returns32602 tests that omitting audio_path returns JSON-RPC error code -32602.
func TestE2E_MissingAudioPath_Returns32602(t *testing.T) {
	srv := newDeterministicServer("raw", "rewritten")
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	body := `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"mcp__voci__transcribe","arguments":{}}}`
	resp := postJSON(t, ts, body)
	defer resp.Body.Close()

	r := decodeResponse(t, resp)
	if r.Error == nil {
		t.Fatal("expected JSON-RPC error when audio_path is missing")
	}
	if r.Error.Code != -32602 {
		t.Errorf("expected error code -32602, got %d", r.Error.Code)
	}
}
