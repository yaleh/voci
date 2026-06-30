package adapter

import (
	"errors"

	vocicontext "github.com/yaleh/voci/internal/context"
	"github.com/yaleh/voci/internal/intent/model"
)

// Channel represents the delivery channel an adapter supports.
type Channel string

const (
	ChannelTmux      Channel = "tmux"
	ChannelMCP       Channel = "mcp"
	ChannelStdin     Channel = "stdin"
	ChannelClipboard Channel = "clipboard"
)

// ErrNotImplemented is returned by skeleton adapter methods that are not yet implemented.
var ErrNotImplemented = errors.New("not implemented")

// Adapter is the unified interface for voci tool integrations.
type Adapter interface {
	// DiscoverContext collects context from the tool's environment.
	DiscoverContext() (vocicontext.Source, error)
	// Deliver sends the action proposal to the tool via an appropriate channel.
	Deliver(model.ActionProposal) error
	// Capabilities returns the set of channels this adapter supports.
	Capabilities() []Channel
}
