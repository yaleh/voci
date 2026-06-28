package executor_test

import (
	"strings"
	"testing"

	"github.com/yalehu/voci/internal/executor"
	"github.com/yalehu/voci/internal/intent"
)

// --- Phase A ---

func TestExecuteDirectPromptReturnsRewritten(t *testing.T) {
	e := executor.NewDefaultExecutor(nil, false)
	proposal := intent.ActionProposal{
		Kind:      intent.KindDirectPrompt,
		Rewritten: "Add a comment to main.go explaining the entry point",
	}
	result, err := e.Execute(proposal)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != proposal.Rewritten {
		t.Errorf("expected %q, got %q", proposal.Rewritten, result)
	}
}

func TestExecuteAmbiguousReturnsError(t *testing.T) {
	e := executor.NewDefaultExecutor(nil, false)
	proposal := intent.ActionProposal{
		Kind:      intent.KindAmbiguous,
		Rewritten: "maybe do something",
	}
	result, err := e.Execute(proposal)
	if err == nil {
		t.Error("expected non-nil error for ambiguous kind, got nil")
	}
	if result != "" {
		t.Errorf("expected empty result for ambiguous kind, got %q", result)
	}
}

// --- Phase B ---

func TestExecuteBacklogActionDryRunPrinted(t *testing.T) {
	called := false
	runner := func(name string, args ...string) (string, error) {
		called = true
		return "should not be called", nil
	}
	e := executor.NewDefaultExecutor(runner, false) // confirmed=false
	proposal := intent.ActionProposal{
		Kind:      intent.KindBacklogAction,
		Rewritten: "backlog task edit TASK-1 --status Done",
	}
	result, err := e.Execute(proposal)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if called {
		t.Error("cmdRunner should NOT be called in dry-run mode")
	}
	if !strings.Contains(result, "[DRY-RUN]") {
		t.Errorf("expected result to contain [DRY-RUN], got %q", result)
	}
}

func TestExecuteBacklogActionConfirmedRuns(t *testing.T) {
	called := false
	runner := func(name string, args ...string) (string, error) {
		called = true
		return "ok\n", nil
	}
	e := executor.NewDefaultExecutor(runner, true) // confirmed=true
	proposal := intent.ActionProposal{
		Kind:      intent.KindBacklogAction,
		Rewritten: "backlog task edit TASK-1 --status Done",
	}
	result, err := e.Execute(proposal)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Error("cmdRunner should be called when confirmed=true")
	}
	if result != "ok\n" {
		t.Errorf("expected cmdRunner output, got %q", result)
	}
}

func TestExecuteBacklogActionParseCommand(t *testing.T) {
	var capturedName string
	var capturedArgs []string
	runner := func(name string, args ...string) (string, error) {
		capturedName = name
		capturedArgs = args
		return "", nil
	}
	e := executor.NewDefaultExecutor(runner, true)
	proposal := intent.ActionProposal{
		Kind:      intent.KindBacklogAction,
		Rewritten: "backlog task edit TASK-1 --status Done",
	}
	_, err := e.Execute(proposal)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedName != "backlog" {
		t.Errorf("expected command name 'backlog', got %q", capturedName)
	}
	expectedArgs := []string{"task", "edit", "TASK-1", "--status", "Done"}
	if len(capturedArgs) != len(expectedArgs) {
		t.Fatalf("expected args %v, got %v", expectedArgs, capturedArgs)
	}
	for i, a := range expectedArgs {
		if capturedArgs[i] != a {
			t.Errorf("arg[%d]: expected %q, got %q", i, a, capturedArgs[i])
		}
	}
}

// --- Phase C ---

func TestExecuteQueryRunsBacklogList(t *testing.T) {
	runner := func(name string, args ...string) (string, error) {
		return "TASK-1: Fix login\n", nil
	}
	e := executor.NewDefaultExecutor(runner, false)
	proposal := intent.ActionProposal{
		Kind:      intent.KindQuery,
		Rewritten: "backlog task list",
	}
	result, err := e.Execute(proposal)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "TASK-1: Fix login\n" {
		t.Errorf("expected list output, got %q", result)
	}
}

func TestExecuteQueryNoWriteSideEffects(t *testing.T) {
	mutatingCmds := map[string]bool{
		"edit":   true,
		"create": true,
		"delete": true,
		"add":    true,
		"remove": true,
	}
	runner := func(name string, args ...string) (string, error) {
		if len(args) > 0 {
			// first sub-arg after "task" would be the action
			for _, a := range args {
				if mutatingCmds[a] {
					t.Errorf("query executor called mutating command arg %q", a)
				}
			}
		}
		return "TASK-1: Fix login\n", nil
	}
	e := executor.NewDefaultExecutor(runner, false)
	proposal := intent.ActionProposal{
		Kind:      intent.KindQuery,
		Rewritten: "backlog task list",
	}
	_, err := e.Execute(proposal)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
