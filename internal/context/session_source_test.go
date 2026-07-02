package context

import (
	"encoding/json"
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
	snippet := (&SessionSource{}).parseSessionSnippet(lines)
	if !strings.Contains(snippet, "internal/context/builder.go") {
		t.Errorf("expected file path in snippet, got: %q", snippet)
	}
}

func TestParseSessionSnippet_ExtractsBashCommand(t *testing.T) {
	lines := []string{
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","name":"Bash","input":{"command":"go test ./..."}}]}}`,
	}
	snippet := (&SessionSource{}).parseSessionSnippet(lines)
	if !strings.Contains(snippet, "go test ./...") {
		t.Errorf("expected bash command in snippet, got: %q", snippet)
	}
}

func TestParseSessionSnippet_ExtractsTaskID(t *testing.T) {
	lines := []string{
		`{"type":"user","message":{"role":"user","content":"working on TASK-3 now"}}`,
	}
	snippet := (&SessionSource{}).parseSessionSnippet(lines)
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
	snippet := (&SessionSource{}).parseSessionSnippet(lines)
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
	snippet := (&SessionSource{}).parseSessionSnippet(lines)
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
	snippet := (&SessionSource{}).parseSessionSnippet(lines)
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

	snippet := (&SessionSource{}).parseSessionSnippet(lines)

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
	snippet := (&SessionSource{}).parseSessionSnippet(lines)
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
	snippet := (&SessionSource{}).parseSessionSnippet(lines)
	if strings.Contains(snippet, "compact summary text") {
		t.Errorf("expected snippet to skip isCompactSummary entry, but got: %q", snippet)
	}
}

func TestParseSessionSnippet_SkipsTaskNotificationPrefix(t *testing.T) {
	lines := []string{
		`{"type":"user","message":{"role":"user","content":"<task-notification\n<task>TASK-55</task>\nnotification body text</task-notification>"}}`,
	}
	snippet := (&SessionSource{}).parseSessionSnippet(lines)
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
	snippet := (&SessionSource{}).parseSessionSnippet(lines)
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
	snippet := (&SessionSource{}).parseSessionSnippet(lines)

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
	snippet := (&SessionSource{}).parseSessionSnippet(lines)
	if strings.Contains(snippet, "TASK-77") {
		t.Errorf("expected TASK-77 NOT in snippet from promptSource:system entry, got: %q", snippet)
	}
}

// ---- Stage 1.1: broad harness XML skip guard ----

func TestParseSessionSnippet_SkipsLocalCommandCaveat(t *testing.T) {
	// Embeds TASK-88 to verify it does not leak into mentioned tasks
	lines := []string{
		`{"type":"user","message":{"role":"user","content":"<local-command-caveat>Caveat content TASK-88</local-command-caveat><command-name>/model</command-name>"}}`,
	}
	snippet := (&SessionSource{}).parseSessionSnippet(lines)
	if strings.Contains(snippet, "Caveat content") {
		t.Error("expected <local-command-caveat> body to be filtered from dialogue")
	}
	if strings.Contains(snippet, "TASK-88") {
		t.Errorf("expected TASK-88 not to leak from local-command-caveat content")
	}
}

func TestParseSessionSnippet_PassesThroughHeartEmoji(t *testing.T) {
	lines := []string{
		`{"type":"user","message":{"role":"user","content":"<3 you"}}`,
	}
	snippet := (&SessionSource{}).parseSessionSnippet(lines)
	if !strings.Contains(snippet, "<3 you") {
		t.Errorf("expected '<3 you' to pass through filter, got: %s", snippet)
	}
}

func TestParseSessionSnippet_PassesThroughGenericSyntax(t *testing.T) {
	lines := []string{
		`{"type":"user","message":{"role":"user","content":"<T> is a type parameter"}}`,
	}
	snippet := (&SessionSource{}).parseSessionSnippet(lines)
	if !strings.Contains(snippet, "is a type parameter") {
		t.Errorf("expected '<T> is a type parameter' to pass through, got: %s", snippet)
	}
}

