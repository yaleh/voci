package daemon

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/yaleh/voci/internal/daemon/session"
	"github.com/yaleh/voci/internal/intent/model"
	"github.com/yaleh/voci/internal/pipeline"
)

func makeServer(t *testing.T) (*Server, *int, *[]string) {
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
		BuildHintFn: func() string {
			hintBuilderCount++
			return "test-hint"
		},
		ChatFn: nil,
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
	srv, _, _ := makeServer(t)
	h := srv.Handler()

	req := httptest.NewRequest(http.MethodGet, "/api/voice/transcribe", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", w.Code)
	}
}

func TestHandler_RejectsEmptyBody(t *testing.T) {
	srv, _, _ := makeServer(t)
	h := srv.Handler()

	req := httptest.NewRequest(http.MethodPost, "/api/voice/transcribe", strings.NewReader(""))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code < 400 || w.Code >= 500 {
		t.Errorf("expected 4xx, got %d", w.Code)
	}
}

func TestHandler_RunsPipelineAndReturnsProposalJSON(t *testing.T) {
	srv, _, _ := makeServer(t)
	h := srv.Handler()

	audioBytes := []byte("fake audio data")
	req := httptest.NewRequest(http.MethodPost, "/api/voice/transcribe", bytes.NewReader(audioBytes))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var proposal model.ActionProposal
	if err := json.NewDecoder(w.Body).Decode(&proposal); err != nil {
		t.Fatalf("decode proposal: %v", err)
	}

	if proposal.Rewritten != "rewritten text" {
		t.Errorf("Rewritten: got %q, want %q", proposal.Rewritten, "rewritten text")
	}
}

