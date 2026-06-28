package intent

// Kind represents the classified intent category.
type Kind string

const (
	// KindDirectPrompt indicates a direct programming instruction.
	KindDirectPrompt Kind = "direct_prompt"
	// KindBacklogAction indicates an action targeting the backlog (create/update/close tasks).
	KindBacklogAction Kind = "backlog_action"
	// KindQuery indicates an information query about the project.
	KindQuery Kind = "query"
	// KindAmbiguous indicates the intent could not be determined with confidence.
	KindAmbiguous Kind = "ambiguous"
)

// ActionProposal represents the classified intent of a rewritten voice command.
type ActionProposal struct {
	// Kind is the intent category.
	Kind Kind
	// Rewritten is the clarified text that was classified.
	Rewritten string
	// RawTranscript is the original ASR output before rewriting.
	RawTranscript string
	// Confidence is the model's confidence in [0.0, 1.0].
	Confidence float64
	// ContextUsed is the provenance key of the context used (e.g. "backlog", "git", "entities").
	ContextUsed string
}
