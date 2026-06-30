package daemon

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"syscall"

	"github.com/yaleh/voci/internal/daemon/auth"
	"github.com/yaleh/voci/internal/intent/model"
	"github.com/yaleh/voci/internal/pipeline"
)

//go:embed web/*
var embeddedFS embed.FS

// TranscribeFn is the function signature for ASR transcription.
type TranscribeFn func(ctx context.Context, key, audioPath, apiURL, language string, entities []string) string

// HintedFn is the function signature for hinted ASR correction.
type HintedFn func(ctx context.Context, raw, hint string, chatFn pipeline.ChatFn) (string, error)

// RewriteFn is the function signature for rewriting.
type RewriteFn func(ctx context.Context, hinted, hint string, chatFn pipeline.ChatFn) (string, error)

// MergedFnType is the function signature for the merged pipeline call that replaces
// TranscribeFn+HintedFn with a single Gemini Audio API call.
type MergedFnType func(ctx context.Context, key, audioPath, hint, language string, entities []string) (model.ActionProposal, error)

// Server is the daemon HTTP server that accepts audio uploads and writes events.
type Server struct {
	// TranscribeFn performs ASR transcription from an audio file path.
	TranscribeFn TranscribeFn
	// HintedFn performs hinted ASR correction.
	HintedFn HintedFn
	// RewriteFn rewrites the hinted text.
	RewriteFn RewriteFn
	// MergedFn, when non-nil, replaces TranscribeFn+HintedFn with a
	// single Gemini Audio API call.
	MergedFn MergedFnType
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
	// BearerToken, when non-empty, requires all /api/* requests to carry
	// "Authorization: Bearer <token>". Static file routes are unaffected.
	BearerToken string
	// OnListening, if non-nil, is called once the TCP listener is ready.
	// The resolved net.Addr is passed so callers can discover the assigned port
	// when --serve-port 0 is used for OS-assigned ephemeral ports.
	OnListening func(net.Addr)
}

// Handler returns an http.Handler routing the voice endpoints and static UI.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/api/voice/transcribe", auth.BearerMiddleware(s.BearerToken, http.HandlerFunc(s.handleTranscribe)))
	mux.Handle("/api/voice/emit", auth.BearerMiddleware(s.BearerToken, http.HandlerFunc(s.handleEmit)))
	mux.Handle("/api/context", auth.BearerMiddleware(s.BearerToken, http.HandlerFunc(s.handleContext)))
	sub, _ := fs.Sub(embeddedFS, "web")
	mux.Handle("/", http.FileServerFS(sub))
	return mux
}

// Start starts the HTTP server on the given address.
func (s *Server) Start(addr string) error {
	return http.ListenAndServe(addr, s.Handler())
}

// StartWithContextFromListener starts the HTTP server on an already-bound listener.
// This allows callers to discover the real OS-assigned port before the server
// starts serving — useful when passing the port to an external process (e.g.
// cloudflared) that must connect back to us.
func (s *Server) StartWithContextFromListener(ctx context.Context, ln net.Listener) error {
	if s.OnListening != nil {
		s.OnListening(ln.Addr())
	}
	hs := &http.Server{Handler: s.Handler()}
	go func() {
		<-ctx.Done()
		hs.Shutdown(context.Background()) //nolint:errcheck
	}()
	err := hs.Serve(ln)
	if ctx.Err() != nil {
		return nil // clean shutdown on context cancel
	}
	return err
}

// StartWithContext starts the HTTP server and shuts it down gracefully when ctx
// is cancelled. This allows tunnel.WatchTunnel to propagate a tunnel exit to the
// server: cancel the context → server stops → voci serve exits → monitor re-arms.
//
// When the port is explicitly non-zero and already bound, returns an error containing
// "already in use" so callers can suggest --serve-port 0 for automatic port selection.
func (s *Server) StartWithContext(ctx context.Context, addr string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		var opErr *net.OpError
		if errors.As(err, &opErr) {
			if errors.Is(opErr.Err, syscall.EADDRINUSE) {
				_, portStr, _ := net.SplitHostPort(addr)
				return fmt.Errorf("port %s is already in use; use --serve-port 0 for automatic port selection: %w", portStr, err)
			}
		}
		return err
	}
	return s.StartWithContextFromListener(ctx, ln)
}
