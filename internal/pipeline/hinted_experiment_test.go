package pipeline

import (
	"context"
	"strings"
	"testing"

	"github.com/yaleh/voci/internal/ollama"
)

// activeTasksHint simulates the kind of hint that contains ## Active Tasks,
// which is the boundary-violation trigger scenario for TASK-49.
const activeTasksHint = `## Active Tasks
- TASK-49: Run RunHinted prompt format experiment
- TASK-48: Add Cloudflare named tunnel support
- TASK-47: Volcengine ASR adapter

## Known Entities
- task forty nine: TASK-49
- task forty eight: TASK-48
`

// makeMockChatFn returns a ChatFn that captures messages and returns a fixed response.
func makeMockChatFn(response string) (ChatFn, *[]ollama.Message) {
	captured := &[]ollama.Message{}
	fn := func(ctx context.Context, msgs []ollama.Message) (string, error) {
		*captured = msgs
		return response, nil
	}
	return fn, captured
}

// --- Variant A ---

func TestRunHintedVariantA_BoundaryViolation(t *testing.T) {
	raw := "列出现有 task，并建议执行顺序"
	// Mock returns a minimal JSON correction — NOT a list of tasks
	chatFn, captured := makeMockChatFn(`{"corrected": "列出现有 task，并建议执行顺序"}`)

	result, err := RunHintedVariantA(context.Background(), raw, activeTasksHint, chatFn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(*captured) == 0 {
		t.Fatal("chatFn was not called")
	}
	// System prompt must mandate JSON output format
	var sysContent string
	for _, m := range *captured {
		if m.Role == "system" {
			sysContent = m.Content
		}
	}
	if !strings.Contains(sysContent, `"corrected"`) {
		t.Errorf("VariantA system prompt missing JSON output instruction; got: %q", sysContent)
	}
	// Result should be the corrected field, not a task list
	if strings.Contains(result, "TASK-49") || strings.Contains(result, "TASK-48") {
		t.Errorf("VariantA result should not contain task list; got: %q", result)
	}
	_ = result
}

func TestRunHintedVariantA_NormalCorrection(t *testing.T) {
	raw := "修复 task 四十九的 bug"
	chatFn, _ := makeMockChatFn(`{"corrected": "修复 TASK-49 的 bug"}`)

	result, err := RunHintedVariantA(context.Background(), raw, activeTasksHint, chatFn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "修复 TASK-49 的 bug" {
		t.Errorf("VariantA expected corrected entity substitution; got: %q", result)
	}
}

func TestRunHintedVariantA_EmptyHint(t *testing.T) {
	raw := "今天天气怎么样"
	chatFn, _ := makeMockChatFn(`{"corrected": "今天天气怎么样"}`)

	result, err := RunHintedVariantA(context.Background(), raw, "", chatFn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "今天天气怎么样" {
		t.Errorf("VariantA empty hint: expected near-original output; got: %q", result)
	}
}

// --- Variant B ---

func TestRunHintedVariantB_BoundaryViolation(t *testing.T) {
	raw := "列出现有 task，并建议执行顺序"
	chatFn, captured := makeMockChatFn(`{"corrected": "列出现有 task，并建议执行顺序"}`)

	result, err := RunHintedVariantB(context.Background(), raw, activeTasksHint, chatFn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(*captured) == 0 {
		t.Fatal("chatFn was not called")
	}

	// User message must be JSON-encoded with raw_transcript field
	var userContent string
	for _, m := range *captured {
		if m.Role == "user" {
			userContent = m.Content
		}
	}
	if !strings.Contains(userContent, `"raw_transcript"`) {
		t.Errorf("VariantB user message must be JSON with raw_transcript field; got: %q", userContent)
	}
	if !strings.Contains(userContent, raw) {
		t.Errorf("VariantB user message must contain raw transcript; got: %q", userContent)
	}
	if strings.Contains(result, "TASK-49") || strings.Contains(result, "TASK-48") {
		t.Errorf("VariantB result should not contain task list; got: %q", result)
	}
}

func TestRunHintedVariantB_NormalCorrection(t *testing.T) {
	raw := "修复 task 四十八的 bug"
	chatFn, _ := makeMockChatFn(`{"corrected": "修复 TASK-48 的 bug"}`)

	result, err := RunHintedVariantB(context.Background(), raw, activeTasksHint, chatFn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "修复 TASK-48 的 bug" {
		t.Errorf("VariantB expected corrected entity substitution; got: %q", result)
	}
}

func TestRunHintedVariantB_EmptyHint(t *testing.T) {
	raw := "今天天气怎么样"
	chatFn, captured := makeMockChatFn(`{"corrected": "今天天气怎么样"}`)

	result, err := RunHintedVariantB(context.Background(), raw, "", chatFn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// User message should still be JSON
	var userContent string
	for _, m := range *captured {
		if m.Role == "user" {
			userContent = m.Content
		}
	}
	if !strings.Contains(userContent, `"raw_transcript"`) {
		t.Errorf("VariantB user must be JSON even with empty hint; got: %q", userContent)
	}
	if result != "今天天气怎么样" {
		t.Errorf("VariantB empty hint: expected near-original output; got: %q", result)
	}
}

// --- Variant C ---

func TestRunHintedVariantC_BoundaryViolation(t *testing.T) {
	raw := "列出现有 task，并建议执行顺序"
	chatFn, captured := makeMockChatFn("<corrected>列出现有 task，并建议执行顺序</corrected>")

	result, err := RunHintedVariantC(context.Background(), raw, activeTasksHint, chatFn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(*captured) == 0 {
		t.Fatal("chatFn was not called")
	}

	// User message must use XML tags
	var userContent string
	for _, m := range *captured {
		if m.Role == "user" {
			userContent = m.Content
		}
	}
	if !strings.Contains(userContent, "<raw_transcript>") {
		t.Errorf("VariantC user message must use <raw_transcript> tag; got: %q", userContent)
	}
	if !strings.Contains(userContent, raw) {
		t.Errorf("VariantC user message must contain raw transcript; got: %q", userContent)
	}
	if strings.Contains(result, "TASK-49") || strings.Contains(result, "TASK-48") {
		t.Errorf("VariantC result should not contain task list; got: %q", result)
	}
}

func TestRunHintedVariantC_NormalCorrection(t *testing.T) {
	raw := "修复 task 四十七的 bug"
	chatFn, _ := makeMockChatFn("<corrected>修复 TASK-47 的 bug</corrected>")

	result, err := RunHintedVariantC(context.Background(), raw, activeTasksHint, chatFn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "修复 TASK-47 的 bug" {
		t.Errorf("VariantC expected corrected entity substitution; got: %q", result)
	}
}

func TestRunHintedVariantC_EmptyHint(t *testing.T) {
	raw := "今天天气怎么样"
	chatFn, captured := makeMockChatFn("<corrected>今天天气怎么样</corrected>")

	result, err := RunHintedVariantC(context.Background(), raw, "", chatFn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Without hint, context tag should not appear
	var userContent string
	for _, m := range *captured {
		if m.Role == "user" {
			userContent = m.Content
		}
	}
	if strings.Contains(userContent, "<context>") {
		t.Errorf("VariantC empty hint should omit <context> tag; got: %q", userContent)
	}
	if result != "今天天气怎么样" {
		t.Errorf("VariantC empty hint: expected near-original output; got: %q", result)
	}
}

// --- JSON/XML parsing edge cases ---

func TestParseJSONCorrection_MarkdownFenced(t *testing.T) {
	resp := "```json\n{\"corrected\": \"hello world\"}\n```"
	result, err := parseJSONCorrection(resp, "fallback")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "hello world" {
		t.Errorf("expected 'hello world', got: %q", result)
	}
}

func TestParseXMLCorrection_Basic(t *testing.T) {
	resp := "<corrected>fix TASK-49</corrected>"
	result, err := parseXMLCorrection(resp, "fallback")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "fix TASK-49" {
		t.Errorf("expected 'fix TASK-49', got: %q", result)
	}
}

func TestParseXMLCorrection_Fallback(t *testing.T) {
	resp := "plain text without tags"
	result, err := parseXMLCorrection(resp, "fallback")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "plain text without tags" {
		t.Errorf("expected original resp as fallback, got: %q", result)
	}
}
