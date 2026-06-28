package intent

import "testing"

func TestKindConstants(t *testing.T) {
	if string(KindDirectPrompt) != "direct_prompt" {
		t.Errorf("KindDirectPrompt = %q, want %q", KindDirectPrompt, "direct_prompt")
	}
	if string(KindBacklogAction) != "backlog_action" {
		t.Errorf("KindBacklogAction = %q, want %q", KindBacklogAction, "backlog_action")
	}
	if string(KindQuery) != "query" {
		t.Errorf("KindQuery = %q, want %q", KindQuery, "query")
	}
	if string(KindAmbiguous) != "ambiguous" {
		t.Errorf("KindAmbiguous = %q, want %q", KindAmbiguous, "ambiguous")
	}
}

func TestActionProposalFields(t *testing.T) {
	p := ActionProposal{
		Kind:          KindDirectPrompt,
		Rewritten:     "add logging to auth.go",
		RawTranscript: "add logging auth",
		Confidence:    0.95,
		ContextUsed:   "backlog",
	}
	if p.Kind != KindDirectPrompt {
		t.Errorf("Kind = %v, want %v", p.Kind, KindDirectPrompt)
	}
	if p.Rewritten != "add logging to auth.go" {
		t.Errorf("Rewritten = %q", p.Rewritten)
	}
	if p.RawTranscript != "add logging auth" {
		t.Errorf("RawTranscript = %q", p.RawTranscript)
	}
	if p.Confidence != 0.95 {
		t.Errorf("Confidence = %v", p.Confidence)
	}
	if p.ContextUsed != "backlog" {
		t.Errorf("ContextUsed = %q", p.ContextUsed)
	}
}

func TestContextUsedProvenanceStrings(t *testing.T) {
	provenances := []string{"backlog", "git", "entities"}
	for _, prov := range provenances {
		p := ActionProposal{ContextUsed: prov}
		if p.ContextUsed != prov {
			t.Errorf("ContextUsed = %q, want %q", p.ContextUsed, prov)
		}
	}
}

func TestZeroKindNotEqualToAnyConstant(t *testing.T) {
	var k Kind
	if k == KindDirectPrompt || k == KindBacklogAction || k == KindQuery || k == KindAmbiguous {
		t.Errorf("zero Kind %q equals a valid constant", k)
	}
}
