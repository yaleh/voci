package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/yalehu/voci/internal/intent"
	"github.com/yalehu/voci/internal/ollama"
	"github.com/yalehu/voci/internal/pipeline"
)

// Phase A tests

func TestServer_ToolsList(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp := postJSON(t, ts, `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	r := decodeResponse(t, resp)
	if r.Error != nil {
		t.Fatalf("unexpected error: %v", r.Error)
	}

	// Result should contain "tools" array with "mcp__voci__transcribe"
	resultMap, ok := r.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("result is not a map: %T", r.Result)
	}
	tools, ok := resultMap["tools"].([]interface{})
	if !ok {
		t.Fatalf("tools is not an array: %T", resultMap["tools"])
	}
	if len(tools) == 0 {
		t.Fatal("tools array is empty")
	}

	found := false
	for _, toolRaw := range tools {
		toolMap, ok := toolRaw.(map[string]interface{})
		if !ok {
			continue
		}
		if toolMap["name"] == "mcp__voci__transcribe" {
			found = true
			break
		}
	}
	if !found {
		t.Error("mcp__voci__transcribe not found in tools list")
	}
}

func TestServer_UnknownMethod(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp := postJSON(t, ts, `{"jsonrpc":"2.0","id":1,"method":"unknown/method","params":{}}`)
	defer resp.Body.Close()

	r := decodeResponse(t, resp)
	if r.Error == nil {
		t.Fatal("expected error for unknown method")
	}
	if r.Error.Code != -32601 {
		t.Errorf("expected error code -32601, got %d", r.Error.Code)
	}
}

func TestServer_MalformedRequest(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Post(ts.URL, "application/json", bytes.NewReader([]byte("not-json{{{")))
	if err != nil {
		t.Fatalf("POST failed: %v", err)
	}
	defer resp.Body.Close()

	// Either HTTP 400 or JSON-RPC parse error
	if resp.StatusCode == http.StatusBadRequest {
		return // acceptable
	}
	if resp.StatusCode == http.StatusOK {
		r := decodeResponse(t, resp)
		if r.Error == nil {
			t.Fatal("expected error for malformed request")
		}
		if r.Error.Code != -32700 {
			t.Errorf("expected parse error code -32700, got %d", r.Error.Code)
		}
	} else {
		t.Errorf("unexpected status code: %d", resp.StatusCode)
	}
}

func TestServer_InitializeMethod(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp := postJSON(t, ts, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"0.0.1"}}}`)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	r := decodeResponse(t, resp)
	if r.Error != nil {
		t.Fatalf("expected no error for initialize, got: %v", r.Error)
	}

	result, ok := r.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("result is not a map: %T", r.Result)
	}
	if result["protocolVersion"] == nil {
		t.Error("initialize response missing protocolVersion")
	}
	if result["capabilities"] == nil {
		t.Error("initialize response missing capabilities")
	}
	if result["serverInfo"] == nil {
		t.Error("initialize response missing serverInfo")
	}
}

