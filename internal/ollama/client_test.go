package ollama

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestChatReturnsContent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		w.Write([]byte(`{"message":{"content":"ok"},"done":true}` + "\n"))
	}))
	defer srv.Close()

	result, err := Chat(context.Background(), srv.URL, "gemma4:e4b", []Message{
		{Role: "user", Content: "hello"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "ok" {
		t.Errorf("expected 'ok', got %q", result)
	}
}

func TestChatSendsModelAndMessages(t *testing.T) {
	var capturedBody []byte

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/x-ndjson")
		w.Write([]byte(`{"message":{"content":"response"},"done":true}` + "\n"))
	}))
	defer srv.Close()

	_, err := Chat(context.Background(), srv.URL, "gemma4:e4b", []Message{
		{Role: "user", Content: "test"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var req chatRequest
	if err := json.Unmarshal(capturedBody, &req); err != nil {
		t.Fatalf("unmarshal request: %v", err)
	}
	if req.Model != "gemma4:e4b" {
		t.Errorf("expected model gemma4:e4b, got %q", req.Model)
	}
	if len(req.Messages) == 0 {
		t.Error("expected non-empty messages")
	}
}

func TestChatHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	_, err := Chat(context.Background(), srv.URL, "gemma4:e4b", []Message{
		{Role: "user", Content: "test"},
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("expected 500 in error, got: %v", err)
	}
}
