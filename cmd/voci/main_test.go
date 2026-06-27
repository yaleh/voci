package main

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"

	"github.com/yalehu/voci/internal/ollama"
	"github.com/yalehu/voci/internal/pipeline"
)

func makeTempWav(t *testing.T) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "test-*.wav")
	if err != nil {
		t.Fatal(err)
	}
	f.Write([]byte("RIFF\x00\x00\x00\x00WAVEfmt "))
	f.Close()
	return f.Name()
}

func setTestEnv(t *testing.T) {
	t.Helper()
	t.Setenv("SILICONFLOW_API_KEY", "sk-test")
	t.Setenv("OLLAMA_HOST", "http://localhost:11434")
}

var fakeTranscribe TranscribeFn = func(ctx context.Context, key, audioPath, apiURL string) (string, error) {
	return "task one fix login bug", nil
}

var fakeHinted = func(ctx context.Context, raw, hint string, chatFn pipeline.ChatFn) (string, error) {
	return "TASK-1 fix login bug", nil
}

var fakeRewrite RewriteFn = func(ctx context.Context, hinted, hint string, chatFn pipeline.ChatFn) (string, error) {
	return "Fix login bug in TASK-1", nil
}

var fakeChatFn = func(ctx context.Context, messages []ollama.Message) (string, error) {
	return "ok", nil
}

func TestCLIFileFlagPrintsRAW(t *testing.T) {
	setTestEnv(t)
	wavPath := makeTempWav(t)

	var stdout bytes.Buffer
	err := run([]string{"--file", wavPath}, &stdout, strings.NewReader(""), fakeTranscribe, fakeHinted, fakeRewrite)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stdout.String(), "RAW") {
		t.Errorf("expected RAW in output, got: %q", stdout.String())
	}
}

func TestCLIFileFlagPrintsHINTED(t *testing.T) {
	setTestEnv(t)
	wavPath := makeTempWav(t)

	var stdout bytes.Buffer
	err := run([]string{"--file", wavPath}, &stdout, strings.NewReader(""), fakeTranscribe, fakeHinted, fakeRewrite)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stdout.String(), "HINTED") {
		t.Errorf("expected HINTED in output, got: %q", stdout.String())
	}
}

func TestCLIFileFlagPrintsREWRITTEN(t *testing.T) {
	setTestEnv(t)
	wavPath := makeTempWav(t)

	var stdout bytes.Buffer
	err := run([]string{"--file", wavPath}, &stdout, strings.NewReader(""), fakeTranscribe, fakeHinted, fakeRewrite)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stdout.String(), "REWRITTEN") {
		t.Errorf("expected REWRITTEN in output, got: %q", stdout.String())
	}
}

func TestCLINoFileExitsNonzero(t *testing.T) {
	setTestEnv(t)

	var stdout bytes.Buffer
	err := run([]string{}, &stdout, strings.NewReader(""), fakeTranscribe, fakeHinted, fakeRewrite)
	if err == nil {
		t.Fatal("expected error for missing --file")
	}
}

func TestCLIFileMissingExitsNonzero(t *testing.T) {
	setTestEnv(t)

	var stdout bytes.Buffer
	err := run([]string{"--file", "/nonexistent.wav"}, &stdout, strings.NewReader(""), fakeTranscribe, fakeHinted, fakeRewrite)
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestCLIIterateFlagAccepted(t *testing.T) {
	setTestEnv(t)
	wavPath := makeTempWav(t)

	var stdout bytes.Buffer
	// Empty stdin means iterate loop exits immediately
	err := run([]string{"--file", wavPath, "--iterate"}, &stdout, strings.NewReader(""), fakeTranscribe, fakeHinted, fakeRewrite)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
