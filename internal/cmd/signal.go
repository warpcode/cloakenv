package cmd

import (
	"context"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"golang.org/x/term"
)

var (
	getStateFunc   = term.GetState
	restoreFunc    = term.Restore
	isTerminalFunc = term.IsTerminal
)

// signalContext returns a context that cancels when SIGINT (Ctrl+C) or SIGTERM is received.
// The caller must defer the returned CancelFunc to restore signal handlers and release resources.
func signalContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}

// restoreTerminalOnInterrupt checks if fd is a terminal, snapshots its state,
// and ensures the original terminal state is restored if ctx is canceled (e.g. by SIGINT/SIGTERM)
// before the prompt completes. The returned cleanup function stops the monitor and restores terminal state.
func restoreTerminalOnInterrupt(ctx context.Context, fd int) func() {
	if !isTerminalFunc(fd) {
		return func() {}
	}

	state, err := getStateFunc(fd)
	if err != nil || state == nil {
		return func() {}
	}

	var once sync.Once
	done := make(chan struct{})

	go func() {
		select {
		case <-ctx.Done():
			once.Do(func() {
				_ = restoreFunc(fd, state)
			})
		case <-done:
		}
	}()

	return func() {
		close(done)
		once.Do(func() {
			_ = restoreFunc(fd, state)
		})
	}
}
