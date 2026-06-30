package intent

import "github.com/yaleh/voci/internal/intent/model"

// Kind is an alias for model.Kind.
type Kind = model.Kind

const (
	KindDirectPrompt  = model.KindDirectPrompt
	KindBacklogAction = model.KindBacklogAction
	KindQuery         = model.KindQuery
	KindAmbiguous     = model.KindAmbiguous
)

// ActionProposal is an alias for model.ActionProposal.
type ActionProposal = model.ActionProposal
