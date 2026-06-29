package context

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
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
			if time.Since(cf.CreatedAt) < 60*time.Second {
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

	// Known Entities first (no leading newline)
	if s, ok := snippets["entities"]; ok && s != "" {
		sb.WriteString(s)
	}

	// Active Tasks (with leading newline separator)
	if s, ok := snippets["backlog"]; ok && s != "" {
		sb.WriteString("\n## Active Tasks\n")
		sb.WriteString(s)
	}

	// CLAUDE.md
	if s, ok := snippets["claude.md"]; ok && s != "" {
		sb.WriteString("\n## CLAUDE.md\n")
		sb.WriteString(s)
		sb.WriteString("\n")
	}

	// Git log
	if s, ok := snippets["git"]; ok && s != "" {
		sb.WriteString("\n## Recent Commits\n")
		sb.WriteString(s)
	}

	// Append any extra snippets not covered above (e.g. session, custom sources)
	handled := map[string]bool{"entities": true, "backlog": true, "claude.md": true, "git": true}
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

	if s, ok := snippets["backlog"]; ok && s != "" {
		sb.WriteString("\n### Backlog Tasks\n")
		sb.WriteString(s)
	}

	if s, ok := snippets["claude.md"]; ok && s != "" {
		sb.WriteString("\n### Project Instructions (CLAUDE.md)\n")
		sb.WriteString(s)
		sb.WriteString("\n")
	}

	if s, ok := snippets["git"]; ok && s != "" {
		sb.WriteString("\n### Recent Commits\n")
		sb.WriteString(s)
	}

	if s, ok := snippets["entities"]; ok && s != "" {
		sb.WriteString("\n### Known Entities\n")
		sb.WriteString(s)
	}

	return sb.String()
}

// ---- Concrete Source implementations ----

// taskFrontmatter holds frontmatter fields from a task markdown file.
type taskFrontmatter struct {
	ID     string `yaml:"id"`
	Title  string `yaml:"title"`
	Status string `yaml:"status"`
}

// BacklogSource reads backlog/tasks/*.md frontmatter.
type BacklogSource struct{}

func (s *BacklogSource) Name() string { return "backlog" }

func (s *BacklogSource) Fetch(root string) (string, string) {
	taskGlob := filepath.Join(root, "backlog", "tasks", "*.md")
	matches, _ := filepath.Glob(taskGlob)

	var taskLines []string
	for _, m := range matches {
		data, err := os.ReadFile(m)
		if err != nil {
			continue
		}
		content := string(data)
		if strings.HasPrefix(content, "---") {
			end := strings.Index(content[3:], "---")
			if end >= 0 {
				yamlContent := content[3 : end+3]
				var fm taskFrontmatter
				if err := yaml.Unmarshal([]byte(yamlContent), &fm); err == nil {
					if fm.ID != "" {
						line := "- " + fm.ID + ": " + fm.Title
						if fm.Status != "" {
							line += " [" + fm.Status + "]"
						}
						line += "\n"
						taskLines = append(taskLines, line)
					}
				}
			}
		}
	}

	if len(taskLines) == 0 {
		return "", "backlog"
	}

	var sb strings.Builder
	for _, line := range taskLines {
		sb.WriteString(line)
	}
	return sb.String(), "backlog"
}

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

// KnownEntitiesSource generates the Known Entities section by reading task IDs from backlog.
type KnownEntitiesSource struct{}

func (s *KnownEntitiesSource) Name() string { return "entities" }

func (s *KnownEntitiesSource) Fetch(root string) (string, string) {
	taskGlob := filepath.Join(root, "backlog", "tasks", "*.md")
	matches, _ := filepath.Glob(taskGlob)

	var taskIDs []string
	for _, m := range matches {
		data, err := os.ReadFile(m)
		if err != nil {
			continue
		}
		content := string(data)
		if strings.HasPrefix(content, "---") {
			end := strings.Index(content[3:], "---")
			if end >= 0 {
				yamlContent := content[3 : end+3]
				var fm taskFrontmatter
				if err := yaml.Unmarshal([]byte(yamlContent), &fm); err == nil {
					if fm.ID != "" {
						taskIDs = append(taskIDs, fm.ID)
					}
				}
			}
		}
	}

	return buildKnownEntities(taskIDs), "entities"
}

// ---- Known Entities helpers ----

var numberWord = map[int]string{
	1: "one", 2: "two", 3: "three", 4: "four", 5: "five",
	6: "six", 7: "seven", 8: "eight", 9: "nine", 10: "ten",
}

// spokenTaskID converts "TASK-N" to "task N_as_word" for N in 1..10.
func spokenTaskID(id string) string {
	if !strings.HasPrefix(id, "TASK-") {
		return ""
	}
	numStr := strings.TrimPrefix(id, "TASK-")
	n, err := strconv.Atoi(numStr)
	if err != nil {
		return ""
	}
	word, ok := numberWord[n]
	if !ok {
		return ""
	}
	return fmt.Sprintf("task %s", word)
}

// buildKnownEntities creates the ## Known Entities section for the hint.
func buildKnownEntities(taskIDs []string) string {
	var sb strings.Builder
	sb.WriteString("## Known Entities\n")
	sb.WriteString("- vocal: voci\n")
	for _, id := range taskIDs {
		spoken := spokenTaskID(id)
		if spoken != "" {
			sb.WriteString(fmt.Sprintf("- %s: %s\n", spoken, id))
		}
	}
	sb.WriteString("- inter nul pipeline: internal/pipeline\n")
	sb.WriteString("- inter nul context: internal/context\n")
	sb.WriteString("- inter nul a s r: internal/asr\n")
	sb.WriteString("- inter nul config: internal/config\n")
	sb.WriteString("- inter nul ollama: internal/ollama\n")
	sb.WriteString("- run hinted: RunHinted\n")
	sb.WriteString("- run a hinted: RunHinted\n")
	sb.WriteString("- build context: BuildContext\n")
	sb.WriteString("- build a context: BuildContext\n")
	sb.WriteString("- c l i: CLI\n")
	sb.WriteString("- dash dash file: --file\n")
	sb.WriteString("- dash dash iterate: --iterate\n")
	return sb.String()
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
	b.Register(&KnownEntitiesSource{})
	b.Register(&DynamicEntitiesSource{})
	b.Register(&BacklogSource{})
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

// BuildContext reads project context and returns an asr_hint string.
// root is the project root directory. gitRunner may be nil (uses DefaultGitRunner).
// This is a backward-compatible wrapper around BuildContextWithSource.
func BuildContext(root string, gitRunner GitRunner) string {
	return BuildContextWithSource(root, &SessionSource{}, gitRunner)
}
