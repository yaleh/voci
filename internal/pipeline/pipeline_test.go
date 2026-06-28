package pipeline

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/yalehu/voci/internal/ollama"
)

type testCase struct {
	ID               string   `json:"id"`
	TTSInput         string   `json:"tts_input"`
	RawASR           string   `json:"raw_asr"`
	ExpectedHinted   string   `json:"expected_hinted"`
	ExpectedEntities []string `json:"expected_entities"`
}

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
	// New format uses "Transcription: <raw>" in user message
	if !strings.Contains(allContent, "Transcription: "+raw) {
		t.Errorf("expected 'Transcription: %s' in messages, got: %s", raw, allContent)
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

func TestRunHintedPromptHasExplicitSubstitution(t *testing.T) {
	hint := "## Known Entities\n- task one: TASK-1\n- vocal: voci\n"
	var capturedSystem string
	fakeFn := func(ctx context.Context, msgs []ollama.Message) (string, error) {
		for _, m := range msgs {
			if m.Role == "system" {
				capturedSystem = m.Content
			}
		}
		return "corrected", nil
	}
	_, err := RunHinted(context.Background(), "fix task one in vocal", hint, fakeFn)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.ToLower(capturedSystem), "replace") {
		t.Errorf("system prompt missing 'replace': %q", capturedSystem)
	}
	if !strings.Contains(strings.ToLower(capturedSystem), "canonical") {
		t.Errorf("system prompt missing 'canonical': %q", capturedSystem)
	}
}

func TestRunHintedGolden(t *testing.T) {
	data, err := os.ReadFile("../../testdata/testcases.json")
	if err != nil {
		t.Fatalf("failed to read testcases.json: %v", err)
	}
	var cases []testCase
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatalf("failed to parse testcases.json: %v", err)
	}

	for _, tc := range cases {
		if len(tc.ExpectedEntities) == 0 {
			continue // skip ambiguous cases
		}
		tc := tc
		t.Run(tc.ID, func(t *testing.T) {
			// Build a simple hint with Known Entities for this test case
			var hintBuilder strings.Builder
			hintBuilder.WriteString("## Known Entities\n")
			for _, entity := range tc.ExpectedEntities {
				// Simple spoken-form: lowercase, replace "-" with " "
				spoken := strings.ToLower(strings.ReplaceAll(entity, "-", " "))
				hintBuilder.WriteString("- " + spoken + ": " + entity + "\n")
			}
			hint := hintBuilder.String()

			var capturedSystem string
			fakeFn := func(ctx context.Context, msgs []ollama.Message) (string, error) {
				for _, m := range msgs {
					if m.Role == "system" {
						capturedSystem = m.Content
					}
				}
				return "corrected", nil
			}

			_, err := RunHinted(context.Background(), tc.TTSInput, hint, fakeFn)
			if err != nil {
				t.Fatalf("RunHinted failed: %v", err)
			}

			// System prompt must contain canonical forms
			for _, entity := range tc.ExpectedEntities {
				if !strings.Contains(capturedSystem, entity) {
					t.Errorf("system prompt missing canonical form %q; system=%q", entity, capturedSystem)
				}
			}

			// System prompt must contain "canonical" or "replace"
			lower := strings.ToLower(capturedSystem)
			if !strings.Contains(lower, "canonical") && !strings.Contains(lower, "replace") {
				t.Errorf("system prompt missing 'canonical' or 'replace': %q", capturedSystem)
			}
		})
	}
}

