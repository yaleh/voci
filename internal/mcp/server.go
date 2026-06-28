package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

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

// Server is a local MCP JSON-RPC HTTP server that exposes the voci pipeline as a tool.
type Server struct {
	TranscribeFn TranscribeFn
	HintedFn     HintedFn
	RewriteFn    RewriteFn
	ClassifyFn   ClassifyFn
	APIKey       string
	ChatFn       pipeline.ChatFn
	Hint         string
}

// NewServer creates a new MCP server with injected pipeline functions.
func NewServer(
	transcribeFn TranscribeFn,
	hintedFn HintedFn,
	rewriteFn RewriteFn,
	classifyFn ClassifyFn,
	apiKey string,
	chatFn pipeline.ChatFn,
	hint string,
) *Server {
	return &Server{
		TranscribeFn: transcribeFn,
		HintedFn:     hintedFn,
		RewriteFn:    rewriteFn,
		ClassifyFn:   classifyFn,
		APIKey:       apiKey,
		ChatFn:       chatFn,
		Hint:         hint,
	}
}

// Handler returns an http.Handler for the MCP server.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleRPC)
	return mux
}

// Start starts the MCP HTTP server on the given address.
func (s *Server) Start(addr string) error {
	return http.ListenAndServe(addr, s.Handler())
}

func writeResponse(w http.ResponseWriter, resp Response) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func errorResponse(id interface{}, code int, message string) Response {
	return Response{
		JSONRPC: "2.0",
		ID:      id,
		Error: &RPCError{
			Code:    code,
			Message: message,
		},
	}
}

func (s *Server) handleRPC(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(Response{
			JSONRPC: "2.0",
			ID:      nil,
			Error: &RPCError{
				Code:    -32700,
				Message: "Parse error: " + err.Error(),
			},
		})
		return
	}

	switch req.Method {
	case "tools/list":
		writeResponse(w, s.toolsList(req))
	case "tools/call":
		writeResponse(w, s.toolsCall(req))
	case "initialize":
		writeResponse(w, Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: map[string]interface{}{
				"protocolVersion": "2024-11-05",
				"capabilities":    map[string]interface{}{"tools": map[string]interface{}{}},
				"serverInfo":      map[string]interface{}{"name": "voci", "version": "0.1.0"},
			},
		})
	case "notifications/initialized":
		w.WriteHeader(http.StatusNoContent)
	default:
		writeResponse(w, errorResponse(req.ID, -32601, fmt.Sprintf("method not found: %s", req.Method)))
	}
}

func (s *Server) toolsList(req Request) Response {
	tools := []Tool{
		{
			Name:        "mcp__voci__transcribe",
			Description: "Transcribe an audio file and run the full ASR→RunHinted→Rewrite→Classify pipeline, returning an ActionProposal JSON.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"audio_path": map[string]interface{}{
						"type":        "string",
						"description": "Path to the audio WAV file to transcribe.",
					},
				},
				"required": []string{"audio_path"},
			},
		},
	}
	return Response{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: map[string]interface{}{
			"tools": tools,
		},
	}
}

type toolsCallParams struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments"`
}

func (s *Server) toolsCall(req Request) Response {
	var params toolsCallParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return errorResponse(req.ID, -32602, "invalid params: "+err.Error())
	}

	if params.Name != "mcp__voci__transcribe" {
		return errorResponse(req.ID, -32601, fmt.Sprintf("unknown tool: %s", params.Name))
	}

	audioPathVal, ok := params.Arguments["audio_path"]
	if !ok {
		return errorResponse(req.ID, -32602, "invalid params: audio_path is required")
	}
	audioPath, ok := audioPathVal.(string)
	if !ok || audioPath == "" {
		return errorResponse(req.ID, -32602, "invalid params: audio_path must be a non-empty string")
	}

	ctx := context.Background()

	raw, err := s.TranscribeFn(ctx, s.APIKey, audioPath, "")
	if err != nil {
		return errorResponse(req.ID, -32603, "ASR error: "+err.Error())
	}

	hinted, err := s.HintedFn(ctx, raw, s.Hint, s.ChatFn)
	if err != nil {
		return errorResponse(req.ID, -32603, "hinted error: "+err.Error())
	}

	rewritten, err := s.RewriteFn(ctx, hinted, s.Hint, s.ChatFn)
	if err != nil {
		return errorResponse(req.ID, -32603, "rewrite error: "+err.Error())
	}

	proposal, err := s.ClassifyFn(ctx, rewritten, s.Hint, s.ChatFn)
	if err != nil {
		return errorResponse(req.ID, -32603, "classify error: "+err.Error())
	}

	proposal.RawTranscript = raw

	proposalJSON, err := json.Marshal(proposal)
	if err != nil {
		return errorResponse(req.ID, -32603, "marshal error: "+err.Error())
	}

	return Response{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: map[string]interface{}{
			"content": []map[string]interface{}{
				{
					"type": "text",
					"text": string(proposalJSON),
				},
			},
		},
	}
}
