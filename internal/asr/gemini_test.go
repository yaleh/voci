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

	intentmodel "github.com/yaleh/voci/internal/intent/model"
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
	innerJSON := `{"transcript":"raw","rewritten":"clean","kind":"direct_prompt","confidence":0.9}`
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
	if proposal.Kind != intentmodel.KindDirectPrompt {
		t.Errorf("Kind: want %q, got %q", intentmodel.KindDirectPrompt, proposal.Kind)
	}
	if proposal.Confidence != 0.9 {
		t.Errorf("Confidence: want 0.9, got %f", proposal.Confidence)
	}
}

func TestTranscribeMerged_EntityInjection(t *testing.T) {
	innerJSON := `{"transcript":"ok","rewritten":"ok","kind":"query","confidence":0.5}`
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