func TestRunHintedPromptDisambiguatesSameCategory(t *testing.T) {
	hint := "## Known Entities\n- dash dash file: --file\n- dash dash iterate: --iterate\n"
	var capturedSystem string
	fakeFn := func(ctx context.Context, msgs []ollama.Message) (string, error) {
		for _, m := range msgs {
			if m.Role == "system" {
				capturedSystem = m.Content
			}
		}
		return "corrected", nil
	}
	_, err := RunHinted(context.Background(), "dash dash file", hint, fakeFn)
	if err != nil {
		t.Fatal(err)
	}
	lower := strings.ToLower(capturedSystem)
	if !strings.Contains(lower, "most closely") && !strings.Contains(lower, "phonetically") && !strings.Contains(lower, "closest") {
		t.Errorf("system prompt missing disambiguation keyword (most closely/phonetically/closest): %q", capturedSystem)
	}
	if !strings.Contains(capturedSystem, "--file") {
		t.Errorf("system prompt missing '--file': %q", capturedSystem)
	}
	if !strings.Contains(capturedSystem, "--iterate") {
		t.Errorf("system prompt missing '--iterate': %q", capturedSystem)
	}
}

func TestRunHintedPromptDisambiguatesTaskIDs(t *testing.T) {
	hint := "## Known Entities\n- task one: TASK-1\n- task eight: TASK-8\n"
	var capturedSystem string
	fakeFn := func(ctx context.Context, msgs []ollama.Message) (string, error) {
		for _, m := range msgs {
			if m.Role == "system" {
				capturedSystem = m.Content
			}
		}
		return "corrected", nil
	}
	_, err := RunHinted(context.Background(), "task one", hint, fakeFn)
	if err != nil {
		t.Fatal(err)
	}
	lower := strings.ToLower(capturedSystem)
	if !strings.Contains(lower, "most closely") && !strings.Contains(lower, "phonetically") && !strings.Contains(lower, "closest") {
		t.Errorf("system prompt missing disambiguation keyword (most closely/phonetically/closest): %q", capturedSystem)
	}
	if !strings.Contains(capturedSystem, "TASK-1") {
		t.Errorf("system prompt missing 'TASK-1': %q", capturedSystem)
	}
	if !strings.Contains(capturedSystem, "TASK-8") {
		t.Errorf("system prompt missing 'TASK-8': %q", capturedSystem)
	}
}

func TestRunHintedPromptConstrainsPathExpansion(t *testing.T) {
	hint := "- inter nul pipeline: internal/pipeline\n"
	var capturedSystem string
	fakeFn := func(ctx context.Context, msgs []ollama.Message) (string, error) {
		for _, m := range msgs {
			if m.Role == "system" {
				capturedSystem = m.Content
			}
		}
		return "corrected", nil
	}
	_, err := RunHinted(context.Background(), "pipeline stage", hint, fakeFn)
	if err != nil {
		t.Fatal(err)
	}
	lower := strings.ToLower(capturedSystem)
	if !strings.Contains(lower, "package") && !strings.Contains(lower, "import") && !strings.Contains(lower, "path") {
		t.Errorf("system prompt missing path-qualification constraint (package/import/path): %q", capturedSystem)
	}
	// Existing instructions preserved
	if !strings.Contains(lower, "replace") {
		t.Errorf("system prompt missing 'replace': %q", capturedSystem)
	}
	if !strings.Contains(lower, "canonical") {
		t.Errorf("system prompt missing 'canonical': %q", capturedSystem)
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

// TestKnownEntities_ExcludesRecentDialogue guards that prose from ## Recent Dialogue
// never leaks into the slice consumed by Rewrite (via knownEntities).
func TestKnownEntities_ExcludesRecentDialogue(t *testing.T) {
	hint := `## Known Entities
spoken: Web 服务器
canonical: Web server

## Claude Code Session
- editing: internal/daemon/server.go

## Recent Dialogue
外部服务器应为Web`

	result := knownEntities(hint)

	if !strings.Contains(result, "Web server") {
		t.Errorf("expected Known Entities content in result, got: %q", result)
	}
	if strings.Contains(result, "外部服务器应为Web") {
		t.Errorf("Recent Dialogue prose must not appear in knownEntities output, got: %q", result)
	}
	if strings.Contains(result, "## Recent Dialogue") {
		t.Errorf("## Recent Dialogue heading must not appear in knownEntities output, got: %q", result)
	}
}
