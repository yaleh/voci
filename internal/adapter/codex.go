package adapter

import (
	"fmt"

	vocicontext "github.com/yaleh/voci/internal/context"
	"github.com/yaleh/voci/internal/intent/model"
)

// CodexAdapter integrates voci with the OpenAI Codex CLI tool.
type CodexAdapter struct{}

func (a *CodexAdapter) DiscoverContext() (vocicontext.Source, error) {
	return nil, fmt.Errorf("CodexAdapter.DiscoverContext: %w", ErrNotImplemented)
}

func (a *CodexAdapter) Deliver(p model.ActionProposal) error {
	return fmt.Errorf("CodexAdapter.Deliver: %w", ErrNotImplemented)
}

func (a *CodexAdapter) Capabilities() []Channel {
	return []Channel{ChannelStdin}
}
