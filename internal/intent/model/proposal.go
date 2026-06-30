// Package model contains the shared types for the intent classification pipeline.
// It is a leaf package with no internal imports, making it safe to import from
// any layer of the application.
package model

// ActionProposal represents the output of a voice command pipeline.
type ActionProposal struct {
	// Rewritten is the clarified text produced by the rewrite stage.
	Rewritten string
	// RawTranscript is the original ASR output before rewriting.
	RawTranscript string
}
