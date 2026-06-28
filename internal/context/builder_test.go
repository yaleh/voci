package context

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func makeTasksDir(t *testing.T, root string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, "backlog", "tasks"), 0755); err != nil {
		t.Fatal(err)
	}
}

func TestBuildContextReturnsString(t *testing.T) {
	tmpDir := t.TempDir()
	makeTasksDir(t, tmpDir)

	result := BuildContext(tmpDir, func(_ string) string { return "" })
	if result == "" {
		// Empty is acceptable if no files, but should not panic
		_ = result
	}
	// Just ensure it doesn't panic and returns a string
}

func TestBuildContextReadsBacklogTasks(t *testing.T) {
	tmpDir := t.TempDir()
	makeTasksDir(t, tmpDir)

	taskContent := `---
id: TASK-1
title: Fix login bug
status: 'Basic: In Progress'
---

## Description
Fix the login bug.
`
	taskFile := filepath.Join(tmpDir, "backlog", "tasks", "task-1.md")
	if err := os.WriteFile(taskFile, []byte(taskContent), 0644); err != nil {
		t.Fatal(err)
	}

	result := BuildContext(tmpDir, func(_ string) string { return "" })

	if !strings.Contains(result, "TASK-1") {
		t.Errorf("expected TASK-1 in result, got: %q", result)
	}
	if !strings.Contains(result, "Fix login bug") {
		t.Errorf("expected 'Fix login bug' in result, got: %q", result)
	}
}

func TestBuildContextReadsCLAUDEMd(t *testing.T) {
	tmpDir := t.TempDir()
	makeTasksDir(t, tmpDir)

	sentinel := "SENTINEL_TEXT_XYZ"
	claudePath := filepath.Join(tmpDir, "CLAUDE.md")
	if err := os.WriteFile(claudePath, []byte(sentinel), 0644); err != nil {
		t.Fatal(err)
	}

	result := BuildContext(tmpDir, func(_ string) string { return "" })

	if !strings.Contains(result, sentinel) {
		t.Errorf("expected sentinel in result, got: %q", result)
	}
}

func TestBuildContextReadsGitLog(t *testing.T) {
	tmpDir := t.TempDir()
	makeTasksDir(t, tmpDir)

	fakeGit := func(_ string) string {
		return "abc1234 add auth\n"
	}

	result := BuildContext(tmpDir, fakeGit)

	if !strings.Contains(result, "add auth") {
		t.Errorf("expected 'add auth' in result, got: %q", result)
	}
}

func TestBuildContextMissingCLAUDEMd(t *testing.T) {
	tmpDir := t.TempDir()
	makeTasksDir(t, tmpDir)

	// Should not error, just return a string
	result := BuildContext(tmpDir, func(_ string) string { return "" })
	_ = result
}

func TestBuildContextEmptyBacklog(t *testing.T) {
	tmpDir := t.TempDir()
	makeTasksDir(t, tmpDir)

	// Empty tasks dir - should not error
	result := BuildContext(tmpDir, func(_ string) string { return "" })
	_ = result
}

func TestBuildContextKnownEntitiesSection(t *testing.T) {
	tmpDir := t.TempDir()
	makeTasksDir(t, tmpDir)

	taskContent := "---\nid: TASK-1\ntitle: Fix login bug\nstatus: 'Basic: In Progress'\n---\n"
	taskFile := filepath.Join(tmpDir, "backlog", "tasks", "task-1.md")
	if err := os.WriteFile(taskFile, []byte(taskContent), 0644); err != nil {
		t.Fatal(err)
	}

	result := BuildContext(tmpDir, func(_ string) string { return "" })
	if !strings.Contains(result, "## Known Entities") {
		t.Errorf("expected '## Known Entities' in result, got: %q", result)
	}
}

func TestBuildContextKnownEntitiesHasTaskID(t *testing.T) {
	tmpDir := t.TempDir()
	makeTasksDir(t, tmpDir)

	taskContent := "---\nid: TASK-1\ntitle: Fix login bug\nstatus: 'Basic: In Progress'\n---\n"
	taskFile := filepath.Join(tmpDir, "backlog", "tasks", "task-1.md")
	if err := os.WriteFile(taskFile, []byte(taskContent), 0644); err != nil {
		t.Fatal(err)
	}

	result := BuildContext(tmpDir, func(_ string) string { return "" })
	if !strings.Contains(result, "task one: TASK-1") {
		t.Errorf("expected 'task one: TASK-1' in Known Entities, got: %q", result)
	}
}

