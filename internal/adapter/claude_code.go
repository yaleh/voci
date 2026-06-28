package adapter

import (
	"fmt"

	vocicontext "github.com/yalehu/voci/internal/context"
	"github.com/yalehu/voci/internal/intent"
)

// ClaudeCodeAdapter integrates voci with the Claude Code CLI tool.
type ClaudeCodeAdapter struct{}

func (a *ClaudeCodeAdapter) DiscoverContext() (vocicontext.Source, error) {
	return nil, fmt.Errorf("ClaudeCodeAdapter.DiscoverContext: %w", ErrNotImplemented)
}

func (a *ClaudeCodeAdapter) Deliver(p intent.ActionProposal) error {
	return fmt.Errorf("ClaudeCodeAdapter.Deliver: %w", ErrNotImplemented)
}

func (a *ClaudeCodeAdapter) Capabilities() []Channel {
	return []Channel{ChannelTmux, ChannelMCP}
}
