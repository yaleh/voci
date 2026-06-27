package pipeline

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/yalehu/voci/internal/ollama"
)

func TestIterateExitsOnEmptyInput(t *testing.T) {
	callCount := 0
	rewriteFn := func(ctx context.Context, hinted, hint string, chatFn ChatFn) (string, error) {
		callCount++
		return "rewritten", nil
	}

	fakeChatFn := func(ctx context.Context, messages []ollama.Message) (string, error) {
		return "response", nil
	}

	r := strings.NewReader("")
	var w bytes.Buffer

	err := IterateLoop(context.Background(), "initial", "hint", r, &w, fakeChatFn, rewriteFn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if callCount != 0 {
		t.Errorf("expected rewriteFn not called, got %d calls", callCount)
	}
}

func TestIterateCallsRewriteWithFeedback(t *testing.T) {
	var capturedPrompts []string

	rewriteFn := func(ctx context.Context, hinted, hint string, chatFn ChatFn) (string, error) {
		capturedPrompts = append(capturedPrompts, hinted)
		return "shorter instruction", nil
	}

	fakeChatFn := func(ctx context.Context, messages []ollama.Message) (string, error) {
		return "response", nil
	}

	// Provide one feedback then empty line
	r := strings.NewReader("make it shorter\n\n")
	var w bytes.Buffer

	err := IterateLoop(context.Background(), "initial rewritten", "hint", r, &w, fakeChatFn, rewriteFn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(capturedPrompts) != 1 {
		t.Errorf("expected 1 call, got %d", len(capturedPrompts))
	}
	// Verify feedback was included in prompt
	if !strings.Contains(capturedPrompts[0], "make it shorter") {
		t.Errorf("expected feedback in prompt, got: %q", capturedPrompts[0])
	}
}

func TestIterateChainsRewrites(t *testing.T) {
	var capturedPrompts []string
	callNum := 0

	rewriteFn := func(ctx context.Context, hinted, hint string, chatFn ChatFn) (string, error) {
		capturedPrompts = append(capturedPrompts, hinted)
		callNum++
		return "result-" + string(rune('0'+callNum)), nil
	}

	fakeChatFn := func(ctx context.Context, messages []ollama.Message) (string, error) {
		return "response", nil
	}

	r := strings.NewReader("f1\nf2\n\n")
	var w bytes.Buffer

	err := IterateLoop(context.Background(), "initial", "hint", r, &w, fakeChatFn, rewriteFn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(capturedPrompts) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(capturedPrompts))
	}
	// Second call should contain the result of first call
	if !strings.Contains(capturedPrompts[1], "result-1") {
		t.Errorf("expected second call to contain result-1, got: %q", capturedPrompts[1])
	}
}

func TestIteratePrintsRewrittenEachRound(t *testing.T) {
	callCount := 0
	rewriteFn := func(ctx context.Context, hinted, hint string, chatFn ChatFn) (string, error) {
		callCount++
		return "new instruction", nil
	}

	fakeChatFn := func(ctx context.Context, messages []ollama.Message) (string, error) {
		return "response", nil
	}

	r := strings.NewReader("feedback1\nfeedback2\n\n")
	var w bytes.Buffer

	err := IterateLoop(context.Background(), "initial", "hint", r, &w, fakeChatFn, rewriteFn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := w.String()
	// Count occurrences of REWRITTEN
	count := strings.Count(output, "REWRITTEN")
	if count != 2 {
		t.Errorf("expected REWRITTEN to appear 2 times, got %d. Output: %q", count, output)
	}
}
