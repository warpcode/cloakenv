package runner

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestRunCommandWithOptions_MasksStdoutAndStderrAndCleansUp(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script test is Unix-specific")
	}
	dir := t.TempDir()
	secretFile := filepath.Join(dir, "ephemeral.yaml")
	if _, err := WriteTemplateFile(secretFile, []byte("api_key: sk-super-secret-9999\n")); err != nil {
		t.Fatalf("WriteTemplateFile failed: %v", err)
	}

	var stdout, stderr bytes.Buffer
	exitCode := RunCommandWithOptions(
		context.Background(),
		[]string{"sh", "-c", `echo "OUT: sk-super-secret-9999"; echo "ERR: sk-super-secret-9999" >&2; cat "$1"`, "--", secretFile},
		os.Environ(),
		RunOptions{
			MaskSecrets: []string{"sk-super-secret-9999", "api_key: sk-super-secret-9999\n"},
			Cleanup: func() {
				_ = SecureZeroAndRemove(secretFile)
			},
			Stdout: &stdout,
			Stderr: &stderr,
		},
	)

	if exitCode != 0 {
		t.Fatalf("exitCode = %d, want 0 (stderr: %s)", exitCode, stderr.String())
	}
	if strings.Contains(stdout.String(), "sk-super-secret-9999") {
		t.Errorf("SECURITY LEAK: secret appeared in stdout: %q", stdout.String())
	}
	if strings.Contains(stderr.String(), "sk-super-secret-9999") {
		t.Errorf("SECURITY LEAK: secret appeared in stderr: %q", stderr.String())
	}
	if !strings.Contains(stdout.String(), "OUT: <REDACTED>") {
		t.Errorf("expected redacted stdout, got: %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "ERR: <REDACTED>") {
		t.Errorf("expected redacted stderr, got: %q", stderr.String())
	}
	if _, err := os.Lstat(secretFile); !os.IsNotExist(err) {
		t.Errorf("expected ephemeral file %q to be deleted after exit, err=%v", secretFile, err)
	}
}

func TestRunCommandWithOptions_SignalCleanup(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX SIGINT/SIGTERM signal delivery test is Unix-specific")
	}

	if targetFile := os.Getenv("GO_WANT_RUNNER_SIGNAL_HELPER"); targetFile != "" {
		readyFile := os.Getenv("GO_WANT_RUNNER_READY_FILE")
		_, cleanup, err := MaterializeTemplates([]TemplateDelivery{
			{
				TemplateName: "sig_tpl",
				Path:         targetFile,
				Content:      "super-secret-signal-payload",
			},
		})
		if err != nil {
			os.Exit(2)
		}
		code := RunCommandWithOptions(
			context.Background(),
			[]string{"sh", "-c", `touch "$1"; exec sleep 30`, "--", readyFile},
			os.Environ(),
			RunOptions{
				MaskSecrets: []string{"super-secret-signal-payload"},
				Cleanup:     cleanup,
			},
		)
		os.Exit(code)
	}

	for _, tc := range []struct {
		name     string
		sig      syscall.Signal
		wantCode int
	}{
		{name: "SIGTERM", sig: syscall.SIGTERM, wantCode: 143},
		{name: "SIGINT", sig: syscall.SIGINT, wantCode: 130},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			targetFile := filepath.Join(dir, "ephemeral_sig.yaml")
			readyFile := filepath.Join(dir, "ready")

			cmd := exec.Command(os.Args[0], "-test.run=^TestRunCommandWithOptions_SignalCleanup$")
			cmd.Env = append(os.Environ(),
				"GO_WANT_RUNNER_SIGNAL_HELPER="+targetFile,
				"GO_WANT_RUNNER_READY_FILE="+readyFile,
			)
			if err := cmd.Start(); err != nil {
				t.Fatalf("failed to start helper subprocess: %v", err)
			}

			// Wait until child has materialized the file and started sleeping.
			deadline := time.Now().Add(5 * time.Second)
			for time.Now().Before(deadline) {
				if _, err := os.Stat(readyFile); err == nil {
					if _, err2 := os.Stat(targetFile); err2 == nil {
						break
					}
				}
				time.Sleep(25 * time.Millisecond)
			}
			if _, err := os.Stat(targetFile); err != nil {
				_ = cmd.Process.Kill()
				t.Fatalf("ephemeral file was never created before signal: %v", err)
			}

			if err := cmd.Process.Signal(tc.sig); err != nil {
				_ = cmd.Process.Kill()
				t.Fatalf("failed sending %v: %v", tc.sig, err)
			}

			waitErr := cmd.Wait()
			if waitErr == nil {
				t.Fatalf("expected non-zero exit on %v, got exit 0", tc.sig)
			}
			var exitErr *exec.ExitError
			if errors.As(waitErr, &exitErr) {
				if exitErr.ExitCode() != tc.wantCode {
					t.Errorf("exit code = %d, want %d", exitErr.ExitCode(), tc.wantCode)
				}
			}

			if _, err := os.Lstat(targetFile); !os.IsNotExist(err) {
				t.Errorf("SECURITY FAILURE: ephemeral file %q was NOT cleaned up after %v (err=%v)", targetFile, tc.sig, err)
			}
		})
	}
}
