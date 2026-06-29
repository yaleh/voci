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
	result := TranscribeGemini(context.Background(), "test-key", wavPath, srv.URL, "", "")
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
	TranscribeGemini(context.Background(), "test-key", wavPath, srv.URL, "", "")

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
	TranscribeGemini(context.Background(), "key", wavPath, srv.URL, "", "")

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
	result := TranscribeGemini(context.Background(), "key", wavPath, srv.URL, "", "")
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
	TranscribeGemini(context.Background(), "key", wavPath, srv.URL, "", "")

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
	TranscribeGemini(context.Background(), apiKey, wavPath, srv.URL, "", "")

	if strings.Contains(capturedQuery, apiKey) {
		t.Errorf("API key must not appear in URL query string, got: %q", capturedQuery)
	}
}

func TestTranscribeGeminiMissingFileReturnsEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(geminiOKResponse("ok"))
	}))
	defer srv.Close()

	result := TranscribeGemini(context.Background(), "key", "/nonexistent/audio.wav", srv.URL, "", "")
	if result != "" {
		t.Errorf("expected empty string for missing file, got %q", result)
	}
}
