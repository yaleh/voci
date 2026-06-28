package adapter

import (
	"errors"
	"testing"
)

var _ Adapter = (*CodexAdapter)(nil)

func TestCodexAdapter_DiscoverContext_NotImplemented(t *testing.T) {
	a := &CodexAdapter{}
	_, err := a.DiscoverContext()
	if !errors.Is(err, ErrNotImplemented) {
		t.Errorf("DiscoverContext() error = %v, want errors.Is ErrNotImplemented", err)
	}
}

func TestCodexAdapter_Deliver_NotImplemented(t *testing.T) {
	a := &CodexAdapter{}
	err := a.Deliver(dummyProposal())
	if !errors.Is(err, ErrNotImplemented) {
		t.Errorf("Deliver() error = %v, want errors.Is ErrNotImplemented", err)
	}
}

func TestCodexAdapter_Capabilities_NonNil(t *testing.T) {
	a := &CodexAdapter{}
	caps := a.Capabilities()
	if len(caps) == 0 {
		t.Error("Capabilities() returned empty slice, want len > 0")
	}
}
