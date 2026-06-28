package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"

	"github.com/yalehu/voci/internal/asr"
	"github.com/yalehu/voci/internal/config"
	vocicontext "github.com/yalehu/voci/internal/context"
	"github.com/yalehu/voci/internal/executor"
	"github.com/yalehu/voci/internal/gate"
	"github.com/yalehu/voci/internal/inject"
	"github.com/yalehu/voci/internal/intent"
	"github.com/yalehu/voci/internal/ollama"
	"github.com/yalehu/voci/internal/output"
	"github.com/yalehu/voci/internal/pipeline"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stdin, nil, nil, nil, nil, nil, nil, nil); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// Dependency types for testing
type TranscribeFn func(ctx context.Context, key, audioPath, apiURL string) (string, error)
type RewriteFn func(ctx context.Context, hinted, hint string, chatFn pipeline.ChatFn) (string, error)
type ClassifyFn func(ctx context.Context, rewritten, fullContext string, chat pipeline.ChatFn) (intent.ActionProposal, error)
type GateFn func(r io.Reader, w io.Writer, proposal intent.ActionProposal) gate.GateResult
type ExecuteFn func(proposal intent.ActionProposal) (string, error)
type InjectFn func(text string) error

// defaultCmdRunner runs an external command and returns its combined output.
func defaultCmdRunner(name string, args ...string) (string, error) {
	out, err := exec.Command(name, args...).Output()
	return string(out), err
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
) error {
	fs := flag.NewFlagSet("voci", flag.ContinueOnError)
	fs.SetOutput(stdout)

	fileFlag := fs.String("file", "", "path to audio WAV file (required)")
	iterateFlag := fs.Bool("iterate", false, "enter iterative feedback loop after initial output")
	noGateFlag := fs.Bool("no-gate", false, "skip human confirmation gate (test only)")
	sessionFlag := fs.String("session", "separate", "session mode: separate|integrated")
	inputFlag := fs.String("input", "preview", "input mode: preview|direct")
	tmuxTargetFlag := fs.String("tmux-target", "", "tmux pane target (e.g. session:window.pane)")

	if err := fs.Parse(args); err != nil {
		return err
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
	hint := vocicontext.BuildContext(cwd, nil)

	// Build chat function
	chatFn := func(ctx context.Context, messages []ollama.Message) (string, error) {
		return ollama.Chat(ctx, cfg.OllamaHost, "gemma4:e4b", messages)
	}

	// Use injected or default functions
	if transcribeFn == nil {
		transcribeFn = asr.Transcribe
	}
	if hintedFn == nil {
		hintedFn = pipeline.RunHinted
	}
	if rewriteFnOpt == nil {
		rewriteFnOpt = pipeline.Rewrite
	}
	if classifyFn == nil {
		classifyFn = func(ctx context.Context, rewritten, fullContext string, chat pipeline.ChatFn) (intent.ActionProposal, error) {
			return intent.Classify(ctx, rewritten, fullContext, chat)
		}
	}
	if gateFn == nil {
		gateFn = gate.Run
	}
	if executeFn == nil {
		executeFn = func(p intent.ActionProposal) (string, error) {
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
	raw, err := transcribeFn(ctx, cfg.SiliconFlowKey, *fileFlag, "")
	if err != nil {
		return fmt.Errorf("ASR: %w", err)
	}

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
	if *sessionFlag == "integrated" {
		return fmt.Errorf("--session=integrated not yet implemented (see TASK-4.3)")
	}
	if *inputFlag == "direct" && (proposal.Kind == intent.KindDirectPrompt || proposal.Kind == intent.KindQuery) {
		if injectFn != nil {
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
