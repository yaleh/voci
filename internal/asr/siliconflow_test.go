package asr

import (
	"context"
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
	text, err := Transcribe(context.Background(), "sk-test", wavPath, srv.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
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
	_, err := Transcribe(context.Background(), "sk-test", wavPath, srv.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

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
	_, err := Transcribe(context.Background(), "sk-test", wavPath, srv.URL)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}
