package context

import (
	"os"
	"os/exec"
	"path/filepath"
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

// BuildContext reads project context and returns an asr_hint string.
// root is the project root directory. gitRunner may be nil (uses DefaultGitRunner).
func BuildContext(root string, gitRunner GitRunner) string {
	if gitRunner == nil {
		gitRunner = DefaultGitRunner
	}

	var sb strings.Builder

	// Read backlog/tasks/*.md frontmatter
	taskGlob := filepath.Join(root, "backlog", "tasks", "*.md")
	matches, _ := filepath.Glob(taskGlob)
	if len(matches) > 0 {
		sb.WriteString("## Active Tasks\n")
		for _, m := range matches {
			data, err := os.ReadFile(m)
			if err != nil {
				continue
			}
			content := string(data)
			// Extract YAML frontmatter between --- delimiters
			if strings.HasPrefix(content, "---") {
				end := strings.Index(content[3:], "---")
				if end >= 0 {
					yamlContent := content[3 : end+3]
					var fm taskFrontmatter
					if err := yaml.Unmarshal([]byte(yamlContent), &fm); err == nil {
						if fm.ID != "" {
							sb.WriteString("- " + fm.ID + ": " + fm.Title)
							if fm.Status != "" {
								sb.WriteString(" [" + fm.Status + "]")
							}
							sb.WriteString("\n")
						}
					}
				}
			}
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
