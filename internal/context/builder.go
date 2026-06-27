package context

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

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

type taskFrontmatter struct {
	ID     string `yaml:"id"`
	Title  string `yaml:"title"`
	Status string `yaml:"status"`
}

var numberWord = map[int]string{
	1: "one", 2: "two", 3: "three", 4: "four", 5: "five",
	6: "six", 7: "seven", 8: "eight", 9: "nine", 10: "ten",
}

// spokenTaskID converts "TASK-N" to "task N_as_word" for N in 1..10.
// Returns "" if N > 10 or not parseable.
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
	sb.WriteString("- build context: BuildContext\n")
	sb.WriteString("- c l i: CLI\n")
	sb.WriteString("- dash dash file: --file\n")
	sb.WriteString("- dash dash iterate: --iterate\n")
	return sb.String()
}

// BuildContext reads project context and returns an asr_hint string.
// root is the project root directory. gitRunner may be nil (uses DefaultGitRunner).
func BuildContext(root string, gitRunner GitRunner) string {
	if gitRunner == nil {
		gitRunner = DefaultGitRunner
	}

	var sb strings.Builder

	// Collect task IDs while reading backlog/tasks/*.md frontmatter
	taskGlob := filepath.Join(root, "backlog", "tasks", "*.md")
	matches, _ := filepath.Glob(taskGlob)

	var taskIDs []string
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
						taskIDs = append(taskIDs, fm.ID)
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

	// Prepend Known Entities section
	sb.WriteString(buildKnownEntities(taskIDs))

	// Active Tasks section
	if len(taskLines) > 0 {
		sb.WriteString("\n## Active Tasks\n")
		for _, line := range taskLines {
			sb.WriteString(line)
		}
	}

	// Read CLAUDE.md if present
	claudePath := filepath.Join(root, "CLAUDE.md")
	claudeData, err := os.ReadFile(claudePath)
	if err == nil {
		sb.WriteString("\n## CLAUDE.md\n")
		sb.Write(claudeData)
		sb.WriteString("\n")
	}

	// Read git log
	gitLog := gitRunner(root)
	if gitLog != "" {
		sb.WriteString("\n## Recent Commits\n")
		sb.WriteString(gitLog)
	}

	return sb.String()
}
