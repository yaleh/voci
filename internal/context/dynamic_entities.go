package context

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	rePascalCase = regexp.MustCompile(`[A-Z][a-z]+(?:[A-Z][a-z]+)+`)
	reCamelCase  = regexp.MustCompile(`[a-z]+[A-Z][a-zA-Z]+`)
	reSnakeCase  = regexp.MustCompile(`[a-z][a-z0-9]*(?:_[a-z][a-z0-9]+)+`)
	reKebabCase  = regexp.MustCompile(`[a-z][a-z0-9]*(?:-[a-z][a-z0-9]+)+`)
	reFileExt    = regexp.MustCompile(`\b[\w\-]+\.(?:go|py|md|ts|sh|json|jsonl|txt|yaml|toml)\b`)
	reCliFlag    = regexp.MustCompile(`--[a-z][a-z0-9\-]+`)
	reAsciiWord  = regexp.MustCompile(`[a-zA-Z]{4,}`)
)

var stopWords = map[string]bool{
	"with": true, "that": true, "from": true, "this": true, "have": true,
	"will": true, "also": true, "just": true, "your": true, "into": true,
	"about": true, "then": true, "when": true, "where": true, "which": true,
	"their": true, "there": true, "these": true, "those": true, "been": true,
	"were": true, "they": true, "them": true, "some": true, "more": true,
	"each": true, "only": true, "such": true, "than": true, "what": true,
	"does": true, "would": true, "could": true, "should": true, "must": true,
}

func extractCodeTokens(text string) []string {
	return (&DynamicEntitiesSource{}).extractCodeTokens(text)
}

// DynamicEntitiesSource extracts code-style tokens from recent dialogue text.
type DynamicEntitiesSource struct {
	// TextFn returns prose text for extraction. If nil, reads from SessionSource.
	TextFn func() string
	// TokenCap caps the number of dynamic tokens returned. Zero means the default of 30.
	TokenCap int
	// MinTokenLen is the minimum token length to keep (also drives the plain-ASCII-word
	// regex quantifier). Zero means the default of 4.
	MinTokenLen int
}

// extractCodeTokens extracts code-style tokens from text, honoring TokenCap/MinTokenLen
// (falling back to the defaults 30/4 when unset).
func (s *DynamicEntitiesSource) extractCodeTokens(text string) []string {
	tokenCap := s.TokenCap
	if tokenCap == 0 {
		tokenCap = 30
	}
	minTokenLen := s.MinTokenLen
	if minTokenLen == 0 {
		minTokenLen = 4
	}

	asciiWordRe := reAsciiWord
	if minTokenLen != 4 {
		asciiWordRe = regexp.MustCompile(fmt.Sprintf(`[a-zA-Z]{%d,}`, minTokenLen))
	}

	seen := make(map[string]bool)
	var results []string

	addToken := func(t string) {
		if len(t) < minTokenLen || stopWords[strings.ToLower(t)] || seen[t] {
			return
		}
		seen[t] = true
		results = append(results, t)
	}

	for _, re := range []*regexp.Regexp{rePascalCase, reCamelCase, reSnakeCase, reKebabCase, reFileExt, reCliFlag} {
		for _, m := range re.FindAllString(text, -1) {
			addToken(m)
		}
	}

	// plain ASCII words >= minTokenLen chars (not already captured)
	for _, m := range asciiWordRe.FindAllString(text, -1) {
		addToken(m)
	}

	if len(results) > tokenCap {
		results = results[:tokenCap]
	}
	return results
}

func (s *DynamicEntitiesSource) Name() string { return "dynamic_entities" }

func (s *DynamicEntitiesSource) Fetch(root string) (string, string) {
	var text string
	if s.TextFn != nil {
		text = s.TextFn()
	} else {
		raw, _ := (&SessionSource{}).Fetch(root)
		// Extract "## Claude Code Session" section (structured session data).
		if idx := strings.Index(raw, "## Claude Code Session\n"); idx >= 0 {
			body := raw[idx+len("## Claude Code Session\n"):]
			// Stop at next "## " header or end of string.
			if next := strings.Index(body, "\n## "); next >= 0 {
				text = body[:next]
			} else {
				text = body
			}
		}
		// Also append git log lines for additional code tokens.
		gitLog := DefaultGitRunner(root)
		if gitLog != "" {
			if text != "" {
				text += "\n"
			}
			text += gitLog
		}
	}

	if strings.TrimSpace(text) == "" {
		return "", "dynamic_entities"
	}

	tokens := s.extractCodeTokens(text)
	if len(tokens) == 0 {
		return "", "dynamic_entities"
	}

	var sb strings.Builder
	sb.WriteString("## Known Entities (dynamic)\n")
	for _, t := range tokens {
		fmt.Fprintf(&sb, "%s: %s\n", t, t)
	}
	return sb.String(), "dynamic_entities"
}