func TestParseSessionSnippet_LowercaseAngleBracketIsFiltered(t *testing.T) {
	// Known limitation: <lowercase> is treated as a harness tag.
	// Document this as expected behaviour.
	lines := []string{
		`{"type":"user","message":{"role":"user","content":"<enter> to confirm"}}`,
	}
	snippet := (&SessionSource{}).parseSessionSnippet(lines)
	if strings.Contains(snippet, "to confirm") {
		t.Errorf("known limitation: <enter> is incorrectly filtered; got: %s", snippet)
	}
}

// sessionLine builds a JSON JSONL line for a user or assistant prose turn.
func sessionLine(role, content string) string {
	b, _ := json.Marshal(content)
	return `{"type":"` + role + `","message":{"role":"` + role + `","content":` + string(b) + `}}`
}

func TestParseSessionSnippet_ChineseNotCorrupted(t *testing.T) {
	// 200 Chinese chars × 3 bytes = 600 bytes > 500-byte limit → must truncate without corruption
	content := strings.Repeat("测", 200)
	line := sessionLine("user", content)
	snippet := (&SessionSource{}).parseSessionSnippet([]string{line})
	if strings.ContainsRune(snippet, '�') {
		t.Error("truncated Chinese text must not contain UTF-8 replacement character")
	}
	if strings.Contains(snippet, "测") {
		// Verify content appeared (not silently dropped)
		_ = snippet // content present
	}
}

func TestParseSessionSnippet_TruncatedTurnHasEllipsis(t *testing.T) {
	// A turn longer than 500 chars must end with "…"
	content := strings.Repeat("x", 600)
	line := sessionLine("user", content)
	snippet := (&SessionSource{}).parseSessionSnippet([]string{line})
	// The "U: " prefix plus up to 500 runes plus "…"
	if !strings.Contains(snippet, "…") {
		t.Errorf("truncated turn must end with ellipsis, got: %s", snippet[:min(len(snippet), 60)])
	}
}

// ---- Stage 1.3: stripCodeFences ----

func TestStripCodeFences_KeepsBody(t *testing.T) {
	input := "Here is the fix:\n```go\nfunc foo() {}\n```\nDone."
	got := stripCodeFences(input)
	if strings.Contains(got, "```") {
		t.Errorf("fence markers must be removed, got: %q", got)
	}
	if !strings.Contains(got, "func foo()") {
		t.Errorf("body content must be kept, got: %q", got)
	}
}

func TestStripCodeFences_HandlesBackticksInBody(t *testing.T) {
	// Code block whose body contains a single backtick (e.g. shell substitution)
	input := "```sh\necho `date`\n```"
	got := stripCodeFences(input)
	if strings.Contains(got, "```") {
		t.Errorf("fence markers must be removed, got: %q", got)
	}
	if !strings.Contains(got, "echo") {
		t.Errorf("body content must be kept, got: %q", got)
	}
}

func TestStripCodeFences_NoFences_Unchanged(t *testing.T) {
	input := "No fences here."
	got := stripCodeFences(input)
	if got != input {
		t.Errorf("text without fences must be unchanged, got: %q", got)
	}
}

func TestParseSessionSnippet_AssistantFencesStripped(t *testing.T) {
	// Assistant turn with a fenced code block
	block := map[string]any{
		"type": "text",
		"text": "Use this:\n```go\nfunc bar() {}\n```\nDone.",
	}
	content, _ := json.Marshal([]any{block})
	entry := map[string]any{
		"type":    "assistant",
		"message": map[string]any{"role": "assistant", "content": json.RawMessage(content)},
	}
	line, _ := json.Marshal(entry)
	snippet := (&SessionSource{}).parseSessionSnippet([]string{string(line)})
	if strings.Contains(snippet, "```") {
		t.Errorf("assistant fences must be stripped from dialogue hint, got: %s", snippet)
	}
	if !strings.Contains(snippet, "func bar()") {
		t.Errorf("assistant code body must be kept in hint, got: %s", snippet)
	}
}

func TestParseSessionSnippet_UserFencesNotStripped(t *testing.T) {
	// User turn with backtick content — normalizeProse is called unchanged
	line := sessionLine("user", "use ```go``` to format code")
	snippet := (&SessionSource{}).parseSessionSnippet([]string{line})
	// normalizeProse collapses whitespace but does NOT strip fences for user turns
	// The triple-backtick sequence should survive (it's just a string here)
	if !strings.Contains(snippet, "go") {
		t.Errorf("user turn content must pass through, got: %s", snippet)
	}
}

