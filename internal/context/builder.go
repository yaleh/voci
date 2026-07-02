package context

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

)

// Source contributes context snippets.
type Source interface {
	Name() string
	// Fetch returns (snippet, provenance) or ("", "") if unavailable.
	Fetch(root string) (snippet string, provenance string)
}

// Result holds the output of Builder.Build.
type Result struct {
	AsrHint    string            `json:"asr_hint"`
	FullContext string            `json:"full_context"`
	Provenance map[string]string `json:"provenance"`
}

// Builder builds context from registered sources.
type Builder struct {
	Sources []Source
	// CacheTTL is how long BuildCached treats context_cache.json as fresh.
	// Zero means the default of 60s.
	CacheTTL time.Duration
}

// Register adds a source to the builder.
func (b *Builder) Register(s Source) {
	b.Sources = append(b.Sources, s)
}

// Build collects context from all registered sources and returns a Result.
// It also writes the result to <root>/.voci/context_cache.json.
func (b *Builder) Build(root string) Result {
	provenance := make(map[string]string)
	snippets := make(map[string]string)

	for _, src := range b.Sources {
		snippet, prov := src.Fetch(root)
		provenance[prov] = snippet
		snippets[src.Name()] = snippet
	}

	asrHint := b.assembleAsrHint(snippets)
	fullContext := b.assembleFullContext(snippets)

	result := Result{
		AsrHint:    asrHint,
		FullContext: fullContext,
		Provenance: provenance,
	}

	// Write cache (best-effort; ignore errors)
	b.writeCache(root, result)

	return result
}

// BuildCached reads the cache if it exists and is < 60s old; otherwise calls Build.
func (b *Builder) BuildCached(root string) Result {
	cachePath := filepath.Join(root, ".voci", "context_cache.json")
	data, err := os.ReadFile(cachePath)
	if err == nil {
		var cf struct {
			Result    Result    `json:"result"`
			CreatedAt time.Time `json:"created_at"`
		}
		if err := json.Unmarshal(data, &cf); err == nil {
			ttl := b.CacheTTL
			if ttl == 0 {
				ttl = 60 * time.Second
			}
			if time.Since(cf.CreatedAt) < ttl {
				return cf.Result
			}
		}
	}
	return b.Build(root)
}

// writeCache writes the result to <root>/.voci/context_cache.json.
func (b *Builder) writeCache(root string, result Result) {
	vociDir := filepath.Join(root, ".voci")
	if err := os.MkdirAll(vociDir, 0755); err != nil {
		return
	}
	cf := struct {
		Result    Result    `json:"result"`
		CreatedAt time.Time `json:"created_at"`
	}{Result: result, CreatedAt: time.Now()}
	data, err := json.Marshal(cf)
	if err != nil {
		return
	}
	_ = os.WriteFile(filepath.Join(vociDir, "context_cache.json"), data, 0644)
}

// assembleAsrHint concatenates source snippets in the legacy format.
func (b *Builder) assembleAsrHint(snippets map[string]string) string {
	var sb strings.Builder

	// CLAUDE.md
	if s, ok := snippets["claude.md"]; ok && s != "" {
		sb.WriteString("## CLAUDE.md\n")
		sb.WriteString(s)
		sb.WriteString("\n")
	}

	// Git log
	if s, ok := snippets["git"]; ok && s != "" {
		sb.WriteString("\n## Recent Commits\n")
		sb.WriteString(s)
	}

	// Append any extra snippets not covered above (e.g. session, custom sources)
	handled := map[string]bool{"claude.md": true, "git": true}
	for name, s := range snippets {
		if !handled[name] && s != "" {
			sb.WriteString("\n")
			sb.WriteString(s)
		}
	}

	return sb.String()
}

// assembleFullContext builds the rich Markdown format with section headers.
func (b *Builder) assembleFullContext(snippets map[string]string) string {
	var sb strings.Builder
	sb.WriteString("## Project Context\n")

	if s, ok := snippets["claude.md"]; ok && s != "" {
		sb.WriteString("\n### Project Instructions (CLAUDE.md)\n")
		sb.WriteString(s)
		sb.WriteString("\n")
	}

	if s, ok := snippets["git"]; ok && s != "" {
		sb.WriteString("\n### Recent Commits\n")
		sb.WriteString(s)
	}

	return sb.String()
}

