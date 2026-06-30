package context

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---- Phase A: tailLines + parseSessionSnippet ----

func TestTailLines_FewerLinesThanN(t *testing.T) {
	path := filepath.Join("testdata", "session_few_lines.jsonl")
	lines, err := tailLines(path, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(lines) != 3 {
		t.Errorf("expected 3 lines, got %d", len(lines))
	}
}

func TestTailLines_MoreLinesThanN(t *testing.T) {
	path := filepath.Join("testdata", "session_many_lines.jsonl")
	lines, err := tailLines(path, 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(lines) != 5 {
		t.Errorf("expected 5 lines, got %d", len(lines))
	}
	// Last 5 lines should be lines 16-20 (echo line16 through echo line20)
	for _, line := range lines {
		found := false
		for _, n := range []string{"line16", "line17", "line18", "line19", "line20"} {
			if strings.Contains(line, n) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("unexpected line content %q — expected one of the last 5 lines", line)
		}
	}
}

func TestTailLines_ExactN(t *testing.T) {
	path := filepath.Join("testdata", "session_few_lines.jsonl")
	lines, err := tailLines(path, 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(lines) != 3 {
		t.Errorf("expected 3 lines, got %d", len(lines))
	}
}

func TestParseSessionSnippet_ExtractsReadPath(t *testing.T) {
	lines := []string{
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","name":"Read","input":{"file_path":"internal/context/builder.go"}}]}}`,
	}
	snippet := parseSessionSnippet(lines)
	if !strings.Contains(snippet, "internal/context/builder.go") {
		t.Errorf("expected file path in snippet, got: %q", snippet)
	}
}

func TestParseSessionSnippet_ExtractsBashCommand(t *testing.T) {
	lines := []string{
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","name":"Bash","input":{"command":"go test ./..."}}]}}`,
	}
	snippet := parseSessionSnippet(lines)
	if !strings.Contains(snippet, "go test ./...") {
		t.Errorf("expected bash command in snippet, got: %q", snippet)
	}
}

func TestParseSessionSnippet_ExtractsTaskID(t *testing.T) {
	lines := []string{
		`{"type":"user","message":{"role":"user","content":"working on TASK-3 now"}}`,
	}
	snippet := parseSessionSnippet(lines)
	if !strings.Contains(snippet, "TASK-3") {
		t.Errorf("expected TASK-3 in snippet, got: %q", snippet)
	}
}

func TestParseSessionSnippet_SkipsBadJSON(t *testing.T) {
	lines := []string{
		"this is invalid json",
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","name":"Bash","input":{"command":"make build"}}]}}`,
		"also bad {{{{",
	}
	// Should not panic
	snippet := parseSessionSnippet(lines)
	if !strings.Contains(snippet, "make build") {
		t.Errorf("expected 'make build' from valid line, got: %q", snippet)
	}
}

// ---- Phase B: SessionSource struct + Fetch + graceful degradation ----

func TestSessionSource_EmptyEnv(t *testing.T) {
	os.Unsetenv("CLAUDE_CODE_SESSION_ID")
	src := &SessionSource{}
	snippet, prov := src.Fetch("/some/root")
	if snippet != "" {
		t.Errorf("expected empty snippet when env not set, got: %q", snippet)
	}
	if prov != "session" {
		t.Errorf("expected provenance 'session', got: %q", prov)
	}
}

func TestSessionSource_FileNotFound(t *testing.T) {
	t.Setenv("CLAUDE_CODE_SESSION_ID", "nonexistent-session-id")
	src := &SessionSource{
		jsonlPathFn: func() string { return "/nonexistent/path/file.jsonl" },
	}
	snippet, prov := src.Fetch("/some/root")
	if snippet != "" {
		t.Errorf("expected empty snippet when file not found, got: %q", snippet)
	}
	if prov != "session" {
		t.Errorf("expected provenance 'session', got: %q", prov)
	}
}

func TestSessionSource_HappyPath(t *testing.T) {
	mixedPath, err := filepath.Abs(filepath.Join("testdata", "session_mixed.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	src := &SessionSource{
		jsonlPathFn: func() string { return mixedPath },
	}
	snippet, prov := src.Fetch("/some/root")
	if snippet == "" {
		t.Error("expected non-empty snippet from session_mixed.jsonl")
	}
	if prov != "session" {
		t.Errorf("expected provenance 'session', got: %q", prov)
	}
	// Should contain file paths from Read/Edit entries
	if !strings.Contains(snippet, "internal/context/builder.go") && !strings.Contains(snippet, "internal/gate/gate.go") {
		t.Errorf("expected file paths in snippet, got: %q", snippet)
	}
	// Should contain bash command
	if !strings.Contains(snippet, "go test ./...") {
		t.Errorf("expected bash command in snippet, got: %q", snippet)
	}
	// Should contain TASK-3
	if !strings.Contains(snippet, "TASK-3") {
		t.Errorf("expected TASK-3 in snippet, got: %q", snippet)
	}
}

func TestSessionSource_Name(t *testing.T) {
	src := &SessionSource{}
	if src.Name() != "session" {
		t.Errorf("expected Name() to return 'session', got: %q", src.Name())
	}
}

// ---- Phase C: defaultBuilder integration ----

func TestDefaultBuilder_IncludesSessionSource(t *testing.T) {
	tmpDir := t.TempDir()
	b := defaultBuilder(tmpDir, nil)
	found := false
	for _, src := range b.Sources {
		if src.Name() == "session" {
			found = true
			break
		}
	}
	if found {
		t.Error("expected defaultBuilder NOT to include a source with Name()=='session'")
	}
}

func TestBuildContext_SessionSourceIntegrated(t *testing.T) {
	tmpDir := t.TempDir()
	mixedPath, err := filepath.Abs(filepath.Join("testdata", "session_mixed.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	b := &Builder{}
	b.Register(&SessionSource{
		jsonlPathFn: func() string { return mixedPath },
	})
	result := b.Build(tmpDir)
	if _, ok := result.Provenance["session"]; !ok {
		t.Error("expected Provenance to have 'session' key")
	}
}

// ---- Phase D: jsonlPathForSession, readSessionFile, sessionIDFn ----

func TestJsonlPathForSession(t *testing.T) {
	cases := []struct {
		home, root, id string
		want           string
	}{
		{
			home: "/home/u",
			root: "/a/b",
			id:   "ID",
			want: "/home/u/.claude/projects/-a-b/ID.jsonl",
		},
		{
			home: "/home/u",
			root: "/a/b/",
			id:   "ID",
			want: "/home/u/.claude/projects/-a-b-/ID.jsonl",
		},
	}
	for _, tc := range cases {
		got := jsonlPathForSession(tc.home, tc.root, tc.id)
		if got != tc.want {
			t.Errorf("jsonlPathForSession(%q, %q, %q) = %q, want %q",
				tc.home, tc.root, tc.id, got, tc.want)
		}
	}
}

func TestReadSessionFile(t *testing.T) {
	tmpDir := t.TempDir()

	// file with "sess-123\n"
	p1 := filepath.Join(tmpDir, "session1")
	if err := os.WriteFile(p1, []byte("sess-123\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// empty file
	p2 := filepath.Join(tmpDir, "session2")
	if err := os.WriteFile(p2, []byte(""), 0644); err != nil {
		t.Fatal(err)
	}

	// file with "  x  \n"
	p3 := filepath.Join(tmpDir, "session3")
	if err := os.WriteFile(p3, []byte("  x  \n"), 0644); err != nil {
		t.Fatal(err)
	}

	// non-existent path
	p4 := filepath.Join(tmpDir, "nonexistent")

	cases := []struct {
		path string
		want string
	}{
		{p1, "sess-123"},
		{p2, ""},
		{p4, ""},
		{p3, "x"},
	}
	for _, tc := range cases {
		got := readSessionFile(tc.path)
		if got != tc.want {
			t.Errorf("readSessionFile(%q) = %q, want %q", tc.path, got, tc.want)
		}
	}
}

func TestSessionSource_FileTakesPrecedenceOverEnv(t *testing.T) {
	t.Setenv("CLAUDE_CODE_SESSION_ID", "env-id")

	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	// Create fixture for "file-id" session
	const root = "/home/yale/work/voci"
	hash := strings.ReplaceAll(root, "/", "-")
	dir := filepath.Join(tmpHome, ".claude", "projects", hash)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	jsonlPath := filepath.Join(dir, "file-id.jsonl")
	content := `{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","name":"Read","input":{"file_path":"internal/context/builder.go"}}]}}` + "\n"
	if err := os.WriteFile(jsonlPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	src := &SessionSource{
		sessionIDFn: func() string { return "file-id" },
	}
	snippet, prov := src.Fetch(root)
	if snippet == "" {
		t.Error("expected non-empty snippet from file-id session")
	}
	if prov != "session" {
		t.Errorf("expected provenance 'session', got: %q", prov)
	}
	if !strings.Contains(snippet, "internal/context/builder.go") {
		t.Errorf("expected file path in snippet from file-id, got: %q", snippet)
	}
}

func TestSessionSource_FallsBackToEnvWhenFileEmpty(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("CLAUDE_CODE_SESSION_ID", "env-id")

	// Create fixture for "env-id" session
	const root = "/home/yale/work/voci"
	hash := strings.ReplaceAll(root, "/", "-")
	dir := filepath.Join(tmpHome, ".claude", "projects", hash)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	jsonlPath := filepath.Join(dir, "env-id.jsonl")
	content := `{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","name":"Bash","input":{"command":"go test ./..."}}]}}` + "\n"
	if err := os.WriteFile(jsonlPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	// sessionIDFn returns "" (empty file simulation)
	src := &SessionSource{
		sessionIDFn: func() string { return "" },
	}
	snippet, prov := src.Fetch(root)
	if snippet == "" {
		t.Error("expected non-empty snippet from env-id fallback")
	}
	if prov != "session" {
		t.Errorf("expected provenance 'session', got: %q", prov)
	}
}

// TestSessionSource_EnvLocatesJSONL proves the voci serve Monitor-subprocess scenario:
// voci serve inherits CLAUDE_CODE_SESSION_ID from the session environment, no ~/.voci/session
// file is written, yet SessionSource can locate the JSONL and return a non-empty snippet.
func TestSessionSource_EnvLocatesJSONL(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("CLAUDE_CODE_SESSION_ID", "serve-env-sess")

	// No ~/.voci/session file created (serves subprocess inherits env, no file handoff).
	const root = "/home/yale/work/voci"
	hash := strings.ReplaceAll(root, "/", "-")
	dir := filepath.Join(tmpHome, ".claude", "projects", hash)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	jsonlPath := filepath.Join(dir, "serve-env-sess.jsonl")
	content := `{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","name":"Read","input":{"file_path":"internal/daemon/server.go"}}]}}` + "\n"
	if err := os.WriteFile(jsonlPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	// Default SessionSource: no sessionIDFn, no jsonlPathFn.
	// readSessionFile(tmpHome/.voci/session) → "" (file absent) → falls back to env.
	src := &SessionSource{}
	snippet, prov := src.Fetch(root)
	if snippet == "" {
		t.Error("expected non-empty snippet: voci serve Monitor subprocess inherits CLAUDE_CODE_SESSION_ID and must locate JSONL without file handoff")
	}
	if prov != "session" {
		t.Errorf("expected provenance 'session', got: %q", prov)
	}
	if !strings.Contains(snippet, "internal/daemon/server.go") {
		t.Errorf("expected file path in snippet, got: %q", snippet)
	}
}

// ---- TASK-23 Phase A: ran denoise ----

func TestParseSessionSnippet_RanFirstLineOnly(t *testing.T) {
	heredoc := "cat <<'EOF'\nbody line\nmore body\nEOF"
	lines := []string{
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","name":"Bash","input":{"command":"` + "cat <<'EOF'\\nbody line\\nmore body\\nEOF" + `"}}]}}`,
	}
	snippet := parseSessionSnippet(lines)
	if !strings.Contains(snippet, "cat <<'EOF'") {
		t.Errorf("expected first line of command in snippet, got: %q", snippet)
	}
	if strings.Contains(snippet, "body line") {
		t.Errorf("expected body lines to be stripped from snippet, got: %q", snippet)
	}
	if strings.Contains(snippet, "more body") {
		t.Errorf("expected body lines to be stripped from snippet, got: %q", snippet)
	}
	_ = heredoc
}

// ---- TASK-23 Phase B: prose extraction ----

func TestParseSessionSnippet_ExtractsRecentProse(t *testing.T) {
	lines := []string{
		`{"type":"user","message":{"role":"user","content":"Web 服务器 在 8080 端口"}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"先看 internal/daemon"}]}}`,
	}
	snippet := parseSessionSnippet(lines)
	if !strings.Contains(snippet, "## Recent Dialogue") {
		t.Errorf("expected ## Recent Dialogue heading, got: %q", snippet)
	}
	for _, want := range []string{"Web", "8080", "端口", "internal/daemon"} {
		if !strings.Contains(snippet, want) {
			t.Errorf("expected %q in snippet, got: %q", want, snippet)
		}
	}
}

func TestParseSessionSnippet_ProseCapped(t *testing.T) {
	// Feed 8 user turns, each >600 chars; only last 6 kept, each capped at 500 chars,
	// so total Recent Dialogue block must be under 3000 chars (maxProseCharsTotal), and oldest turns absent.
	longText := strings.Repeat("X", 650)
	var lines []string
	for i := 0; i < 8; i++ {
		marker := ""
		if i == 0 {
			marker = "OLDEST_UNIQUE_MARKER"
		}
		text := marker + longText
		// Escape for JSON
		line := `{"type":"user","message":{"role":"user","content":"` + text + `"}}`
		lines = append(lines, line)
	}
	// Add a recent unique marker in the last turn
	recentMarker := "RECENT_UNIQUE_MARKER_XYZ"
	lines = append(lines, `{"type":"user","message":{"role":"user","content":"`+recentMarker+`"}}`)

	snippet := parseSessionSnippet(lines)

	// The ## Recent Dialogue block must exist
	if !strings.Contains(snippet, "## Recent Dialogue") {
		t.Errorf("expected ## Recent Dialogue heading, got: %q", snippet)
	}

	// Extract the block
	const heading = "## Recent Dialogue"
	idx := strings.Index(snippet, heading)
	if idx < 0 {
		t.Fatalf("## Recent Dialogue not found in snippet: %q", snippet)
	}
	block := snippet[idx:]
	if next := strings.Index(block[len(heading):], "\n## "); next >= 0 {
		block = block[:len(heading)+next]
	}
	if len(block) > 3000 {
		t.Errorf("## Recent Dialogue block too large: %d chars (cap 3000)", len(block))
	}
	// Oldest turn absent
	if strings.Contains(block, "OLDEST_UNIQUE_MARKER") {
		t.Errorf("oldest turn should be excluded from capped prose block")
	}
	// Recent turn present
	if !strings.Contains(block, recentMarker) {
		t.Errorf("most recent turn should be in prose block, got: %q", block)
	}
}

func TestSessionSource_EmptyEverywhere_Degrades(t *testing.T) {
	os.Unsetenv("CLAUDE_CODE_SESSION_ID")
	src := &SessionSource{
		sessionIDFn: func() string { return "" },
	}
	snippet, prov := src.Fetch("/some/root")
	if snippet != "" {
		t.Errorf("expected empty snippet when everything empty, got: %q", snippet)
	}
	if prov != "session" {
		t.Errorf("expected provenance 'session', got: %q", prov)
	}
}

// ---- TASK-70: filter system-injected turns ----

func TestParseSessionSnippet_SkipsPromptSourceSystem(t *testing.T) {
	lines := []string{
		`{"type":"user","promptSource":"system","message":{"role":"user","content":"should not appear TASK-42"}}`,
	}
	snippet := parseSessionSnippet(lines)
	if strings.Contains(snippet, "should not appear") {
		t.Errorf("expected snippet to skip promptSource:system entry, but got: %q", snippet)
	}
	if strings.Contains(snippet, "TASK-42") {
		t.Errorf("expected TASK-42 NOT in snippet from promptSource:system entry, got: %q", snippet)
	}
}

func TestParseSessionSnippet_SkipsIsCompactSummary(t *testing.T) {
	lines := []string{
		`{"type":"user","isCompactSummary":true,"message":{"role":"user","content":"compact summary text"}}`,
	}
	snippet := parseSessionSnippet(lines)
	if strings.Contains(snippet, "compact summary text") {
		t.Errorf("expected snippet to skip isCompactSummary entry, but got: %q", snippet)
	}
}

func TestParseSessionSnippet_SkipsTaskNotificationPrefix(t *testing.T) {
	lines := []string{
		`{"type":"user","message":{"role":"user","content":"<task-notification\n<task>TASK-55</task>\nnotification body text</task-notification>"}}`,
	}
	snippet := parseSessionSnippet(lines)
	if strings.Contains(snippet, "notification body text") {
		t.Errorf("expected snippet to skip task-notification content, but got: %q", snippet)
	}
	if strings.Contains(snippet, "TASK-55") {
		t.Errorf("expected TASK-55 NOT in mentioned section from task-notification, got: %q", snippet)
	}
}

func TestParseSessionSnippet_SkipsSystemReminderPrefix(t *testing.T) {
	lines := []string{
		`{"type":"user","message":{"role":"user","content":"<system-reminder>\nreminder schema body\n</system-reminder>"}}`,
	}
	snippet := parseSessionSnippet(lines)
	if strings.Contains(snippet, "reminder schema body") {
		t.Errorf("expected snippet to skip system-reminder content, but got: %q", snippet)
	}
}

func TestParseSessionSnippet_MixedRealAndSystemTurns(t *testing.T) {
	path, err := filepath.Abs(filepath.Join("testdata", "session_with_system_turns.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	snippet := parseSessionSnippet(lines)

	if !strings.Contains(snippet, "real user message") {
		t.Errorf("expected 'real user message' in snippet, got: %q", snippet)
	}
	if !strings.Contains(snippet, "go build ./...") {
		t.Errorf("expected 'go build ./...' in snippet, got: %q", snippet)
	}
	if strings.Contains(snippet, "task-notification body") {
		t.Errorf("expected 'task-notification body' NOT in snippet, got: %q", snippet)
	}
	if strings.Contains(snippet, "system-reminder body") {
		t.Errorf("expected 'system-reminder body' NOT in snippet, got: %q", snippet)
	}
	if strings.Contains(snippet, "compact body") {
		t.Errorf("expected 'compact body' NOT in snippet, got: %q", snippet)
	}
	if strings.Contains(snippet, "TASK-99") {
		t.Errorf("expected TASK-99 NOT in snippet, got: %q", snippet)
	}
}

func TestParseSessionSnippet_SystemTurnTaskIDNotExtracted(t *testing.T) {
	lines := []string{
		`{"type":"user","promptSource":"system","message":{"role":"user","content":"working on TASK-77"}}`,
	}
	snippet := parseSessionSnippet(lines)
	if strings.Contains(snippet, "TASK-77") {
		t.Errorf("expected TASK-77 NOT in snippet from promptSource:system entry, got: %q", snippet)
	}
}
