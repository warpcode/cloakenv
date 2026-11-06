package cmd

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/warpcode/cloakenv/internal/config"
	"github.com/zalando/go-keyring"
)

func captureStdoutStderr(fn func() int) (int, string, string) {
	origOut := os.Stdout
	origErr := os.Stderr

	rOut, wOut, _ := os.Pipe()
	rErr, wErr, _ := os.Pipe()

	os.Stdout = wOut
	os.Stderr = wErr

	outC := make(chan string)
	errC := make(chan string)

	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, rOut)
		outC <- buf.String()
	}()

	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, rErr)
		errC <- buf.String()
	}()

	code := fn()

	_ = wOut.Close()
	_ = wErr.Close()

	stdoutStr := <-outC
	stderrStr := <-errC

	os.Stdout = origOut
	os.Stderr = origErr

	return code, stdoutStr, stderrStr
}

func TestSyncCommand(t *testing.T) {
	keyring.MockInit()

	t.Run("help flag", func(t *testing.T) {
		code, stdout, _ := captureStdoutStderr(func() int {
			return Sync([]string{"--help"}, nil)
		})
		if code != 0 {
			t.Errorf("expected exit 0, got %d", code)
		}
		if !strings.Contains(stdout, "Usage:\n  cloakenv sync") {
			t.Errorf("expected help usage, got %s", stdout)
		}
	})

	t.Run("missing arguments", func(t *testing.T) {
		code, _, stderr := captureStdoutStderr(func() int {
			return Sync([]string{"only_source"}, nil)
		})
		if code != 1 {
			t.Errorf("expected exit 1, got %d", code)
		}
		if !strings.Contains(stderr, "Usage: cloakenv sync <source> <target> [flags]") {
			t.Errorf("expected usage message, got %s", stderr)
		}
	})

	t.Run("unknown flag", func(t *testing.T) {
		code, _, stderr := captureStdoutStderr(func() int {
			return Sync([]string{"--invalid-flag", "src", "dst"}, nil)
		})
		if code != 1 {
			t.Errorf("expected exit 1, got %d", code)
		}
		if !strings.Contains(stderr, "unknown flag") {
			t.Errorf("expected unknown flag error, got %s", stderr)
		}
	})

	t.Run("read-only target error", func(t *testing.T) {
		t.Setenv("SRC_VAR", "secret123")
		code, _, stderr := captureStdoutStderr(func() int {
			return Sync([]string{"env://SRC_VAR", "env://TARGET_VAR"}, nil)
		})
		if code != 1 {
			t.Errorf("expected exit 1, got %d", code)
		}
		if !strings.Contains(stderr, "read-only") {
			t.Errorf("expected read-only error in stderr, got: %s", stderr)
		}
	})

	t.Run("dry-run and synchronization output with zero secret leakage", func(t *testing.T) {
		cfg := &config.Config{
			Vaults: map[string]config.VaultConfig{
				"mock_vault": {
					Provider: "custom_vault",
					Entities: map[string]config.CustomEntityConfig{
						"creds": {
							Title: "Credentials",
							Tags:  []string{"sync:keyring"},
							Attributes: map[string]any{
								"API_KEY": "super_secret_42",
							},
						},
					},
				},
			},
		}

		// 1. Dry run
		code, stdout, stderr := captureStdoutStderr(func() int {
			return Sync([]string{"--dry-run", "--tags", "sync:keyring", "mock_vault://", "keyring://my_service"}, cfg)
		})
		if code != 0 {
			t.Fatalf("expected exit 0 for dry-run, got %d, stderr: %s", code, stderr)
		}
		if !strings.Contains(stdout, "[DRY RUN] Secret synchronization preview:") {
			t.Errorf("expected preview header, got: %s", stdout)
		}
		if !strings.Contains(stdout, "+ creds/API_KEY (added)") {
			t.Errorf("expected added key, got: %s", stdout)
		}
		if strings.Contains(stdout, "super_secret_42") || strings.Contains(stderr, "super_secret_42") {
			t.Errorf("SECURITY LEAK: output contains secret value: %s", stdout)
		}

		// 2. Real sync execution
		code, stdout, stderr = captureStdoutStderr(func() int {
			return Sync([]string{"--tags", "sync:keyring", "mock_vault://", "keyring://my_service"}, cfg)
		})
		if code != 0 {
			t.Fatalf("expected exit 0 for sync, got %d, stderr: %s", code, stderr)
		}
		if !strings.Contains(stdout, "Secret synchronization complete:") {
			t.Errorf("expected completion header, got: %s", stdout)
		}
		if !strings.Contains(stdout, "+ creds/API_KEY (added)") {
			t.Errorf("expected added key, got: %s", stdout)
		}
		if strings.Contains(stdout, "super_secret_42") || strings.Contains(stderr, "super_secret_42") {
			t.Errorf("SECURITY LEAK: output contains secret value: %s", stdout)
		}

		// 3. Second run with unchanged values (skipped)
		code, stdout, stderr = captureStdoutStderr(func() int {
			return Sync([]string{"--tags", "sync:keyring", "mock_vault://", "keyring://my_service"}, cfg)
		})
		if code != 0 {
			t.Fatalf("expected exit 0, got %d, stderr: %s", code, stderr)
		}
		if !strings.Contains(stdout, "creds/API_KEY (skipped: unchanged)") {
			t.Errorf("expected skipped key, got: %s", stdout)
		}
		if strings.Contains(stdout, "super_secret_42") || strings.Contains(stderr, "super_secret_42") {
			t.Errorf("SECURITY LEAK: output contains secret value: %s", stdout)
		}
	})
}
