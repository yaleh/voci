package asr

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func writeTempWav(t *testing.T) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "test-*.wav")
	if err != nil {
		t.Fatal(err)
	}
	// Write minimal WAV-like data
	f.Write([]byte("RIFF\x00\x00\x00\x00WAVEfmt "))
	f.Close()
	return f.Name()
}

func TestTranscribeReturnsText(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"text":"hello"}`))
	}))
	defer srv.Close()

	wavPath := writeTempWav(t)
	text := Transcribe(context.Background(), "sk-test", wavPath, srv.URL, "zh")
	if text != "hello" {
		t.Errorf("expected 'hello', got %q", text)
	}
}

func TestTranscribeSendsMultipartWithModel(t *testing.T) {
	var capturedCT string
	var capturedBody []byte

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedCT = r.Header.Get("Content-Type")
		capturedBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"text":"ok"}`))
	}))
	defer srv.Close()

	wavPath := writeTempWav(t)
	_ = Transcribe(context.Background(), "sk-test", wavPath, srv.URL, "zh")

	if !strings.HasPrefix(capturedCT, "multipart/form-data") {
		t.Errorf("expected multipart/form-data, got %q", capturedCT)
	}

	bodyStr := string(capturedBody)
	if !strings.Contains(bodyStr, "TeleAI/TeleSpeechASR") {
		t.Errorf("expected model field in body, got: %s", bodyStr)
	}
}

func TestTranscribeHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal error"))
	}))
	defer srv.Close()

	wavPath := writeTempWav(t)
	result := Transcribe(context.Background(), "sk-test", wavPath, srv.URL, "zh")
	if result != "" {
		t.Errorf("expected empty string on HTTP error, got %q", result)
	}
}

func TestTranscribeZhUsesTelespeech(t *testing.T) {
	// httptest server that captures request body
	var capturedModel string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseMultipartForm(1 << 20)
		capturedModel = r.FormValue("model")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"choices": []map[string]interface{}{{"message": map[string]interface{}{"content": "transcribed"}}},
		})
	}))
	defer srv.Close()
	// Create a temp audio file
	f, _ := os.CreateTemp("", "*.wav")
	f.Close()
	defer os.Remove(f.Name())
	result := Transcribe(context.Background(), "key", f.Name(), srv.URL, "zh")
	if !strings.Contains(capturedModel, "TeleSpeechASR") {
		t.Errorf("zh should use TeleSpeechASR, got model=%q, result=%q", capturedModel, result)
	}
}

func TestTranscribeEnUsesWhisper(t *testing.T) {
	var capturedModel string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseMultipartForm(1 << 20)
		capturedModel = r.FormValue("model")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"choices": []map[string]interface{}{{"message": map[string]interface{}{"content": "transcribed"}}},
		})
	}))
	defer srv.Close()
	f, _ := os.CreateTemp("", "*.wav")
	f.Close()
	defer os.Remove(f.Name())
	result := Transcribe(context.Background(), "key", f.Name(), srv.URL, "en")
	if strings.Contains(capturedModel, "TeleSpeechASR") {
		t.Errorf("en should use Whisper, not TeleSpeechASR, got model=%q, result=%q", capturedModel, result)
	}
	if !strings.Contains(capturedModel, "whisper") {
		t.Errorf("en should use whisper model, got model=%q", capturedModel)
	}
}
