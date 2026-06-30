// Package gate implements the human confirmation gate for ActionProposals.
// It prints a summary to an io.Writer and reads user decisions from an io.Reader,
// supporting confirm / edit / discard actions. Ambiguous proposals force the user
// to provide clarification text before a normal action can be chosen.
package gate

import (
	"bufio"
	"fmt"
	"io"
	"strings"

	"github.com/yaleh/voci/internal/intent/model"
)

// GateResult holds the outcome of the human confirmation gate.
type GateResult struct {
	// Action is one of: "confirm", "edit", "discard".
	Action string
	// EditedText is the corrected text supplied by the user when Action=="edit".
	EditedText string
	// ClarifiedText is the clarification supplied by the user for ambiguous proposals.
	ClarifiedText string
}

// PrintSummary writes a formatted summary of proposal to w.
func PrintSummary(w io.Writer, proposal model.ActionProposal) {
	fmt.Fprintf(w, "--- Intent Summary ---\n")
	fmt.Fprintf(w, "Kind:       %s\n", proposal.Kind)
	fmt.Fprintf(w, "Rewritten:  %s\n", proposal.Rewritten)
	fmt.Fprintf(w, "Confidence: %.2f\n", proposal.Confidence)
	if proposal.Kind == model.KindAmbiguous {
		fmt.Fprintf(w, "Status:     AMBIGUOUS — clarification required before confirming.\n")
		fmt.Fprintf(w, "Please provide a clarification for this intent:\n")
	}
	fmt.Fprintf(w, "----------------------\n")
}

// Run drives the interactive confirmation loop.
// For ambiguous proposals it first collects a clarification line, then proceeds
// with the standard [confirm/edit/discard] loop.
func Run(r io.Reader, w io.Writer, proposal model.ActionProposal) GateResult {
	scanner := bufio.NewScanner(r)
	result := GateResult{}

	PrintSummary(w, proposal)

	// Ambiguous proposals require clarification before action selection.
	if proposal.Kind == model.KindAmbiguous {
		fmt.Fprintf(w, "> ")
		if scanner.Scan() {
			result.ClarifiedText = strings.TrimSpace(scanner.Text())
		}
		fmt.Fprintf(w, "Clarification recorded. Now choose an action.\n")
	}

	// Action selection loop.
	for {
		fmt.Fprintf(w, "Action [confirm/edit/discard]: ")
		if !scanner.Scan() {
			// EOF — default to discard
			result.Action = "discard"
			return result
		}
		choice := strings.TrimSpace(strings.ToLower(scanner.Text()))
		switch choice {
		case "confirm":
			result.Action = "confirm"
			return result
		case "edit":
			fmt.Fprintf(w, "Enter corrected text: ")
			if scanner.Scan() {
				result.EditedText = strings.TrimSpace(scanner.Text())
			}
			result.Action = "edit"
			return result
		case "discard":
			result.Action = "discard"
			return result
		default:
			fmt.Fprintf(w, "Invalid choice %q. Please enter confirm, edit, or discard.\n", choice)
		}
	}
}
