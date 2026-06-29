package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yaleh/voci/internal/intent"
	"github.com/yaleh/voci/internal/ollama"
	"github.com/yaleh/voci/internal/pipeline"
)

// newTestServer returns a Server with simple stub functions for basic tests.
func newTestServer() *Server {
	return newDeterministicServer("raw transcript", "rewritten text")
}

// newDeterministicServer constructs a *Server with four deterministic stubs.
// rawASR is returned by TranscribeFn; rewritten is returned by RewriteFn and set on the proposal.
func newDeterministicServer(rawASR, rewritten string) *Server {
	return NewServer(
		func(ctx context.Context, key, audioPath, apiURL, language string, entities []string) string {
			return rawASR
		},
		func(ctx context.Context, raw, hint string, chatFn pipeline.ChatFn) (string, error) {
			return "hinted text", nil
		},
		func(ctx context.Context, hinted, hint string, chatFn pipeline.ChatFn) (string, error) {
			return rewritten, nil
		},
		func(ctx context.Context, r, fullContext string, chat pipeline.ChatFn) (intent.ActionProposal, error) {
			return intent.ActionProposal{
				Kind:      intent.KindDirectPrompt,
				Rewritten: r,
			}, nil
		},
		"test-api-key",
		func(ctx context.Context, messages []ollama.Message) (string, error) {
			return "chat response", nil
		},
		"",
		"zh",
	)
}

// postJSON sends a JSON POST request to the test server and returns the response.
func postJSON(t *testing.T, ts *httptest.Server, body string) *http.Response {
	t.Helper()
	resp, err := http.Post(ts.URL, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST failed: %v", err)
	}
	return resp
}

// decodeResponse decodes the JSON-RPC Response from an HTTP response.
func decodeResponse(t *testing.T, resp *http.Response) Response {
	t.Helper()
	var r Response
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		t.Fatalf("decode response failed: %v", err)
	}
	return r
}

// decodeProposal extracts and JSON-decodes the inner ActionProposal from the MCP result content.
func decodeProposal(t *testing.T, r Response) intent.ActionProposal {
	t.Helper()
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
	var proposal intent.ActionProposal
	if err := json.Unmarshal([]byte(text), &proposal); err != nil {
		t.Fatalf("content[0].text is not valid JSON ActionProposal: %v", err)
	}
	return proposal
}
