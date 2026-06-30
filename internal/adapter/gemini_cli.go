package adapter

import (
	"fmt"

	vocicontext "github.com/yaleh/voci/internal/context"
	"github.com/yaleh/voci/internal/intent/model"
)

// GeminiCLIAdapter integrates voci with the Gemini CLI tool.
type GeminiCLIAdapter struct{}

func (a *GeminiCLIAdapter) DiscoverContext() (vocicontext.Source, error) {
	return nil, fmt.Errorf("GeminiCLIAdapter.DiscoverContext: %w", ErrNotImplemented)
}

func (a *GeminiCLIAdapter) Deliver(p model.ActionProposal) error {
	return fmt.Errorf("GeminiCLIAdapter.Deliver: %w", ErrNotImplemented)
}

func (a *GeminiCLIAdapter) Capabilities() []Channel {
	return []Channel{ChannelClipboard}
}
