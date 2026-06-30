package daemon

import (
	"context"
	"errors"
	"testing"

	"github.com/yaleh/voci/internal/asr"
)

// daemonFakeTranscriber is a test double implementing asr.Transcriber.
type daemonFakeTranscriber struct {
	text string
	err  error
}

func (f daemonFakeTranscriber) Transcribe(_ context.Context, _ asr.Options) (asr.Result, error) {
	return asr.Result{Text: f.text}, f.err
}

func TestDaemonAcceptsFnFromTranscriber(t *testing.T) {
	fake := daemonFakeTranscriber{text: "daemon-test"}
	fn := asr.FnFromTranscriber(fake)

	// Assign to the daemon.TranscribeFn type to prove compatibility.
	var tfn TranscribeFn = fn

	// Also wire into Server.
	s := &Server{TranscribeFn: fn}
	_ = s

	// Call the function and verify text flows through.
	got := tfn(context.Background(), "", "", "", "", nil)
	if got != "daemon-test" {
		t.Errorf("expected 'daemon-test', got %q", got)
	}
}

func TestDaemonFnFromTranscriberEmptyOnError(t *testing.T) {
	fake := daemonFakeTranscriber{err: errors.New("fail")}
	fn := asr.FnFromTranscriber(fake)

	var tfn TranscribeFn = fn
	got := tfn(context.Background(), "", "", "", "", nil)
	if got != "" {
		t.Errorf("expected empty string on error, got %q", got)
	}
}
