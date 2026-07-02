package asr

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func geminiOKResponse(text string) []byte {
	resp := map[string]interface{}{
		"candidates": []map[string]interface{}{
			{
				"content": map[string]interface{}{
					"parts": []map[string]interface{}{
						{"text": text},
					},
				},
			},
		},
	}
	b, _ := json.Marshal(resp)
	return b
}

func TestTranscribeGeminiReturnsText(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(geminiOKResponse("hello"))
	}))
	defer srv.Close()

	wavPath := writeTempWav(t)
	result := TranscribeGemini(context.Background(), "test-key", wavPath, srv.URL, "", "", nil)
	if result != "hello" {
		t.Errorf("want hello, got %q", result)
	}
}

func TestTranscribeGeminiSetsXGoogAPIKeyHeader(t *testing.T) {
	var gotKey, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("x-goog-api-key")
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		w.Write(geminiOKResponse("ok"))
	}))
	defer srv.Close()

	wavPath := writeTempWav(t)
	TranscribeGemini(context.Background(), "test-key", wavPath, srv.URL, "", "", nil)

	if gotKey != "test-key" {
		t.Errorf("x-goog-api-key: want test-key, got %q", gotKey)
	}
	if gotAuth != "" {
		t.Errorf("Authorization header should be empty, got %q", gotAuth)
	}
}

func TestTranscribeGeminiRequestBodyStructure(t *testing.T) {
	type inlineData struct {
		MimeType string `json:"mimeType"`
		Data     string `json:"data"`
	}
	type part struct {
		Text       string      `json:"text,omitempty"`
		InlineData *inlineData `json:"inlineData,omitempty"`
	}
	type content struct {
		Parts []part `json:"parts"`
	}
	type body struct {
		Contents []content `json:"contents"`
	}

	var captured body
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		json.Unmarshal(raw, &captured)
		w.Header().Set("Content-Type", "application/json")
		w.Write(geminiOKResponse("ok"))
	}))
	defer srv.Close()

	wavPath := writeTempWav(t)
	TranscribeGemini(context.Background(), "key", wavPath, srv.URL, "", "", nil)

	if len(captured.Contents) == 0 || len(captured.Contents[0].Parts) < 2 {
		t.Fatalf("expected at least 2 parts in contents[0], got %+v", captured)
	}
	if captured.Contents[0].Parts[0].Text != "Transcribe the following audio." {
		t.Errorf("parts[0].text: want 'Transcribe the following audio.', got %q", captured.Contents[0].Parts[0].Text)
	}
	if captured.Contents[0].Parts[1].InlineData == nil {
		t.Fatal("parts[1].inlineData is nil")
	}
	if captured.Contents[0].Parts[1].InlineData.MimeType != "audio/wav" {
		t.Errorf("mimeType: want audio/wav, got %q", captured.Contents[0].Parts[1].InlineData.MimeType)
	}
	if _, err := base64.StdEncoding.DecodeString(captured.Contents[0].Parts[1].InlineData.Data); err != nil {
		t.Errorf("inlineData.data is not valid base64: %v", err)
	}
}

func TestTranscribeGeminiHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"invalid request"}`))
	}))
	defer srv.Close()

	wavPath := writeTempWav(t)
	result := TranscribeGemini(context.Background(), "key", wavPath, srv.URL, "", "", nil)
	if result != "" {
		t.Errorf("expected empty string on HTTP error, got %q", result)
	}
}

func TestTranscribeGeminiDefaultModel(t *testing.T) {
	var capturedPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.Write(geminiOKResponse("ok"))
	}))
	defer srv.Close()

	wavPath := writeTempWav(t)
	TranscribeGemini(context.Background(), "key", wavPath, srv.URL, "", "", nil)

	if !strings.Contains(capturedPath, DefaultGeminiModel) {
		t.Errorf("URL path should contain %q, got %q", DefaultGeminiModel, capturedPath)
	}
}

func TestTranscribeGeminiKeyNotInURL(t *testing.T) {
	const apiKey = "super-secret-key"
	var capturedQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		w.Write(geminiOKResponse("ok"))
	}))
	defer srv.Close()

	wavPath := writeTempWav(t)
	TranscribeGemini(context.Background(), apiKey, wavPath, srv.URL, "", "", nil)

	if strings.Contains(capturedQuery, apiKey) {
		t.Errorf("API key must not appear in URL query string, got: %q", capturedQuery)
	}
}

func TestTranscribeGeminiMissingFileReturnsEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(geminiOKResponse("ok"))
	}))
	defer srv.Close()

	result := TranscribeGemini(context.Background(), "key", "/nonexistent/audio.wav", srv.URL, "", "", nil)
	if result != "" {
		t.Errorf("expected empty string for missing file, got %q", result)
	}
}

func TestTranscribeGeminiConfigCPromptWhenEntities(t *testing.T) {
	type part struct {
		Text       string      `json:"text,omitempty"`
		InlineData interface{} `json:"inlineData,omitempty"`
	}
	type content struct {
		Parts []part `json:"parts"`
	}
	type body struct {
		Contents []content `json:"contents"`
	}

	var captured body
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		json.Unmarshal(raw, &captured)
		w.Header().Set("Content-Type", "application/json")
		w.Write(geminiOKResponse("ok"))
	}))
	defer srv.Close()

	wavPath := writeTempWav(t)
	TranscribeGemini(context.Background(), "key", wavPath, srv.URL, "", "", []string{"Sentry", "TASK-43"})

	if len(captured.Contents) == 0 || len(captured.Contents[0].Parts) == 0 {
		t.Fatalf("expected parts in contents[0], got %+v", captured)
	}
	promptText := captured.Contents[0].Parts[0].Text
	if !strings.Contains(promptText, "Example") {
		t.Errorf("prompt should contain 'Example', got: %q", promptText)
	}
	if !strings.Contains(promptText, "Sentry") {
		t.Errorf("prompt should contain 'Sentry', got: %q", promptText)
	}
	if !strings.Contains(promptText, "Known technical terms") {
		t.Errorf("prompt should contain 'Known technical terms', got: %q", promptText)
	}
}

func TestTranscribeGeminiConfigAFallbackNoEntities(t *testing.T) {
	type part struct {
		Text       string      `json:"text,omitempty"`
		InlineData interface{} `json:"inlineData,omitempty"`
	}
	type content struct {
		Parts []part `json:"parts"`
	}
	type body struct {
		Contents []content `json:"contents"`
	}

	var captured body
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		json.Unmarshal(raw, &captured)
		w.Header().Set("Content-Type", "application/json")
		w.Write(geminiOKResponse("ok"))
	}))
	defer srv.Close()

	wavPath := writeTempWav(t)
	TranscribeGemini(context.Background(), "key", wavPath, srv.URL, "", "", nil)

	if len(captured.Contents) == 0 || len(captured.Contents[0].Parts) == 0 {
		t.Fatalf("expected parts in contents[0], got %+v", captured)
	}
	if captured.Contents[0].Parts[0].Text != "Transcribe the following audio." {
		t.Errorf("want 'Transcribe the following audio.', got %q", captured.Contents[0].Parts[0].Text)
	}
}

func TestTranscribeMerged_ParsesJSON(t *testing.T) {
	innerJSON := `{"transcript":"raw","rewritten":"clean"}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(geminiOKResponse(innerJSON))
	}))
	defer srv.Close()

	old := geminiMergedTestBaseURL
	geminiMergedTestBaseURL = srv.URL
	defer func() { geminiMergedTestBaseURL = old }()

	wavPath := writeTempWav(t)
	proposal, err := TranscribeMerged(context.Background(), "test-key", wavPath, "", "", "", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if proposal.RawTranscript != "raw" {
		t.Errorf("RawTranscript: want %q, got %q", "raw", proposal.RawTranscript)
	}
	if proposal.Rewritten != "clean" {
		t.Errorf("Rewritten: want %q, got %q", "clean", proposal.Rewritten)
	}
}

