package intent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/yaleh/voci/internal/ollama"
	"github.com/yaleh/voci/internal/pipeline"
)

type classifyResponse struct {
	Kind       string  `json:"kind"`
	Confidence float64 `json:"confidence"`
}

// Classify calls chat to classify rewritten text into one of four intent categories.
// fullContext is included in the user message to help the model; if non-empty,
// ContextUsed is set to "context" in the returned proposal.
func Classify(ctx context.Context, rewritten, fullContext string, chat pipeline.ChatFn) (ActionProposal, error) {
	systemPrompt := `You are an intent classifier for a voice-driven developer assistant.

Classify the given text into exactly one of these four intent categories:
- direct_prompt: a direct programming instruction to be executed (e.g. "add logging to auth.go", "fix the bug in parser.go")
- backlog_action: an action targeting the task backlog (e.g. "mark TASK-5 as done", "create a task for refactoring")
- query: an information request about the project (e.g. "what tasks are open?", "what does the auth module do?")
- ambiguous: the intent cannot be determined with confidence

Respond with a JSON object containing exactly two keys:
- "kind": one of "direct_prompt", "backlog_action", "query", "ambiguous"
- "confidence": a float between 0.0 and 1.0 representing your confidence

Example: {"kind":"direct_prompt","confidence":0.92}

Return ONLY the JSON object, no other text.`

	var userMsg strings.Builder
	userMsg.WriteString(fmt.Sprintf("Classify this text: %s", rewritten))
	if fullContext != "" {
		userMsg.WriteString(fmt.Sprintf("\n\nContext:\n%s", fullContext))
	}

	messages := []ollama.Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: userMsg.String()},
	}

	rawResponse, err := chat(ctx, messages)
	if err != nil {
		return ActionProposal{}, fmt.Errorf("classify chat: %w", err)
	}

	// Try to parse the JSON response.
	// The model may wrap it in markdown code fences — strip them.
	cleaned := strings.TrimSpace(rawResponse)
	if idx := strings.Index(cleaned, "{"); idx >= 0 {
		cleaned = cleaned[idx:]
	}
	if idx := strings.LastIndex(cleaned, "}"); idx >= 0 {
		cleaned = cleaned[:idx+1]
	}

	var cr classifyResponse
	if err := json.Unmarshal([]byte(cleaned), &cr); err != nil {
		// Unparsable: fall back to ambiguous with zero confidence
		return ActionProposal{
			Kind:          KindAmbiguous,
			Rewritten:     rewritten,
			Confidence:    0,
			ContextUsed:   contextUsedKey(fullContext),
		}, nil
	}

	// Map the kind string to a Kind constant; invalid → ambiguous.
	kind, valid := mapKind(cr.Kind)
	if !valid {
		return ActionProposal{
			Kind:          KindAmbiguous,
			Rewritten:     rewritten,
			Confidence:    0,
			ContextUsed:   contextUsedKey(fullContext),
		}, nil
	}

	// Clamp confidence to [0.0, 1.0].
	conf := cr.Confidence
	if conf < 0.0 {
		conf = 0.0
	}
	if conf > 1.0 {
		conf = 1.0
	}

	return ActionProposal{
		Kind:        kind,
		Rewritten:   rewritten,
		Confidence:  conf,
		ContextUsed: contextUsedKey(fullContext),
	}, nil
}

// mapKind converts a raw kind string to a Kind constant.
// Returns (kind, true) on success, (KindAmbiguous, false) if unrecognised.
func mapKind(s string) (Kind, bool) {
	switch Kind(s) {
	case KindDirectPrompt:
		return KindDirectPrompt, true
	case KindBacklogAction:
		return KindBacklogAction, true
	case KindQuery:
		return KindQuery, true
	case KindAmbiguous:
		return KindAmbiguous, true
	default:
		return KindAmbiguous, false
	}
}

// contextUsedKey returns the provenance key when context was provided.
func contextUsedKey(fullContext string) string {
	if fullContext == "" {
		return ""
	}
	return "context"
}
