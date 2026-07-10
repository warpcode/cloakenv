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
func captureOutput(f func() int) (int, string, string) {
	oldStdout := os.Stdout
	oldStderr := os.Stderr
	defer func() {
		os.Stdout = oldStdout
		os.Stderr = oldStderr
	}()

	rOut, wOut, _ := os.Pipe()
	rErr, wErr, _ := os.Pipe()

	os.Stdout = wOut
	os.Stderr = wErr

	outC := make(chan string)
	errC := make(chan string)

	go func() {
		var buf bytes.Buffer
		io.Copy(&buf, rOut)
		outC <- buf.String()
	}()

	go func() {
		var buf bytes.Buffer
		io.Copy(&buf, rErr)
		errC <- buf.String()
	}()

	exitCode := f()

	wOut.Close()
	wErr.Close()

	return exitCode, <-outC, <-errC
}

func TestDelete(t *testing.T) {
	t.Run("HelpFlag", func(t *testing.T) {
		exitCode, stdout, _ := captureOutput(func() int {
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
		exitCode, _, stderr := captureOutput(func() int {
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
		exitCode, _, stderr := captureOutput(func() int {
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

		// Set secret (we suppress output from Set to not mess up testing, but since it's not captured, it will go to real stdout if not careful,
		// so we can also capture it or run it independently)
		captureOutput(func() int {
			return Set([]string{"cache://test", "testvalue"}, cfg)
		})

		exitCode, stdout, _ := captureOutput(func() int {
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
		// Pass a config that will cause an error (e.g. invalid vault provider)
		cfg := &config.Config{
			Vaults: map[string]config.VaultConfig{
				"bad": {Provider: "invalid_provider"},
			},
		}

		exitCode, _, stderr := captureOutput(func() int {
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

		exitCode, _, stderr := captureOutput(func() int {
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