func TestBuildContextKnownEntitiesHasProjectName(t *testing.T) {
	tmpDir := t.TempDir()
	makeTasksDir(t, tmpDir)

	result := BuildContext(tmpDir, func(_ string) string { return "" })
	if !strings.Contains(result, "vocal: voci") {
		t.Errorf("expected 'vocal: voci' in Known Entities, got: %q", result)
	}
}

func TestBuildContextKnownEntitiesHasPackagePaths(t *testing.T) {
	tmpDir := t.TempDir()
	makeTasksDir(t, tmpDir)

	result := BuildContext(tmpDir, func(_ string) string { return "" })
	for _, pkg := range []string{"internal/pipeline", "internal/context", "internal/asr"} {
		if !strings.Contains(result, pkg) {
			t.Errorf("expected %q in Known Entities, got: %q", pkg, result)
		}
	}
}

func TestBuildContextKnownEntitiesBeforeActiveTasks(t *testing.T) {
	tmpDir := t.TempDir()
	makeTasksDir(t, tmpDir)

	taskContent := "---\nid: TASK-1\ntitle: Fix login bug\nstatus: 'Basic: In Progress'\n---\n"
	taskFile := filepath.Join(tmpDir, "backlog", "tasks", "task-1.md")
	if err := os.WriteFile(taskFile, []byte(taskContent), 0644); err != nil {
		t.Fatal(err)
	}

	result := BuildContext(tmpDir, func(_ string) string { return "" })
	idxEntities := strings.Index(result, "## Known Entities")
	idxTasks := strings.Index(result, "## Active Tasks")
	if idxEntities < 0 {
		t.Fatal("'## Known Entities' not found")
	}
	if idxTasks < 0 {
		t.Fatal("'## Active Tasks' not found")
	}
	if idxEntities >= idxTasks {
		t.Errorf("expected '## Known Entities' before '## Active Tasks', got positions %d and %d", idxEntities, idxTasks)
	}
}

// ---- Increment 1: Source interface + plugin registration ----

func TestSourceInterfaceBacklog(t *testing.T) {
	tmpDir := t.TempDir()
	makeTasksDir(t, tmpDir)

	taskContent := "---\nid: TASK-1\ntitle: Fix login bug\nstatus: 'Basic: In Progress'\n---\n"
	taskFile := filepath.Join(tmpDir, "backlog", "tasks", "task-1.md")
	if err := os.WriteFile(taskFile, []byte(taskContent), 0644); err != nil {
		t.Fatal(err)
	}

	src := &BacklogSource{}
	snippet, prov := src.Fetch(tmpDir)
	if snippet == "" {
		t.Error("expected non-empty snippet from BacklogSource")
	}
	if prov != "backlog" {
		t.Errorf("expected provenance 'backlog', got %q", prov)
	}
}

func TestSourceInterfaceClaudeMd(t *testing.T) {
	tmpDir := t.TempDir()
	sentinel := "CLAUDE_SENTINEL_ABC"
	claudePath := filepath.Join(tmpDir, "CLAUDE.md")
	if err := os.WriteFile(claudePath, []byte(sentinel), 0644); err != nil {
		t.Fatal(err)
	}

	src := &ClaudeMdSource{}
	snippet, prov := src.Fetch(tmpDir)
	if !strings.Contains(snippet, sentinel) {
		t.Errorf("expected snippet to contain sentinel %q, got %q", sentinel, snippet)
	}
	if prov != "claude.md" {
		t.Errorf("expected provenance 'claude.md', got %q", prov)
	}
}

func TestSourceInterfaceGitLog(t *testing.T) {
	src := &GitLogSource{Runner: func() string { return "abc1234 add auth\n" }}
	snippet, prov := src.Fetch("/irrelevant")
	if !strings.Contains(snippet, "add auth") {
		t.Errorf("expected snippet to contain 'add auth', got %q", snippet)
	}
	if prov != "git" {
		t.Errorf("expected provenance 'git', got %q", prov)
	}
}

// ---- Increment 2: Provenance + Result ----

func TestBuilderResultHasAstHint(t *testing.T) {
	tmpDir := t.TempDir()
	makeTasksDir(t, tmpDir)

	b := &Builder{}
	b.Register(&KnownEntitiesSource{})
	b.Register(&BacklogSource{})
	b.Register(&ClaudeMdSource{})
	b.Register(&GitLogSource{Runner: func() string { return "abc add auth\n" }})

	result := b.Build(tmpDir)
	if result.AsrHint == "" {
		t.Error("expected non-empty AsrHint")
	}
}

