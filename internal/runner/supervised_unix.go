//go:build !windows
// +build !windows

package runner

import (
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
)

func notifyForwardSignals(ch chan<- os.Signal) {
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
}

func forwardSignalToChild(proc *os.Process, sig os.Signal) error {
	if proc == nil {
		return nil
	}
	return proc.Signal(sig)
}

func exitCodeFromWaitError(err error, receivedSig os.Signal) int {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		if status, ok := exitErr.Sys().(syscall.WaitStatus); ok && status.Signaled() {
			return 128 + int(status.Signal())
		}
		return exitErr.ExitCode()
	}
	if receivedSig == syscall.SIGTERM {
		return 143
	}
	if receivedSig == os.Interrupt {
		return 130
	}
	return 1
}

func isBatchFileBlocked(_ string) bool {
	return false
}
