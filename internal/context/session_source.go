package context

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// tailLines reads the last n lines from the file at path using io.SeekEnd strategy.
// It does not read the entire file into memory.
func tailLines(path string, n int) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	// Get file size
	size, err := f.Seek(0, io.SeekEnd)
	if err != nil {
		return nil, err
	}

	if size == 0 {
		return nil, nil
	}

	// Walk backwards collecting bytes until we've seen n newlines
	buf := make([]byte, 0, 4096)
	newlinesFound := 0
	pos := size

	const chunkSize = 4096
	for pos > 0 && newlinesFound <= n {
		readSize := int64(chunkSize)
		if readSize > pos {
			readSize = pos
		}
		pos -= readSize

		chunk := make([]byte, readSize)
		if _, err := f.ReadAt(chunk, pos); err != nil {
			return nil, err
		}

		// Prepend chunk to buf
		buf = append(chunk, buf...)

		// Count newlines in the newly prepended bytes
		for i := int(readSize) - 1; i >= 0; i-- {
			if chunk[i] == '\n' {
				newlinesFound++
				if newlinesFound > n {
					// We have enough: trim buf to start after this newline position
					// position within buf: i + (len(buf) - len(chunk) ... wait, we prepended)
					// chunk is at buf[0:readSize], so position i in chunk is buf[i]
					// But we need to discard everything before this newline
					buf = buf[i+1:]
					pos = 0 // stop outer loop
					break
				}
			}
		}
	}

	// Parse buf into lines
	content := strings.TrimRight(string(buf), "\n")
	if content == "" {
		return nil, nil
	}
	all := strings.Split(content, "\n")

	// Filter empty lines
	var result []string
	for _, l := range all {
		if l != "" {
			result = append(result, l)
		}
	}
	return result, nil
}

// sessionEntry is a minimal representation of a Claude Code JSONL session entry.
type sessionEntry struct {
	Type    string `json:"type"`
	Message struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

// toolUse represents a tool_use content block.
type toolUse struct {
	Type  string          `json:"type"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

// toolInput represents common tool input fields.
type toolInput struct {
	FilePath string `json:"file_path"`
	Command  string `json:"command"`
}

// taskIDPattern matches TASK-N references.
var taskIDPattern = regexp.MustCompile(`TASK-\d+`)

// parseSessionSnippet extracts relevant information from JSONL session lines.
// It returns a formatted snippet with editing, commands, and task mentions.
func parseSessionSnippet(lines []string) string {
	fileSet := make(map[string]bool)
	cmdSet := make(map[string]bool)
	taskSet := make(map[string]bool)

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		var entry sessionEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}

		// Try to parse content as an array of tool_use blocks (assistant messages)
		if entry.Message.Content != nil {
			var contentArr []toolUse
			if err := json.Unmarshal(entry.Message.Content, &contentArr); err == nil {
				for _, block := range contentArr {
					if block.Type == "tool_use" {
						var inp toolInput
						_ = json.Unmarshal(block.Input, &inp)
						switch block.Name {
						case "Read", "Edit", "Write":
							if inp.FilePath != "" {
								fileSet[inp.FilePath] = true
							}
						case "Bash":
							if inp.Command != "" {
								cmdSet[inp.Command] = true
							}
						}
					}
				}
			} else {
				// Try as a plain string (user messages)
				var contentStr string
				if err := json.Unmarshal(entry.Message.Content, &contentStr); err == nil {
					for _, id := range taskIDPattern.FindAllString(contentStr, -1) {
						taskSet[id] = true
					}
				}
			}
		}
	}

	if len(fileSet) == 0 && len(cmdSet) == 0 && len(taskSet) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("## Claude Code Session\n")

	if len(fileSet) > 0 {
		files := make([]string, 0, len(fileSet))
		for f := range fileSet {
			files = append(files, f)
		}
		sb.WriteString("- editing: ")
		sb.WriteString(strings.Join(files, ", "))
		sb.WriteString("\n")
	}

	if len(cmdSet) > 0 {
		cmds := make([]string, 0, len(cmdSet))
		for c := range cmdSet {
			cmds = append(cmds, c)
		}
		sb.WriteString("- ran: ")
		sb.WriteString(strings.Join(cmds, "; "))
		sb.WriteString("\n")
	}

	if len(taskSet) > 0 {
		tasks := make([]string, 0, len(taskSet))
		for id := range taskSet {
			tasks = append(tasks, id)
		}
		sb.WriteString("- mentioned: ")
		sb.WriteString(strings.Join(tasks, ", "))
		sb.WriteString("\n")
	}

	return sb.String()
}

// SessionSource reads Claude Code session JSONL and contributes recent activity context.
type SessionSource struct {
	Lines       int            // number of tail lines to read; default 100
	jsonlPathFn func() string  // override for testing; nil = use CLAUDE_CODE_SESSION_ID env var
}

// Name returns "session".
func (s *SessionSource) Name() string { return "session" }

// Fetch returns a session context snippet and provenance "session".
// Returns ("", "session") if the session file is unavailable.
func (s *SessionSource) Fetch(root string) (string, string) {
	var path string
	if s.jsonlPathFn != nil {
		path = s.jsonlPathFn()
	} else {
		id := os.Getenv("CLAUDE_CODE_SESSION_ID")
		if id == "" {
			return "", "session"
		}
		home, _ := os.UserHomeDir()
		projectHash := strings.ReplaceAll(root, "/", "-")
		path = filepath.Join(home, ".claude", "projects", projectHash, id+".jsonl")
	}

	n := s.Lines
	if n == 0 {
		n = 100
	}

	lines, err := tailLines(path, n)
	if err != nil {
		return "", "session"
	}

	return parseSessionSnippet(lines), "session"
}
