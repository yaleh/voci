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
	seen := make(map[string]bool)
	var results []string

	addToken := func(t string) {
		if len(t) < 4 || stopWords[strings.ToLower(t)] || seen[t] {
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

	// plain ASCII words >= 4 chars (not already captured)
	for _, m := range reAsciiWord.FindAllString(text, -1) {
		addToken(m)
	}

	if len(results) > 30 {
		results = results[:30]
	}
	return results
}

// DynamicEntitiesSource extracts code-style tokens from recent dialogue text.
type DynamicEntitiesSource struct {
	// TextFn returns prose text for extraction. If nil, reads from SessionSource.
	TextFn func() string
}

func (s *DynamicEntitiesSource) Name() string { return "dynamic_entities" }

func (s *DynamicEntitiesSource) Fetch(root string) (string, string) {
	var text string
	if s.TextFn != nil {
		text = s.TextFn()
	} else {
		raw, _ := (&SessionSource{}).Fetch(root)
		// Strip everything before and including "## Recent Dialogue" header, keep prose
		if idx := strings.Index(raw, "## Recent Dialogue\n"); idx >= 0 {
			text = raw[idx+len("## Recent Dialogue\n"):]
		} else if idx := strings.Index(raw, "\n"); idx >= 0 {
			text = raw[idx+1:]
		} else {
			text = raw
		}
	}

	if strings.TrimSpace(text) == "" {
		return "", "dynamic_entities"
	}

	// Get static entity values to suppress (both keys and values from buildKnownEntities)
	staticTerms := make(map[string]bool)
	for _, line := range strings.Split(buildKnownEntities(nil), "\n") {
		// lines are like "- vocal: voci" or "- run hinted: RunHinted"
		// strip leading "- "
		line = strings.TrimPrefix(line, "- ")
		if parts := strings.SplitN(line, ": ", 2); len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			val := strings.TrimSpace(parts[1])
			// Add individual words from key and the value itself
			for _, w := range strings.Fields(key) {
				staticTerms[w] = true
			}
			staticTerms[val] = true
		}
	}

	tokens := extractCodeTokens(text)
	var dynamic []string
	for _, t := range tokens {
		if !staticTerms[t] {
			dynamic = append(dynamic, t)
		}
	}

	if len(dynamic) == 0 {
		return "", "dynamic_entities"
	}

	var sb strings.Builder
	sb.WriteString("## Known Entities (dynamic)\n")
	for _, t := range dynamic {
		fmt.Fprintf(&sb, "%s: %s\n", t, t)
	}
	return sb.String(), "dynamic_entities"
}
