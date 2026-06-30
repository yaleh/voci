package daemon

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/yaleh/voci/internal/asr"
	"github.com/yaleh/voci/internal/daemon/session"
)

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

	rewritten := hinted
	if rewriteFn := s.RewriteFn; rewriteFn != nil {
		var err error
		rewritten, err = rewriteFn(ctx, hinted, hint, s.ChatFn)
		if err != nil {
			http.Error(w, "rewrite error: "+err.Error(), http.StatusInternalServerError)
			return
		}
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
	ev := session.Event{
		Rewritten: text,
		Kind:      kind,
	}

	if data, merr := json.Marshal(ev); merr == nil {
		s.EventWriter.Write(append(data, '\n'))
	}

	if s.EventPath != "" {
		_ = session.AppendEvent(s.EventPath, ev)
	}

	w.WriteHeader(http.StatusNoContent)
}
