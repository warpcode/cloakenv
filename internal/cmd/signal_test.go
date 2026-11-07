package cmd

import (
	"context"
	"errors"
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
		restoreFunc = func(fd int, st *term.State) error {
			if st != fakeState {
				return errors.New("unexpected state")
			}
			atomic.AddInt32(&restoreCount, 1)
			return nil
		}

		ctx, cancel := context.WithCancel(context.Background())
		cleanup := restoreTerminalOnInterrupt(ctx, 0)

		cancel()
		time.Sleep(50 * time.Millisecond)
		cleanup()

		if count := atomic.LoadInt32(&restoreCount); count != 1 {
			t.Errorf("expected restore to be called exactly once, got %d", count)
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