// ---- Concrete Source implementations ----

// ClaudeMdSource reads CLAUDE.md.
type ClaudeMdSource struct{}

func (s *ClaudeMdSource) Name() string { return "claude.md" }

func (s *ClaudeMdSource) Fetch(root string) (string, string) {
	claudePath := filepath.Join(root, "CLAUDE.md")
	data, err := os.ReadFile(claudePath)
	if err != nil {
		return "", "claude.md"
	}
	return string(data), "claude.md"
}

// GitLogSource runs git log. Runner is injected for tests; nil uses DefaultGitRunner.
type GitLogSource struct {
	Runner func() string
}

func (s *GitLogSource) Name() string { return "git" }

func (s *GitLogSource) Fetch(root string) (string, string) {
	var log string
	if s.Runner != nil {
		log = s.Runner()
	} else {
		log = DefaultGitRunner(root)
	}
	return log, "git"
}

// ---- Backward compatibility ----

// GitRunner is a function that returns git log output.
type GitRunner func(root string) string

// DefaultGitRunner runs real git log.
func DefaultGitRunner(root string) string {
	cmd := exec.Command("git", "-C", root, "log", "--oneline", "-10")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return string(out)
}

// defaultBuilder creates a Builder with the standard sources (excluding session source).
// If gitRunner is provided, it wraps it for GitLogSource.
func defaultBuilder(root string, gitRunner GitRunner) *Builder {
	var runner func() string
	if gitRunner != nil {
		capturedRoot := root
		capturedRunner := gitRunner
		runner = func() string { return capturedRunner(capturedRoot) }
	}

	b := &Builder{}
	b.Register(&DynamicEntitiesSource{})
	b.Register(&ClaudeMdSource{})
	b.Register(&GitLogSource{Runner: runner})
	return b
}

// BuildContextWithSource builds context with an optional extra Source (e.g. a session source).
// root is the project root directory. src may be nil (no session source registered).
// gitRunner may be nil (uses DefaultGitRunner).
func BuildContextWithSource(root string, src Source, gitRunner GitRunner) string {
	b := defaultBuilder(root, gitRunner)
	if src != nil {
		b.Register(src)
	}
	return b.Build(root).AsrHint
}

// BuilderTuning holds B-class tuning knobs (sourced from config.Config) that get
// threaded into Builder/DynamicEntitiesSource construction. Zero fields fall back
// to each component's own internal default (see defaultBuilderWithTuning).
type BuilderTuning struct {
	CacheTTL          time.Duration
	EntityTokenCap    int
	EntityMinTokenLen int
}

// defaultBuilderWithTuning is like defaultBuilder but applies tuning to the
// Builder itself and to the registered DynamicEntitiesSource.
func defaultBuilderWithTuning(root string, gitRunner GitRunner, tuning BuilderTuning) *Builder {
	b := defaultBuilder(root, gitRunner)
	b.CacheTTL = tuning.CacheTTL
	for _, src := range b.Sources {
		if des, ok := src.(*DynamicEntitiesSource); ok {
			des.TokenCap = tuning.EntityTokenCap
			des.MinTokenLen = tuning.EntityMinTokenLen
		}
	}
	return b
}

// BuildContextWithSourceAndTuning is like BuildContextWithSource but also applies
// BuilderTuning (B-class config values) to the Builder and DynamicEntitiesSource.
func BuildContextWithSourceAndTuning(root string, src Source, gitRunner GitRunner, tuning BuilderTuning) string {
	b := defaultBuilderWithTuning(root, gitRunner, tuning)
	if src != nil {
		b.Register(src)
	}
	return b.Build(root).AsrHint
}

// BuildContext reads project context and returns an asr_hint string.
// root is the project root directory. gitRunner may be nil (uses DefaultGitRunner).
// This is a backward-compatible wrapper around BuildContextWithSource.
func BuildContext(root string, gitRunner GitRunner) string {
	return BuildContextWithSource(root, &SessionSource{}, gitRunner)
}
