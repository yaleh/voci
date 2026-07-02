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
	Type             string `json:"type"`
	IsCompactSummary bool   `json:"isCompactSummary"`
	PromptSource     string `json:"promptSource"`
	Message          struct {
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

// Default values used when the corresponding SessionSource field is zero.
const (
	defaultMaxProseTurns        = 6
	defaultMaxProseCharsPerTurn = 500
	defaultMaxProseCharsTotal   = 3000
)

// taskIDPattern matches TASK-N references.
var taskIDPattern = regexp.MustCompile(`TASK-\d+`)

// fencedCodeBlock matches triple-backtick code fences (with optional language tag).
// [\s\S]*? matches any character including newlines, non-greedy.
var fencedCodeBlock = regexp.MustCompile("```[\\s\\S]*?```")

// parseSessionSnippet extracts relevant information from JSONL session lines.
// It returns a formatted snippet with editing, commands, task mentions, and recent prose.
func (s *SessionSource) parseSessionSnippet(lines []string) string {
	maxProseTurns := s.MaxProseTurns
	if maxProseTurns == 0 {
		maxProseTurns = defaultMaxProseTurns
	}
	maxProseCharsPerTurn := s.MaxProseCharsPerTurn
	if maxProseCharsPerTurn == 0 {
		maxProseCharsPerTurn = defaultMaxProseCharsPerTurn
	}
	maxProseCharsTotal := s.MaxProseCharsTotal
	if maxProseCharsTotal == 0 {
		maxProseCharsTotal = defaultMaxProseCharsTotal
	}

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

		// Skip system-injected turns: compaction summaries and system-prompt-source entries.
		if entry.IsCompactSummary {
			continue
		}
		if entry.PromptSource == "system" {
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
					if t := normalizeProse(stripCodeFences(block.Text)); t != "" {
						proseTurns = append(proseTurns, "A: "+t)
					}
				}
			}
		} else {
			// Try as a plain string (user messages)
			var contentStr string
			if err := json.Unmarshal(entry.Message.Content, &contentStr); err == nil {
				// Skip harness-injected XML wrapper turns. All harness tags start with
				// '<' followed by a lowercase ASCII letter. This filters <task-notification>,
				// <system-reminder>, <local-command-caveat>, etc.
				// Known limitation: user messages literally starting with a lowercase
				// angle-bracket word (e.g. "<enter>") are also filtered. Acceptable for
				// a voice-first UI where dictated text does not start with raw XML tags.
				if len(contentStr) >= 2 && contentStr[0] == '<' &&
					contentStr[1] >= 'a' && contentStr[1] <= 'z' {
					continue
				}
				for _, id := range taskIDPattern.FindAllString(contentStr, -1) {
					taskSet[id] = true
				}
				if t := normalizeProse(contentStr); t != "" {
					proseTurns = append(proseTurns, "U: "+t)
				}
			}
		}
	}

	// Apply caps: keep last maxProseTurns, trim each to maxProseCharsPerTurn
	if len(proseTurns) > maxProseTurns {
		proseTurns = proseTurns[len(proseTurns)-maxProseTurns:]
	}
	// Cap per turn: prefix ("A: "/"U: ") is 3 chars; cap applies to total including prefix.
	// Use rune-based slicing to avoid splitting multi-byte UTF-8 sequences (e.g. Chinese).
	for i, t := range proseTurns {
		r := []rune(t)
		if len(r) > maxProseCharsPerTurn {
			proseTurns[i] = string(r[:maxProseCharsPerTurn]) + "…"
		}
	}
	// maxProseCharsPerTurn = 500 means up to 497 chars of content after the 3-char prefix.

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
			rc := []rune(t)
			if total+len(rc) > maxProseCharsTotal {
				break
			}
			sb.WriteString(t)
			sb.WriteString("\n")
			total += len(rc)
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

// stripCodeFences removes fenced code block markers (``` lines) while keeping
// the body content. This preserves function names and identifiers for ASR entity
// injection while removing the visual noise of fence markers.
func stripCodeFences(s string) string {
	return fencedCodeBlock.ReplaceAllStringFunc(s, func(block string) string {
		// Find end of opening fence line (```lang\n)
		start := strings.Index(block, "\n")
		// Find start of closing fence
		end := strings.LastIndex(block, "\n```")
		if start < 0 || end <= start {
			return " "
		}
		body := strings.TrimSpace(block[start+1 : end])
		if body == "" {
			return " "
		}
		return " " + body + " "
	})
}

// DialogueTurn is a single conversation turn for Web UI display. Unlike the
// flattened ASR hint (## Recent Dialogue), Text preserves full Markdown fidelity:
// newlines, blank lines, GFM tables, and code fences. It is serialized to JSON
// and rendered by the frontend via marked + DOMPurify.
type DialogueTurn struct {
	Role string `json:"role"` // "user" or "assistant"
	Text string `json:"text"` // full Markdown, unmodified
}

