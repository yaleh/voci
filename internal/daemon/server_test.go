package daemon

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yaleh/voci/internal/intent"
	"github.com/yaleh/voci/internal/pipeline"
)

func makeServer(t *testing.T, eventPath string) (*Server, *int, *[]string) {
	t.Helper()

	hintBuilderCount := 0
	capturedHints := []string{}

	srv := &Server{
		TranscribeFn: func(ctx context.Context, key, audioPath, apiURL, language string, entities []string) string {
			return "raw transcript"
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

func TestHandleTranscribePassesLanguage(t *testing.T) {
	var gotLang string
	stub := func(ctx context.Context, key, path, url, lang string, entities []string) string {
		gotLang = lang
		return "ok"
	}
	srv := &Server{
		TranscribeFn: stub,
		Language:     "en",
		APIKey:       "k",
		HintedFn: func(ctx context.Context, raw, hint string, chatFn pipeline.ChatFn) (string, error) {
			return raw, nil
		},
		RewriteFn: func(ctx context.Context, hinted, hint string, chatFn pipeline.ChatFn) (string, error) {
			return hinted, nil
		},
		ClassifyFn: func(ctx context.Context, rewritten, fullContext string, chat pipeline.ChatFn) (intent.ActionProposal, error) {
			return intent.ActionProposal{Kind: intent.KindDirectPrompt, Rewritten: rewritten}, nil
		},
	}
	h := srv.Handler()

	audioBytes := []byte("fake audio data")
	req := httptest.NewRequest(http.MethodPost, "/api/voice/transcribe", bytes.NewReader(audioBytes))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if gotLang != "en" {
		t.Errorf("want en, got %q", gotLang)
	}
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

// TestHandler_AppendsEventPerCall: /transcribe must NOT write to EventPath (emit is /emit's job).
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

	if _, err := os.Stat(eventPath); !os.IsNotExist(err) {
		t.Error("expected EventPath file NOT to be created by /transcribe")
	}
}

// TestHandler_WritesEventLineToStdout: /transcribe must NOT write to EventWriter.
func TestHandler_WritesEventLineToStdout(t *testing.T) {
	var buf bytes.Buffer
	srv, _, _ := makeServer(t, "")
	srv.EventWriter = &buf
	h := srv.Handler()

	req := httptest.NewRequest(http.MethodPost, "/api/voice/transcribe", bytes.NewReader([]byte("fake audio")))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	if buf.Len() != 0 {
		t.Errorf("expected /transcribe NOT to write to EventWriter, but got: %q", buf.String())
	}
}

// TestHandler_StdoutOneLinePerCall: /transcribe must not write any lines regardless of call count.
func TestHandler_StdoutOneLinePerCall(t *testing.T) {
	var buf bytes.Buffer
	srv, _, _ := makeServer(t, "")
	srv.EventWriter = &buf
	h := srv.Handler()

	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/voice/transcribe", bytes.NewReader([]byte("fake audio")))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("request %d: expected 200, got %d", i+1, w.Code)
		}
	}

	if buf.Len() != 0 {
		t.Errorf("expected /transcribe NOT to write to EventWriter after 2 calls, got %d bytes", buf.Len())
	}
}

func TestHandler_StillReturnsProposalJSONToHTTP(t *testing.T) {
	var buf bytes.Buffer
	srv, _, _ := makeServer(t, "")
	srv.EventWriter = &buf
	h := srv.Handler()

	req := httptest.NewRequest(http.MethodPost, "/api/voice/transcribe", bytes.NewReader([]byte("fake audio")))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var proposal intent.ActionProposal
	if err := json.NewDecoder(w.Body).Decode(&proposal); err != nil {
		t.Fatalf("HTTP response not valid ActionProposal JSON: %v", err)
	}
	if proposal.Kind != intent.KindDirectPrompt {
		t.Errorf("HTTP response Kind: got %q, want %q", proposal.Kind, intent.KindDirectPrompt)
	}
}

// TestHandler_EventPathOptional: EventWriter must remain empty when /transcribe is called.
func TestHandler_EventPathOptional(t *testing.T) {
	var buf bytes.Buffer
	srv, _, _ := makeServer(t, "") // EventPath = ""
	srv.EventWriter = &buf
	h := srv.Handler()

	req := httptest.NewRequest(http.MethodPost, "/api/voice/transcribe", bytes.NewReader([]byte("fake audio")))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if buf.Len() != 0 {
		t.Errorf("expected /transcribe NOT to write to EventWriter, got: %q", buf.String())
	}
	// No file should have been created
	if _, err := os.Stat("events.log"); !os.IsNotExist(err) {
		t.Error("expected no events.log file to be created when EventPath is empty")
	}
}

func TestHandler_HintBuilderResultReachesHintedStage(t *testing.T) {
	dir := t.TempDir()
	eventPath := filepath.Join(dir, "events.log")

	capturedHintsInHinted := []string{}

	hintBuilderCount := 0
	srv := &Server{
		TranscribeFn: func(ctx context.Context, key, audioPath, apiURL, language string, entities []string) string {
			return "raw transcript"
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

	// EventPath file must NOT be written by /transcribe (emit is /emit's job)
	if _, err := os.Stat(eventPath); !os.IsNotExist(err) {
		t.Error("expected EventPath file NOT to be created by /transcribe")
	}

	if len(capturedHintsInHinted) == 0 {
		t.Fatal("HintedFn was not called")
	}
	if capturedHintsInHinted[0] != "injected-hint-value" {
		t.Errorf("HintedFn got hint %q, want %q", capturedHintsInHinted[0], "injected-hint-value")
	}
}

// ---- Phase A: explicit no-emit tests ----

func TestTranscribe_DoesNotWriteToEventWriter(t *testing.T) {
	var buf bytes.Buffer
	srv, _, _ := makeServer(t, "")
	srv.EventWriter = &buf
	h := srv.Handler()

	req := httptest.NewRequest(http.MethodPost, "/api/voice/transcribe", bytes.NewReader([]byte("audio data")))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if buf.Len() != 0 {
		t.Errorf("handleTranscribe must not write to EventWriter; got %d bytes: %q", buf.Len(), buf.String())
	}
}

func TestTranscribe_DoesNotAppendToEventPath(t *testing.T) {
	dir := t.TempDir()
	eventPath := filepath.Join(dir, "events.log")
	srv, _, _ := makeServer(t, eventPath)
	h := srv.Handler()

	req := httptest.NewRequest(http.MethodPost, "/api/voice/transcribe", bytes.NewReader([]byte("audio data")))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if _, err := os.Stat(eventPath); !os.IsNotExist(err) {
		t.Error("handleTranscribe must not write to EventPath; file was created")
	}
}

// ---- Phase B: /api/voice/emit tests ----

func TestEmit_WritesOneEventLineToEventWriter(t *testing.T) {
	var buf bytes.Buffer
	srv, _, _ := makeServer(t, "")
	srv.EventWriter = &buf
	h := srv.Handler()

	body := strings.NewReader(`{"text":"hello world"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/voice/emit", body)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}

	lines := bytes.Split(bytes.TrimRight(buf.Bytes(), "\n"), []byte("\n"))
	if len(lines) != 1 || len(lines[0]) == 0 {
		t.Fatalf("expected 1 non-empty event line, got %d lines: %q", len(lines), buf.String())
	}

	var ev Event
	if err := json.Unmarshal(lines[0], &ev); err != nil {
		t.Fatalf("Unmarshal event: %v", err)
	}
	if ev.Rewritten != "hello world" {
		t.Errorf("ev.Rewritten: got %q, want %q", ev.Rewritten, "hello world")
	}
}

func TestEmit_RejectsNonPost(t *testing.T) {
	srv, _, _ := makeServer(t, "")
	h := srv.Handler()

	req := httptest.NewRequest(http.MethodGet, "/api/voice/emit", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", w.Code)
	}
}

func TestEmit_RejectsEmptyText(t *testing.T) {
	var buf bytes.Buffer
	srv, _, _ := makeServer(t, "")
	srv.EventWriter = &buf
	h := srv.Handler()

	for _, body := range []string{`{"text":""}`, `{"text":"   "}`, `{}`} {
		buf.Reset()
		req := httptest.NewRequest(http.MethodPost, "/api/voice/emit", strings.NewReader(body))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code < 400 || w.Code >= 500 {
			t.Errorf("body %q: expected 4xx, got %d", body, w.Code)
		}
	}
}

func TestEmit_Returns503WhenEventWriterNil(t *testing.T) {
	srv, _, _ := makeServer(t, "")
	srv.EventWriter = nil
	h := srv.Handler()

	req := httptest.NewRequest(http.MethodPost, "/api/voice/emit", strings.NewReader(`{"text":"hello"}`))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", w.Code)
	}
}

func TestEmit_WritesExactlyOneLinePerCall(t *testing.T) {
	var buf bytes.Buffer
	srv, _, _ := makeServer(t, "")
	srv.EventWriter = &buf
	h := srv.Handler()

	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/voice/emit", strings.NewReader(`{"text":"cmd"}`))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != http.StatusNoContent {
			t.Fatalf("call %d: expected 204, got %d", i+1, w.Code)
		}
	}

	lines := bytes.Split(bytes.TrimRight(buf.Bytes(), "\n"), []byte("\n"))
	if len(lines) != 2 {
		t.Errorf("expected 2 event lines, got %d", len(lines))
	}
}

func TestEmit_AlsoAppendsToEventPath(t *testing.T) {
	dir := t.TempDir()
	eventPath := filepath.Join(dir, "events.log")

	var buf bytes.Buffer
	srv, _, _ := makeServer(t, eventPath)
	srv.EventWriter = &buf
	h := srv.Handler()

	req := httptest.NewRequest(http.MethodPost, "/api/voice/emit", strings.NewReader(`{"text":"write both"}`))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}

	// EventWriter
	if buf.Len() == 0 {
		t.Error("expected EventWriter to have content")
	}

	// EventPath file
	f, err := os.Open(eventPath)
	if err != nil {
		t.Fatalf("expected EventPath file to be created: %v", err)
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	if !scanner.Scan() {
		t.Fatal("expected at least 1 event line in EventPath file")
	}
	var ev Event
	if err := json.Unmarshal(scanner.Bytes(), &ev); err != nil {
		t.Fatalf("Unmarshal EventPath line: %v", err)
	}
	if ev.Rewritten != "write both" {
		t.Errorf("EventPath ev.Rewritten: got %q, want %q", ev.Rewritten, "write both")
	}
}

func TestEmit_EventPathOptional(t *testing.T) {
	var buf bytes.Buffer
	srv, _, _ := makeServer(t, "") // EventPath = ""
	srv.EventWriter = &buf
	h := srv.Handler()

	req := httptest.NewRequest(http.MethodPost, "/api/voice/emit", strings.NewReader(`{"text":"no file"}`))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}
	if buf.Len() == 0 {
		t.Error("expected EventWriter to have content")
	}
	if _, err := os.Stat("events.log"); !os.IsNotExist(err) {
		t.Error("expected no events.log file when EventPath is empty")
	}
}

func TestEmit_PreservesKind(t *testing.T) {
	var buf bytes.Buffer
	srv, _, _ := makeServer(t, "")
	srv.EventWriter = &buf
	h := srv.Handler()

	req := httptest.NewRequest(http.MethodPost, "/api/voice/emit",
		strings.NewReader(`{"text":"做个任务","kind":"backlog_action"}`))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}

	scanner := bufio.NewScanner(&buf)
	scanner.Scan()
	var ev Event
	if err := json.Unmarshal(scanner.Bytes(), &ev); err != nil {
		t.Fatalf("unmarshal event: %v", err)
	}
	if ev.Kind != "backlog_action" {
		t.Errorf("ev.Kind: got %q, want %q", ev.Kind, "backlog_action")
	}
}

func TestEmit_DefaultsKindWhenAbsent(t *testing.T) {
	var buf bytes.Buffer
	srv, _, _ := makeServer(t, "")
	srv.EventWriter = &buf
	h := srv.Handler()

	req := httptest.NewRequest(http.MethodPost, "/api/voice/emit",
		strings.NewReader(`{"text":"hi"}`))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}

	scanner := bufio.NewScanner(&buf)
	scanner.Scan()
	var ev Event
	if err := json.Unmarshal(scanner.Bytes(), &ev); err != nil {
		t.Fatalf("unmarshal event: %v", err)
	}
	if ev.Kind != "direct_prompt" {
		t.Errorf("ev.Kind: got %q, want %q", ev.Kind, "direct_prompt")
	}
}

// Phase TASK-46: BearerToken auth on API routes

func TestHandler_StaticFilesUnprotected(t *testing.T) {
	s, _, _ := makeServer(t, "")
	s.BearerToken = "tok"
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	resp, err := http.Get(ts.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("want 200 for static, got %d", resp.StatusCode)
	}
}

func TestHandler_APIRequiresTokenWhenSet(t *testing.T) {
	s, _, _ := makeServer(t, "")
	s.BearerToken = "tok"
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	resp, err := http.Post(ts.URL+"/api/voice/emit", "application/json",
		strings.NewReader(`{"text":"hi","kind":"direct_prompt"}`))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("want 401, got %d", resp.StatusCode)
	}
}

// Phase D: /api/context endpoint

func TestHandleContext_ReturnsHint(t *testing.T) {
	srv, _, _ := makeServer(t, "")
	srv.HintFn = func(ctx context.Context) (string, error) {
		return "## Known Entities\nspoken: Foo\n", nil
	}
	h := srv.Handler()

	req := httptest.NewRequest(http.MethodGet, "/api/context", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !strings.Contains(resp["hint"], "Known Entities") {
		t.Errorf("hint missing Known Entities: %q", resp["hint"])
	}
}

func TestHandleContext_HintFnError(t *testing.T) {
	srv, _, _ := makeServer(t, "")
	srv.HintFn = func(ctx context.Context) (string, error) {
		return "", fmt.Errorf("context build failed")
	}
	h := srv.Handler()

	req := httptest.NewRequest(http.MethodGet, "/api/context", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleContext_NilHintFnReturnsEmpty(t *testing.T) {
	srv, _, _ := makeServer(t, "")
	// HintFn is nil (not set)
	h := srv.Handler()

	req := httptest.NewRequest(http.MethodGet, "/api/context", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if _, ok := resp["hint"]; !ok {
		t.Error("response must have 'hint' key even when HintFn is nil")
	}
}
