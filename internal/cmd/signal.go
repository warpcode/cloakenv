package cmd

import (
	"context"
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"

	"golang.org/x/term"
)

var (
	getStateFunc   = term.GetState
	restoreFunc    = term.Restore
	isTerminalFunc = term.IsTerminal
)

// signalContext returns a context that cancels when SIGINT (Ctrl+C) or SIGTERM is received.
// A companion listener is registered so that a second SIGINT/SIGTERM terminates the process
// immediately with exit code 130 or 143.
// The caller must call the returned CancelFunc to restore signal handlers and release resources.
func signalContext() (context.Context, context.CancelFunc) {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	sigCh := make(chan os.Signal, 2)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	var signalCount int32
	go func() {
		for sig := range sigCh {
			if atomic.AddInt32(&signalCount, 1) >= 2 {
				exitCode := 130
				if sig == syscall.SIGTERM {
					exitCode = 143
				}
				os.Exit(exitCode)
			}
		}
	}()

	var stopOnce sync.Once
	stop := func() {
		stopOnce.Do(func() {
			signal.Stop(sigCh)
			close(sigCh)
			cancel()
		})
	}

	return ctx, stop
}

// exitCodeForContext maps a context cancellation caused by SIGINT/SIGTERM to standard exit codes:
// 130 for SIGINT (Ctrl+C) and 143 for SIGTERM. If the context was not canceled or caused by signals,
// defaultCode is returned.
func exitCodeForContext(ctx context.Context, defaultCode int) int {
	if ctx == nil || ctx.Err() == nil {
		return defaultCode
	}

	cause := context.Cause(ctx)
	if cause != nil {
		msg := strings.ToLower(cause.Error())
		if strings.Contains(msg, "interrupt") {
			return 130
		}
		if strings.Contains(msg, "terminated") {
			return 143
		}
	}

	return defaultCode
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

	done := make(chan struct{})

	go func() {
		select {
		case <-ctx.Done():
			_ = restoreFunc(fd, state)
		case <-done:
		}
	}()

	return func() {
		select {
		case <-done:
		default:
			close(done)
		}
		_ = restoreFunc(fd, state)
	}
}
