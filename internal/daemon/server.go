package daemon

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"

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
	// Written by /api/voice/emit only (not by /api/voice/transcribe).
	EventWriter io.Writer
	// EventPath is the optional path to the event log file (debug/fallback only).
	EventPath string
}

// Handler returns an http.Handler routing the two voice endpoints.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/voice/transcribe", s.handleTranscribe)
	mux.HandleFunc("/api/voice/emit", s.handleEmit)
	return mux
}

// Start starts the HTTP server on the given address.
func (s *Server) Start(addr string) error {
	return http.ListenAndServe(addr, s.Handler())
}

// handleTranscribe runs the full ASR pipeline and returns ActionProposal JSON.
// It does NOT write to EventWriter or EventPath — that is handleEmit's job.
func (s *Server) handleTranscribe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	audioBytes, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}
	if len(audioBytes) == 0 {
		http.Error(w, "empty body: audio data required", http.StatusBadRequest)
		return
	}

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

	hint := ""
	if s.BuildHintFn != nil {
		hint = s.BuildHintFn()
	}

	ctx := r.Context()

	raw, err := s.TranscribeFn(ctx, s.APIKey, tmpFile.Name(), "")
	if err != nil {
		http.Error(w, "ASR error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	hinted, err := s.HintedFn(ctx, raw, hint, s.ChatFn)
	if err != nil {
		http.Error(w, "hinted error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	rewritten, err := s.RewriteFn(ctx, hinted, hint, s.ChatFn)
	if err != nil {
		http.Error(w, "rewrite error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	proposal, err := s.ClassifyFn(ctx, rewritten, hint, s.ChatFn)
	if err != nil {
		http.Error(w, "classify error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	proposal.RawTranscript = raw

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(proposal)
}

type emitRequest struct {
	Text string `json:"text"`
}

// handleEmit accepts browser-confirmed text and emits it to EventWriter (→ Monitor) and
// optionally to EventPath (debug sidecar). It does not invoke any pipeline stage.
func (s *Server) handleEmit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req emitRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}

	text := strings.TrimSpace(req.Text)
	if text == "" {
		http.Error(w, "text is required", http.StatusBadRequest)
		return
	}

	if s.EventWriter == nil {
		http.Error(w, "EventWriter not configured", http.StatusServiceUnavailable)
		return
	}

	ev := Event{
		Rewritten: text,
		Kind:      "direct_prompt",
	}

	if data, merr := json.Marshal(ev); merr == nil {
		s.EventWriter.Write(append(data, '\n'))
	}

	if s.EventPath != "" {
		_ = AppendEvent(s.EventPath, ev)
	}

	w.WriteHeader(http.StatusNoContent)
}
