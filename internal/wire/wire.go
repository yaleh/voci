package wire

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/yaleh/voci/internal/adapter"
	"github.com/yaleh/voci/internal/asr"
	"github.com/yaleh/voci/internal/config"
	vocicontext "github.com/yaleh/voci/internal/context"
	"github.com/yaleh/voci/internal/daemon"
	"github.com/yaleh/voci/internal/daemon/auth"
	"github.com/yaleh/voci/internal/daemon/session"
	"github.com/yaleh/voci/internal/daemon/tunnel"
	"github.com/yaleh/voci/internal/executor"
	"github.com/yaleh/voci/internal/gate"
	"github.com/yaleh/voci/internal/inject"
	"github.com/yaleh/voci/internal/intent"
	"github.com/yaleh/voci/internal/intent/model"
	"github.com/yaleh/voci/internal/mcp"
	"github.com/yaleh/voci/internal/ollama"
	"github.com/yaleh/voci/internal/output"
	"github.com/yaleh/voci/internal/pipeline"
)

// Dependency types for testing
type TranscribeFn func(ctx context.Context, key, audioPath, apiURL, language string, entities []string) string
type RewriteFn func(ctx context.Context, hinted, hint string, chatFn pipeline.ChatFn) (string, error)
type ClassifyFn func(ctx context.Context, rewritten, fullContext string, chat pipeline.ChatFn) (model.ActionProposal, error)
type GateFn func(r io.Reader, w io.Writer, proposal model.ActionProposal) gate.GateResult
type ExecuteFn func(proposal model.ActionProposal) (string, error)
type InjectFn func(text string) error
type StartMCPServerFn func(addr string) error
type BuildHintFn func(root string) string
type StartDaemonFn func(addr, eventsPath string, buildHintFn func() string) error
type StartServeFn func(addr string, eventWriter io.Writer, buildHintFn func() string) error
type StartManagedTunnelFn func(ctx context.Context, cfg tunnel.ManagedTunnelConfig, port int, logW io.Writer) (*exec.Cmd, string, error)

// Run is the main entry point for the voci CLI. It receives os.Args (program
// name at index 0), builds wiring, dispatches, and returns an exit code.
func Run(args []string) int {
	target := os.Getenv("TMUX_PANE")
	ccAdapter := adapter.NewClaudeCodeAdapter(target, "")
	buildHintFn := BuildHintFn(func(root string) string {
		src, err := ccAdapter.DiscoverContext()
		if err != nil || src == nil {
			return vocicontext.BuildContext(root, nil)
		}
		return vocicontext.BuildContextWithSource(root, src, nil)
	})
	if err := dispatch(args[1:], os.Stdout, os.Stdin,
		nil, nil, nil, nil, nil, nil, nil, nil,
		buildHintFn, ccAdapter.Deliver, nil, nil, nil,
	); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	return 0
}

// dispatch routes bare subcommands (serve, mcp, once) to run(), translating them
// to the equivalent legacy flags. Leading-flag args fall through to run() verbatim.
func dispatch(
	args []string,
	stdout io.Writer,
	stdin io.Reader,
	transcribeFn TranscribeFn,
	hintedFn func(ctx context.Context, raw, hint string, chatFn pipeline.ChatFn) (string, error),
	rewriteFnOpt RewriteFn,
	classifyFn ClassifyFn,
	gateFn GateFn,
	executeFn ExecuteFn,
	injectFn InjectFn,
	startMCPServerFn StartMCPServerFn,
	buildHintFn BuildHintFn,
	deliverFn func(model.ActionProposal) error,
	startDaemonFn StartDaemonFn,
	startServeFn StartServeFn,
	startManagedTunnelFn StartManagedTunnelFn,
) error {
	fwd := func(a []string) error {
		return run(a, stdout, stdin, transcribeFn, hintedFn, rewriteFnOpt, classifyFn, gateFn, executeFn, injectFn, startMCPServerFn, buildHintFn, deliverFn, startDaemonFn, startServeFn, startManagedTunnelFn)
	}
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return fwd(args)
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "serve":
		return fwd(append([]string{"--serve"}, rest...))
	case "mcp":
		return fwd(append([]string{"--session=integrated"}, rest...))
	case "once":
		return fwd(rest)
	default:
		return fmt.Errorf("unknown subcommand %q; use serve, mcp, or once", sub)
	}
}

