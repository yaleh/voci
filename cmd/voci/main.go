package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/yalehu/voci/internal/asr"
	"github.com/yalehu/voci/internal/config"
	vocicontext "github.com/yalehu/voci/internal/context"
	"github.com/yalehu/voci/internal/ollama"
	"github.com/yalehu/voci/internal/output"
	"github.com/yalehu/voci/internal/pipeline"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stdin, nil, nil, nil); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// Dependency types for testing
type TranscribeFn func(ctx context.Context, key, audioPath, apiURL string) (string, error)
type RewriteFn func(ctx context.Context, hinted, hint string, chatFn pipeline.ChatFn) (string, error)

// run is the testable entry point.
func run(
	args []string,
	stdout io.Writer,
	stdin io.Reader,
	transcribeFn TranscribeFn,
	hintedFn func(ctx context.Context, raw, hint string, chatFn pipeline.ChatFn) (string, error),
	rewriteFnOpt RewriteFn,
) error {
	fs := flag.NewFlagSet("voci", flag.ContinueOnError)
	fs.SetOutput(stdout)

	fileFlag := fs.String("file", "", "path to audio WAV file (required)")
	iterateFlag := fs.Bool("iterate", false, "enter iterative feedback loop after initial output")

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

	// Stage 6: Iterate
	if *iterateFlag {
		rewriteWithFeedback := pipeline.RewriteWithFeedbackFn(rewriteFnOpt)
		if err := pipeline.IterateLoop(ctx, rewritten, hint, stdin, stdout, chatFn, rewriteWithFeedback); err != nil {
			return fmt.Errorf("iterate: %w", err)
		}
	}

	return nil
}
