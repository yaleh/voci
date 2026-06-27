package pipeline

import (
	"context"
	"strings"
	"testing"

	"github.com/yalehu/voci/internal/ollama"
)

func TestRunHintedCallsChatWithHint(t *testing.T) {
	var capturedMessages []ollama.Message

	fakeChatFn := func(ctx context.Context, messages []ollama.Message) (string, error) {
		capturedMessages = messages
		return "TASK-1 fix login bug", nil
	}

	raw := "task one fix login bug"
	hint := "## Active Tasks\n- TASK-1: Fix login bug\n"

	_, err := RunHinted(context.Background(), raw, hint, fakeChatFn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(capturedMessages) == 0 {
		t.Fatal("expected messages to be sent")
	}

	// Check that both raw text and hint appear in messages
	allContent := ""
	for _, m := range capturedMessages {
		allContent += m.Content
	}
	if !strings.Contains(allContent, raw) {
		t.Errorf("expected raw text in messages, got: %s", allContent)
	}
	if !strings.Contains(allContent, hint) {
		t.Errorf("expected hint in messages, got: %s", allContent)
	}
}

func TestRunHintedEmptyHint(t *testing.T) {
	var capturedMessages []ollama.Message

	fakeChatFn := func(ctx context.Context, messages []ollama.Message) (string, error) {
		capturedMessages = messages
		return "fix login", nil
	}

	_, err := RunHinted(context.Background(), "fix login", "", fakeChatFn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(capturedMessages) == 0 {
		t.Fatal("expected chatFn to be called")
	}
	// Just ensure it was called even with empty hint
}

func TestRewriteReturnsClearedInstruction(t *testing.T) {
	expected := "add logging to auth.go"

	fakeChatFn := func(ctx context.Context, messages []ollama.Message) (string, error) {
		return expected, nil
	}

	result, err := Rewrite(context.Background(), "add logging to auth", "project context", fakeChatFn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

func TestRewritePassesThroughAmbiguous(t *testing.T) {
	fakeChatFn := func(ctx context.Context, messages []ollama.Message) (string, error) {
		return "[ambiguous] unclear intent", nil
	}

	result, err := Rewrite(context.Background(), "make it faster somehow", "", fakeChatFn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "[ambiguous]") {
		t.Errorf("expected [ambiguous] in result, got: %q", result)
	}
}
