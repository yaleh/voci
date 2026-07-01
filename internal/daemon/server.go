package daemon

import (
	"bytes"
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"strings"
	"syscall"
	"time"

	vocicontext "github.com/yaleh/voci/internal/context"
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
	// DialogueFn returns structured conversation turns for the Web UI, preserving
	// full Markdown (tables, code blocks, blank lines). If nil, /api/context omits
	// the "dialogue" field.
	DialogueFn func(ctx context.Context) ([]vocicontext.DialogueTurn, error)
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
	// VADThreshold and MinAudioMs are D-class frontend VAD tuning values, sourced
	// from config.Config and served read-only to the browser via /api/config.
	VADThreshold float64
	MinAudioMs   int
}

// Handler returns an http.Handler routing the voice endpoints and static UI.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/api/voice/transcribe", auth.BearerMiddleware(s.BearerToken, http.HandlerFunc(s.handleTranscribe)))
	mux.Handle("/api/voice/emit", auth.BearerMiddleware(s.BearerToken, http.HandlerFunc(s.handleEmit)))
	mux.Handle("/api/context", auth.BearerMiddleware(s.BearerToken, http.HandlerFunc(s.handleContext)))
	mux.Handle("/api/config", auth.BearerMiddleware(s.BearerToken, http.HandlerFunc(s.handleConfig)))
	sub, _ := fs.Sub(embeddedFS, "web")
	mux.Handle("/", staticHandler(sub))
	return mux
}

// staticHandler serves embedded web assets with a content-based ETag and
// Cache-Control: no-cache. Because embed.FS files carry a zero ModTime,
// http.FileServerFS emits no validators, so browsers heuristically cache the
// bundle and can keep a stale copy across rebuilds. no-cache forces the browser
// to revalidate every load; the content ETag lets the server answer 304 when the
// asset is unchanged (cheap) and 200 with fresh bytes when it changes.
func staticHandler(fsys fs.FS) http.Handler {
	// bundleVer is a content hash of the JS bundle, injected into index.html as a
	// ?v= query so a rebuilt bundle changes its URL. This defeats the Cloudflare
	// tunnel's edge cache (which overrides origin Cache-Control with max-age),
	// forcing a fresh fetch on every deploy. index.html itself is served fresh
	// (no-cache; Cloudflare does not edge-cache text/html).
	bundleVer := ""
	if b, err := fs.ReadFile(fsys, "recorder.bundle.js"); err == nil {
		sum := sha256.Sum256(b)
		bundleVer = hex.EncodeToString(sum[:8])
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/")
		if name == "" {
			name = "index.html"
		}
		b, err := fs.ReadFile(fsys, name)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		if name == "index.html" && bundleVer != "" {
			b = bytes.Replace(b, []byte("recorder.bundle.js"),
				[]byte("recorder.bundle.js?v="+bundleVer), 1)
		}
		sum := sha256.Sum256(b)
		etag := `"` + hex.EncodeToString(sum[:16]) + `"`
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Etag", etag)
		// ServeContent honors the pre-set Etag for If-None-Match (→ 304) and sets
		// Content-Type from the file extension.
		http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(b))
	})
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