func TestHandler_RebuildsHintPerCall(t *testing.T) {
	srv, count, _ := makeServer(t)
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

// TestHandler_WritesEventLineToStdout: /transcribe must NOT write to EventWriter.
func TestHandler_WritesEventLineToStdout(t *testing.T) {
	var buf bytes.Buffer
	srv, _, _ := makeServer(t)
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
	srv, _, _ := makeServer(t)
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
	srv, _, _ := makeServer(t)
	srv.EventWriter = &buf
	h := srv.Handler()

	req := httptest.NewRequest(http.MethodPost, "/api/voice/transcribe", bytes.NewReader([]byte("fake audio")))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var proposal model.ActionProposal
	if err := json.NewDecoder(w.Body).Decode(&proposal); err != nil {
		t.Fatalf("HTTP response not valid ActionProposal JSON: %v", err)
	}
}

// TestHandler_EventPathOptional: EventWriter must remain empty when /transcribe is called.


// ---- Phase A: explicit no-emit tests ----

func TestTranscribe_DoesNotWriteToEventWriter(t *testing.T) {
	var buf bytes.Buffer
	srv, _, _ := makeServer(t)
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


// ---- Phase B: /api/voice/emit tests ----

func TestEmit_WritesOneEventLineToEventWriter(t *testing.T) {
	var buf bytes.Buffer
	srv, _, _ := makeServer(t)
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

	var ev session.Event
	if err := json.Unmarshal(lines[0], &ev); err != nil {
		t.Fatalf("Unmarshal event: %v", err)
	}
	if ev.Rewritten != "hello world" {
		t.Errorf("ev.Rewritten: got %q, want %q", ev.Rewritten, "hello world")
	}
}

func TestEmit_RejectsNonPost(t *testing.T) {
	srv, _, _ := makeServer(t)
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
	srv, _, _ := makeServer(t)
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
	srv, _, _ := makeServer(t)
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
	srv, _, _ := makeServer(t)
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



func TestEmit_PreservesKind(t *testing.T) {
	var buf bytes.Buffer
	srv, _, _ := makeServer(t)
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
	var ev session.Event
	if err := json.Unmarshal(scanner.Bytes(), &ev); err != nil {
		t.Fatalf("unmarshal event: %v", err)
	}
	if ev.Kind != "backlog_action" {
		t.Errorf("ev.Kind: got %q, want %q", ev.Kind, "backlog_action")
	}
}

func TestEmit_DefaultsKindWhenAbsent(t *testing.T) {
	var buf bytes.Buffer
	srv, _, _ := makeServer(t)
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
	var ev session.Event
	if err := json.Unmarshal(scanner.Bytes(), &ev); err != nil {
		t.Fatalf("unmarshal event: %v", err)
	}
	if ev.Kind != "direct_prompt" {
		t.Errorf("ev.Kind: got %q, want %q", ev.Kind, "direct_prompt")
	}
}

// Phase TASK-46: BearerToken auth on API routes

func TestHandler_StaticFilesUnprotected(t *testing.T) {
	s, _, _ := makeServer(t)
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
	s, _, _ := makeServer(t)
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
	srv, _, _ := makeServer(t)
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
	srv, _, _ := makeServer(t)
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

// TestStartWithContext_StopsWhenContextCancelled verifies that StartWithContext
// returns when its context is cancelled, so tunnel.WatchTunnel can propagate a
// tunnel exit to the HTTP server.
func TestStartWithContext_StopsWhenContextCancelled(t *testing.T) {
	srv, _, _ := makeServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	started := make(chan struct{})
	errCh := make(chan error, 1)
	srv.OnListening = func(net.Addr) { close(started) }

	go func() {
		errCh <- srv.StartWithContext(ctx, "127.0.0.1:0")
	}()

	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("server did not start within 3s")
	}

	cancel()
	select {
	case <-errCh:
		// good: server returned after cancel
	case <-time.After(3 * time.Second):
		t.Fatal("StartWithContext did not return within 3s after context cancel")
	}
}

func TestStartWithContext_Port0_AssignsEphemeralPort(t *testing.T) {
	srv, _, _ := makeServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	addrCh := make(chan net.Addr, 1)
	srv.OnListening = func(a net.Addr) { addrCh <- a }

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.StartWithContext(ctx, "127.0.0.1:0")
	}()

	var resolved net.Addr
	select {
	case resolved = <-addrCh:
	case <-time.After(3 * time.Second):
		t.Fatal("server did not start within 3s")
	}

	_, portStr, err := net.SplitHostPort(resolved.String())
	if err != nil {
		t.Fatalf("SplitHostPort: %v", err)
	}
	if portStr == "0" || portStr == "" {
		t.Errorf("expected non-zero ephemeral port, got %q", portStr)
	}

	// Verify the server actually responds on that port.
	resp, err := http.Get("http://" + resolved.String() + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 500 {
		t.Errorf("unexpected status %d", resp.StatusCode)
	}

	cancel()
}

func TestStartWithContext_ExplicitPortConflict_ReturnsError(t *testing.T) {
	// Bind a port so it is already in use.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("pre-bind: %v", err)
	}
	defer ln.Close()
	addr := ln.Addr().String()

	srv, _, _ := makeServer(t)
	err = srv.StartWithContext(context.Background(), addr)
	if err == nil {
		t.Fatal("expected error for already-in-use port, got nil")
	}
	if !strings.Contains(err.Error(), "already in use") {
		t.Errorf("expected 'already in use' in error, got: %v", err)
	}
}

// TestStartWithContextFromListener verifies that the new method accepts a
// pre-bound listener, calls OnListening with its addr, serves requests, and
// shuts down cleanly on context cancel.
func TestStartWithContextFromListener(t *testing.T) {
	srv, _, _ := makeServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	boundAddr := ln.Addr().String()

	var gotAddr net.Addr
	srv.OnListening = func(a net.Addr) { gotAddr = a }

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.StartWithContextFromListener(ctx, ln)
	}()

	// Give the server a moment to call OnListening and start serving.
	time.Sleep(20 * time.Millisecond)
	if gotAddr == nil {
		t.Fatal("OnListening was not called")
	}
	if gotAddr.String() != boundAddr {
		t.Errorf("OnListening addr = %q, want %q", gotAddr.String(), boundAddr)
	}
	resp, httpErr := http.Get("http://" + boundAddr + "/")
	if httpErr != nil {
		t.Fatalf("GET /: %v", httpErr)
	}
	resp.Body.Close()

	cancel()
	select {
	case <-errCh:
	case <-time.After(3 * time.Second):
		t.Fatal("StartWithContextFromListener did not return within 3s after cancel")
	}
}

// ---- Timing log tests ----

func TestHandleTranscribeLogsTimings(t *testing.T) {
	var logBuf bytes.Buffer
	log.SetOutput(&logBuf)
	defer log.SetOutput(os.Stderr)

	srv, _, _ := makeServer(t)
	h := srv.Handler()

	req := httptest.NewRequest(http.MethodPost, "/api/voice/transcribe", bytes.NewReader([]byte("fake audio")))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	logOutput := logBuf.String()
	for _, want := range []string{"asr:", "hinted:", "rewrite:", "total:"} {
		if !strings.Contains(logOutput, want) {
			t.Errorf("log missing %q; got: %s", want, logOutput)
		}
	}
}

func TestHandleTranscribeLogsTimings_NilRewrite(t *testing.T) {
	var logBuf bytes.Buffer
	log.SetOutput(&logBuf)
	defer log.SetOutput(os.Stderr)

	srv, _, _ := makeServer(t)
	srv.RewriteFn = nil
	h := srv.Handler()

	req := httptest.NewRequest(http.MethodPost, "/api/voice/transcribe", bytes.NewReader([]byte("fake audio")))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	logOutput := logBuf.String()
	if !strings.Contains(logOutput, "rewrite: -") {
		t.Errorf("log should contain 'rewrite: -'; got: %s", logOutput)
	}
}

func TestHandleTranscribeLogsTimings_HintedError(t *testing.T) {
	var logBuf bytes.Buffer
	log.SetOutput(&logBuf)
	defer log.SetOutput(os.Stderr)

	srv, _, _ := makeServer(t)
	srv.HintedFn = func(ctx context.Context, raw, hint string, chatFn pipeline.ChatFn) (string, error) {
		return "", fmt.Errorf("hinted failure")
	}
	h := srv.Handler()

	req := httptest.NewRequest(http.MethodPost, "/api/voice/transcribe", bytes.NewReader([]byte("fake audio")))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}

	logOutput := logBuf.String()
	if !strings.Contains(logOutput, "asr:") {
		t.Errorf("log missing 'asr:'; got: %s", logOutput)
	}
	if !strings.Contains(logOutput, "hinted: (error)") {
		t.Errorf("log missing 'hinted: (error)'; got: %s", logOutput)
	}
}

