package daemon

import (
	"context"
	"embed"
	"encoding/json"
	"io"
	"io/fs"
	"net/http"
	"os"
	"strings"

	"github.com/yalehu/voci/internal/asr"
	"github.com/yalehu/voci/internal/intent"
	"github.com/yalehu/voci/internal/pipeline"
)

//go:embed web/*
var embeddedFS embed.FS

// TranscribeFn is the function signature for ASR transcription.
type TranscribeFn func(ctx context.Context, key, audioPath, apiURL, language string, entities []string) string

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
	// HintFn returns the full assembled hint for the context preview panel (/api/context).
	// If nil, /api/context returns an empty hint.
	HintFn func(ctx context.Context) (string, error)
	// ChatFn is the LLM chat function.
	ChatFn pipeline.ChatFn
	// APIKey is the ASR API key.
	APIKey string
	// Language is the ASR language code (e.g. "zh", "en"). Selects the ASR model.
	Language string
	// EventWriter is the writer for event output (e.g. os.Stdout for Monitor-host mode).
	// Written by /api/voice/emit only (not by /api/voice/transcribe).
	EventWriter io.Writer
	// EventPath is the optional path to the event log file (debug/fallback only).
	EventPath string
}

// Handler returns an http.Handler routing the voice endpoints and static UI.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/voice/transcribe", s.handleTranscribe)
	mux.HandleFunc("/api/voice/emit", s.handleEmit)
	mux.HandleFunc("/api/context", s.handleContext)
	sub, _ := fs.Sub(embeddedFS, "web")
	mux.Handle("/", http.FileServerFS(sub))
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
	entities := asr.ExtractEntities(hint)

	raw := s.TranscribeFn(ctx, s.APIKey, tmpFile.Name(), "", s.Language, entities)

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

// handleContext returns the current assembled ASR hint as JSON for the Web UI context panel.
func (s *Server) handleContext(w http.ResponseWriter, r *http.Request) {
	hint := ""
	if s.HintFn != nil {
		var err error
		hint, err = s.HintFn(r.Context())
		if err != nil {
			http.Error(w, "context build failed: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"hint": hint})
}

type emitRequest struct {
	Text string `json:"text"`
	Kind string `json:"kind"`
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

	kind := strings.TrimSpace(req.Kind)
	if kind == "" {
		kind = "direct_prompt"
	}
	ev := Event{
		Rewritten: text,
		Kind:      kind,
	}

	if data, merr := json.Marshal(ev); merr == nil {
		s.EventWriter.Write(append(data, '\n'))
	}

	if s.EventPath != "" {
		_ = AppendEvent(s.EventPath, ev)
	}

	w.WriteHeader(http.StatusNoContent)
}
