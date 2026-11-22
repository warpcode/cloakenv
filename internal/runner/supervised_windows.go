//go:build windows
// +build windows

package runner

import (
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
)

func notifyForwardSignals(ch chan<- os.Signal) {
	signal.Notify(ch, os.Interrupt)
}

func forwardSignalToChild(proc *os.Process, _ os.Signal) error {
	if proc == nil {
		return nil
	}
	return proc.Kill()
}

func exitCodeFromSignal(sig os.Signal) int {
	if sig == os.Interrupt {
		return 130
	}
	return 1
}

func exitCodeFromWaitError(err error, receivedSig os.Signal) int {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return exitCodeFromSignal(receivedSig)
}

func isBatchFileBlocked(absBinary string) bool {
	cleanBinary := strings.TrimRight(absBinary, ". ")
	ext := strings.ToLower(filepath.Ext(cleanBinary))
	return ext == ".bat" || ext == ".cmd"
}
