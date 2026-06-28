package pipeline

import (
	"context"
	"fmt"
	"strings"

	"github.com/yalehu/voci/internal/ollama"
)

// ChatFn is a function that calls an LLM with messages and returns the response.
type ChatFn func(ctx context.Context, messages []ollama.Message) (string, error)

// RunHinted uses the LLM to correct ASR errors using the provided context hint.
// It returns a HINTED text where specialized terms (task IDs, project names, paths)
// are corrected based on the hint context.
func RunHinted(ctx context.Context, raw, hint string, chatFn ChatFn) (string, error) {
	var systemPrompt strings.Builder
	systemPrompt.WriteString("You are an ASR correction assistant.\n\n")
	systemPrompt.WriteString("## Instructions\n")
	systemPrompt.WriteString("The hint below contains a '## Known Entities' section.\n")
	systemPrompt.WriteString("For each entry in that section formatted as `spoken-form: canonical-form`,\n")
	systemPrompt.WriteString("replace any occurrence of the spoken-form in the transcription\n")
	systemPrompt.WriteString("with the exact canonical spelling.\n")
	systemPrompt.WriteString("Apply all substitutions first, then fix remaining grammar.\n")
	systemPrompt.WriteString("When multiple candidates of the same kind (e.g. task IDs sharing a 'TASK-' prefix, or CLI flags sharing '--') could match a phrase, choose the candidate whose spoken form most closely matches the exact words in the ASR transcription — do not default to the first candidate listed.\n")
	systemPrompt.WriteString("Only substitute a package path such as 'internal/xxx' when the transcription explicitly refers to a Go package or import path — if the word appears as a standalone concept (e.g. 'pipeline stage', 'context object'), leave it as is.\n")
	systemPrompt.WriteString("Return only the corrected text, nothing else.\n")
	if hint != "" {
		systemPrompt.WriteString("\n")
		systemPrompt.WriteString(hint)
	}

	userMsg := fmt.Sprintf("Transcription: %s", raw)

	messages := []ollama.Message{
		{Role: "system", Content: systemPrompt.String()},
		{Role: "user", Content: userMsg},
	}

	return chatFn(ctx, messages)
}

// Rewrite uses the LLM to rewrite a corrected ASR text into a clear programming instruction.
// If the intent cannot be determined, the result should contain [ambiguous].
func Rewrite(ctx context.Context, hinted, hint string, chatFn ChatFn) (string, error) {
	var systemPrompt strings.Builder
	systemPrompt.WriteString("You are a developer assistant. ")
	systemPrompt.WriteString("Rewrite the given text as a clear, actionable programming instruction. ")
	systemPrompt.WriteString("If the intent is unclear or too vague to be actionable, start your response with [ambiguous]. ")
	systemPrompt.WriteString("Be concise and specific.\n")
	if hint != "" {
		systemPrompt.WriteString("\nProject context:\n")
		systemPrompt.WriteString(hint)
	}

	userMsg := fmt.Sprintf("Rewrite as a clear programming instruction: %s", hinted)

	messages := []ollama.Message{
		{Role: "system", Content: systemPrompt.String()},
		{Role: "user", Content: userMsg},
	}

	return chatFn(ctx, messages)
}
