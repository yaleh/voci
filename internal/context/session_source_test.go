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
