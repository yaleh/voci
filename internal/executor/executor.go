// Package executor dispatches execution based on ActionProposal.Kind.
package executor

import (
	"fmt"
	"strings"

	"github.com/yaleh/voci/internal/intent"
)

// CmdRunner is a function that runs an external command and returns its combined output.
type CmdRunner func(name string, args ...string) (string, error)

// Executor defines the interface for executing an ActionProposal.
type Executor interface {
	Execute(proposal intent.ActionProposal) (string, error)
}

// DefaultExecutor implements Executor with an injectable CmdRunner for testability.
type DefaultExecutor struct {
	// CmdRunner is called to invoke external commands (e.g. the backlog CLI).
	// If nil, any path that requires a runner will return an error.
	CmdRunner CmdRunner
	// Confirmed indicates that the human gate has been passed and side-effecting
	// commands may be executed.
	Confirmed bool
}

// NewDefaultExecutor creates a DefaultExecutor with the given runner and confirmation state.
func NewDefaultExecutor(runner CmdRunner, confirmed bool) *DefaultExecutor {
	return &DefaultExecutor{CmdRunner: runner, Confirmed: confirmed}
}

// Execute dispatches execution based on the proposal's Kind.
func (e *DefaultExecutor) Execute(proposal intent.ActionProposal) (string, error) {
	switch proposal.Kind {
	case intent.KindDirectPrompt:
		// Passthrough: return rewritten text as-is.
		return proposal.Rewritten, nil

	case intent.KindAmbiguous:
		return "", fmt.Errorf("ambiguous intent: cannot execute without clarification")

	case intent.KindBacklogAction:
		return e.executeBacklogAction(proposal)

	case intent.KindQuery:
		return e.executeQuery(proposal)

	default:
		return "", fmt.Errorf("unknown intent kind: %q", proposal.Kind)
	}
}

// executeBacklogAction handles backlog_action kind.
// In dry-run mode (Confirmed==false) it returns a [DRY-RUN] description.
// When confirmed it invokes CmdRunner with the parsed backlog CLI arguments.
func (e *DefaultExecutor) executeBacklogAction(proposal intent.ActionProposal) (string, error) {
	args := parseBacklogArgs(proposal.Rewritten)

	if !e.Confirmed {
		return "[DRY-RUN] backlog " + strings.Join(args, " "), nil
	}

	if e.CmdRunner == nil {
		return "", fmt.Errorf("executor: no CmdRunner configured")
	}
	return e.CmdRunner("backlog", args...)
}

// executeQuery handles query kind — always read-only, no gate required.
func (e *DefaultExecutor) executeQuery(proposal intent.ActionProposal) (string, error) {
	args := parseBacklogArgs(proposal.Rewritten)

	if e.CmdRunner == nil {
		return "", fmt.Errorf("executor: no CmdRunner configured")
	}
	return e.CmdRunner("backlog", args...)
}

// parseBacklogArgs tokenises the rewritten command and strips a leading "backlog" token
// so callers receive only the sub-command arguments.
func parseBacklogArgs(rewritten string) []string {
	tokens := strings.Fields(rewritten)
	if len(tokens) > 0 && tokens[0] == "backlog" {
		tokens = tokens[1:]
	}
	return tokens
}