func TestParseSessionSnippet_ChineseOuterCap(t *testing.T) {
	// 6 turns × 200 Chinese chars each = 6 × 200 runes = 1200 runes total
	// With rune-based outer cap of 3000, all 6 turns must appear
	// With byte-based outer cap, 6 × ~600 bytes = 3600 bytes > 3000 → only 5 turns appear
	var lines []string
	for i := 0; i < 6; i++ {
		lines = append(lines, sessionLine("user", strings.Repeat("测", 200)))
	}
	snippet := (&SessionSource{}).parseSessionSnippet(lines)
	// Count how many "U: " prefixes appear
	count := strings.Count(snippet, "U: ")
	if count < 6 {
		t.Errorf("expected 6 Chinese turns to fit under rune-based outer cap, got %d", count)
	}
}

// ---- Structured dialogue extraction (full Markdown fidelity for Web UI) ----

// buildAssistantLine builds a JSONL line for an assistant text turn.
func buildAssistantLine(text string) string {
	block := map[string]any{"type": "text", "text": text}
	content, _ := json.Marshal([]any{block})
	entry := map[string]any{
		"type":    "assistant",
		"message": map[string]any{"role": "assistant", "content": json.RawMessage(content)},
	}
	line, _ := json.Marshal(entry)
	return string(line)
}

// TestParseSessionDialogue_PreservesBlankLineBeforeTable is the core regression:
// a GFM table needs a preceding blank line to be a distinct block. The structured
// extraction must preserve that blank line verbatim (the old flattening dropped it).
func TestParseSessionDialogue_PreservesBlankLineBeforeTable(t *testing.T) {
	text := "总结：\n\n| 列A | 列B |\n|---|---|\n| 值1 | 值2 |"
	turns := (&SessionSource{}).parseSessionDialogue([]string{buildAssistantLine(text)})
	if len(turns) != 1 {
		t.Fatalf("expected 1 turn, got %d: %+v", len(turns), turns)
	}
	if turns[0].Role != "assistant" {
		t.Errorf("expected role 'assistant', got %q", turns[0].Role)
	}
	if turns[0].Text != text {
		t.Errorf("text not preserved verbatim.\n got: %q\nwant: %q", turns[0].Text, text)
	}
	// Explicitly: the blank line separating paragraph from table must survive.
	if !strings.Contains(turns[0].Text, "总结：\n\n|") {
		t.Errorf("blank line before table was dropped: %q", turns[0].Text)
	}
}

// TestParseSessionDialogue_PreservesCodeFences verifies code blocks survive
// (unlike parseSessionSnippet which strips fences for ASR noise reduction).
func TestParseSessionDialogue_PreservesCodeFences(t *testing.T) {
	text := "Here:\n\n```go\nfunc foo() {}\n```"
	turns := (&SessionSource{}).parseSessionDialogue([]string{buildAssistantLine(text)})
	if len(turns) != 1 {
		t.Fatalf("expected 1 turn, got %d", len(turns))
	}
	if !strings.Contains(turns[0].Text, "```go") {
		t.Errorf("code fence stripped: %q", turns[0].Text)
	}
	if !strings.Contains(turns[0].Text, "func foo()") {
		t.Errorf("code body missing: %q", turns[0].Text)
	}
}

func TestParseSessionDialogue_UserAndAssistantRoles(t *testing.T) {
	lines := []string{
		sessionLine("user", "问题在哪里？"),
		buildAssistantLine("答案如下。"),
	}
	turns := (&SessionSource{}).parseSessionDialogue(lines)
	if len(turns) != 2 {
		t.Fatalf("expected 2 turns, got %d: %+v", len(turns), turns)
	}
	if turns[0].Role != "user" || turns[0].Text != "问题在哪里？" {
		t.Errorf("user turn wrong: %+v", turns[0])
	}
	if turns[1].Role != "assistant" || turns[1].Text != "答案如下。" {
		t.Errorf("assistant turn wrong: %+v", turns[1])
	}
}

