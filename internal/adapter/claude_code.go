package adapter

import (
	"fmt"

	vocicontext "github.com/yaleh/voci/internal/context"
	"github.com/yaleh/voci/internal/inject"
	"github.com/yaleh/voci/internal/intent/model"
)

// ClaudeCodeAdapter integrates voci with the Claude Code CLI tool.
type ClaudeCodeAdapter struct {
	src     vocicontext.Source
	inj     inject.Injector
	mcpAddr string
}

// NewClaudeCodeAdapter creates a ClaudeCodeAdapter with a SessionSource and default injector.
func NewClaudeCodeAdapter(tmuxTarget, mcpAddr string) *ClaudeCodeAdapter {
	return NewClaudeCodeAdapterWithSource(tmuxTarget, mcpAddr, &vocicontext.SessionSource{Lines: 100})
}

// NewClaudeCodeAdapterWithSource creates a ClaudeCodeAdapter with a caller-supplied
// Source (e.g. a SessionSource configured from config.Config) and default injector.
func NewClaudeCodeAdapterWithSource(tmuxTarget, mcpAddr string, src vocicontext.Source) *ClaudeCodeAdapter {
	return &ClaudeCodeAdapter{
		src:     src,
		inj:     inject.NewDefaultInjector(tmuxTarget),
		mcpAddr: mcpAddr,
	}
}

func (a *ClaudeCodeAdapter) DiscoverContext() (vocicontext.Source, error) {
	return a.src, nil
}

func (a *ClaudeCodeAdapter) Deliver(p model.ActionProposal) error {
	if a.mcpAddr != "" {
		return nil // integrated mode: MCP server handles end-to-end
	}
	if a.inj == nil {
		return fmt.Errorf("ClaudeCodeAdapter.Deliver: %w", ErrNotImplemented)
	}
	return a.inj.Inject(p.Rewritten)
}

func (a *ClaudeCodeAdapter) Capabilities() []Channel {
	var caps []Channel
	if a.inj != nil {
		caps = append(caps, ChannelTmux, ChannelClipboard)
	}
	if a.mcpAddr != "" {
		caps = append(caps, ChannelMCP)
	}
	return caps
}