func TestServer_NotificationsInitialized(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp := postJSON(t, ts, `{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", resp.StatusCode)
	}
}

// Phase B tests

func TestServer_ToolsCall_HappyPath(t *testing.T) {
	srv := NewServer(
		func(ctx context.Context, key, audioPath, apiURL string) (string, error) {
			return "raw", nil
		},
		func(ctx context.Context, raw, hint string, chatFn pipeline.ChatFn) (string, error) {
			return "hinted", nil
		},
		func(ctx context.Context, hinted, hint string, chatFn pipeline.ChatFn) (string, error) {
			return "rewritten", nil
		},
		func(ctx context.Context, rewritten, fullContext string, chat pipeline.ChatFn) (intent.ActionProposal, error) {
			return intent.ActionProposal{
				Kind:      intent.KindDirectPrompt,
				Rewritten: "rewritten",
			}, nil
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
	if r.Error != nil {
		t.Fatalf("unexpected error: %v", r.Error)
	}

	resultMap, ok := r.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("result is not a map: %T", r.Result)
	}
	content, ok := resultMap["content"].([]interface{})
	if !ok || len(content) == 0 {
		t.Fatalf("content is missing or empty")
	}
	item, ok := content[0].(map[string]interface{})
	if !ok {
		t.Fatalf("content[0] is not a map: %T", content[0])
	}
	text, ok := item["text"].(string)
	if !ok || text == "" {
		t.Fatal("content[0].text is missing or empty")
	}

	// text should be valid JSON containing "kind":"direct_prompt"
	var proposal map[string]interface{}
	if err := json.Unmarshal([]byte(text), &proposal); err != nil {
		t.Fatalf("content[0].text is not valid JSON: %v", err)
	}
	if proposal["Kind"] != string(intent.KindDirectPrompt) {
		t.Errorf("expected kind=direct_prompt, got: %v", proposal["Kind"])
	}
}

func TestServer_ToolsCall_WrongToolName(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	body := `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"unknown_tool","arguments":{"audio_path":"/tmp/test.wav"}}}`
	resp := postJSON(t, ts, body)
	defer resp.Body.Close()

	r := decodeResponse(t, resp)
	if r.Error == nil {
		t.Fatal("expected error for wrong tool name")
	}
}

func TestServer_ToolsCall_MissingAudioPath(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	body := `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"mcp__voci__transcribe","arguments":{}}}`
	resp := postJSON(t, ts, body)
	defer resp.Body.Close()

	r := decodeResponse(t, resp)
	if r.Error == nil {
		t.Fatal("expected error for missing audio_path")
	}
	if r.Error.Code != -32602 {
		t.Errorf("expected error code -32602, got %d", r.Error.Code)
	}
}

func TestServer_ToolsCall_ASRError(t *testing.T) {
	srv := NewServer(
		func(ctx context.Context, key, audioPath, apiURL string) (string, error) {
			return "", errors.New("ASR service unavailable")
		},
		func(ctx context.Context, raw, hint string, chatFn pipeline.ChatFn) (string, error) {
			return "hinted", nil
		},
		func(ctx context.Context, hinted, hint string, chatFn pipeline.ChatFn) (string, error) {
			return "rewritten", nil
		},
		func(ctx context.Context, rewritten, fullContext string, chat pipeline.ChatFn) (intent.ActionProposal, error) {
			return intent.ActionProposal{}, nil
		},
		"test-key",
		func(ctx context.Context, messages []ollama.Message) (string, error) {
			return "ok", nil
		},
		"",
	)

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	body := `{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"mcp__voci__transcribe","arguments":{"audio_path":"/tmp/test.wav"}}}`
	resp := postJSON(t, ts, body)
	defer resp.Body.Close()

	r := decodeResponse(t, resp)
	if r.Error == nil {
		t.Fatal("expected error when ASR fails")
	}
	if r.Error.Code != -32603 {
		t.Errorf("expected error code -32603, got %d", r.Error.Code)
	}
}

// TestServer_ToolsCall_RawTranscript asserts that RawTranscript is populated from the ASR output.
func TestServer_ToolsCall_RawTranscript(t *testing.T) {
	const expectedRaw = "raw-asr-output"

	srv := newDeterministicServer(expectedRaw, "rewritten text")
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	body := `{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"mcp__voci__transcribe","arguments":{"audio_path":"/tmp/test.wav"}}}`
	resp := postJSON(t, ts, body)
	defer resp.Body.Close()

	r := decodeResponse(t, resp)
	if r.Error != nil {
		t.Fatalf("unexpected error: %v", r.Error)
	}

	proposal := decodeProposal(t, r)
	if proposal.RawTranscript != expectedRaw {
		t.Errorf("expected RawTranscript=%q, got %q", expectedRaw, proposal.RawTranscript)
	}
}
