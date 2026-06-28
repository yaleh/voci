package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/yalehu/voci/internal/gate"
	"github.com/yalehu/voci/internal/intent"
	"github.com/yalehu/voci/internal/ollama"
	"github.com/yalehu/voci/internal/pipeline"
)

func makeTempWav(t *testing.T) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "test-*.wav")
	if err != nil {
		t.Fatal(err)
	}
	f.Write([]byte("RIFF\x00\x00\x00\x00WAVEfmt "))
	f.Close()
	return f.Name()
}

func setTestEnv(t *testing.T) {
	t.Helper()
	t.Setenv("SILICONFLOW_API_KEY", "sk-test")
	t.Setenv("OLLAMA_HOST", "http://localhost:11434")
}

var fakeTranscribe TranscribeFn = func(ctx context.Context, key, audioPath, apiURL string) (string, error) {
	return "task one fix login bug", nil
}

var fakeHinted = func(ctx context.Context, raw, hint string, chatFn pipeline.ChatFn) (string, error) {
	return "TASK-1 fix login bug", nil
}

var fakeRewrite RewriteFn = func(ctx context.Context, hinted, hint string, chatFn pipeline.ChatFn) (string, error) {
	return "Fix login bug in TASK-1", nil
}

var fakeChatFn = func(ctx context.Context, messages []ollama.Message) (string, error) {
	return "ok", nil
}

var fakeClassify ClassifyFn = func(ctx context.Context, rewritten, fullContext string, chat pipeline.ChatFn) (intent.ActionProposal, error) {
	return intent.ActionProposal{
		Kind:       intent.KindDirectPrompt,
		Rewritten:  rewritten,
		Confidence: 0.9,
	}, nil
}

var fakeGateConfirm GateFn = func(r io.Reader, w io.Writer, proposal intent.ActionProposal) gate.GateResult {
	return gate.GateResult{Action: "confirm"}
}

var fakeGateDiscard GateFn = func(r io.Reader, w io.Writer, proposal intent.ActionProposal) gate.GateResult {
	return gate.GateResult{Action: "discard"}
}

var fakeExecute ExecuteFn = func(proposal intent.ActionProposal) (string, error) {
	return "executed", nil
}

func TestCLIFileFlagPrintsRAW(t *testing.T) {
	setTestEnv(t)
	wavPath := makeTempWav(t)

	var stdout bytes.Buffer
	err := run([]string{"--file", wavPath, "--no-gate"}, &stdout, strings.NewReader(""), fakeTranscribe, fakeHinted, fakeRewrite, fakeClassify, nil, fakeExecute, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stdout.String(), "RAW") {
		t.Errorf("expected RAW in output, got: %q", stdout.String())
	}
}

func TestCLIFileFlagPrintsHINTED(t *testing.T) {
	setTestEnv(t)
	wavPath := makeTempWav(t)

	var stdout bytes.Buffer
	err := run([]string{"--file", wavPath, "--no-gate"}, &stdout, strings.NewReader(""), fakeTranscribe, fakeHinted, fakeRewrite, fakeClassify, nil, fakeExecute, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stdout.String(), "HINTED") {
		t.Errorf("expected HINTED in output, got: %q", stdout.String())
	}
}

func TestCLIFileFlagPrintsREWRITTEN(t *testing.T) {
	setTestEnv(t)
	wavPath := makeTempWav(t)

	var stdout bytes.Buffer
	err := run([]string{"--file", wavPath, "--no-gate"}, &stdout, strings.NewReader(""), fakeTranscribe, fakeHinted, fakeRewrite, fakeClassify, nil, fakeExecute, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stdout.String(), "REWRITTEN") {
		t.Errorf("expected REWRITTEN in output, got: %q", stdout.String())
	}
}

func TestCLINoFileExitsNonzero(t *testing.T) {
	setTestEnv(t)

	var stdout bytes.Buffer
	err := run([]string{}, &stdout, strings.NewReader(""), fakeTranscribe, fakeHinted, fakeRewrite, fakeClassify, fakeGateConfirm, fakeExecute, nil)
	if err == nil {
		t.Fatal("expected error for missing --file")
	}
}