// parseSessionDialogue extracts conversation turns with full Markdown
// fidelity for Web UI rendering. It shares the system-turn / harness-XML
// filtering with parseSessionSnippet but does NOT flatten whitespace, drop
// blank lines, or strip code fences — the frontend renders this Markdown as-is.
// maxTurns controls how many of the most recent turns are kept; pass -1 for
// no limit (Dialogue path). Returns nil when there are no displayable turns.
func (s *SessionSource) parseSessionDialogue(lines []string, maxTurns int) []DialogueTurn {
	var turns []DialogueTurn

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		var entry sessionEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		if entry.IsCompactSummary || entry.PromptSource == "system" {
			continue
		}
		if entry.Message.Content == nil {
			continue
		}

		// Assistant messages: array of content blocks. Keep text blocks verbatim.
		var contentArr []contentBlock
		if err := json.Unmarshal(entry.Message.Content, &contentArr); err == nil {
			for _, block := range contentArr {
				if block.Type == "text" {
					if t := strings.TrimSpace(block.Text); t != "" {
						turns = append(turns, DialogueTurn{Role: "assistant", Text: t})
					}
				}
			}
			continue
		}

		// User messages: plain string. Skip harness-injected XML wrapper turns
		// (same guard as parseSessionSnippet: '<' followed by a lowercase letter).
		var contentStr string
		if err := json.Unmarshal(entry.Message.Content, &contentStr); err == nil {
			if len(contentStr) >= 2 && contentStr[0] == '<' &&
				contentStr[1] >= 'a' && contentStr[1] <= 'z' {
				continue
			}
			if t := strings.TrimSpace(contentStr); t != "" {
				turns = append(turns, DialogueTurn{Role: "user", Text: t})
			}
		}
	}

	// Keep only the last maxTurns turns (most recent conversation).
	// maxTurns < 0 means no limit (Dialogue path for the browser).
	if maxTurns >= 0 && len(turns) > maxTurns {
		turns = turns[len(turns)-maxTurns:]
	}
	return turns
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
	Lines                int // number of tail lines to read; default 100
	MaxProseTurns        int // recent turns kept in hint/dialogue; default 6
	MaxProseCharsPerTurn int // per-turn char cap in the flattened hint; default 500
	MaxProseCharsTotal   int // total char cap in the flattened hint; default 3000

	jsonlPathFn func() string // override for testing; nil = use sessionIDFn or env var
	sessionIDFn func() string // override for testing; nil = read ~/.voci/session file
}

// Name returns "session".
func (s *SessionSource) Name() string { return "session" }

// ResolveJSONLPath is the exported wrapper around resolveJSONLPath.
func (s *SessionSource) ResolveJSONLPath(root string) string {
	return s.resolveJSONLPath(root)
}

// resolveJSONLPath resolves the session JSONL file path using the priority:
// jsonlPathFn > sessionIDFn/~/.voci/session > CLAUDE_CODE_SESSION_ID env.
// Returns "" when no session can be resolved.
func (s *SessionSource) resolveJSONLPath(root string) string {
	if s.jsonlPathFn != nil {
		return s.jsonlPathFn()
	}
	home, _ := os.UserHomeDir()

	// Determine session ID: use sessionIDFn hook if set, else read ~/.voci/session file.
	var id string
	if s.sessionIDFn != nil {
		id = s.sessionIDFn()
	} else {
		id = readSessionFile(filepath.Join(home, ".voci", "session"))
	}
	if id == "" {
		id = os.Getenv("CLAUDE_CODE_SESSION_ID") // fall back to env var
	}
	if id == "" {
		return ""
	}
	return jsonlPathForSession(home, root, id)
}

// allSessionLines reads the complete JSONL session file into memory and
// returns all non-empty lines. Used by Dialogue() to give the browser the
// full conversation history unconditionally.
// Returns nil when no session is available or the file cannot be read.
func (s *SessionSource) allSessionLines(root string) []string {
	path := s.resolveJSONLPath(root)
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	content := strings.TrimRight(string(data), "\n")
	if content == "" {
		return nil
	}
	all := strings.Split(content, "\n")
	var result []string
	for _, l := range all {
		if l != "" {
			result = append(result, l)
		}
	}
	return result
}

// tailSessionLines resolves the JSONL path and returns its last N lines.
// Returns nil when no session is available or the file cannot be read.
func (s *SessionSource) tailSessionLines(root string) []string {
	path := s.resolveJSONLPath(root)
	if path == "" {
		return nil
	}
	n := s.Lines
	if n == 0 {
		n = 100
	}
	lines, err := tailLines(path, n)
	if err != nil {
		return nil
	}
	return lines
}

// Fetch returns a session context snippet and provenance "session".
// Priority: jsonlPathFn > ~/.voci/session (or sessionIDFn) > CLAUDE_CODE_SESSION_ID env > graceful degrade.
// Returns ("", "session") if the session file is unavailable.
func (s *SessionSource) Fetch(root string) (string, string) {
	lines := s.tailSessionLines(root)
	if lines == nil {
		return "", "session"
	}
	return s.parseSessionSnippet(lines), "session"
}

// Dialogue returns all conversation turns with full Markdown fidelity for the
// Web UI. Unlike Fetch (which reads only the tail and caps prose turns for ASR
// token budget), Dialogue reads the complete JSONL file with no turn limit.
// Returns nil when no session is available.
func (s *SessionSource) Dialogue(root string) []DialogueTurn {
	lines := s.allSessionLines(root)
	if lines == nil {
		return nil
	}
	return s.parseSessionDialogue(lines, -1)
}
