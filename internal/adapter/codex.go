package adapter

import (
	"fmt"

	vocicontext "github.com/yalehu/voci/internal/context"
	"github.com/yalehu/voci/internal/intent"
)

// CodexAdapter integrates voci with the OpenAI Codex CLI tool.
type CodexAdapter struct{}

func (a *CodexAdapter) DiscoverContext() (vocicontext.Source, error) {
	return nil, fmt.Errorf("CodexAdapter.DiscoverContext: %w", ErrNotImplemented)
}

func (a *CodexAdapter) Deliver(p intent.ActionProposal) error {
	return fmt.Errorf("CodexAdapter.Deliver: %w", ErrNotImplemented)
}

func (a *CodexAdapter) Capabilities() []Channel {
	return []Channel{ChannelStdin}
}
