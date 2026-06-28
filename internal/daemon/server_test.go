package daemon

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yalehu/voci/internal/intent"
	"github.com/yalehu/voci/internal/pipeline"
)

func makeServer(t *testing.T, eventPath string) (*Server, *int, *[]string) {
	t.Helper()

	hintBuilderCount := 0
	capturedHints := []string{}

	srv := &Server{
		TranscribeFn: func(ctx context.Context, key, audioPath, apiURL string) (string, error) {
			return "raw transcript", nil
		},
		HintedFn: func(ctx context.Context, raw, hint string, chatFn pipeline.ChatFn) (string, error) {
			capturedHints = append(capturedHints, hint)
			return "hinted transcript", nil
		},
		RewriteFn: func(ctx context.Context, hinted, hint string, chatFn pipeline.ChatFn) (string, error) {
			return "rewritten text", nil
		},
		ClassifyFn: func(ctx context.Context, rewritten, fullContext string, chat pipeline.ChatFn) (intent.ActionProposal, error) {
			return intent.ActionProposal{
				Kind:       intent.KindDirectPrompt,
				Rewritten:  rewritten,
				Confidence: 0.95,
			}, nil
		},
		BuildHintFn: func() string {
			hintBuilderCount++
			return "test-hint"
		},
		ChatFn:    nil,
		EventPath: eventPath,
	}

	return srv, &hintBuilderCount, &capturedHints
}

func TestHandler_RejectsNonPost(t *testing.T) {
	srv, _, _ := makeServer(t, "")
	h := srv.Handler()

	req := httptest.NewRequest(http.MethodGet, "/api/voice/transcribe", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", w.Code)
	}
}

func TestHandler_RejectsEmptyBody(t *testing.T) {
	srv, _, _ := makeServer(t, "")
	h := srv.Handler()

	req := httptest.NewRequest(http.MethodPost, "/api/voice/transcribe", strings.NewReader(""))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code < 400 || w.Code >= 500 {
		t.Errorf("expected 4xx, got %d", w.Code)
	}
}

func TestHandler_RunsPipelineAndReturnsProposalJSON(t *testing.T) {
	srv, _, _ := makeServer(t, "")
	h := srv.Handler()

	audioBytes := []byte("fake audio data")
	req := httptest.NewRequest(http.MethodPost, "/api/voice/transcribe", bytes.NewReader(audioBytes))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var proposal intent.ActionProposal
	if err := json.NewDecoder(w.Body).Decode(&proposal); err != nil {
		t.Fatalf("decode proposal: %v", err)
	}

	if proposal.Kind != intent.KindDirectPrompt {
		t.Errorf("Kind: got %q, want %q", proposal.Kind, intent.KindDirectPrompt)
	}
	if proposal.Rewritten != "rewritten text" {
		t.Errorf("Rewritten: got %q, want %q", proposal.Rewritten, "rewritten text")
	}
}

func TestHandler_RebuildsHintPerCall(t *testing.T) {
	srv, count, _ := makeServer(t, "")
	h := srv.Handler()

	audioBytes := []byte("fake audio data")

	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/voice/transcribe", bytes.NewReader(audioBytes))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("request %d: expected 200, got %d: %s", i+1, w.Code, w.Body.String())
		}
	}

	if *count != 2 {
		t.Errorf("BuildHintFn called %d times, want 2", *count)
	}
}

func TestHandler_AppendsEventPerCall(t *testing.T) {
	dir := t.TempDir()
	eventPath := filepath.Join(dir, "events.log")

	srv, _, _ := makeServer(t, eventPath)
	h := srv.Handler()

	audioBytes := []byte("fake audio data")
	req := httptest.NewRequest(http.MethodPost, "/api/voice/transcribe", bytes.NewReader(audioBytes))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	data, err := os.ReadFile(eventPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	lines := bytes.Split(bytes.TrimSpace(data), []byte("\n"))
	if len(lines) != 1 {
		t.Fatalf("expected 1 event line, got %d", len(lines))
	}

	var ev Event
	if err := json.Unmarshal(lines[0], &ev); err != nil {
		t.Fatalf("Unmarshal event: %v", err)
	}

	if ev.Rewritten != "rewritten text" {
		t.Errorf("event Rewritten: got %q, want %q", ev.Rewritten, "rewritten text")
	}
}

func TestHandler_HintBuilderResultReachesHintedStage(t *testing.T) {
	dir := t.TempDir()
	eventPath := filepath.Join(dir, "events.log")

	capturedHintsInHinted := []string{}

	hintBuilderCount := 0
	srv := &Server{
		TranscribeFn: func(ctx context.Context, key, audioPath, apiURL string) (string, error) {
			return "raw transcript", nil
		},
		HintedFn: func(ctx context.Context, raw, hint string, chatFn pipeline.ChatFn) (string, error) {
			capturedHintsInHinted = append(capturedHintsInHinted, hint)
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
		BuildHintFn: func() string {
			hintBuilderCount++
			return "injected-hint-value"
		},
		EventPath: eventPath,
	}

	h := srv.Handler()
	audioBytes := []byte("fake audio data")
	req := httptest.NewRequest(http.MethodPost, "/api/voice/transcribe", bytes.NewReader(audioBytes))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	f, err := os.Open(eventPath)
	if err != nil {
		t.Fatalf("open event log: %v", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	if !scanner.Scan() {
		t.Fatal("no event lines written")
	}

	if len(capturedHintsInHinted) == 0 {
		t.Fatal("HintedFn was not called")
	}
	if capturedHintsInHinted[0] != "injected-hint-value" {
		t.Errorf("HintedFn got hint %q, want %q", capturedHintsInHinted[0], "injected-hint-value")
	}
}
