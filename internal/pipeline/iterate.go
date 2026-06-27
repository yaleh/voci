package pipeline

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/yalehu/voci/internal/ollama"
)

// RewriteWithFeedbackFn is a function that rewrites with context of previous result and user feedback.
type RewriteWithFeedbackFn func(ctx context.Context, hinted, hint string, chatFn ChatFn) (string, error)

// IterateLoop reads user feedback from r, rewrites the instruction, and prints to w.
// It continues until the user provides an empty line.
func IterateLoop(
	ctx context.Context,
	initialRewritten string,
	hint string,
	r io.Reader,
	w io.Writer,
	chatFn ChatFn,
	rewriteFn RewriteWithFeedbackFn,
) error {
	current := initialRewritten
	scanner := bufio.NewScanner(r)

	for {
		fmt.Fprint(w, "\nFeedback (empty to stop): ")
		if !scanner.Scan() {
			break
		}
		feedback := strings.TrimSpace(scanner.Text())
		if feedback == "" {
			break
		}

		// Build prompt that includes previous rewritten result and user feedback
		prompt := buildFeedbackPrompt(current, feedback)

		// Build a custom chatFn that prepends context
		feedbackChatFn := func(ctx context.Context, messages []ollama.Message) (string, error) {
			return chatFn(ctx, messages)
		}

		newRewritten, err := rewriteFn(ctx, prompt, hint, feedbackChatFn)
		if err != nil {
			return fmt.Errorf("rewrite with feedback: %w", err)
		}

		current = newRewritten
		fmt.Fprintf(w, "\nREWRITTEN:\n%s\n", current)
	}

	return nil
}

func buildFeedbackPrompt(previousRewritten, feedback string) string {
	return fmt.Sprintf("Previous instruction: %s\nUser feedback: %s\nRewrite with this feedback applied.", previousRewritten, feedback)
}
