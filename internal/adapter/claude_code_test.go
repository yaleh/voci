package adapter

import (
	"errors"
	"testing"

	vocicontext "github.com/yaleh/voci/internal/context"
	"github.com/yaleh/voci/internal/intent/model"
)

var _ Adapter = (*ClaudeCodeAdapter)(nil)

// mockSource implements vocicontext.Source for testing.
type mockSource struct{}

func (m *mockSource) Name() string                  { return "mock" }
func (m *mockSource) Fetch(root string) (string, string) { return "snippet", "mock" }

// mockInjector implements inject.Injector for testing.
type mockInjector struct {
	called bool
	text   string
	err    error
}

func (m *mockInjector) Inject(text string) error {
	m.called = true
	m.text = text
	return m.err
}

// containsChannel reports whether caps contains c.
func containsChannel(caps []Channel, c Channel) bool {
	for _, ch := range caps {
		if ch == c {
			return true
		}
	}
	return false
}

func TestClaudeCodeAdapter_DiscoverContext_ReturnsSrc(t *testing.T) {
	src := &mockSource{}
	a := &ClaudeCodeAdapter{src: src}
	got, err := a.DiscoverContext()
	if err != nil {
		t.Fatalf("DiscoverContext() unexpected error: %v", err)
	}
	if got != vocicontext.Source(src) {
		t.Errorf("DiscoverContext() = %v, want %v", got, src)
	}
}

func TestClaudeCodeAdapter_DiscoverContext_DefaultIsSessionSource(t *testing.T) {
	a := NewClaudeCodeAdapter("", "")
	got, err := a.DiscoverContext()
	if err != nil {
		t.Fatalf("DiscoverContext() unexpected error: %v", err)
	}
	if got == nil {
		t.Fatal("DiscoverContext() returned nil Source, want non-nil")
	}
	if _, ok := got.(*vocicontext.SessionSource); !ok {
		t.Errorf("DiscoverContext() returned %T, want *context.SessionSource", got)
	}
}

func TestNewClaudeCodeAdapterWithSource_UsesGivenSource(t *testing.T) {
	src := &vocicontext.SessionSource{Lines: 42}
	a := NewClaudeCodeAdapterWithSource("", "", src)
	got, err := a.DiscoverContext()
	if err != nil {
		t.Fatalf("DiscoverContext() unexpected error: %v", err)
	}
	ss, ok := got.(*vocicontext.SessionSource)
	if !ok {
		t.Fatalf("DiscoverContext() returned %T, want *context.SessionSource", got)
	}
	if ss.Lines != 42 {
		t.Errorf("Lines = %d, want 42", ss.Lines)
	}
}

func TestClaudeCodeAdapter_Deliver_CallsInjector(t *testing.T) {
	mi := &mockInjector{}
	a := &ClaudeCodeAdapter{inj: mi}
	err := a.Deliver(model.ActionProposal{Rewritten: "hi"})
	if err != nil {
		t.Fatalf("Deliver() unexpected error: %v", err)
	}
	if !mi.called {
		t.Error("expected injector to be called")
	}
	if mi.text != "hi" {
		t.Errorf("injector received %q, want %q", mi.text, "hi")
	}
}

func TestClaudeCodeAdapter_Deliver_InjectorError(t *testing.T) {
	mi := &mockInjector{err: errors.New("fail")}
	a := &ClaudeCodeAdapter{inj: mi}
	err := a.Deliver(model.ActionProposal{Rewritten: "hi"})
	if err == nil {
		t.Fatal("expected error from Deliver")
	}
	if err.Error() != "fail" {
		t.Errorf("Deliver() error = %v, want 'fail'", err)
	}
}

func TestClaudeCodeAdapter_Deliver_IntegratedNoOp(t *testing.T) {
	a := &ClaudeCodeAdapter{mcpAddr: ":9473"}
	err := a.Deliver(model.ActionProposal{Rewritten: "hi"})
	if err != nil {
		t.Fatalf("Deliver() unexpected error in integrated mode: %v", err)
	}
}

func TestClaudeCodeAdapter_Capabilities_WithInjector(t *testing.T) {
	a := &ClaudeCodeAdapter{inj: &mockInjector{}}
	caps := a.Capabilities()
	if !containsChannel(caps, ChannelTmux) {
		t.Error("expected ChannelTmux in capabilities")
	}
	if !containsChannel(caps, ChannelClipboard) {
		t.Error("expected ChannelClipboard in capabilities")
	}
}

func TestClaudeCodeAdapter_Capabilities_WithMCPAddr(t *testing.T) {
	a := &ClaudeCodeAdapter{mcpAddr: ":9473"}
	caps := a.Capabilities()
	if !containsChannel(caps, ChannelMCP) {
		t.Error("expected ChannelMCP in capabilities")
	}
}

func TestClaudeCodeAdapter_Capabilities_BothModes(t *testing.T) {
	a := &ClaudeCodeAdapter{inj: &mockInjector{}, mcpAddr: ":9473"}
	caps := a.Capabilities()
	if !containsChannel(caps, ChannelTmux) {
		t.Error("expected ChannelTmux in capabilities")
	}
	if !containsChannel(caps, ChannelClipboard) {
		t.Error("expected ChannelClipboard in capabilities")
	}
	if !containsChannel(caps, ChannelMCP) {
		t.Error("expected ChannelMCP in capabilities")
	}
}

func TestClaudeCodeAdapter_Capabilities_NonNil(t *testing.T) {
	a := NewClaudeCodeAdapter("pane", "")
	caps := a.Capabilities()
	if len(caps) == 0 {
		t.Error("Capabilities() returned empty slice, want len > 0")
	}
}
