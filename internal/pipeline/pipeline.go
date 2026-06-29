package pipeline

import (
	"context"
	"fmt"
	"strings"

	"github.com/yaleh/voci/internal/ollama"
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

// Rewrite normalizes a corrected ASR text into a clean, well-formed instruction.
// It is a scope-preserving normalizer, NOT an intent elaborator: it must not add
// content, infer unstated goals, translate, or pick specific targets the speaker
// did not name. If the intent is genuinely too vague to act on, the result starts
// with [ambiguous].
//
// Only the Known Entities portion of the hint is forwarded (for resolving entity
// references the speaker actually made); the broader project context (active tasks,
// etc.) is deliberately withheld so it cannot become raw material for over-expansion.
func Rewrite(ctx context.Context, hinted, hint string, chatFn ChatFn) (string, error) {
	var systemPrompt strings.Builder
	systemPrompt.WriteString("You normalize a voice transcription into a clean instruction.\n")
	systemPrompt.WriteString("## Rules\n")
	systemPrompt.WriteString("- Preserve the speaker's exact scope, intent, and LANGUAGE. Do NOT translate.\n")
	systemPrompt.WriteString("- Only fix grammar/disfluency and resolve entity references the speaker explicitly made (using Known Entities).\n")
	systemPrompt.WriteString("- Do NOT add details, steps, or specific targets the speaker did not say.\n")
	systemPrompt.WriteString("- Do NOT pick a specific task/file/feature when the speaker spoke generally.\n")
	systemPrompt.WriteString("- Do NOT answer, plan, or act on the instruction — only clean it up.\n")
	systemPrompt.WriteString("- If genuinely too vague to act on, start your response with [ambiguous].\n")
	systemPrompt.WriteString("Return only the normalized instruction, nothing else.\n")
	if entities := knownEntities(hint); entities != "" {
		systemPrompt.WriteString("\n")
		systemPrompt.WriteString(entities)
	}

	userMsg := fmt.Sprintf("Normalize this transcription: %s", hinted)

	messages := []ollama.Message{
		{Role: "system", Content: systemPrompt.String()},
		{Role: "user", Content: userMsg},
	}

	return chatFn(ctx, messages)
}

// knownEntities extracts only the "## Known Entities" section from a context hint,
// so Rewrite receives entity references for resolution without the broader project
// context (active tasks, git log, etc.) that could fuel over-elaboration. Returns ""
// if no such section exists.
func knownEntities(hint string) string {
	const marker = "## Known Entities"
	idx := strings.Index(hint, marker)
	if idx < 0 {
		return ""
	}
	section := hint[idx:]
	// Stop at the next top-level heading, if any.
	if next := strings.Index(section[len(marker):], "\n## "); next >= 0 {
		section = section[:len(marker)+next]
	}
	return strings.TrimRight(section, "\n")
}