func TestTranscribeMerged_EntityInjection(t *testing.T) {
	innerJSON := `{"transcript":"ok","rewritten":"ok"}`
	var capturedBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.Write(geminiOKResponse(innerJSON))
	}))
	defer srv.Close()

	old := geminiMergedTestBaseURL
	geminiMergedTestBaseURL = srv.URL
	defer func() { geminiMergedTestBaseURL = old }()

	wavPath := writeTempWav(t)
	_, err := TranscribeMerged(context.Background(), "key", wavPath, "", "", "", []string{"voci", "TASK-64"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	bodyStr := string(capturedBody)
	if !strings.Contains(bodyStr, "voci, TASK-64") {
		t.Errorf("request body should contain %q; got: %q", "voci, TASK-64", bodyStr)
	}
	if !strings.Contains(bodyStr, "response_mime_type") {
		t.Errorf("request body should contain %q; got: %q", "response_mime_type", bodyStr)
	}
}

func TestExtractEntities_KnownEntitiesSection(t *testing.T) {
	hint := "## Known Entities\n- spoken: Canonical\n- run hinted: RunHinted\n"
	got := ExtractEntities(hint)
	want := []string{"Canonical", "RunHinted"}
	if len(got) != len(want) {
		t.Fatalf("want %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("index %d: want %q, got %q", i, want[i], got[i])
		}
	}
}

func TestExtractEntities_Empty(t *testing.T) {
	got := ExtractEntities("")
	if len(got) != 0 {
		t.Errorf("expected empty slice, got %v", got)
	}
}

func TestExtractEntities_NoSection(t *testing.T) {
	hint := "Some context without known entities section\n## Other Section\n- item"
	got := ExtractEntities(hint)
	if len(got) != 0 {
		t.Errorf("expected empty slice, got %v", got)
	}
}

func TestExtractEntities_DynamicSection(t *testing.T) {
	hint := "## Known Entities (dynamic)\n- Sentry: Sentry\n"
	got := ExtractEntities(hint)
	want := []string{"Sentry"}
	if len(got) != len(want) {
		t.Fatalf("want %v, got %v", want, got)
	}
	if got[0] != want[0] {
		t.Errorf("want %q, got %q", want[0], got[0])
	}
}

// geminiEmptyResponse returns a geminiResponse with no candidates (for testing empty responses).
func geminiEmptyResponse() []byte {
	resp := map[string]interface{}{
		"candidates": []map[string]interface{}{},
	}
	b, _ := json.Marshal(resp)
	return b
}

func TestTranscribeMerged_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":"internal error"}`))
	}))
	defer srv.Close()

	old := geminiMergedTestBaseURL
	geminiMergedTestBaseURL = srv.URL
	defer func() { geminiMergedTestBaseURL = old }()

	wavPath := writeTempWav(t)
	_, err := TranscribeMerged(context.Background(), "test-key", wavPath, "", "", "", nil)
	if err == nil {
		t.Fatal("expected error for HTTP 500")
	}
	if !strings.Contains(err.Error(), "API error 500") {
		t.Errorf("error should mention 'API error 500', got: %v", err)
	}
}

func TestTranscribeMerged_EmptyCandidates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(geminiEmptyResponse())
	}))
	defer srv.Close()

	old := geminiMergedTestBaseURL
	geminiMergedTestBaseURL = srv.URL
	defer func() { geminiMergedTestBaseURL = old }()

	wavPath := writeTempWav(t)
	_, err := TranscribeMerged(context.Background(), "test-key", wavPath, "", "", "", nil)
	if err == nil {
		t.Fatal("expected error for empty candidates")
	}
	if !strings.Contains(err.Error(), "empty candidates") {
		t.Errorf("error should mention 'empty candidates', got: %v", err)
	}
}

