package runner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sync"
	"time"
)

// RunOptions configures supervised process execution with stdout/stderr secret
// masking and guaranteed cleanup hooks on normal exit or signal termination.
type RunOptions struct {
	MaskSecrets         []string
	Cleanup             func()
	OnSignalsRegistered func()
	PreStartExitCode    func() (int, bool)
	Stdin               io.Reader
	Stdout              io.Writer
	Stderr              io.Writer
}

// RunCommandWithOptions executes cmdArgs with env under the provided options.
// When neither secret masking, cleanup hooks, nor custom stdio writers are
// requested, it delegates directly to RunCommand (syscall.Exec on Unix).
// Otherwise, it supervises the child process, streaming stdout/stderr through
// an Aho-Corasick masking trie, forwarding SIGINT/SIGTERM to the child, and
// guaranteeing opts.Cleanup executes before returning.
func RunCommandWithOptions(ctx context.Context, cmdArgs []string, env []string, opts RunOptions) int {
	var cleanupOnce sync.Once
	runCleanup := func() {
		if opts.Cleanup != nil {
			cleanupOnce.Do(opts.Cleanup)
		}
	}
	defer runCleanup()

	trie := NewMaskTrie(opts.MaskSecrets)
	if trie == nil && opts.Cleanup == nil && opts.Stdin == nil && opts.Stdout == nil && opts.Stderr == nil {
		if opts.PreStartExitCode != nil {
			if code, abort := opts.PreStartExitCode(); abort {
				return code
			}
		}
		if opts.OnSignalsRegistered != nil {
			opts.OnSignalsRegistered()
		}
		return RunCommand(cmdArgs, env)
	}

	sigCh := make(chan os.Signal, 2)
	notifyForwardSignals(sigCh)
	defer signal.Stop(sigCh)
	if opts.PreStartExitCode != nil {
		if code, abort := opts.PreStartExitCode(); abort {
			return code
		}
	}
	if opts.OnSignalsRegistered != nil {
		opts.OnSignalsRegistered()
	}
	select {
	case sig := <-sigCh:
		return exitCodeFromSignal(sig)
	default:
	}

	stderr := opts.Stderr
	if stderr == nil {
		stderr = os.Stderr
	}
	stdout := opts.Stdout
	if stdout == nil {
		stdout = os.Stdout
	}
	stdin := opts.Stdin
	if stdin == nil {
		stdin = os.Stdin
	}

	if code := validateCommand(cmdArgs, env); code != 0 {
		return code
	}

	commandName := cmdArgs[0]
	binary, err := exec.LookPath(commandName)
	if err != nil {
		if errors.Is(err, exec.ErrDot) {
			fmt.Fprintf(stderr, "Execution of relative command %q from PATH is blocked due to security risks\n", commandName)
			return 1
		}
		fmt.Fprintf(stderr, "Command not found: %v\n", err)
		return 1
	}

	absBinary, err := filepath.Abs(binary)
	if err != nil {
		fmt.Fprintf(stderr, "Failed to resolve absolute path: %v\n", err)
		return 1
	}

	if isBatchFileBlocked(absBinary) {
		fmt.Fprintf(stderr, "Execution of batch file %q is blocked due to security risks\n", absBinary)
		return 1
	}

	/* #nosec G204 */
	cmd := exec.Command(absBinary, cmdArgs[1:]...) //nolint:gosec // by design; command provenance is the operator's config
	cmd.Env = env
	cmd.Stdin = stdin

	var stdoutMasker, stderrMasker *MaskingWriter
	if trie != nil {
		stdoutMasker = NewMaskingWriter(stdout, trie)
		stderrMasker = NewMaskingWriter(stderr, trie)
		cmd.Stdout = stdoutMasker
		cmd.Stderr = stderrMasker
	} else {
		cmd.Stdout = stdout
		cmd.Stderr = stderr
	}

	if err := cmd.Start(); err != nil {
		fmt.Fprintf(stderr, "Execution failed: %v\n", err)
		return 1
	}

	doneCh := make(chan struct{})

	var sigMu sync.Mutex
	var receivedSig os.Signal

	var ctxDone <-chan struct{}
	if ctx != nil {
		ctxDone = ctx.Done()
	}

	go func() {
		sigCount := 0
		for {
			select {
			case <-doneCh:
				return
			case <-ctxDone:
				_ = forwardSignalToChild(cmd.Process, os.Interrupt)
				select {
				case <-doneCh:
					return
				case <-time.After(3 * time.Second):
					if cmd.Process != nil {
						_ = cmd.Process.Kill()
					}
					return
				}
			case sig, ok := <-sigCh:
				if !ok {
					return
				}
				sigMu.Lock()
				if receivedSig == nil {
					receivedSig = sig
				}
				sigMu.Unlock()
				sigCount++
				if sigCount == 1 {
					_ = forwardSignalToChild(cmd.Process, sig)
				} else if cmd.Process != nil {
					_ = cmd.Process.Kill()
				}
			}
		}
	}()

	waitErr := cmd.Wait()
	close(doneCh)
	signal.Stop(sigCh)

	if stdoutMasker != nil {
		_ = stdoutMasker.Flush()
	}
	if stderrMasker != nil {
		_ = stderrMasker.Flush()
	}

	runCleanup()

	sigMu.Lock()
	sig := receivedSig
	sigMu.Unlock()

	if waitErr != nil {
		var exitErr *exec.ExitError
		if !errors.As(waitErr, &exitErr) && sig == nil {
			fmt.Fprintf(stderr, "Execution failed: %v\n", waitErr)
			return 1
		}
		return exitCodeFromWaitError(waitErr, sig)
	}
	return 0
}
