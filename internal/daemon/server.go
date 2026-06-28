package daemon

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"

	"github.com/yalehu/voci/internal/intent"
	"github.com/yalehu/voci/internal/pipeline"
)

// TranscribeFn is the function signature for ASR transcription.
type TranscribeFn func(ctx context.Context, key, audioPath, apiURL string) (string, error)

// HintedFn is the function signature for hinted ASR correction.
type HintedFn func(ctx context.Context, raw, hint string, chatFn pipeline.ChatFn) (string, error)

// RewriteFn is the function signature for rewriting.
type RewriteFn func(ctx context.Context, hinted, hint string, chatFn pipeline.ChatFn) (string, error)

// ClassifyFn is the function signature for intent classification.
type ClassifyFn func(ctx context.Context, rewritten, fullContext string, chat pipeline.ChatFn) (intent.ActionProposal, error)

// Server is the daemon HTTP server that accepts audio uploads and writes events.
type Server struct {
	// TranscribeFn performs ASR transcription from an audio file path.
	TranscribeFn TranscribeFn
	// HintedFn performs hinted ASR correction.
	HintedFn HintedFn
	// RewriteFn rewrites the hinted text.
	RewriteFn RewriteFn
	// ClassifyFn classifies the intent from rewritten text.
	ClassifyFn ClassifyFn
	// BuildHintFn builds the context hint; called once per request.
	BuildHintFn func() string
	// ChatFn is the LLM chat function.
	ChatFn pipeline.ChatFn
	// APIKey is the ASR API key.
	APIKey string
	// EventWriter is the writer for event output (e.g. os.Stdout for Monitor-host mode).
	// One JSON line per event is written here on every successful request.
	EventWriter io.Writer
	// EventPath is the optional path to the event log file (debug/fallback only).
	EventPath string
}

// Handler returns an http.Handler routing POST /api/voice/transcribe.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/voice/transcribe", s.handleTranscribe)
	return mux
}

// Start starts the HTTP server on the given address.
func (s *Server) Start(addr string) error {
	return http.ListenAndServe(addr, s.Handler())
}

func (s *Server) handleTranscribe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Read audio bytes from body
	audioBytes, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}
	if len(audioBytes) == 0 {
		http.Error(w, "empty body: audio data required", http.StatusBadRequest)
		return
	}

	// Write audio bytes to a temp file for the transcribe function
	tmpFile, err := os.CreateTemp("", "voci-audio-*.wav")
	if err != nil {
		http.Error(w, "failed to create temp file", http.StatusInternalServerError)
		return
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.Write(audioBytes); err != nil {
		tmpFile.Close()
		http.Error(w, "failed to write temp file", http.StatusInternalServerError)
		return
	}
	tmpFile.Close()

	// Rebuild hint per call
	hint := ""
	if s.BuildHintFn != nil {
		hint = s.BuildHintFn()
	}

	ctx := r.Context()

	// Stage 1: ASR transcription
	raw, err := s.TranscribeFn(ctx, s.APIKey, tmpFile.Name(), "")
	if err != nil {
		http.Error(w, "ASR error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Stage 2: Hinted correction
	hinted, err := s.HintedFn(ctx, raw, hint, s.ChatFn)
	if err != nil {
		http.Error(w, "hinted error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Stage 3: Rewrite
	rewritten, err := s.RewriteFn(ctx, hinted, hint, s.ChatFn)
	if err != nil {
		http.Error(w, "rewrite error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Stage 4: Classify
	proposal, err := s.ClassifyFn(ctx, rewritten, hint, s.ChatFn)
	if err != nil {
		http.Error(w, "classify error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	proposal.RawTranscript = raw

	ev := Event{
		Rewritten:     proposal.Rewritten,
		Kind:          string(proposal.Kind),
		RawTranscript: proposal.RawTranscript,
		Confidence:    proposal.Confidence,
	}

	// Primary output channel: EventWriter (stdout in Monitor-host mode).
	if s.EventWriter != nil {
		if data, merr := json.Marshal(ev); merr == nil {
			s.EventWriter.Write(append(data, '\n'))
		}
	}

	// Optional file sidecar for debugging/legacy.
	if s.EventPath != "" {
		_ = AppendEvent(s.EventPath, ev)
	}

	// Return proposal as JSON
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(proposal)
}
