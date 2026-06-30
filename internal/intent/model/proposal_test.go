package model

import "testing"

func TestActionProposalFields(t *testing.T) {
	p := ActionProposal{
		Rewritten:     "add logging to auth.go",
		RawTranscript: "add logging auth",
	}
	if p.Rewritten != "add logging to auth.go" {
		t.Errorf("Rewritten = %q", p.Rewritten)
	}
	if p.RawTranscript != "add logging auth" {
		t.Errorf("RawTranscript = %q", p.RawTranscript)
	}
}

func TestZeroValueActionProposal(t *testing.T) {
	p := ActionProposal{}
	if p.Rewritten != "" {
		t.Errorf("zero Rewritten = %q, want empty", p.Rewritten)
	}
	if p.RawTranscript != "" {
		t.Errorf("zero RawTranscript = %q, want empty", p.RawTranscript)
	}
}
