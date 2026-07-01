package wire

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/yaleh/voci/internal/daemon"
	"github.com/yaleh/voci/internal/daemon/session"
	"github.com/yaleh/voci/internal/daemon/tunnel"
	"github.com/yaleh/voci/internal/pipeline"
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

var fakeTranscribe TranscribeFn = func(ctx context.Context, key, audioPath, apiURL, language string, entities []string) string {
	return "task one fix login bug"
}

var fakeHinted = func(ctx context.Context, raw, hint string, chatFn pipeline.ChatFn) (string, error) {
	return "TASK-1 fix login bug", nil
}

var fakeRewrite RewriteFn = func(ctx context.Context, hinted, hint string, chatFn pipeline.ChatFn) (string, error) {
	return "Fix login bug in TASK-1", nil
}

// noopInject is a safe no-op injector for tests — it never executes real commands.
var noopInject = InjectFn(func(text string) error { return nil })

func TestCLIFileFlagPrintsRAW(t *testing.T) {
	setTestEnv(t)
	wavPath := makeTempWav(t)

	var stdout bytes.Buffer
	err := run([]string{"--file", wavPath}, &stdout, strings.NewReader(""), fakeTranscribe, fakeHinted, fakeRewrite, noopInject, nil, nil, nil, nil)
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
	err := run([]string{"--file", wavPath}, &stdout, strings.NewReader(""), fakeTranscribe, fakeHinted, fakeRewrite, noopInject, nil, nil, nil, nil)
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
	err := run([]string{"--file", wavPath}, &stdout, strings.NewReader(""), fakeTranscribe, fakeHinted, fakeRewrite, noopInject, nil, nil, nil, nil)
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
	err := run([]string{}, &stdout, strings.NewReader(""), fakeTranscribe, fakeHinted, fakeRewrite, noopInject, nil, nil, nil, nil)
	if err == nil {
		t.Fatal("expected error for missing --file")
	}
}