func TestCLIFileMissingExitsNonzero(t *testing.T) {
	setTestEnv(t)

	var stdout bytes.Buffer
	err := run([]string{"--file", "/nonexistent.wav"}, &stdout, strings.NewReader(""), fakeTranscribe, fakeHinted, fakeRewrite, fakeClassify, fakeGateConfirm, fakeExecute, nil)
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestCLIIterateFlagAccepted(t *testing.T) {
	setTestEnv(t)
	wavPath := makeTempWav(t)

	var stdout bytes.Buffer
	// Empty stdin means iterate loop exits immediately; --no-gate skips interactive gate
	err := run([]string{"--file", wavPath, "--iterate", "--no-gate"}, &stdout, strings.NewReader(""), fakeTranscribe, fakeHinted, fakeRewrite, fakeClassify, nil, fakeExecute, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunFullPipelineWithGate(t *testing.T) {
	setTestEnv(t)
	wavPath := makeTempWav(t)

	classifyCalled := false
	gateCalled := false
	executeCalled := false

	classifyFn := ClassifyFn(func(ctx context.Context, rewritten, fullContext string, chat pipeline.ChatFn) (intent.ActionProposal, error) {
		classifyCalled = true
		return intent.ActionProposal{
			Kind:       intent.KindDirectPrompt,
			Rewritten:  rewritten,
			Confidence: 0.9,
		}, nil
	})

	gateFn := GateFn(func(r io.Reader, w io.Writer, proposal intent.ActionProposal) gate.GateResult {
		gateCalled = true
		return gate.GateResult{Action: "confirm"}
	})

	executeFn := ExecuteFn(func(proposal intent.ActionProposal) (string, error) {
		executeCalled = true
		return "executed", nil
	})

	var stdout bytes.Buffer
	err := run(
		[]string{"--file", wavPath},
		&stdout, strings.NewReader(""),
		fakeTranscribe, fakeHinted, fakeRewrite,
		classifyFn, gateFn, executeFn, nil,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !classifyCalled {
		t.Error("expected classifyFn to be called")
	}
	if !gateCalled {
		t.Error("expected gateFn to be called")
	}
	if !executeCalled {
		t.Error("expected executeFn to be called")
	}
	if !strings.Contains(stdout.String(), "executed") {
		t.Errorf("expected 'executed' in output, got: %q", stdout.String())
	}
}

func TestRunFullPipelineGateDiscard(t *testing.T) {
	setTestEnv(t)
	wavPath := makeTempWav(t)

	executeCalled := false

	executeFn := ExecuteFn(func(proposal intent.ActionProposal) (string, error) {
		executeCalled = true
		return "executed", nil
	})

	var stdout bytes.Buffer
	err := run(
		[]string{"--file", wavPath},
		&stdout, strings.NewReader(""),
		fakeTranscribe, fakeHinted, fakeRewrite,
		fakeClassify, fakeGateDiscard, executeFn, nil,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if executeCalled {
		t.Error("expected executeFn NOT to be called when gate discards")
	}
	if !strings.Contains(stdout.String(), "Discarded") {
		t.Errorf("expected 'Discarded' in output, got: %q", stdout.String())
	}
}

func TestCLINoGateFlagSkipsGate(t *testing.T) {
	setTestEnv(t)
	wavPath := makeTempWav(t)

	gateCalled := false
	gateFn := GateFn(func(r io.Reader, w io.Writer, proposal intent.ActionProposal) gate.GateResult {
		gateCalled = true
		return gate.GateResult{Action: "confirm"}
	})

	var stdout bytes.Buffer
	err := run(
		[]string{"--file", wavPath, "--no-gate"},
		&stdout, strings.NewReader(""),
		fakeTranscribe, fakeHinted, fakeRewrite,
		fakeClassify, gateFn, fakeExecute, nil,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gateCalled {
		t.Error("expected gateFn NOT to be called when --no-gate is set")
	}
}

func TestRun_SessionFlag_Defaults(t *testing.T) {
	setTestEnv(t)
	wavPath := makeTempWav(t)

	var stdout bytes.Buffer
	err := run(
		[]string{"--file", wavPath, "--no-gate"},
		&stdout, strings.NewReader(""),
		fakeTranscribe, fakeHinted, fakeRewrite,
		fakeClassify, nil, fakeExecute, nil,
	)
	if err != nil {
		t.Fatalf("unexpected error with default session/input flags: %v", err)
	}
}

func TestRun_InputDirect_KindDirectPrompt_SkipsGate(t *testing.T) {
	setTestEnv(t)
	wavPath := makeTempWav(t)

	gateCalled := false
	injectCalled := false
	var injectedText string

	gateFn := GateFn(func(r io.Reader, w io.Writer, proposal intent.ActionProposal) gate.GateResult {
		gateCalled = true
		return gate.GateResult{Action: "confirm"}
	})

	classifyFn := ClassifyFn(func(ctx context.Context, rewritten, fullContext string, chat pipeline.ChatFn) (intent.ActionProposal, error) {
		return intent.ActionProposal{Kind: intent.KindDirectPrompt, Rewritten: rewritten, Confidence: 0.9}, nil
	})

	injectFn := InjectFn(func(text string) error {
		injectCalled = true
		injectedText = text
		return nil
	})

	var stdout bytes.Buffer
	err := run(
		[]string{"--file", wavPath, "--input=direct"},
		&stdout, strings.NewReader(""),
		fakeTranscribe, fakeHinted, fakeRewrite,
		classifyFn, gateFn, fakeExecute, injectFn,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gateCalled {
		t.Error("expected gateFn NOT to be called for KindDirectPrompt with --input=direct")
	}
	if !injectCalled {
		t.Error("expected injectFn to be called for KindDirectPrompt with --input=direct")
	}
	if injectedText == "" {
		t.Error("expected injected text to be non-empty")
	}
}

func TestRun_InputDirect_KindQuery_SkipsGate(t *testing.T) {
	setTestEnv(t)
	wavPath := makeTempWav(t)

	gateCalled := false
	injectCalled := false

	gateFn := GateFn(func(r io.Reader, w io.Writer, proposal intent.ActionProposal) gate.GateResult {
		gateCalled = true
		return gate.GateResult{Action: "confirm"}
	})

	classifyFn := ClassifyFn(func(ctx context.Context, rewritten, fullContext string, chat pipeline.ChatFn) (intent.ActionProposal, error) {
		return intent.ActionProposal{Kind: intent.KindQuery, Rewritten: rewritten, Confidence: 0.9}, nil
	})

	injectFn := InjectFn(func(text string) error {
		injectCalled = true
		return nil
	})

	var stdout bytes.Buffer
	err := run(
		[]string{"--file", wavPath, "--input=direct"},
		&stdout, strings.NewReader(""),
		fakeTranscribe, fakeHinted, fakeRewrite,
		classifyFn, gateFn, fakeExecute, injectFn,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gateCalled {
		t.Error("expected gateFn NOT to be called for KindQuery with --input=direct")
	}
	if !injectCalled {
		t.Error("expected injectFn to be called for KindQuery with --input=direct")
	}
}

func TestRun_InputDirect_KindBacklogAction_UsesGate(t *testing.T) {
	setTestEnv(t)
	wavPath := makeTempWav(t)

	gateCalled := false
	injectCalled := false

	gateFn := GateFn(func(r io.Reader, w io.Writer, proposal intent.ActionProposal) gate.GateResult {
		gateCalled = true
		return gate.GateResult{Action: "confirm"}
	})

	classifyFn := ClassifyFn(func(ctx context.Context, rewritten, fullContext string, chat pipeline.ChatFn) (intent.ActionProposal, error) {
		return intent.ActionProposal{Kind: intent.KindBacklogAction, Rewritten: rewritten, Confidence: 0.9}, nil
	})

	injectFn := InjectFn(func(text string) error {
		injectCalled = true
		return nil
	})

	var stdout bytes.Buffer
	err := run(
		[]string{"--file", wavPath, "--input=direct"},
		&stdout, strings.NewReader(""),
		fakeTranscribe, fakeHinted, fakeRewrite,
		classifyFn, gateFn, fakeExecute, injectFn,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !gateCalled {
		t.Error("expected gateFn to be called for KindBacklogAction even with --input=direct")
	}
	if injectCalled {
		t.Error("expected injectFn NOT to be called for KindBacklogAction")
	}
}

func TestRun_InputDirect_KindAmbiguous_UsesGate(t *testing.T) {
	setTestEnv(t)
	wavPath := makeTempWav(t)

	gateCalled := false
	injectCalled := false

	gateFn := GateFn(func(r io.Reader, w io.Writer, proposal intent.ActionProposal) gate.GateResult {
		gateCalled = true
		return gate.GateResult{Action: "confirm"}
	})

	classifyFn := ClassifyFn(func(ctx context.Context, rewritten, fullContext string, chat pipeline.ChatFn) (intent.ActionProposal, error) {
		return intent.ActionProposal{Kind: intent.KindAmbiguous, Rewritten: rewritten, Confidence: 0.3}, nil
	})

	injectFn := InjectFn(func(text string) error {
		injectCalled = true
		return nil
	})

	var stdout bytes.Buffer
	err := run(
		[]string{"--file", wavPath, "--input=direct"},
		&stdout, strings.NewReader(""),
		fakeTranscribe, fakeHinted, fakeRewrite,
		classifyFn, gateFn, fakeExecute, injectFn,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !gateCalled {
		t.Error("expected gateFn to be called for KindAmbiguous even with --input=direct")
	}
	if injectCalled {
		t.Error("expected injectFn NOT to be called for KindAmbiguous")
	}
}

func TestRun_SessionIntegrated_ReturnsError(t *testing.T) {
	setTestEnv(t)
	wavPath := makeTempWav(t)

	var stdout bytes.Buffer
	err := run(
		[]string{"--file", wavPath, "--session=integrated"},
		&stdout, strings.NewReader(""),
		fakeTranscribe, fakeHinted, fakeRewrite,
		fakeClassify, fakeGateConfirm, fakeExecute, nil,
	)
	if err == nil {
		t.Fatal("expected error for --session=integrated (not yet implemented)")
	}
}
