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
	systemPrompt.WriteString("You are an ASR correction assistant. ")
	systemPrompt.WriteString("Correct any speech-to-text errors in the transcription, ")
	systemPrompt.WriteString("especially for technical terms, task IDs, project names, and file paths.\n")
	if hint != "" {
		systemPrompt.WriteString("\nContext (use this to correct ASR errors):\n")
		systemPrompt.WriteString(hint)
	}

	userMsg := fmt.Sprintf("Correct the ASR transcription. Return only the corrected text, nothing else.\n\nTranscription: %s", raw)

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
