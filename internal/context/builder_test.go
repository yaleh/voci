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
