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

// contentBlock represents any content block (tool_use, text, tool_result, etc.).
type contentBlock struct {
	Type  string          `json:"type"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
	Text  string          `json:"text"`
}

// toolUse is an alias kept for clarity; same layout as contentBlock.
type toolUse = contentBlock

// toolInput represents common tool input fields.
type toolInput struct {
	FilePath string `json:"file_path"`
	Command  string `json:"command"`
}

const (
	maxProseTurns       = 6
	maxProseCharsPerTurn = 200
	maxProseCharsTotal  = 1200
)

// taskIDPattern matches TASK-N references.
var taskIDPattern = regexp.MustCompile(`TASK-\d+`)

// parseSessionSnippet extracts relevant information from JSONL session lines.
// It returns a formatted snippet with editing, commands, task mentions, and recent prose.
func parseSessionSnippet(lines []string) string {
	fileSet := make(map[string]bool)
	cmdSet := make(map[string]bool)
	taskSet := make(map[string]bool)
	var proseTurns []string // collected in chronological order

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		var entry sessionEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}

		if entry.Message.Content == nil {
			continue
		}

		// Try as array of content blocks (assistant messages)
		var contentArr []contentBlock
		if err := json.Unmarshal(entry.Message.Content, &contentArr); err == nil {
			for _, block := range contentArr {
				switch block.Type {
				case "tool_use":
					var inp toolInput
					_ = json.Unmarshal(block.Input, &inp)
					switch block.Name {
					case "Read", "Edit", "Write":
						if inp.FilePath != "" {
							fileSet[inp.FilePath] = true
						}
					case "Bash":
						if inp.Command != "" {
							first := strings.SplitN(strings.TrimSpace(inp.Command), "\n", 2)[0]
							if first != "" {
								cmdSet[first] = true
							}
						}
					}
				case "text":
					if t := normalizeProse(block.Text); t != "" {
						proseTurns = append(proseTurns, t)
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
				if t := normalizeProse(contentStr); t != "" {
					proseTurns = append(proseTurns, t)
				}
			}
		}
	}

	// Apply caps: keep last maxProseTurns, trim each to maxProseCharsPerTurn
	if len(proseTurns) > maxProseTurns {
		proseTurns = proseTurns[len(proseTurns)-maxProseTurns:]
	}
	for i, t := range proseTurns {
		if len(t) > maxProseCharsPerTurn {
			proseTurns[i] = t[:maxProseCharsPerTurn]
		}
	}

	hasSession := len(fileSet) > 0 || len(cmdSet) > 0 || len(taskSet) > 0
	hasProse := len(proseTurns) > 0
	if !hasSession && !hasProse {
		return ""
	}

	var sb strings.Builder

	if hasSession {
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
	}

	if hasProse {
		sb.WriteString("## Recent Dialogue\n")
		total := 0
		for _, t := range proseTurns {
			if total+len(t) > maxProseCharsTotal {
				break
			}
			sb.WriteString(t)
			sb.WriteString("\n")
			total += len(t)
		}
	}

	return sb.String()
}

// normalizeProse collapses internal whitespace/newlines to single spaces and trims.
// Returns "" for empty or whitespace-only input.
func normalizeProse(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	return strings.TrimSpace(s)
}

// jsonlPathForSession returns the JSONL path for a given home directory, project root, and session ID.
func jsonlPathForSession(home, root, id string) string {
	hash := strings.ReplaceAll(root, "/", "-")
	return filepath.Join(home, ".claude", "projects", hash, id+".jsonl")
}

// readSessionFile reads a session ID from a file, trimming whitespace.
// Returns "" if the file does not exist or cannot be read.
func readSessionFile(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// SessionSource reads Claude Code session JSONL and contributes recent activity context.
type SessionSource struct {
	Lines       int            // number of tail lines to read; default 100
	jsonlPathFn func() string  // override for testing; nil = use sessionIDFn or env var
	sessionIDFn func() string  // override for testing; nil = read ~/.voci/session file
}

// Name returns "session".
func (s *SessionSource) Name() string { return "session" }

// Fetch returns a session context snippet and provenance "session".
// Priority: jsonlPathFn > ~/.voci/session (or sessionIDFn) > CLAUDE_CODE_SESSION_ID env > graceful degrade.
// Returns ("", "session") if the session file is unavailable.
func (s *SessionSource) Fetch(root string) (string, string) {
	var path string
	if s.jsonlPathFn != nil {
		path = s.jsonlPathFn()
	} else {
		home, _ := os.UserHomeDir()

		// Determine session ID: use sessionIDFn hook if set, else read ~/.voci/session file
		var id string
		if s.sessionIDFn != nil {
			id = s.sessionIDFn()
		} else {
			id = readSessionFile(filepath.Join(home, ".voci", "session"))
		}

		if id != "" {
			path = jsonlPathForSession(home, root, id)
		} else {
			// Fall back to CLAUDE_CODE_SESSION_ID env var
			envID := os.Getenv("CLAUDE_CODE_SESSION_ID")
			if envID == "" {
				return "", "session"
			}
			path = jsonlPathForSession(home, root, envID)
		}
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
