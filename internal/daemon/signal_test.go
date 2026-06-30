//go:build !e2e

package daemon

import (
	"context"
	"os"
	"syscall"
	"testing"
	"time"
)

func TestWithSignalCancel_CancelsOnSIGTERM(t *testing.T) {
	ctx, cancel := WithSignalCancel(context.Background())
	defer cancel()

	// Send SIGTERM to ourselves; signal.Notify intercepts it before the default handler.
	if err := syscall.Kill(os.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatalf("Kill SIGTERM: %v", err)
	}

	select {
	case <-ctx.Done():
		// good
	case <-time.After(1 * time.Second):
		t.Fatal("context not cancelled within 1s after SIGTERM")
	}
}

func TestWithSignalCancel_CancelsOnSIGINT(t *testing.T) {
	ctx, cancel := WithSignalCancel(context.Background())
	defer cancel()

	if err := syscall.Kill(os.Getpid(), syscall.SIGINT); err != nil {
		t.Fatalf("Kill SIGINT: %v", err)
	}

	select {
	case <-ctx.Done():
		// good
	case <-time.After(1 * time.Second):
		t.Fatal("context not cancelled within 1s after SIGINT")
	}
}

func TestWithSignalCancel_NotCancelledWithoutSignal(t *testing.T) {
	ctx, cancel := WithSignalCancel(context.Background())
	defer cancel()

	select {
	case <-ctx.Done():
		t.Fatal("context was cancelled without sending a signal")
	case <-time.After(200 * time.Millisecond):
		// good: no cancellation without a signal
	}
	if ctx.Err() != nil {
		t.Errorf("expected ctx.Err() == nil, got: %v", ctx.Err())
	}
}
