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

func exitCodeFromSignal(sig os.Signal) int {
	if sig == syscall.SIGTERM {
		return 143
	}
	if sig == os.Interrupt || sig == syscall.SIGINT {
		return 130
	}
	if sysSig, ok := sig.(syscall.Signal); ok && sysSig > 0 {
		return 128 + int(sysSig)
	}
	return 1
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
	return exitCodeFromSignal(receivedSig)
}

func isBatchFileBlocked(_ string) bool {
	return false
}