func TestCLIFileMissingExitsNonzero(t *testing.T) {
	setTestEnv(t)

	var stdout bytes.Buffer
	err := run([]string{"--file", "/nonexistent.wav"}, &stdout, strings.NewReader(""), fakeTranscribe, fakeHinted, fakeRewrite, noopInject, nil, nil, nil, nil)
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestCLIIterateFlagAccepted(t *testing.T) {
	setTestEnv(t)
	wavPath := makeTempWav(t)

	var stdout bytes.Buffer
	// Empty stdin means iterate loop exits immediately.
	err := run([]string{"--file", wavPath, "--iterate"}, &stdout, strings.NewReader(""), fakeTranscribe, fakeHinted, fakeRewrite, noopInject, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCLIOnce_InjectsAfterRewrite(t *testing.T) {
	setTestEnv(t)
	wavPath := makeTempWav(t)

	injectCalled := false
	var injectedText string
	injectFn := InjectFn(func(text string) error {
		injectCalled = true
		injectedText = text
		return nil
	})

	var stdout bytes.Buffer
	err := run(
		[]string{"--file", wavPath},
		&stdout, strings.NewReader(""),
		fakeTranscribe, fakeHinted, fakeRewrite,
		injectFn, nil, nil, nil, nil,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !injectCalled {
		t.Error("expected injectFn to be called after rewrite (no gate/classify)")
	}
	if injectedText != "Fix login bug in TASK-1" {
		t.Errorf("expected injected text %q, got %q", "Fix login bug in TASK-1", injectedText)
	}
}

func TestRun_NoDaemonStillRequiresFile(t *testing.T) {
	setTestEnv(t)

	var stdout bytes.Buffer
	err := run(
		[]string{},
		&stdout, strings.NewReader(""),
		nil, nil, nil, nil, nil, nil, nil, nil,
	)
	if err == nil {
		t.Fatal("expected error for missing --file without --daemon")
	}
	if !strings.Contains(err.Error(), "--file is required") {
		t.Errorf("expected '--file is required' error, got: %v", err)
	}
}

func TestRun_ServeStartsServer(t *testing.T) {
	setTestEnv(t)

	serveCalled := false
	var calledAddr string
	startServeFn := StartServeFn(func(addr string, eventWriter io.Writer, buildHintFn func() string) error {
		serveCalled = true
		calledAddr = addr
		return nil
	})

	var stdout bytes.Buffer
	err := run(
		[]string{"--serve", "--serve-port=9475"},
		&stdout, strings.NewReader(""),
		nil, nil, nil, nil, nil, nil, startServeFn, nil,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !serveCalled {
		t.Error("expected startServeFn to be called")
	}
	if !strings.Contains(calledAddr, ":9475") {
		t.Errorf("expected addr to contain :9475, got: %s", calledAddr)
	}
}

func TestRun_ServeNoFileRequired(t *testing.T) {
	setTestEnv(t)

	startServeFn := StartServeFn(func(addr string, eventWriter io.Writer, buildHintFn func() string) error {
		return nil
	})

	var stdout bytes.Buffer
	err := run(
		[]string{"--serve"},
		&stdout, strings.NewReader(""),
		nil, nil, nil, nil, nil, nil, startServeFn, nil,
	)
	if err != nil {
		t.Fatalf("expected no error for --serve without --file, got: %v", err)
	}
}

func TestRun_ServeUsesStdoutSink(t *testing.T) {
	setTestEnv(t)

	var capturedWriter io.Writer
	startServeFn := StartServeFn(func(addr string, eventWriter io.Writer, buildHintFn func() string) error {
		capturedWriter = eventWriter
		return nil
	})

	var stdout bytes.Buffer
	err := run(
		[]string{"--serve"},
		&stdout, strings.NewReader(""),
		nil, nil, nil, nil, nil, nil, startServeFn, nil,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedWriter != &stdout {
		t.Errorf("expected eventWriter to be stdout, got %T", capturedWriter)
	}
}

func TestDispatch_ServeSubcommand(t *testing.T) {
	setTestEnv(t)

	serveCalled := false
	var calledAddr string
	startServeFn := StartServeFn(func(addr string, eventWriter io.Writer, buildHintFn func() string) error {
		serveCalled = true
		calledAddr = addr
		return nil
	})

	var stdout bytes.Buffer
	err := dispatch(
		[]string{"serve"},
		&stdout, strings.NewReader(""),
		nil, nil, nil, nil, nil, nil, startServeFn, nil,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !serveCalled {
		t.Error("expected startServeFn to be called for 'voci serve'")
	}
	if !strings.Contains(calledAddr, ":9474") {
		t.Errorf("expected addr to contain :9474, got: %s", calledAddr)
	}
}

func TestDispatch_OnceSubcommand(t *testing.T) {
	setTestEnv(t)
	wavPath := makeTempWav(t)

	var stdout bytes.Buffer
	err := dispatch(
		[]string{"once", "--file", wavPath},
		&stdout, strings.NewReader(""),
		fakeTranscribe, fakeHinted, fakeRewrite, noopInject, nil, nil, nil, nil,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stdout.String(), "RAW") {
		t.Errorf("expected RAW in output, got: %q", stdout.String())
	}
}

func TestDispatch_LeadingFlagFallsBackToLegacy(t *testing.T) {
	setTestEnv(t)
	wavPath := makeTempWav(t)

	var stdout bytes.Buffer
	err := dispatch(
		[]string{"--file", wavPath},
		&stdout, strings.NewReader(""),
		fakeTranscribe, fakeHinted, fakeRewrite, noopInject, nil, nil, nil, nil,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stdout.String(), "RAW") {
		t.Errorf("expected RAW in output, got: %q", stdout.String())
	}
}

func TestListenPreflightDispatch(t *testing.T) {
	dir := t.TempDir()
	// No lock files, no stop sentinel → coldstart path.
	var stdout bytes.Buffer
	err := dispatch(
		[]string{"listen-preflight", "--lock-dir=" + dir},
		&stdout, strings.NewReader(""),
		nil, nil, nil, nil, nil, nil, nil, nil,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	line := strings.TrimSpace(stdout.String())
	if !strings.HasPrefix(line, "coldstart ") {
		t.Errorf("expected output to start with 'coldstart ', got: %q", line)
	}
}

func TestListenPreflightDispatch_Stopped(t *testing.T) {
	dir := t.TempDir()
	// Create stop sentinel.
	if err := os.WriteFile(dir+"/.listen-stop", []byte(""), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	err := dispatch(
		[]string{"listen-preflight", "--lock-dir=" + dir},
		&stdout, strings.NewReader(""),
		nil, nil, nil, nil, nil, nil, nil, nil,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	line := strings.TrimSpace(stdout.String())
	if line != "stopped" {
		t.Errorf("expected 'stopped', got: %q", line)
	}
}

func TestDispatch_UnknownSubcommandErrors(t *testing.T) {
	var stdout bytes.Buffer
	err := dispatch(
		[]string{"bogus"},
		&stdout, strings.NewReader(""),
		nil, nil, nil, nil, nil, nil, nil, nil,
	)
	if err == nil {
		t.Fatal("expected error for unknown subcommand")
	}
	for _, word := range []string{"serve", "once", "listen-preflight"} {
		if !strings.Contains(err.Error(), word) {
			t.Errorf("expected error to mention %q, got: %v", word, err)
		}
	}
}

func TestServeGeminiUsesMergedFn(t *testing.T) {
	setTestEnv(t)
	setCFEnv(t)
	t.Setenv("ASR_PROVIDER", "gemini")
	t.Setenv("ASR_API_KEY", "sk-test")

	var capturedMergedFn daemon.MergedFnType
	old := testOnServerBuilt
	testOnServerBuilt = func(srvIface interface{}) {
		if s, ok := srvIface.(*daemon.Server); ok {
			capturedMergedFn = s.MergedFn
		}
	}
	defer func() { testOnServerBuilt = old }()

	fakeManagedFn := StartManagedTunnelFn(func(ctx context.Context, cfg tunnel.ManagedTunnelConfig, port int, logW io.Writer) (*exec.Cmd, string, error) {
		cmd := exec.Command("true")
		if err := cmd.Start(); err != nil {
			return nil, "", err
		}
		return cmd, "https://voci-test.example.com", nil
	})

	var stdout bytes.Buffer
	err := run(
		[]string{"--serve", "--share", "--serve-port=0", "--share-auth=tok"},
		&stdout, strings.NewReader(""),
		nil, nil, nil, nil, nil, nil, nil, fakeManagedFn,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedMergedFn == nil {
		t.Error("expected srv.MergedFn to be non-nil when ASR_PROVIDER=gemini and ASR_API_KEY is set")
	}
}

func setCFEnv(t *testing.T) {
	t.Helper()
	t.Setenv("CLOUDFLARE_API_TOKEN", "fake-cf-token")
	t.Setenv("CF_ACCOUNT_ID", "fake-account")
	t.Setenv("CF_ZONE_ID", "fake-zone")
	t.Setenv("CF_TUNNEL_DOMAIN", "voci.example.com")
}

func TestServeWritesLock(t *testing.T) {
	setTestEnv(t)
	setCFEnv(t)

	dir := t.TempDir()
	lockCh := make(chan session.LockEntry, 1)

	fakeManagedFn := StartManagedTunnelFn(func(ctx context.Context, cfg tunnel.ManagedTunnelConfig, port int, logW io.Writer) (*exec.Cmd, string, error) {
		cmd := exec.Command("sleep", "10")
		if err := cmd.Start(); err != nil {
			return nil, "", err
		}
		go func() {
			deadline := time.Now().Add(5 * time.Second)
			for time.Now().Before(deadline) {
				entry, err := session.ReadLock(dir, "test-sess")
				if err == nil && entry.Port > 0 {
					select {
					case lockCh <- entry:
					default:
					}
					cmd.Process.Kill() //nolint:errcheck
					return
				}
				time.Sleep(10 * time.Millisecond)
			}
			cmd.Process.Kill() //nolint:errcheck
		}()
		return cmd, "https://voci-test.voci.example.com", nil
	})

	var stdout bytes.Buffer
	err := run(
		[]string{"--serve", "--share", "--serve-port=0", "--share-auth=tok",
			"--lock-dir=" + dir, "--session-id=test-sess"},
		&stdout, strings.NewReader(""),
		nil, nil, nil, nil, nil, nil, nil, fakeManagedFn,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	select {
	case entry := <-lockCh:
		if entry.PID != os.Getpid() {
			t.Errorf("lock PID = %d, want %d (current process)", entry.PID, os.Getpid())
		}
		if entry.Port <= 0 {
			t.Errorf("lock Port = %d, want > 0", entry.Port)
		}
	default:
		t.Error("lock file was never written during serve")
	}
}

func TestServeCleansUpLock(t *testing.T) {
	setTestEnv(t)
	setCFEnv(t)

	dir := t.TempDir()

	fakeManagedFn := StartManagedTunnelFn(func(ctx context.Context, cfg tunnel.ManagedTunnelConfig, port int, logW io.Writer) (*exec.Cmd, string, error) {
		cmd := exec.Command("sleep", "0.3")
		if err := cmd.Start(); err != nil {
			return nil, "", err
		}
		return cmd, "https://voci-test.voci.example.com", nil
	})

	var stdout bytes.Buffer
	err := run(
		[]string{"--serve", "--share", "--serve-port=0", "--share-auth=tok",
			"--lock-dir=" + dir, "--session-id=test-sess"},
		&stdout, strings.NewReader(""),
		nil, nil, nil, nil, nil, nil, nil, fakeManagedFn,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, statErr := os.Stat(dir + "/test-sess.lock"); statErr == nil {
		t.Error("expected lock file to be removed after serve exits, but it still exists")
	}
}

func TestServeWritesStatus(t *testing.T) {
	setTestEnv(t)
	setCFEnv(t)

	dir := t.TempDir()
	statusCh := make(chan session.StatusEntry, 1)

	fakeManagedFn := StartManagedTunnelFn(func(ctx context.Context, cfg tunnel.ManagedTunnelConfig, port int, logW io.Writer) (*exec.Cmd, string, error) {
		cmd := exec.Command("sleep", "10")
		if err := cmd.Start(); err != nil {
			return nil, "", err
		}
		go func() {
			deadline := time.Now().Add(5 * time.Second)
			for time.Now().Before(deadline) {
				entry, err := session.ReadStatus(dir, "status-sess")
				if err == nil && entry.LocalURL != "" {
					select {
					case statusCh <- entry:
					default:
					}
					cmd.Process.Kill() //nolint:errcheck
					return
				}
				time.Sleep(10 * time.Millisecond)
			}
			cmd.Process.Kill() //nolint:errcheck
		}()
		return cmd, "https://voci-test.voci.example.com", nil
	})

	var stdout bytes.Buffer
	err := run(
		[]string{"--serve", "--share", "--serve-port=0", "--share-auth=test-token",
			"--lock-dir=" + dir, "--session-id=status-sess"},
		&stdout, strings.NewReader(""),
		nil, nil, nil, nil, nil, nil, nil, fakeManagedFn,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	select {
	case entry := <-statusCh:
		if !strings.Contains(entry.LocalURL, "http://127.0.0.1:") {
			t.Errorf("LocalURL = %q, want http://127.0.0.1:<port>", entry.LocalURL)
		}
		if entry.ShareURL != "https://voci-test.voci.example.com" {
			t.Errorf("ShareURL = %q, want https://voci-test.voci.example.com", entry.ShareURL)
		}
		if entry.BearerToken != "test-token" {
			t.Errorf("BearerToken = %q, want test-token", entry.BearerToken)
		}
	default:
		t.Error("status file was never written during serve")
	}
}

func TestServeCleansUpStatus(t *testing.T) {
	setTestEnv(t)
	setCFEnv(t)

	dir := t.TempDir()

	fakeManagedFn := StartManagedTunnelFn(func(ctx context.Context, cfg tunnel.ManagedTunnelConfig, port int, logW io.Writer) (*exec.Cmd, string, error) {
		cmd := exec.Command("sleep", "0.3")
		if err := cmd.Start(); err != nil {
			return nil, "", err
		}
		return cmd, "https://voci-test.voci.example.com", nil
	})

	var stdout bytes.Buffer
	err := run(
		[]string{"--serve", "--share", "--serve-port=0", "--share-auth=tok",
			"--lock-dir=" + dir, "--session-id=cleanup-sess"},
		&stdout, strings.NewReader(""),
		nil, nil, nil, nil, nil, nil, nil, fakeManagedFn,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, statErr := os.Stat(dir + "/cleanup-sess.status"); statErr == nil {
		t.Error("expected status file to be removed after serve exits, but it still exists")
	}
}

func TestServeStdoutOnlyEvents(t *testing.T) {
	setTestEnv(t)
	setCFEnv(t)

	fakeManagedFn := StartManagedTunnelFn(func(ctx context.Context, cfg tunnel.ManagedTunnelConfig, port int, logW io.Writer) (*exec.Cmd, string, error) {
		cmd := exec.Command("true")
		if err := cmd.Start(); err != nil {
			return nil, "", err
		}
		return cmd, "https://voci-test.voci.example.com", nil
	})

	var stdout bytes.Buffer
	err := run(
		[]string{"--serve", "--share", "--serve-port=0", "--share-auth=tok"},
		&stdout, strings.NewReader(""),
		nil, nil, nil, nil, nil, nil, nil, fakeManagedFn,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := stdout.String()
	// Plain-text labels stay on stderr; stdout must contain the startup JSON event.
	for _, forbidden := range []string{"voci local URL", "voci share URL", "Bearer token:"} {
		if strings.Contains(out, forbidden) {
			t.Errorf("stdout should not contain plain-text label %q; got: %q", forbidden, out)
		}
	}
	if !strings.Contains(out, `"type":"startup"`) {
		t.Errorf("stdout should contain startup event; got: %q", out)
	}
}

func TestServeStartupEventOnStdout(t *testing.T) {
	setTestEnv(t)
	setCFEnv(t)

	fakeManagedFn := StartManagedTunnelFn(func(ctx context.Context, cfg tunnel.ManagedTunnelConfig, port int, logW io.Writer) (*exec.Cmd, string, error) {
		cmd := exec.Command("true")
		if err := cmd.Start(); err != nil {
			return nil, "", err
		}
		return cmd, "https://voci-test.voci.example.com", nil
	})

	var stdout bytes.Buffer
	err := run(
		[]string{"--serve", "--share", "--serve-port=0", "--share-auth=tok"},
		&stdout, strings.NewReader(""),
		nil, nil, nil, nil, nil, nil, nil, fakeManagedFn,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := stdout.String()
	for _, want := range []string{
		`"type":"startup"`,
		`"local_url":"http://127.0.0.1:`,
		`"share_url":"https://voci-test.voci.example.com"`,
		`"bearer_token":"tok"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("stdout should contain %q; got: %q", want, out)
		}
	}
}

func TestServeStartupEventWithLockDir(t *testing.T) {
	setTestEnv(t)
	setCFEnv(t)

	dir := t.TempDir()

	fakeManagedFn := StartManagedTunnelFn(func(ctx context.Context, cfg tunnel.ManagedTunnelConfig, port int, logW io.Writer) (*exec.Cmd, string, error) {
		cmd := exec.Command("true")
		if err := cmd.Start(); err != nil {
			return nil, "", err
		}
		return cmd, "https://voci-test.voci.example.com", nil
	})

	var stdout bytes.Buffer
	err := run(
		[]string{"--serve", "--share", "--serve-port=0", "--share-auth=tok",
			"--lock-dir=" + dir, "--session-id=startup-sess"},
		&stdout, strings.NewReader(""),
		nil, nil, nil, nil, nil, nil, nil, fakeManagedFn,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := stdout.String()
	if !strings.Contains(out, `"type":"startup"`) {
		t.Errorf("stdout should contain startup event; got: %q", out)
	}
	if !strings.Contains(out, `"local_url":"http://127.0.0.1:`) {
		t.Errorf("stdout should contain local_url; got: %q", out)
	}
}

func TestRun_ExitCode(t *testing.T) {
	setTestEnv(t)

	tests := []struct {
		name     string
		args     []string
		wantCode int
	}{
		{
			name:     "unknown subcommand returns 1",
			args:     []string{"voci", "bogus"},
			wantCode: 1,
		},
		{
			name:     "missing file returns 1",
			args:     []string{"voci", "--file", "/no/such/file.wav"},
			wantCode: 1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Run(tc.args)
			if got != tc.wantCode {
				t.Errorf("Run(%v) = %d, want %d", tc.args, got, tc.wantCode)
			}
		})
	}
}

func TestDefaultCmdRunner_Success(t *testing.T) {
	out, err := defaultCmdRunner("echo", "hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "hello") {
		t.Errorf("expected output to contain 'hello', got: %q", out)
	}
}

func TestDefaultCmdRunner_Failure(t *testing.T) {
	_, err := defaultCmdRunner("false")
	if err == nil {
		t.Fatal("expected error for 'false' command")
	}
}

func TestFirstNonEmpty_AllEmpty(t *testing.T) {
	got := firstNonEmpty("", "", "")
	if got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

func TestFirstNonEmpty_ReturnsFirst(t *testing.T) {
	got := firstNonEmpty("", "second", "third")
	if got != "second" {
		t.Errorf("expected 'second', got %q", got)
	}
}

// TestAccidentalTmuxInjection verifies that tests with nil injectFn do not
// accidentally execute real tmux send-keys, even when TMUX_PANE is set.
func TestAccidentalTmuxInjection(t *testing.T) {
	origTMUX := os.Getenv("TMUX_PANE")
	os.Setenv("TMUX_PANE", "%140")
	defer os.Setenv("TMUX_PANE", origTMUX)

	setTestEnv(t)
	wavPath := makeTempWav(t)

	var stdout bytes.Buffer
	err := run(
		[]string{"--file", wavPath},
		&stdout, strings.NewReader(""),
		fakeTranscribe, fakeHinted, fakeRewrite,
		nil, nil, nil, nil, nil,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := stdout.String()
	if strings.Contains(output, "tmux") || strings.Contains(output, "send-keys") {
		t.Errorf("stdout must NOT contain tmux/send-keys when injectFn is nil, got: %q", output)
	}
}