func TestBuilderResultHasFullContext(t *testing.T) {
	tmpDir := t.TempDir()
	makeTasksDir(t, tmpDir)

	b := &Builder{}
	b.Register(&KnownEntitiesSource{})
	b.Register(&BacklogSource{})
	b.Register(&ClaudeMdSource{})
	b.Register(&GitLogSource{Runner: func() string { return "" }})

	result := b.Build(tmpDir)
	if !strings.Contains(result.FullContext, "## Project Context") {
		t.Errorf("expected FullContext to contain '## Project Context', got: %q", result.FullContext)
	}
}

func TestBuilderResultHasProvenance(t *testing.T) {
	tmpDir := t.TempDir()
	makeTasksDir(t, tmpDir)

	b := &Builder{}
	b.Register(&KnownEntitiesSource{})
	b.Register(&BacklogSource{})
	b.Register(&ClaudeMdSource{})
	b.Register(&GitLogSource{Runner: func() string { return "abc\n" }})

	result := b.Build(tmpDir)
	for _, key := range []string{"backlog", "claude.md", "git"} {
		if _, ok := result.Provenance[key]; !ok {
			t.Errorf("expected Provenance to have key %q", key)
		}
	}
}

// ---- Increment 4: context_cache.json ----

func TestBuildCachedWritesFile(t *testing.T) {
	tmpDir := t.TempDir()
	makeTasksDir(t, tmpDir)

	b := &Builder{}
	b.Register(&KnownEntitiesSource{})
	b.Register(&BacklogSource{})
	b.Register(&ClaudeMdSource{})
	b.Register(&GitLogSource{Runner: func() string { return "" }})

	_ = b.BuildCached(tmpDir)

	cachePath := filepath.Join(tmpDir, ".voci", "context_cache.json")
	if _, err := os.Stat(cachePath); err != nil {
		t.Errorf("expected cache file at %q, got error: %v", cachePath, err)
	}
}

func TestBuildCachedReadsCache(t *testing.T) {
	tmpDir := t.TempDir()
	makeTasksDir(t, tmpDir)

	// Write a fake cache with a sentinel AsrHint
	vociDir := filepath.Join(tmpDir, ".voci")
	if err := os.MkdirAll(vociDir, 0755); err != nil {
		t.Fatal(err)
	}
	cached := Result{
		AsrHint:    "cached_sentinel_hint",
		FullContext: "## Project Context\ncached",
		Provenance: map[string]string{"backlog": "x", "claude.md": "y", "git": "z"},
	}
	cf := struct {
		Result    Result    `json:"result"`
		CreatedAt time.Time `json:"created_at"`
	}{Result: cached, CreatedAt: time.Now()}
	data, _ := json.Marshal(cf)
	if err := os.WriteFile(filepath.Join(vociDir, "context_cache.json"), data, 0644); err != nil {
		t.Fatal(err)
	}

	b := &Builder{}
	b.Register(&BacklogSource{})
	b.Register(&ClaudeMdSource{})
	b.Register(&GitLogSource{Runner: func() string { return "" }})

	result := b.BuildCached(tmpDir)
	if result.AsrHint != "cached_sentinel_hint" {
		t.Errorf("expected cached AsrHint 'cached_sentinel_hint', got %q", result.AsrHint)
	}
}

// ---- TASK-10: filler-word spoken variants ----

func TestBuildKnownEntitiesHasFunctionExpansions(t *testing.T) {
	result := buildKnownEntities(nil)

	// Filler-word variants
	if !strings.Contains(result, "build a context: BuildContext") {
		t.Errorf("expected 'build a context: BuildContext' in output, got: %q", result)
	}
	if !strings.Contains(result, "run a hinted: RunHinted") {
		t.Errorf("expected 'run a hinted: RunHinted' in output, got: %q", result)
	}
	// Original entries preserved
	if !strings.Contains(result, "build context: BuildContext") {
		t.Errorf("expected 'build context: BuildContext' in output, got: %q", result)
	}
	if !strings.Contains(result, "run hinted: RunHinted") {
		t.Errorf("expected 'run hinted: RunHinted' in output, got: %q", result)
	}
}

// ---- Backward compat ----

func TestBuildContextBackwardCompat(t *testing.T) {
	tmpDir := t.TempDir()
	makeTasksDir(t, tmpDir)

	result := BuildContext(tmpDir, nil)
	if result == "" {
		t.Error("expected non-empty result from BuildContext with nil gitRunner")
	}
}