// ---- Phase TASK-64: MergedFn path tests ----

func TestHandleTranscribe_MergedPath(t *testing.T) {
	mergedCalled := false
	srv := &Server{
		MergedFn: func(ctx context.Context, key, audioPath, hint, language string, entities []string) (model.ActionProposal, error) {
			mergedCalled = true
			return model.ActionProposal{
				RawTranscript: "r",
				Rewritten:     "w",
			}, nil
		},
		TranscribeFn: func(ctx context.Context, key, audioPath, apiURL, language string, entities []string) string {
			t.Fatal("TranscribeFn should not be called when MergedFn is set")
			return ""
		},
		HintedFn: func(ctx context.Context, raw, hint string, chatFn pipeline.ChatFn) (string, error) {
			t.Fatal("HintedFn should not be called when MergedFn is set")
			return "", nil
		},
	}
	h := srv.Handler()

	req := httptest.NewRequest(http.MethodPost, "/api/voice/transcribe", bytes.NewReader([]byte("fake audio")))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if !mergedCalled {
		t.Error("expected MergedFn to be called")
	}
	var proposal model.ActionProposal
	if err := json.NewDecoder(w.Body).Decode(&proposal); err != nil {
		t.Fatalf("decode proposal: %v", err)
	}
	if proposal.Rewritten != "w" {
		t.Errorf("Rewritten: want %q, got %q", "w", proposal.Rewritten)
	}
}


func TestHandleTranscribe_MergedError(t *testing.T) {
	srv := &Server{
		MergedFn: func(ctx context.Context, key, audioPath, hint, language string, entities []string) (model.ActionProposal, error) {
			return model.ActionProposal{}, fmt.Errorf("merged pipeline failed")
		},
	}
	h := srv.Handler()

	req := httptest.NewRequest(http.MethodPost, "/api/voice/transcribe", bytes.NewReader([]byte("fake audio")))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleContext_NilHintFnReturnsEmpty(t *testing.T) {
	srv, _, _ := makeServer(t)
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

func TestHandleTranscribe_FallbackReturnsHintedOutput(t *testing.T) {
	var logBuf bytes.Buffer
	log.SetOutput(&logBuf)
	defer log.SetOutput(os.Stderr)

	srv := &Server{
		MergedFn: nil,
		TranscribeFn: func(ctx context.Context, key, audioPath, apiURL, language string, entities []string) string {
			return "raw text"
		},
		HintedFn: func(ctx context.Context, raw, hint string, chatFn pipeline.ChatFn) (string, error) {
			return "hinted text", nil
		},
	}
	h := srv.Handler()

	req := httptest.NewRequest(http.MethodPost, "/api/voice/transcribe", bytes.NewReader([]byte("fake audio")))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var proposal model.ActionProposal
	if err := json.NewDecoder(w.Body).Decode(&proposal); err != nil {
		t.Fatalf("decode proposal: %v", err)
	}
	if proposal.RawTranscript != "raw text" {
		t.Errorf("RawTranscript: want %q, got %q", "raw text", proposal.RawTranscript)
	}
	if proposal.Rewritten != "hinted text" {
		t.Errorf("Rewritten: want %q, got %q", "hinted text", proposal.Rewritten)
	}
}