func TestTranscribeMerged_InvalidInnerJSON(t *testing.T) {
	innerText := `not-json{{{`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(geminiOKResponse(innerText))
	}))
	defer srv.Close()

	old := geminiMergedTestBaseURL
	geminiMergedTestBaseURL = srv.URL
	defer func() { geminiMergedTestBaseURL = old }()

	wavPath := writeTempWav(t)
	_, err := TranscribeMerged(context.Background(), "test-key", wavPath, "", "", "", nil)
	if err == nil {
		t.Fatal("expected error for invalid inner JSON")
	}
	if !strings.Contains(err.Error(), "unmarshal inner JSON") {
		t.Errorf("error should mention 'unmarshal inner JSON', got: %v", err)
	}
}

func TestTranscribeMerged_MissingFileReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(geminiOKResponse("ok"))
	}))
	defer srv.Close()

	old := geminiMergedTestBaseURL
	geminiMergedTestBaseURL = srv.URL
	defer func() { geminiMergedTestBaseURL = old }()

	_, err := TranscribeMerged(context.Background(), "key", "/nonexistent/audio.wav", "", "", "", nil)
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	if !strings.Contains(err.Error(), "read audio") {
		t.Errorf("error should mention 'read audio', got: %v", err)
	}
}

func TestGeminiChat_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(geminiOKResponse("chat-ok"))
	}))
	defer srv.Close()

	old := geminiChatTestBaseURL
	geminiChatTestBaseURL = srv.URL
	defer func() { geminiChatTestBaseURL = old }()

	result, err := GeminiChat(context.Background(), "test-key", "", []string{"user"}, []string{"hello"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "chat-ok" {
		t.Errorf("want 'chat-ok', got %q", result)
	}
}

func TestGeminiChat_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":"internal error"}`))
	}))
	defer srv.Close()

	old := geminiChatTestBaseURL
	geminiChatTestBaseURL = srv.URL
	defer func() { geminiChatTestBaseURL = old }()

	_, err := GeminiChat(context.Background(), "test-key", "", []string{"user"}, []string{"hello"})
	if err == nil {
		t.Fatal("expected error for HTTP 500")
	}
	if !strings.Contains(err.Error(), "API error 500") {
		t.Errorf("error should mention 'API error 500', got: %v", err)
	}
}

func TestGeminiChat_EmptyCandidates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(geminiEmptyResponse())
	}))
	defer srv.Close()

	old := geminiChatTestBaseURL
	geminiChatTestBaseURL = srv.URL
	defer func() { geminiChatTestBaseURL = old }()

	_, err := GeminiChat(context.Background(), "test-key", "", []string{"user"}, []string{"hello"})
	if err == nil {
		t.Fatal("expected error for empty candidates")
	}
	if !strings.Contains(err.Error(), "empty response") {
		t.Errorf("error should mention 'empty response', got: %v", err)
	}
}

func TestTranscribeGemini_EmptyCandidates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(geminiEmptyResponse())
	}))
	defer srv.Close()

	wavPath := writeTempWav(t)
	result := TranscribeGemini(context.Background(), "key", wavPath, srv.URL, "", "", nil)
	if result != "" {
		t.Errorf("expected empty string for empty candidates, got %q", result)
	}
}

