package asr

import (
	"context"
	"encoding/base64"
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
	text := Transcribe(context.Background(), "sk-test", wavPath, srv.URL, "zh", "siliconflow", "")
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
	_ = Transcribe(context.Background(), "sk-test", wavPath, srv.URL, "zh", "siliconflow", "")

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
	result := Transcribe(context.Background(), "sk-test", wavPath, srv.URL, "zh", "siliconflow", "")
	if result != "" {
		t.Errorf("expected empty string on HTTP error, got %q", result)
	}
}

func TestTranscribeZhUsesTelespeech(t *testing.T) {
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
	result := Transcribe(context.Background(), "key", f.Name(), srv.URL, "zh", "siliconflow", "")
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
	result := Transcribe(context.Background(), "key", f.Name(), srv.URL, "en", "siliconflow", "")
	if strings.Contains(capturedModel, "TeleSpeechASR") {
		t.Errorf("en should use Whisper, not TeleSpeechASR, got model=%q, result=%q", capturedModel, result)
	}
	if !strings.Contains(capturedModel, "whisper") {
		t.Errorf("en should use whisper model, got model=%q", capturedModel)
	}
}

func TestTranscribeOpenRouterSendsJSONBase64(t *testing.T) {
	var capturedCT string
	var capturedBody map[string]interface{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedCT = r.Header.Get("Content-Type")
		json.NewDecoder(r.Body).Decode(&capturedBody)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"text":"transcribed"}`))
	}))
	defer srv.Close()

	wavPath := writeTempWav(t)
	_ = Transcribe(context.Background(), "sk-or", wavPath, srv.URL, "", "openrouter", "")

	if capturedCT != "application/json" {
		t.Errorf("expected Content-Type application/json, got %q", capturedCT)
	}
	inputAudio, ok := capturedBody["input_audio"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected 'input_audio' object in JSON body, got %v", capturedBody)
	}
	if _, ok := capturedBody["model"]; !ok {
		t.Errorf("expected 'model' key in JSON body, got %v", capturedBody)
	}
	data, _ := inputAudio["data"].(string)
	if _, err := base64.StdEncoding.DecodeString(data); err != nil {
		t.Errorf("input_audio.data is not valid base64: %v", err)
	}
}

func TestTranscribeOpenRouterUsesDefaultModel(t *testing.T) {
	var capturedBody map[string]string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&capturedBody)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"text":"ok"}`))
	}))
	defer srv.Close()

	wavPath := writeTempWav(t)
	_ = Transcribe(context.Background(), "sk-or", wavPath, srv.URL, "", "openrouter", "")

	if capturedBody["model"] != DefaultOpenRouterModel {
		t.Errorf("want model %q, got %q", DefaultOpenRouterModel, capturedBody["model"])
	}
}

func TestTranscribeOpenRouterUsesCustomModel(t *testing.T) {
	var capturedBody map[string]string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&capturedBody)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"text":"ok"}`))
	}))
	defer srv.Close()

	wavPath := writeTempWav(t)
	_ = Transcribe(context.Background(), "sk-or", wavPath, srv.URL, "", "openrouter", "microsoft/mai-transcribe-1.5")

	if capturedBody["model"] != "microsoft/mai-transcribe-1.5" {
		t.Errorf("want model microsoft/mai-transcribe-1.5, got %q", capturedBody["model"])
	}
}

func TestTranscribeSiliconflowStillMultipart(t *testing.T) {
	var capturedCT string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedCT = r.Header.Get("Content-Type")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"text":"ok"}`))
	}))
	defer srv.Close()

	wavPath := writeTempWav(t)
	_ = Transcribe(context.Background(), "sk-sf", wavPath, srv.URL, "zh", "siliconflow", "")

	if !strings.HasPrefix(capturedCT, "multipart/form-data") {
		t.Errorf("siliconflow should use multipart/form-data, got %q", capturedCT)
	}
}

func TestTranscribeModelOverrideForSiliconflow(t *testing.T) {
	var capturedBody []byte

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"text":"ok"}`))
	}))
	defer srv.Close()

	wavPath := writeTempWav(t)
	_ = Transcribe(context.Background(), "sk-sf", wavPath, srv.URL, "zh", "siliconflow", "my-custom-model")

	if !strings.Contains(string(capturedBody), "my-custom-model") {
		t.Errorf("expected my-custom-model in body, got: %s", capturedBody)
	}
}