// testOnServerBuilt, when non-nil, is called right after the daemon.Server is
// constructed in the --serve path. Only set this in tests.
var testOnServerBuilt func(srv interface{})

// firstNonEmpty returns the first non-empty string from the arguments.
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// defaultCmdRunner runs an external command and returns its combined output.
func defaultCmdRunner(name string, args ...string) (string, error) {
	out, err := exec.Command(name, args...).Output()
	return string(out), err
}

// openCloudflaredLog opens ~/.voci/cloudflared.log in append mode for persistent
// cloudflared diagnostic logging. Returns a no-op writer and a no-op closer on
// any error so callers never need to check.
func openCloudflaredLog() (io.Writer, func()) {
	home, err := os.UserHomeDir()
	if err != nil {
		return io.Discard, func() {}
	}
	dir := filepath.Join(home, ".voci")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return io.Discard, func() {}
	}
	f, err := os.OpenFile(filepath.Join(dir, "cloudflared.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return io.Discard, func() {}
	}
	return f, func() { f.Close() }
}

// run is the testable entry point.
func run(
	args []string,
	stdout io.Writer,
	stdin io.Reader,
	transcribeFn TranscribeFn,
	hintedFn func(ctx context.Context, raw, hint string, chatFn pipeline.ChatFn) (string, error),
	rewriteFnOpt RewriteFn,
	classifyFn ClassifyFn,
	gateFn GateFn,
	executeFn ExecuteFn,
	injectFn InjectFn,
	startMCPServerFn StartMCPServerFn,
	buildHintFn BuildHintFn,
	deliverFn func(model.ActionProposal) error,
	startDaemonFn StartDaemonFn,
	startServeFn StartServeFn,
	startManagedTunnelFn StartManagedTunnelFn,
) error {
	fs := flag.NewFlagSet("voci", flag.ContinueOnError)
	fs.SetOutput(stdout)

	fileFlag := fs.String("file", "", "path to audio WAV file (required)")
	iterateFlag := fs.Bool("iterate", false, "enter iterative feedback loop after initial output")
	noGateFlag := fs.Bool("no-gate", false, "skip human confirmation gate (test only)")
	sessionFlag := fs.String("session", "separate", "session mode: separate|integrated")
	inputFlag := fs.String("input", "preview", "input mode: preview|direct")
	tmuxTargetFlag := fs.String("tmux-target", "", "tmux pane target (e.g. session:window.pane)")
	mcpPortFlag := fs.Int("mcp-port", 9473, "port for MCP server (used with --session=integrated)")
	daemonFlag := fs.Bool("daemon", false, "run as HTTP daemon accepting audio uploads")
	daemonPortFlag := fs.Int("daemon-port", 9474, "port for daemon HTTP server (used with --daemon)")
	eventsPathFlag := fs.String("events-path", "", "path to event log file (default: ~/.voci/events.log)")
	serveFlag := fs.Bool("serve", false, "run as Monitor-host server; writes event lines to stdout")
	servePortFlag := fs.Int("serve-port", 9474, "port for serve HTTP server (used with --serve)")
	serveHostFlag := fs.String("serve-host", "127.0.0.1", "bind host for serve HTTP server (use 0.0.0.0 for LAN access)")
	shareFlag := fs.Bool("share", false, "expose serve port via Cloudflare Quick Tunnel")
	shareAuthFlag := fs.String("share-auth", "", "Bearer token for --share (auto-generated if empty)")
	lockDirFlag := fs.String("lock-dir", "", "directory for per-session lock files (empty = no lock)")
	sessionIDFlag := fs.String("session-id", "", "session ID for lock file (auto-generated if --lock-dir set and empty)")

	if err := fs.Parse(args); err != nil {
		return err
	}

	// buildHint uses the injected BuildHintFn if provided, otherwise falls back to BuildContext.
	buildHint := func(root string) string {
		if buildHintFn != nil {
			return buildHintFn(root)
		}
		return vocicontext.BuildContext(root, nil)
	}

	// --serve: Monitor-host mode; writes one JSON event line per utterance to stdout.
	if *serveFlag {
		serveCtx, serveCancel := daemon.WithSignalCancel(context.Background())
		defer serveCancel()

		addr := fmt.Sprintf("%s:%d", *serveHostFlag, *servePortFlag)
		perCallHint := func() string {
			cwd, err := os.Getwd()
			if err != nil {
				cwd = "."
			}
			return buildHint(cwd)
		}

		if startServeFn != nil {
			return startServeFn(addr, stdout, perCallHint)
		}

		// Per-session lock file management.
		// The lock is written inside OnListening (after the real port is known) and
		// removed via defer when StartWithContext returns (clean shutdown or context cancel).
		lockDir := *lockDirFlag
		sessionID := *sessionIDFlag
		if lockDir != "" {
			if sessionID == "" {
				sessionID = session.NewSessionID()
			}
			if err := session.SweepStaleLocks(lockDir); err != nil {
				return fmt.Errorf("sweep stale locks: %w", err)
			}
			defer session.RemoveLock(lockDir, sessionID) //nolint:errcheck
		}

		// Default: build real Server with stdout as the event sink.
		cfg, err := config.LoadConfig()
		if err != nil {
			return fmt.Errorf("config: %w", err)
		}
		var chatFn pipeline.ChatFn
		if cfg.ASRProvider == "gemini" && cfg.ASRAPIKey != "" {
			chatFn = func(ctx context.Context, messages []ollama.Message) (string, error) {
				roles := make([]string, len(messages))
				contents := make([]string, len(messages))
				for i, m := range messages {
					roles[i] = m.Role
					contents[i] = m.Content
				}
				return asr.GeminiChat(ctx, cfg.ASRAPIKey, cfg.ASRModel, roles, contents)
			}
		} else {
			chatFn = func(ctx context.Context, messages []ollama.Message) (string, error) {
				return ollama.Chat(ctx, cfg.OllamaHost, "gemma4:e4b", messages)
			}
		}
		if transcribeFn == nil {
			transcribeFn = func(ctx context.Context, key, audioPath, apiURL, language string, entities []string) string {
				return asr.Transcribe(ctx, key, audioPath, apiURL, language, cfg.ASRProvider, cfg.ASRModel, entities)
			}
		}
		if hintedFn == nil {
			hintedFn = pipeline.RunHinted
		}
		// --serve path intentionally skips Rewrite (RewriteFn stays nil so server.go's nil-guard skips it)
		if classifyFn == nil {
			classifyFn = func(ctx context.Context, rewritten, fullContext string, chat pipeline.ChatFn) (model.ActionProposal, error) {
				return intent.Classify(ctx, rewritten, fullContext, chat)
			}
		}
		ccAdapter := adapter.NewClaudeCodeAdapter(os.Getenv("TMUX_PANE"), "")
		serveHint := func() string {
			cwd, err := os.Getwd()
			if err != nil {
				cwd = "."
			}
			src, discErr := ccAdapter.DiscoverContext()
			if discErr != nil || src == nil {
				return vocicontext.BuildContext(cwd, nil)
			}
			return vocicontext.BuildContextWithSource(cwd, src, nil)
		}
		srv := &daemon.Server{
			TranscribeFn: daemon.TranscribeFn(transcribeFn),
			HintedFn:     daemon.HintedFn(hintedFn),
			RewriteFn:    daemon.RewriteFn(rewriteFnOpt),
			ClassifyFn:   daemon.ClassifyFn(classifyFn),
			BuildHintFn:  serveHint,
			HintFn: func(_ context.Context) (string, error) {
				return serveHint(), nil
			},
			ChatFn:      chatFn,
			APIKey:      cfg.ASRAPIKey,
			Language:    cfg.Language,
			EventWriter: os.Stdout,
			EventPath:   *eventsPathFlag,
		}
		if cfg.ASRProvider == "gemini" && cfg.ASRAPIKey != "" {
			apiKey := cfg.ASRAPIKey
			asrModel := cfg.ASRModel
			srv.MergedFn = func(ctx context.Context, key, audioPath, hintStr, language string, entities []string) (model.ActionProposal, error) {
				return asr.TranscribeMerged(ctx, apiKey, audioPath, hintStr, language, asrModel, entities)
			}
		}
		if testOnServerBuilt != nil {
			testOnServerBuilt(srv)
		}
		srv.OnListening = func(a net.Addr) {
			fmt.Fprintf(os.Stderr, "voci serve: listening on %s\n", a.String())
			if lockDir != "" {
				_, portStr, _ := net.SplitHostPort(a.String())
				port, _ := strconv.Atoi(portStr)
				if err := session.WriteLock(lockDir, sessionID, os.Getpid(), port); err != nil {
					fmt.Fprintf(os.Stderr, "voci serve: WriteLock: %v\n", err)
				}
			}
		}
		if *shareFlag {
			token := *shareAuthFlag
			if token == "" {
				var genErr error
				token, genErr = auth.GenerateToken()
				if genErr != nil {
					return fmt.Errorf("generate token: %w", genErr)
				}
			}
			srv.BearerToken = token
			tunnelCtx, tunnelCancel := context.WithCancel(serveCtx)
			defer tunnelCancel()
			tunnelLogW, tunnelLogClose := openCloudflaredLog()
			defer tunnelLogClose()
			logW := io.MultiWriter(os.Stderr, tunnelLogW)

			// Pre-bind the listener so we know the real OS-assigned port before
			// starting cloudflared. When --serve-port 0 is used the configured
			// addr contains port "0"; cloudflared must receive the actual port.
			ln, listenErr := net.Listen("tcp", addr)
			if listenErr != nil {
				return fmt.Errorf("--share: listen: %w", listenErr)
			}
			_, portStr, _ := net.SplitHostPort(ln.Addr().String())
			port, _ := strconv.Atoi(portStr)

			cfToken := firstNonEmpty(os.Getenv("CLOUDFLARE_API_TOKEN"), cfg.CloudflareAPIToken)
			cfAccount := firstNonEmpty(os.Getenv("CF_ACCOUNT_ID"), cfg.CloudflareAccountID)
			cfZone := firstNonEmpty(os.Getenv("CF_ZONE_ID"), cfg.CloudflareZoneID)
			cfDomain := firstNonEmpty(os.Getenv("CF_TUNNEL_DOMAIN"), cfg.CloudflareTunnelDomain)

			var tunnelCmd *exec.Cmd
			var publicURL string
			var tunnelErr error
			if cfToken != "" && cfAccount != "" && cfZone != "" && cfDomain != "" {
				managedCfg := tunnel.ManagedTunnelConfig{
					APIToken:     cfToken,
					AccountID:    cfAccount,
					ZoneID:       cfZone,
					TunnelDomain: cfDomain,
					TTL:          20 * time.Hour,
				}
				managedFn := startManagedTunnelFn
				if managedFn == nil {
					managedFn = tunnel.StartManagedTunnel
				}
				tunnelCmd, publicURL, tunnelErr = managedFn(tunnelCtx, managedCfg, port, logW)
			} else {
				tunnelCmd, publicURL, tunnelErr = tunnel.StartTunnel(tunnelCtx, port, logW)
			}
			if tunnelErr != nil {
				return fmt.Errorf("--share: %w", tunnelErr)
			}
			defer tunnelCmd.Process.Kill()
			tunnel.WatchTunnel(tunnelCmd, tunnelCancel)
			fmt.Fprintf(os.Stderr, "voci local URL: http://127.0.0.1:%d\n", port)
			fmt.Fprintf(os.Stderr, "voci share URL: %s\n", publicURL)
			fmt.Fprintf(os.Stderr, "Bearer token:   %s\n", token)
			fmt.Fprintf(os.Stderr, "Note: audio and transcriptions route through Cloudflare infrastructure.\n")
			if lockDir != "" {
				if err := session.WriteStatus(lockDir, sessionID, fmt.Sprintf("http://127.0.0.1:%d", port), publicURL, token); err != nil {
					fmt.Fprintf(os.Stderr, "voci: warning: failed to write status file: %v\n", err)
				}
				defer session.RemoveStatus(lockDir, sessionID) //nolint:errcheck
			}
			return srv.StartWithContextFromListener(tunnelCtx, ln)
		}
		return srv.StartWithContext(serveCtx, addr)
	}

	// --daemon: start HTTP daemon accepting audio uploads, no --file required
	if *daemonFlag {
		fmt.Fprintln(stdout, "voci: --daemon is deprecated; use 'voci serve' (see TASK-16)")
		eventsPath := *eventsPathFlag
		if eventsPath == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				home = "."
			}
			eventsPath = filepath.Join(home, ".voci", "events.log")
		}
		addr := fmt.Sprintf("127.0.0.1:%d", *daemonPortFlag)

		if startDaemonFn != nil {
			return startDaemonFn(addr, eventsPath, func() string {
				cwd, err := os.Getwd()
				if err != nil {
					cwd = "."
				}
				return buildHint(cwd)
			})
		}

		// Default daemon implementation
		cfg, err := config.LoadConfig()
		if err != nil {
			return fmt.Errorf("config: %w", err)
		}
		chatFn := func(ctx context.Context, messages []ollama.Message) (string, error) {
			return ollama.Chat(ctx, cfg.OllamaHost, "gemma4:e4b", messages)
		}
		if transcribeFn == nil {
			transcribeFn = func(ctx context.Context, key, audioPath, apiURL, language string, entities []string) string {
				return asr.Transcribe(ctx, key, audioPath, apiURL, language, cfg.ASRProvider, cfg.ASRModel, entities)
			}
		}
		if hintedFn == nil {
			hintedFn = pipeline.RunHinted
		}
		if rewriteFnOpt == nil {
			rewriteFnOpt = pipeline.Rewrite
		}
		if classifyFn == nil {
			classifyFn = func(ctx context.Context, rewritten, fullContext string, chat pipeline.ChatFn) (model.ActionProposal, error) {
				return intent.Classify(ctx, rewritten, fullContext, chat)
			}
		}

		ccAdapter := adapter.NewClaudeCodeAdapter(os.Getenv("TMUX_PANE"), "")
		buildHint := func() string {
			cwd, err := os.Getwd()
			if err != nil {
				cwd = "."
			}
			src, discErr := ccAdapter.DiscoverContext()
			if discErr != nil || src == nil {
				return vocicontext.BuildContext(cwd, nil)
			}
			return vocicontext.BuildContextWithSource(cwd, src, nil)
		}
		srv := &daemon.Server{
			TranscribeFn: daemon.TranscribeFn(transcribeFn),
			HintedFn:     daemon.HintedFn(hintedFn),
			RewriteFn:    daemon.RewriteFn(rewriteFnOpt),
			ClassifyFn:   daemon.ClassifyFn(classifyFn),
			BuildHintFn:  buildHint,
			HintFn: func(_ context.Context) (string, error) {
				return buildHint(), nil
			},
			ChatFn:    chatFn,
			APIKey:    cfg.ASRAPIKey,
			Language:  cfg.Language,
			EventPath: eventsPath,
		}
		return srv.Start(addr)
	}

	// --session=integrated: start MCP server, no --file required
	if *sessionFlag == "integrated" {
		addr := fmt.Sprintf("127.0.0.1:%d", *mcpPortFlag)
		if startMCPServerFn == nil {
			cfg, err := config.LoadConfig()
			if err != nil {
				return fmt.Errorf("config: %w", err)
			}
			cwd, err := os.Getwd()
			if err != nil {
				cwd = "."
			}
			hint := buildHint(cwd)
			chatFn := func(ctx context.Context, messages []ollama.Message) (string, error) {
				return ollama.Chat(ctx, cfg.OllamaHost, "gemma4:e4b", messages)
			}
			if transcribeFn == nil {
				transcribeFn = func(ctx context.Context, key, audioPath, apiURL, language string, entities []string) string {
					return asr.Transcribe(ctx, key, audioPath, apiURL, language, cfg.ASRProvider, cfg.ASRModel, entities)
				}
			}
			if hintedFn == nil {
				hintedFn = pipeline.RunHinted
			}
			if rewriteFnOpt == nil {
				rewriteFnOpt = pipeline.Rewrite
			}
			if classifyFn == nil {
				classifyFn = func(ctx context.Context, rewritten, fullContext string, chat pipeline.ChatFn) (model.ActionProposal, error) {
					return intent.Classify(ctx, rewritten, fullContext, chat)
				}
			}
			startMCPServerFn = func(addr string) error {
				srv := mcp.NewServer(
					mcp.TranscribeFn(transcribeFn),
					mcp.HintedFn(hintedFn),
					mcp.RewriteFn(rewriteFnOpt),
					mcp.ClassifyFn(classifyFn),
					cfg.ASRAPIKey,
					chatFn,
					hint,
					cfg.Language,
				)
				return srv.Start(addr)
			}
		}
		return startMCPServerFn(addr)
	}

	if *fileFlag == "" {
		return fmt.Errorf("--file is required")
	}

	if _, err := os.Stat(*fileFlag); err != nil {
		return fmt.Errorf("audio file not found: %w", err)
	}

	cfg, err := config.LoadConfig()
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}

	ctx := context.Background()

	// Stage 1: Build context hint
	cwd, err := os.Getwd()
	if err != nil {
		cwd = "."
	}
	hint := buildHint(cwd)

	// Build chat function
	chatFn := func(ctx context.Context, messages []ollama.Message) (string, error) {
		return ollama.Chat(ctx, cfg.OllamaHost, "gemma4:e4b", messages)
	}

	// Use injected or default functions
	if transcribeFn == nil {
		transcribeFn = func(ctx context.Context, key, audioPath, apiURL, language string, entities []string) string {
			return asr.Transcribe(ctx, key, audioPath, apiURL, language, cfg.ASRProvider, cfg.ASRModel, entities)
		}
	}
	if hintedFn == nil {
		hintedFn = pipeline.RunHinted
	}
	if rewriteFnOpt == nil {
		rewriteFnOpt = pipeline.Rewrite
	}
	if classifyFn == nil {
		classifyFn = func(ctx context.Context, rewritten, fullContext string, chat pipeline.ChatFn) (model.ActionProposal, error) {
			return intent.Classify(ctx, rewritten, fullContext, chat)
		}
	}
	if gateFn == nil {
		gateFn = gate.Run
	}
	if executeFn == nil {
		executeFn = func(p model.ActionProposal) (string, error) {
			ex := &executor.DefaultExecutor{CmdRunner: defaultCmdRunner, Confirmed: true}
			return ex.Execute(p)
		}
	}
	if injectFn == nil {
		target := *tmuxTargetFlag
		if target == "" {
			target = os.Getenv("TMUX_PANE")
		}
		inj := inject.NewDefaultInjector(target)
		injectFn = inj.Inject
	}

	// Stage 2: ASR transcription
	entities := asr.ExtractEntities(hint)
	raw := transcribeFn(ctx, cfg.ASRAPIKey, *fileFlag, "", cfg.Language, entities)

	// Stage 3: Hinted correction
	hinted, err := hintedFn(ctx, raw, hint, chatFn)
	if err != nil {
		return fmt.Errorf("hinted: %w", err)
	}

	// Stage 4: Rewrite
	rewritten, err := rewriteFnOpt(ctx, hinted, hint, chatFn)
	if err != nil {
		return fmt.Errorf("rewrite: %w", err)
	}

	// Stage 5: Output
	output.PrintComparison(stdout, raw, hinted, rewritten)

	// Stage 5b: Iterate (existing functionality)
	if *iterateFlag {
		rewriteWithFeedback := pipeline.RewriteWithFeedbackFn(rewriteFnOpt)
		if err := pipeline.IterateLoop(ctx, rewritten, hint, stdin, stdout, chatFn, rewriteWithFeedback); err != nil {
			return fmt.Errorf("iterate: %w", err)
		}
	}

	// Stage 6: Classify intent
	proposal, err := classifyFn(ctx, rewritten, hint, chatFn)
	if err != nil {
		return fmt.Errorf("classify: %w", err)
	}

	// Stage 6b: Session/input routing
	if *inputFlag == "direct" && (proposal.Kind == model.KindDirectPrompt || proposal.Kind == model.KindQuery) {
		if deliverFn != nil {
			return deliverFn(proposal)
		} else if injectFn != nil {
			return injectFn(proposal.Rewritten)
		}
		return nil
	}

	// Stage 7: Human gate (skipped with --no-gate)
	if !*noGateFlag {
		gate.PrintSummary(stdout, proposal)
		result := gateFn(stdin, stdout, proposal)
		if result.Action == "discard" {
			fmt.Fprintln(stdout, "Discarded.")
			return nil
		}
		if result.Action == "edit" {
			proposal.Rewritten = result.EditedText
		}
	}

	// Stage 8: Execute
	execResult, err := executeFn(proposal)
	if err != nil {
		return fmt.Errorf("execute: %w", err)
	}

	// Stage 9: Print execution result
	if execResult != "" {
		fmt.Fprintln(stdout, "RESULT:", execResult)
	}

	return nil
}