func TestTranscribeGemini_InvalidResponseJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{invalid json`))
	}))
	defer srv.Close()

	wavPath := writeTempWav(t)
	result := TranscribeGemini(context.Background(), "key", wavPath, srv.URL, "", "", nil)
	if result != "" {
		t.Errorf("expected empty string for invalid JSON, got %q", result)
	}
}

func TestTranscribeGemini_HTTPNetworkError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// accept connection but do nothing — not needed; we close server before request
	}))
	srv.Close() // close listener → connection refused on next dial
	wavPath := writeTempWav(t)
	result := TranscribeGemini(context.Background(), "key", wavPath, srv.URL, "", "", nil)
	if result != "" {
		t.Errorf("expected empty string on network error, got %q", result)
	}
}

func TestGeminiChat_HTTPNetworkError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srv.Close()

	old := geminiChatTestBaseURL
	geminiChatTestBaseURL = srv.URL
	defer func() { geminiChatTestBaseURL = old }()

	_, err := GeminiChat(context.Background(), "test-key", "", []string{"user"}, []string{"hello"})
	if err == nil {
		t.Fatal("expected error on network failure")
	}
	if !strings.Contains(err.Error(), "http") {
		t.Errorf("error should mention 'http', got: %v", err)
	}
}

func TestGeminiChat_DecodeError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{invalid`))
	}))
	defer srv.Close()

	old := geminiChatTestBaseURL
	geminiChatTestBaseURL = srv.URL
	defer func() { geminiChatTestBaseURL = old }()

	_, err := GeminiChat(context.Background(), "test-key", "", []string{"user"}, []string{"hello"})
	if err == nil {
		t.Fatal("expected error for invalid JSON response")
	}
	if !strings.Contains(err.Error(), "decode") {
		t.Errorf("error should mention 'decode', got: %v", err)
	}
}

// ---- Phase C: extractSessionSection ----

func TestExtractSessionSection_WithSection(t *testing.T) {
	hint := "## Known Entities (dynamic)\n- builder.go: builder.go\n\n## Claude Code Session\n- editing: builder.go\n- ran: go test\n\n## Next Section\n- something\n"
	got := extractSessionSection(hint)
	if !strings.Contains(got, "builder.go") {
		t.Errorf("expected builder.go in session section, got: %q", got)
	}
	if strings.Contains(got, "## Next") {
		t.Errorf("session section should not contain '## Next', got: %q", got)
	}
}

func TestExtractSessionSection_WithoutSection(t *testing.T) {
	hint := "## Known Entities (dynamic)\n- builder.go: builder.go\n\n## Other Section\n- something\n"
	got := extractSessionSection(hint)
	if got != "" {
		t.Errorf("expected empty string, got: %q", got)
	}
}

func TestTranscribeMerged_PromptIncludesSessionContext(t *testing.T) {
	innerJSON := `{"transcript":"ok","rewritten":"ok"}`
	var capturedBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.Write(geminiOKResponse(innerJSON))
	}))
	defer srv.Close()

	old := geminiMergedTestBaseURL
	geminiMergedTestBaseURL = srv.URL
	defer func() { geminiMergedTestBaseURL = old }()

	wavPath := writeTempWav(t)
	hint := "## Claude Code Session\n- editing: recorder.src.js\n- ran: go test\n"
	_, err := TranscribeMerged(context.Background(), "key", wavPath, hint, "", "", []string{"voci"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	bodyStr := string(capturedBody)
	if !strings.Contains(bodyStr, "recorder.src.js") {
		t.Errorf("request body should contain recorder.src.js; got: %q", bodyStr)
	}
}

func TestTranscribeMerged_PromptNoSessionSection_Graceful(t *testing.T) {
	innerJSON := `{"transcript":"ok","rewritten":"ok"}`
	var capturedBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.Write(geminiOKResponse(innerJSON))
	}))
	defer srv.Close()

	old := geminiMergedTestBaseURL
	geminiMergedTestBaseURL = srv.URL
	defer func() { geminiMergedTestBaseURL = old }()

	wavPath := writeTempWav(t)
	hint := "## Known Entities (dynamic)\n- voci: voci\n"
	_, err := TranscribeMerged(context.Background(), "key", wavPath, hint, "", "", []string{"voci"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	bodyStr := string(capturedBody)
	if strings.Contains(bodyStr, "{SESSION_PLACEHOLDER}") {
		t.Errorf("request body should NOT contain literal SESSION_PLACEHOLDER; got: %q", bodyStr)
	}
}

