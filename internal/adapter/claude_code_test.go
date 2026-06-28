package adapter

import (
	"errors"
	"testing"
)

var _ Adapter = (*ClaudeCodeAdapter)(nil)

func TestClaudeCodeAdapter_DiscoverContext_NotImplemented(t *testing.T) {
	a := &ClaudeCodeAdapter{}
	_, err := a.DiscoverContext()
	if !errors.Is(err, ErrNotImplemented) {
		t.Errorf("DiscoverContext() error = %v, want errors.Is ErrNotImplemented", err)
	}
}

func TestClaudeCodeAdapter_Deliver_NotImplemented(t *testing.T) {
	a := &ClaudeCodeAdapter{}
	err := a.Deliver(dummyProposal())
	if !errors.Is(err, ErrNotImplemented) {
		t.Errorf("Deliver() error = %v, want errors.Is ErrNotImplemented", err)
	}
}

func TestClaudeCodeAdapter_Capabilities_NonNil(t *testing.T) {
	a := &ClaudeCodeAdapter{}
	caps := a.Capabilities()
	if len(caps) == 0 {
		t.Error("Capabilities() returned empty slice, want len > 0")
	}
}
