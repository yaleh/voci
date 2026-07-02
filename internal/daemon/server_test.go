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
	"path/filepath"
	"strings"
	"testing"
	"time"

	vocicontext "github.com/yaleh/voci/internal/context"
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

func TestHandleContext_ReturnsDialogue(t *testing.T) {
	srv, _, _ := makeServer(t)
	srv.HintFn = func(ctx context.Context) (string, error) { return "", nil }
	// A GFM table with a preceding blank line — the exact structure that must
	// survive transmission for marked to render it as a <table>.
	md := "总结：\n\n| 列A | 列B |\n|---|---|\n| 值1 | 值2 |"
	srv.DialogueFn = func(ctx context.Context) ([]vocicontext.DialogueTurn, error) {
		return []vocicontext.DialogueTurn{
			{Role: "user", Text: "问题？"},
			{Role: "assistant", Text: md},
		}, nil
	}
	h := srv.Handler()

	req := httptest.NewRequest(http.MethodGet, "/api/context", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Hint     string                     `json:"hint"`
		Dialogue []vocicontext.DialogueTurn `json:"dialogue"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.Dialogue) != 2 {
		t.Fatalf("expected 2 dialogue turns, got %d: %+v", len(resp.Dialogue), resp.Dialogue)
	}
	if resp.Dialogue[0].Role != "user" || resp.Dialogue[0].Text != "问题？" {
		t.Errorf("turn 0 wrong: %+v", resp.Dialogue[0])
	}
	// Transmission fidelity: the markdown (incl. blank line before table) must
	// round-trip byte-for-byte through JSON.
	if resp.Dialogue[1].Text != md {
		t.Errorf("markdown not preserved through JSON.\n got: %q\nwant: %q", resp.Dialogue[1].Text, md)
	}
	if !strings.Contains(resp.Dialogue[1].Text, "总结：\n\n|") {
		t.Errorf("blank line before table lost in transmission: %q", resp.Dialogue[1].Text)
	}
}

// TestHandleContext_NoDialogueFn verifies the dialogue field is omitted when
// DialogueFn is nil, keeping the response a plain {hint} object.
func TestHandleContext_NoDialogueFn(t *testing.T) {
	srv, _, _ := makeServer(t)
	srv.HintFn = func(ctx context.Context) (string, error) { return "h", nil }
	// DialogueFn intentionally nil.
	h := srv.Handler()

	req := httptest.NewRequest(http.MethodGet, "/api/context", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if _, ok := parseJSONKeys(t, w.Body.Bytes())["dialogue"]; ok {
		t.Errorf("dialogue key must be absent when DialogueFn is nil: %s", w.Body.String())
	}
}

func parseJSONKeys(t *testing.T, b []byte) map[string]json.RawMessage {
	t.Helper()
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return m
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

// /api/config endpoint (D-class VAD tuning values)

func TestHandleConfig_ReturnsVADFields(t *testing.T) {
	srv, _, _ := makeServer(t)
	srv.VADThreshold = 0.05
	srv.MinAudioMs = 500
	h := srv.Handler()

	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["vadThreshold"] != 0.05 {
		t.Errorf("vadThreshold = %v, want 0.05", resp["vadThreshold"])
	}
	if resp["minAudioMs"] != float64(500) {
		t.Errorf("minAudioMs = %v, want 500", resp["minAudioMs"])
	}
}

func TestHandleConfig_DefaultsWhenUnset(t *testing.T) {
	srv, _, _ := makeServer(t)
	h := srv.Handler()

	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["vadThreshold"] != float64(0) {
		t.Errorf("vadThreshold = %v, want 0", resp["vadThreshold"])
	}
	if resp["minAudioMs"] != float64(0) {
		t.Errorf("minAudioMs = %v, want 0", resp["minAudioMs"])
	}
}

func TestHandleConfig_RequiresTokenWhenSet(t *testing.T) {
	srv, _, _ := makeServer(t)
	srv.BearerToken = "tok"
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	resp, err := http.Get(ts.URL + "/api/config")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("want 401, got %d", resp.StatusCode)
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

// ── /api/activity SSE endpoint ────────────────────────────

func TestHandleActivity_RequiresTokenWhenSet(t *testing.T) {
	srv := &Server{
		BearerToken:    "secure-token",
		ActivityPathFn: func() string { return "" },
	}
	h := srv.Handler()

	req := httptest.NewRequest(http.MethodGet, "/api/activity", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 when token required, got %d", w.Code)
	}
}

func TestHandleActivity_ContentTypeSSE(t *testing.T) {
	srv := &Server{ActivityPathFn: func() string { return "" }}
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/activity")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	ct := resp.Header.Get("Content-Type")
	if ct != "text/event-stream" {
		t.Errorf("Content-Type: want text/event-stream, got %q", ct)
	}
}

func TestHandleActivity_NoSession(t *testing.T) {
	srv := &Server{ActivityPathFn: func() string { return "" }}
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/activity")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	// Read the first few bytes to verify we get an SSE stream with idle events.
	buf := make([]byte, 4096)
	timeout := time.After(1 * time.Second)
	readDone := make(chan int)
	go func() {
		n, _ := resp.Body.Read(buf)
		readDone <- n
	}()
	select {
	case n := <-readDone:
		body := string(buf[:n])
		if !strings.Contains(body, "event: idle") {
			t.Errorf("expected idle event, got body: %q", truncStr(body, 200))
		}
	case <-timeout:
		t.Error("timeout waiting for SSE event")
	}
}

func TestHandleActivity_StreamsToolCall(t *testing.T) {
	dir := t.TempDir()
	jsonlPath := filepath.Join(dir, "test.jsonl")

	toolLine := `{"isCompacted":"",` +
		`"message":{"content":[{"type":"tool_use","id":"1","name":"Read","input":{"file_path":"/test/file.go"}}]}}` + "\n"
	if err := os.WriteFile(jsonlPath, []byte(toolLine), 0644); err != nil {
		t.Fatal(err)
	}

	srv := &Server{ActivityPathFn: func() string { return jsonlPath }}
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/activity")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	buf := make([]byte, 4096)
	timeout := time.After(2 * time.Second)
	readDone := make(chan int)
	go func() {
		n, _ := resp.Body.Read(buf)
		readDone <- n
	}()
	select {
	case n := <-readDone:
		body := string(buf[:n])
		if !strings.Contains(body, "event: tool_call") {
			t.Errorf("expected tool_call event, got body: %q", truncStr(body, 300))
		}
	case <-timeout:
		t.Error("timeout waiting for tool_call event")
	}
}

func truncStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

