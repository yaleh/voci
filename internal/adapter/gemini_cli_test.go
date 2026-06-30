package adapter

import (
	"errors"
	"testing"

	"github.com/yaleh/voci/internal/intent/model"
)

var _ Adapter = (*GeminiCLIAdapter)(nil)

func TestGeminiCLIAdapter_DiscoverContext_NotImplemented(t *testing.T) {
	a := &GeminiCLIAdapter{}
	_, err := a.DiscoverContext()
	if !errors.Is(err, ErrNotImplemented) {
		t.Errorf("DiscoverContext() error = %v, want errors.Is ErrNotImplemented", err)
	}
}

func TestGeminiCLIAdapter_Deliver_NotImplemented(t *testing.T) {
	a := &GeminiCLIAdapter{}
	err := a.Deliver(dummyProposal())
	if !errors.Is(err, ErrNotImplemented) {
		t.Errorf("Deliver() error = %v, want errors.Is ErrNotImplemented", err)
	}
}

func TestGeminiCLIAdapter_Capabilities_NonNil(t *testing.T) {
	a := &GeminiCLIAdapter{}
	caps := a.Capabilities()
	if len(caps) == 0 {
		t.Error("Capabilities() returned empty slice, want len > 0")
	}
}

func dummyProposal() model.ActionProposal {
	return model.ActionProposal{
		Kind:      model.KindDirectPrompt,
		Rewritten: "test",
	}
}
