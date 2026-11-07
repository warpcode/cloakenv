package cmd

import (
	"context"
	"errors"
	"os"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/term"
)

func TestSignalContext_Cancellation(t *testing.T) {
	ctx, stop := signalContext()
	defer stop()

	if ctx.Err() != nil {
		t.Fatalf("expected active context, got %v", ctx.Err())
	}

	if runtime.GOOS != "windows" {
		proc, err := os.FindProcess(os.Getpid())
		if err != nil {
			t.Fatalf("failed to find current process: %v", err)
		}
		if err := proc.Signal(os.Interrupt); err != nil {
			t.Fatalf("failed to send interrupt signal: %v", err)
		}

		select {
		case <-ctx.Done():
			if ctx.Err() == nil {
				t.Errorf("expected non-nil context error upon receiving interrupt")
			}
			if code := exitCodeForContext(ctx, 1); code != 130 {
				t.Errorf("exitCodeForContext = %d, want 130 for SIGINT", code)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("context was not canceled after sending os.Interrupt")
		}
	}
}

func TestExitCodeForContext(t *testing.T) {
	t.Run("nil or non-canceled context returns default", func(t *testing.T) {
		//nolint:staticcheck // testing defensive nil context handling
		if code := exitCodeForContext(nil, 42); code != 42 {
			t.Errorf("got %d, want 42", code)
		}
		if code := exitCodeForContext(context.Background(), 1); code != 1 {
			t.Errorf("got %d, want 1", code)
		}
	})

	t.Run("standard canceled context returns default", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if code := exitCodeForContext(ctx, 5); code != 5 {
			t.Errorf("got %d, want 5", code)
		}
	})
}

func TestRestoreTerminalOnInterrupt(t *testing.T) {
	oldIsTerminal := isTerminalFunc
	oldGetState := getStateFunc
	oldRestore := restoreFunc
	defer func() {
		isTerminalFunc = oldIsTerminal
		getStateFunc = oldGetState
		restoreFunc = oldRestore
	}()

	t.Run("non-terminal fd does not register restore", func(t *testing.T) {
		isTerminalFunc = func(fd int) bool { return false }
		var restored bool
		restoreFunc = func(fd int, st *term.State) error {
			restored = true
			return nil
		}

		ctx, cancel := context.WithCancel(context.Background())
		cleanup := restoreTerminalOnInterrupt(ctx, 0)
		cancel()
		cleanup()

		if restored {
			t.Errorf("expected restore not to be called for non-terminal")
		}
	})

	t.Run("terminal fd restores state on context cancel", func(t *testing.T) {
		isTerminalFunc = func(fd int) bool { return true }
		fakeState := &term.State{}
		getStateFunc = func(fd int) (*term.State, error) {
			return fakeState, nil
		}
		var restoreCount int32
		restoredCh := make(chan struct{}, 1)
		restoreFunc = func(fd int, st *term.State) error {
			if st != fakeState {
				return errors.New("unexpected state")
			}
			atomic.AddInt32(&restoreCount, 1)
			select {
			case restoredCh <- struct{}{}:
			default:
			}
			return nil
		}

		ctx, cancel := context.WithCancel(context.Background())
		cleanup := restoreTerminalOnInterrupt(ctx, 0)

		cancel()
		select {
		case <-restoredCh:
		case <-time.After(2 * time.Second):
			t.Fatal("restoreFunc was not called upon context cancellation")
		}

		cleanup()

		// Cleanup should also execute restore safely (idempotent restore on terminal)
		if count := atomic.LoadInt32(&restoreCount); count < 1 {
			t.Errorf("expected restore to be called at least once, got %d", count)
		}
	})

	t.Run("normal prompt completion restores once", func(t *testing.T) {
		isTerminalFunc = func(fd int) bool { return true }
		fakeState := &term.State{}
		getStateFunc = func(fd int) (*term.State, error) {
			return fakeState, nil
		}
		var restoreCount int32
		restoreFunc = func(fd int, st *term.State) error {
			atomic.AddInt32(&restoreCount, 1)
			return nil
		}

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		cleanup := restoreTerminalOnInterrupt(ctx, 0)
		cleanup()

		if count := atomic.LoadInt32(&restoreCount); count != 1 {
			t.Errorf("expected restore to be called once on cleanup, got %d", count)
		}
	})
}
