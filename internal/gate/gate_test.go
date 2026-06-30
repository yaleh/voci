package gate_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/yaleh/voci/internal/gate"
	"github.com/yaleh/voci/internal/intent/model"
)

func TestPrintProposalSummary(t *testing.T) {
	proposal := model.ActionProposal{
		Rewritten: "add a login endpoint",
	}
	var buf bytes.Buffer
	gate.PrintSummary(&buf, proposal)
	out := buf.String()

	if !strings.Contains(out, "add a login endpoint") {
		t.Errorf("expected output to contain rewritten text, got: %s", out)
	}
}

func TestRunConfirm(t *testing.T) {
	proposal := model.ActionProposal{
		Rewritten: "add a login endpoint",
	}
	r := strings.NewReader("confirm\n")
	var w bytes.Buffer
	result := gate.Run(r, &w, proposal)
	if result.Action != "confirm" {
		t.Errorf("expected action 'confirm', got %q", result.Action)
	}
}

func TestRunEdit(t *testing.T) {
	proposal := model.ActionProposal{
		Rewritten: "add a login endpoint",
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
	proposal := model.ActionProposal{
		Rewritten: "add a login endpoint",
	}
	r := strings.NewReader("discard\n")
	var w bytes.Buffer
	result := gate.Run(r, &w, proposal)
	if result.Action != "discard" {
		t.Errorf("expected action 'discard', got %q", result.Action)
	}
}

func TestRunInvalidThenConfirm(t *testing.T) {
	proposal := model.ActionProposal{
		Rewritten: "add a login endpoint",
	}
	r := strings.NewReader("invalid\nconfirm\n")
	var w bytes.Buffer
	result := gate.Run(r, &w, proposal)
	if result.Action != "confirm" {
		t.Errorf("expected action 'confirm', got %q", result.Action)
	}
}
