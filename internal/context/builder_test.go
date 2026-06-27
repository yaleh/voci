package context

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
