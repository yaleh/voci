package gate_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/yalehu/voci/internal/gate"
	"github.com/yalehu/voci/internal/intent"
)

// Phase A tests

func TestPrintProposalSummary(t *testing.T) {
	proposal := intent.ActionProposal{
		Kind:       intent.KindDirectPrompt,
		Rewritten:  "add a login endpoint",
		Confidence: 0.92,
	}
	var buf bytes.Buffer
	gate.PrintSummary(&buf, proposal)
	out := buf.String()

	if !strings.Contains(out, string(intent.KindDirectPrompt)) {
		t.Errorf("expected output to contain kind %q, got: %s", intent.KindDirectPrompt, out)
	}
	if !strings.Contains(out, "add a login endpoint") {
		t.Errorf("expected output to contain rewritten text, got: %s", out)
	}
	if !strings.Contains(out, "0.92") {
		t.Errorf("expected output to contain confidence, got: %s", out)
	}
}

func TestPrintProposalSummaryAmbiguous(t *testing.T) {
	proposal := intent.ActionProposal{
		Kind:       intent.KindAmbiguous,
		Rewritten:  "do something",
		Confidence: 0.4,
	}
	var buf bytes.Buffer
	gate.PrintSummary(&buf, proposal)
	out := buf.String()

	if !strings.Contains(strings.ToLower(out), "ambiguous") {
		t.Errorf("expected output to contain 'ambiguous', got: %s", out)
	}
	if !strings.Contains(strings.ToLower(out), "clarif") {
		t.Errorf("expected output to contain clarification prompt, got: %s", out)
	}
}

// Phase B tests

func TestRunConfirm(t *testing.T) {
	proposal := intent.ActionProposal{
		Kind:       intent.KindDirectPrompt,
		Rewritten:  "add a login endpoint",
		Confidence: 0.9,
	}
	r := strings.NewReader("confirm\n")
	var w bytes.Buffer
	result := gate.Run(r, &w, proposal)
	if result.Action != "confirm" {
		t.Errorf("expected action 'confirm', got %q", result.Action)
	}
}

func TestRunEdit(t *testing.T) {
	proposal := intent.ActionProposal{
		Kind:       intent.KindDirectPrompt,
		Rewritten:  "add a login endpoint",
		Confidence: 0.9,
	}
	r := strings.NewReader("edit\nfixed text\n")
	var w bytes.Buffer
	result := gate.Run(r, &w, proposal)
	if result.Action != "edit" {
		t.Errorf("expected action 'edit', got %q", result.Action)
	}
	if result.EditedText != "fixed text" {
		t.Errorf("expected EditedText 'fixed text', got %q", result.EditedText)
	}
}

func TestRunDiscard(t *testing.T) {
	proposal := intent.ActionProposal{
		Kind:       intent.KindDirectPrompt,
		Rewritten:  "add a login endpoint",
		Confidence: 0.9,
	}
	r := strings.NewReader("discard\n")
	var w bytes.Buffer
	result := gate.Run(r, &w, proposal)
	if result.Action != "discard" {
		t.Errorf("expected action 'discard', got %q", result.Action)
	}
}

func TestRunInvalidThenConfirm(t *testing.T) {
	proposal := intent.ActionProposal{
		Kind:       intent.KindDirectPrompt,
		Rewritten:  "add a login endpoint",
		Confidence: 0.9,
	}
	r := strings.NewReader("invalid\nconfirm\n")
	var w bytes.Buffer
	result := gate.Run(r, &w, proposal)
	if result.Action != "confirm" {
		t.Errorf("expected action 'confirm', got %q", result.Action)
	}
}

// Phase C tests

func TestGateAmbiguousForcesClarification(t *testing.T) {
	proposal := intent.ActionProposal{
		Kind:       intent.KindAmbiguous,
		Rewritten:  "do something vague",
		Confidence: 0.3,
	}
	r := strings.NewReader("my clarification\nconfirm\n")
	var w bytes.Buffer
	result := gate.Run(r, &w, proposal)
	if result.Action != "confirm" {
		t.Errorf("expected action 'confirm', got %q", result.Action)
	}
	if result.ClarifiedText != "my clarification" {
		t.Errorf("expected ClarifiedText 'my clarification', got %q", result.ClarifiedText)
	}
}

func TestGateAmbiguousCannotDirectlyConfirm(t *testing.T) {
	proposal := intent.ActionProposal{
		Kind:       intent.KindAmbiguous,
		Rewritten:  "do something vague",
		Confidence: 0.3,
	}
	// Provide "confirm" as the clarification text (non-empty, not blank),
	// then discard so the test terminates deterministically.
	r := strings.NewReader("confirm\ndiscard\n")
	var w bytes.Buffer
	result := gate.Run(r, &w, proposal)
	// The first "confirm" line must be read as clarification text, not as
	// the action choice, so the final action should be "discard".
	if result.Action == "confirm" && result.ClarifiedText == "" {
		t.Errorf("gate returned confirm without clarification text — direct confirm must not be allowed for ambiguous proposals")
	}
	// Check that output told the user clarification was needed
	out := w.String()
	if !strings.Contains(strings.ToLower(out), "clarif") {
		t.Errorf("expected output to mention clarification requirement, got: %s", out)
	}
}