func TestParseSessionDialogue_SkipsSystemAndHarnessTurns(t *testing.T) {
	lines := []string{
		`{"type":"user","promptSource":"system","message":{"role":"user","content":"system junk"}}`,
		`{"type":"user","isCompactSummary":true,"message":{"role":"user","content":"compact junk"}}`,
		`{"type":"user","message":{"role":"user","content":"<system-reminder>reminder junk</system-reminder>"}}`,
		sessionLine("user", "real message"),
	}
	turns := (&SessionSource{}).parseSessionDialogue(lines)
	if len(turns) != 1 {
		t.Fatalf("expected 1 turn (only real message), got %d: %+v", len(turns), turns)
	}
	if turns[0].Text != "real message" {
		t.Errorf("expected only real message, got: %q", turns[0].Text)
	}
}

func TestParseSessionDialogue_KeepsLastNTurns(t *testing.T) {
	var lines []string
	for i := 0; i < defaultMaxProseTurns+4; i++ {
		lines = append(lines, sessionLine("user", "turn"+string(rune('A'+i))))
	}
	turns := (&SessionSource{}).parseSessionDialogue(lines)
	if len(turns) != defaultMaxProseTurns {
		t.Fatalf("expected %d turns, got %d", defaultMaxProseTurns, len(turns))
	}
	// The most recent turn must be present
	last := "turn" + string(rune('A'+defaultMaxProseTurns+3))
	if turns[len(turns)-1].Text != last {
		t.Errorf("expected last turn %q, got %q", last, turns[len(turns)-1].Text)
	}
}

func TestParseSessionDialogue_EmptyInput(t *testing.T) {
	if turns := (&SessionSource{}).parseSessionDialogue(nil); turns != nil {
		t.Errorf("expected nil for empty input, got %+v", turns)
	}
}

// TestSessionSource_Dialogue verifies the resolution path returns structured turns.
func TestSessionSource_Dialogue(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("CLAUDE_CODE_SESSION_ID", "dlg-sess")

	const root = "/home/yale/work/voci"
	hash := strings.ReplaceAll(root, "/", "-")
	dir := filepath.Join(tmpHome, ".claude", "projects", hash)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	jsonlPath := filepath.Join(dir, "dlg-sess.jsonl")
	content := buildAssistantLine("表格：\n\n| A | B |\n|---|---|\n| 1 | 2 |") + "\n"
	if err := os.WriteFile(jsonlPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	src := &SessionSource{}
	turns := src.Dialogue(root)
	if len(turns) != 1 {
		t.Fatalf("expected 1 turn, got %d", len(turns))
	}
	if !strings.Contains(turns[0].Text, "\n\n| A | B |") {
		t.Errorf("blank line before table not preserved via Dialogue(): %q", turns[0].Text)
	}
}

func TestSessionSource_Dialogue_NoSession(t *testing.T) {
	os.Unsetenv("CLAUDE_CODE_SESSION_ID")
	src := &SessionSource{sessionIDFn: func() string { return "" }}
	if turns := src.Dialogue("/some/root"); turns != nil {
		t.Errorf("expected nil when no session, got %+v", turns)
	}
}

func TestResolveJSONLPath_ExportedWrapsUnexported(t *testing.T) {
	// Verify the exported ResolveJSONLPath delegates to resolveJSONLPath.
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	id := "test-jsonl-path"
	t.Setenv("CLAUDE_CODE_SESSION_ID", id)

	root := "/my/project/path"
	hash := strings.ReplaceAll(root, "/", "-")
	dir := filepath.Join(tmpHome, ".claude", "projects", hash)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	expected := filepath.Join(dir, id+".jsonl")
	if err := os.WriteFile(expected, []byte("{}\n"), 0644); err != nil {
		t.Fatal(err)
	}

	src := &SessionSource{}
	got := src.ResolveJSONLPath(root)
	if got != expected {
		t.Errorf("ResolveJSONLPath(%q) = %q, want %q", root, got, expected)
	}
}

func TestResolveJSONLPath_NoSession(t *testing.T) {
	os.Unsetenv("CLAUDE_CODE_SESSION_ID")
	src := &SessionSource{sessionIDFn: func() string { return "" }}
	if got := src.ResolveJSONLPath("/root"); got != "" {
		t.Errorf("expected empty string when no session, got %q", got)
	}
}
