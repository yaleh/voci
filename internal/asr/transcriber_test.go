package asr

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// fakeTranscriber is a test double that satisfies Transcriber.
type fakeTranscriber struct {
	text string
	err  error
}

func (f fakeTranscriber) Transcribe(_ context.Context, _ Options) (Result, error) {
	return Result{Text: f.text}, f.err
}

func TestResultHasText(t *testing.T) {
	r := Result{Text: "hello"}
	if r.Text != "hello" {
		t.Errorf("expected 'hello', got %q", r.Text)
	}
}

func TestFakeTranscriberSatisfiesInterface(t *testing.T) {
	var _ Transcriber = fakeTranscriber{}
}

func TestNewProviderTranscriberImplementsInterface(t *testing.T) {
	tr := NewProviderTranscriber()
	if tr == nil {
		t.Fatal("NewProviderTranscriber() returned nil")
	}
}

func TestProviderTranscriberReturnsResultText(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"text":"hi"}`))
	}))
	defer srv.Close()

	wavPath := writeTempWav(t)
	tr := NewProviderTranscriber()
	result, err := tr.Transcribe(context.Background(), Options{
		Key:       "sk-test",
		AudioPath: wavPath,
		APIURL:    srv.URL,
		Provider:  "siliconflow",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Text != "hi" {
		t.Errorf("expected 'hi', got %q", result.Text)
	}
}

func TestFnFromTranscriberForwards(t *testing.T) {
	fake := fakeTranscriber{text: "fake"}
	fn := FnFromTranscriber(fake)
	got := fn(context.Background(), "", "", "", "", nil)
	if got != "fake" {
		t.Errorf("expected 'fake', got %q", got)
	}
}

func TestFnFromTranscriberReturnsEmptyOnError(t *testing.T) {
	fake := fakeTranscriber{err: errors.New("boom")}
	fn := FnFromTranscriber(fake)
	got := fn(context.Background(), "", "", "", "", nil)
	if got != "" {
		t.Errorf("expected empty string on error, got %q", got)
	}
}
