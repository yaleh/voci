package intent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yaleh/voci/internal/intent/model"
	"github.com/yaleh/voci/internal/ollama"
	"github.com/yaleh/voci/internal/pipeline"
)

// makeMockChatFn creates a ChatFn backed by an httptest.Server that returns the
// given JSON response body for each call. The caller is responsible for closing
// the server after the test.
func makeMockChatFn(t *testing.T, responseJSON string, statusCode int) (pipeline.ChatFn, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if statusCode != http.StatusOK {
			http.Error(w, "server error", statusCode)
			return
		}
		// Return a streaming NDJSON response (single chunk, done=true)
		chunk := map[string]interface{}{
			"message": map[string]string{"content": responseJSON},
			"done":    true,
		}
		line, _ := json.Marshal(chunk)
		fmt.Fprintf(w, "%s\n", line)
	}))

	chatFn := func(ctx context.Context, messages []ollama.Message) (string, error) {
		return ollama.Chat(ctx, srv.URL, "test-model", messages)
	}
	return chatFn, srv
}

func TestClassifyDirectPrompt(t *testing.T) {
	resp := `{"kind":"direct_prompt","confidence":0.92}`
	chatFn, srv := makeMockChatFn(t, resp, http.StatusOK)
	defer srv.Close()

	proposal, err := Classify(context.Background(), "add logging to auth.go", "", chatFn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if proposal.Kind != model.KindDirectPrompt {
		t.Errorf("Kind = %q, want %q", proposal.Kind, model.KindDirectPrompt)
	}
	if proposal.Confidence < 0.0 || proposal.Confidence > 1.0 {
		t.Errorf("Confidence %v out of range [0,1]", proposal.Confidence)
	}
}

func TestClassifyBacklogAction(t *testing.T) {
	resp := `{"kind":"backlog_action","confidence":0.88}`
	chatFn, srv := makeMockChatFn(t, resp, http.StatusOK)
	defer srv.Close()

	proposal, err := Classify(context.Background(), "mark TASK-5 as done", "", chatFn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if proposal.Kind != model.KindBacklogAction {
		t.Errorf("Kind = %q, want %q", proposal.Kind, model.KindBacklogAction)
	}
}

func TestClassifyQuery(t *testing.T) {
	resp := `{"kind":"query","confidence":0.75}`
	chatFn, srv := makeMockChatFn(t, resp, http.StatusOK)
	defer srv.Close()

	proposal, err := Classify(context.Background(), "what tasks are open?", "", chatFn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if proposal.Kind != model.KindQuery {
		t.Errorf("Kind = %q, want %q", proposal.Kind, model.KindQuery)
	}
}

func TestClassifyAmbiguous(t *testing.T) {
	resp := `{"kind":"ambiguous","confidence":0.3}`
	chatFn, srv := makeMockChatFn(t, resp, http.StatusOK)
	defer srv.Close()

	proposal, err := Classify(context.Background(), "make it better", "", chatFn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if proposal.Kind != model.KindAmbiguous {
		t.Errorf("Kind = %q, want %q", proposal.Kind, model.KindAmbiguous)
	}
}

func TestClassifyRewrittenFieldMatches(t *testing.T) {
	resp := `{"kind":"direct_prompt","confidence":0.9}`
	chatFn, srv := makeMockChatFn(t, resp, http.StatusOK)
	defer srv.Close()

	rewritten := "refactor the auth module"
	proposal, err := Classify(context.Background(), rewritten, "", chatFn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if proposal.Rewritten != rewritten {
		t.Errorf("Rewritten = %q, want %q", proposal.Rewritten, rewritten)
	}
}

func TestClassifyContextUsedPopulated(t *testing.T) {
	resp := `{"kind":"query","confidence":0.8}`
	chatFn, srv := makeMockChatFn(t, resp, http.StatusOK)
	defer srv.Close()

	proposal, err := Classify(context.Background(), "what tasks are open?", "some context", chatFn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if proposal.ContextUsed == "" {
		t.Error("ContextUsed should be non-empty when fullContext is provided")
	}
}

func TestClassifyContextUsedEmptyWhenNoContext(t *testing.T) {
	resp := `{"kind":"query","confidence":0.8}`
	chatFn, srv := makeMockChatFn(t, resp, http.StatusOK)
	defer srv.Close()

	proposal, err := Classify(context.Background(), "what tasks are open?", "", chatFn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if proposal.ContextUsed != "" {
		t.Errorf("ContextUsed = %q, want empty when no context provided", proposal.ContextUsed)
	}
}

func TestClassifyHTTP500ReturnsError(t *testing.T) {
	chatFn, srv := makeMockChatFn(t, "", http.StatusInternalServerError)
	defer srv.Close()

	_, err := Classify(context.Background(), "do something", "", chatFn)
	if err == nil {
		t.Error("expected non-nil error on HTTP 500")
	}
}

func TestClassifyConfidenceClamped(t *testing.T) {
	resp := `{"kind":"direct_prompt","confidence":1.5}`
	chatFn, srv := makeMockChatFn(t, resp, http.StatusOK)
	defer srv.Close()

	proposal, err := Classify(context.Background(), "fix the bug", "", chatFn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if proposal.Confidence > 1.0 {
		t.Errorf("Confidence %v not clamped to 1.0", proposal.Confidence)
	}
}

func TestClassifyConfidenceClampedBelow(t *testing.T) {
	resp := `{"kind":"direct_prompt","confidence":-0.5}`
	chatFn, srv := makeMockChatFn(t, resp, http.StatusOK)
	defer srv.Close()

	proposal, err := Classify(context.Background(), "fix the bug", "", chatFn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if proposal.Confidence < 0.0 {
		t.Errorf("Confidence %v not clamped to 0.0", proposal.Confidence)
	}
}

func TestClassifyInvalidKindFallsBackToAmbiguous(t *testing.T) {
	resp := `{"kind":"unknown_kind","confidence":0.6}`
	chatFn, srv := makeMockChatFn(t, resp, http.StatusOK)
	defer srv.Close()

	proposal, err := Classify(context.Background(), "do something", "", chatFn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if proposal.Kind != model.KindAmbiguous {
		t.Errorf("expected model.KindAmbiguous for invalid kind, got %q", proposal.Kind)
	}
	if proposal.Confidence != 0 {
		t.Errorf("expected Confidence=0 for invalid kind, got %v", proposal.Confidence)
	}
}

func TestClassifyUnparsableResponseFallsBackToAmbiguous(t *testing.T) {
	resp := `not valid json at all`
	chatFn, srv := makeMockChatFn(t, resp, http.StatusOK)
	defer srv.Close()

	proposal, err := Classify(context.Background(), "do something", "", chatFn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if proposal.Kind != model.KindAmbiguous {
		t.Errorf("expected model.KindAmbiguous for unparsable response, got %q", proposal.Kind)
	}
}

func TestClassifyMessagesContainRewritten(t *testing.T) {
	resp := `{"kind":"direct_prompt","confidence":0.9}`
	var capturedMessages []ollama.Message
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		chunk := map[string]interface{}{
			"message": map[string]string{"content": resp},
			"done":    true,
		}
		line, _ := json.Marshal(chunk)
		fmt.Fprintf(w, "%s\n", line)
	}))
	defer srv.Close()

	chatFn := func(ctx context.Context, messages []ollama.Message) (string, error) {
		capturedMessages = messages
		return ollama.Chat(ctx, srv.URL, "test-model", messages)
	}

	rewritten := "write unit tests for the parser"
	_, err := Classify(context.Background(), rewritten, "", chatFn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	allContent := ""
	for _, m := range capturedMessages {
		allContent += m.Content
	}
	if !strings.Contains(allContent, rewritten) {
		t.Errorf("messages do not contain rewritten text %q; got: %s", rewritten, allContent)
	}
}
