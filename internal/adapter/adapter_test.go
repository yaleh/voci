package adapter

import (
	"testing"

	vocicontext "github.com/yalehu/voci/internal/context"
	"github.com/yalehu/voci/internal/intent"
)

func TestChannelConstants(t *testing.T) {
	if ChannelTmux != "tmux" {
		t.Errorf("ChannelTmux = %q, want %q", ChannelTmux, "tmux")
	}
	if ChannelMCP != "mcp" {
		t.Errorf("ChannelMCP = %q, want %q", ChannelMCP, "mcp")
	}
	if ChannelStdin != "stdin" {
		t.Errorf("ChannelStdin = %q, want %q", ChannelStdin, "stdin")
	}
	if ChannelClipboard != "clipboard" {
		t.Errorf("ChannelClipboard = %q, want %q", ChannelClipboard, "clipboard")
	}
}

type mockAdapter struct{}

func (m *mockAdapter) DiscoverContext() (vocicontext.Source, error) {
	return nil, nil
}

func (m *mockAdapter) Deliver(p intent.ActionProposal) error {
	return nil
}

func (m *mockAdapter) Capabilities() []Channel {
	return []Channel{ChannelTmux}
}

func TestAdapterInterfaceViaMock(t *testing.T) {
	var a Adapter = &mockAdapter{}
	caps := a.Capabilities()
	if caps == nil {
		t.Error("Capabilities() returned nil, want non-nil slice")
	}
}
