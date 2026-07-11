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

// captureOutput captures stdout and stderr for the given function
func captureOutput(t *testing.T, f func() int) (int, string, string) {
	t.Helper()
	oldStdout := os.Stdout
	oldStderr := os.Stderr
	defer func() {
		os.Stdout = oldStdout
		os.Stderr = oldStderr
	}()

	rOut, wOut, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create stdout pipe: %v", err)
	}
	defer wOut.Close()

	rErr, wErr, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create stderr pipe: %v", err)
	}
	defer wErr.Close()

	os.Stdout = wOut
	os.Stderr = wErr

	outC := make(chan string)
	errC := make(chan string)

	go func() {
		var buf bytes.Buffer
		if _, err := io.Copy(&buf, rOut); err != nil {
			t.Errorf("failed to read from stdout pipe: %v", err)
		}
		outC <- buf.String()
	}()

	go func() {
		var buf bytes.Buffer
		if _, err := io.Copy(&buf, rErr); err != nil {
			t.Errorf("failed to read from stderr pipe: %v", err)
		}
		errC <- buf.String()
	}()

	exitCode := f()

	// wOut and wErr will be closed by deferred calls above when captureOutput returns.
	// We need to close them here to unblock the readers in the goroutines.
	// The deferred calls will be no-ops if they are already closed.
	wOut.Close()
	wErr.Close()

	return exitCode, <-outC, <-errC
}

func TestDelete(t *testing.T) {
	t.Run("HelpFlag", func(t *testing.T) {
		exitCode, stdout, _ := captureOutput(t, func() int {
			return Delete([]string{"--help"}, &config.Config{})
		})

		if exitCode != 0 {
			t.Errorf("expected exit code 0 for help flag, got %d", exitCode)
		}

		if !strings.Contains(stdout, "Usage:") {
			t.Errorf("expected help text, got %q", stdout)
		}
	})

	t.Run("InvalidArgs", func(t *testing.T) {
		exitCode, _, stderr := captureOutput(t, func() int {
			return Delete([]string{}, &config.Config{})
		})

		if exitCode != 1 {
			t.Errorf("expected exit code 1 for invalid args, got %d", exitCode)
		}

		if !strings.Contains(stderr, "Usage:") {
			t.Errorf("expected usage text, got %q", stderr)
		}
	})

	t.Run("InvalidURI", func(t *testing.T) {
		exitCode, _, stderr := captureOutput(t, func() int {
			return Delete([]string{"invalid-uri"}, &config.Config{})
		})

		if exitCode != 1 {
			t.Errorf("expected exit code 1 for invalid URI, got %d", exitCode)
		}

		if !strings.Contains(stderr, "Failed to delete") {
			t.Errorf("expected failed to delete message, got %q", stderr)
		}
	})

	t.Run("Success", func(t *testing.T) {
		keyring.MockInit()

		cfg := &config.Config{
			Vaults: make(map[string]config.VaultConfig),
		}

		cacheDir := t.TempDir()
		t.Setenv("XDG_CACHE_HOME", cacheDir)
		t.Setenv("CLOAKENV_ENCRYPTION_KEY", "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff")

		// Set secret
		captureOutput(t, func() int {
			return Set([]string{"cache://test", "testvalue"}, cfg)
		})

		exitCode, stdout, _ := captureOutput(t, func() int {
			return Delete([]string{"cache://test"}, cfg)
		})

		if exitCode != 0 {
			t.Errorf("expected exit code 0 for successful delete, got %d", exitCode)
		}

		if !strings.Contains(stdout, "Secret successfully deleted") {
			t.Errorf("expected success message, got %q", stdout)
		}
	})

	t.Run("ConfigError", func(t *testing.T) {
		cfg := &config.Config{
			Vaults: map[string]config.VaultConfig{
				"bad": {Provider: "invalid_provider"},
			},
		}

		exitCode, _, stderr := captureOutput(t, func() int {
			return Delete([]string{"cache://test"}, cfg)
		})

		if exitCode != 1 {
			t.Errorf("expected exit code 1 for config error, got %d", exitCode)
		}

		if !strings.Contains(stderr, "Config error:") {
			t.Errorf("expected Config error message, got %q", stderr)
		}
	})

	t.Run("NonExistent", func(t *testing.T) {
		keyring.MockInit()
		cfg := &config.Config{
			Vaults: make(map[string]config.VaultConfig),
		}

		cacheDir := t.TempDir()
		t.Setenv("XDG_CACHE_HOME", cacheDir)
		t.Setenv("CLOAKENV_ENCRYPTION_KEY", "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff")

		exitCode, _, stderr := captureOutput(t, func() int {
			return Delete([]string{"cache://nonexistent"}, cfg)
		})

		if exitCode != 1 {
			t.Errorf("expected exit code 1 for nonexistent delete, got %d", exitCode)
		}

		if !strings.Contains(stderr, "Failed to delete secret") {
			t.Errorf("expected failed to delete message, got %q", stderr)
		}
	})
}
