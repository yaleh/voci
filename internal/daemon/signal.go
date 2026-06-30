package daemon

import (
	"context"
	"os"
	"os/signal"
	"syscall"
)

// WithSignalCancel returns a derived context that is cancelled when the process
// receives SIGTERM or SIGINT. The caller must call the returned cancel function
// to release resources when done.
func WithSignalCancel(ctx context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(ctx)
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGTERM, os.Interrupt)
	go func() {
		select {
		case <-ch:
			cancel()
		case <-ctx.Done():
		}
		signal.Stop(ch)
	}()
	return ctx, cancel
}
