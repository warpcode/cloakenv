package cmd

import (
	"bytes"
	"errors"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zalando/go-keyring"

	"github.com/warpcode/cloakenv/internal/config"
)

func TestCacheRouting(t *testing.T) {
	keyring.MockInit()

	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)
	t.Setenv("XDG_CACHE_HOME", tempDir)
	t.Setenv("LocalAppData", tempDir)

	tests := []struct {
		name           string
		args           []string
		expectedCode   int
		expectedStdout string
		expectedStderr string
	}{
		{
			name:           "help flag without clear",
			args:           []string{"--help"},
			expectedCode:   0,
			expectedStdout: "Manage local encrypted cache",
			expectedStderr: "",
		},
		{
			name:           "no args",
			args:           []string{},
			expectedCode:   1,
			expectedStdout: "",
			expectedStderr: "Usage: cloakenv cache <clear|prune>",
		},
		{
			name:           "invalid subcommand",
			args:           []string{"invalid"},
			expectedCode:   1,
			expectedStdout: "",
			expectedStderr: "Usage: cloakenv cache <clear|prune>",
		},
		{
			name:           "prune subcommand with help",
			args:           []string{"prune", "--help"},
			expectedCode:   0,
			expectedStdout: "Remove expired entries",
			expectedStderr: "",
		},
		{
			name:           "prune subcommand extra args",
			args:           []string{"prune", "extra"},
			expectedCode:   1,
			expectedStdout: "",
			expectedStderr: "Usage: cloakenv cache prune",
		},
		{
			name:           "prune subcommand valid empty config",
			args:           []string{"prune"},
			expectedCode:   0,
			expectedStdout: "Cache pruned successfully:",
			expectedStderr: "",
		},
		{
			name:           "clear subcommand with help",
			args:           []string{"clear", "--help"},
			expectedCode:   0,
			expectedStdout: "Clear all entries in the local encrypted cache",
			expectedStderr: "",
		},
		{
			name:           "clear subcommand extra args",
			args:           []string{"clear", "extra"},
			expectedCode:   1,
			expectedStdout: "",
			expectedStderr: "Usage: cloakenv cache clear",
		},
		{
			name:           "clear subcommand valid empty config",
			args:           []string{"clear"},
			expectedCode:   0,
			expectedStdout: "Cache cleared successfully.",
			expectedStderr: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{}

			exitCode, stdout, stderr := captureOutputWithExitCode(t, func() int {
				return Cache(tt.args, cfg)
			})

			if exitCode != tt.expectedCode {
				t.Errorf("Cache() exit code = %d, want %d", exitCode, tt.expectedCode)
			}

			if tt.expectedStdout != "" && !strings.Contains(stdout, tt.expectedStdout) {
				t.Errorf("Cache() stdout = %q, want substring %q", stdout, tt.expectedStdout)
			}

			if tt.expectedStderr != "" && !strings.Contains(stderr, tt.expectedStderr) {
				t.Errorf("Cache() stderr = %q, want substring %q", stderr, tt.expectedStderr)
			}
		})
	}
}

func TestCachePruneExecution(t *testing.T) {
	keyring.MockInit()
	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)
	t.Setenv("XDG_CACHE_HOME", tempDir)
	t.Setenv("LocalAppData", tempDir)

	cfg := &config.Config{
		Vaults: make(map[string]config.VaultConfig),
	}

	// 1. Set an active secret with long TTL
	mockStdin(t, "active_secret_val\n")
	exitCode, _, stderr := captureOutputWithExitCode(t, func() int {
		return Set([]string{"cache://active_key", "--ttl", "1h"}, cfg)
	})
	if exitCode != 0 {
		t.Fatalf("Set active_key failed: %s", stderr)
	}

	// 2. Set an expired secret with 1ns TTL
	mockStdin(t, "expired_secret_val\n")
	exitCode, _, stderr = captureOutputWithExitCode(t, func() int {
		return Set([]string{"cache://expired_key", "--ttl", "1ns"}, cfg)
	})
	if exitCode != 0 {
		t.Fatalf("Set expired_key failed: %s", stderr)
	}

	// Wait briefly to guarantee expiration
	time.Sleep(10 * time.Millisecond)

	// 3. Run cache prune
	exitCode, stdout, stderr := captureOutputWithExitCode(t, func() int {
		return Cache([]string{"prune"}, cfg)
	})
	if exitCode != 0 {
		t.Fatalf("Cache prune failed with code %d, stderr: %s", exitCode, stderr)
	}

	if !strings.Contains(stdout, "1 expired entry removed") {
		t.Errorf("expected '1 expired entry removed' in stdout, got %q", stdout)
	}
	if !strings.Contains(stdout, "reclaimed") {
		t.Errorf("expected 'reclaimed' in stdout, got %q", stdout)
	}

	// 4. Verify active key is preserved and retrievable
	exitCode, stdout, stderr = captureOutputWithExitCode(t, func() int {
		return Get([]string{"cache://active_key"}, cfg)
	})
	if exitCode != 0 || stdout != "active_secret_val" {
		t.Errorf("Get active_key failed: code=%d, stdout=%q, stderr=%s", exitCode, stdout, stderr)
	}

	// 5. Verify expired key is gone
	exitCode, _, _ = captureOutputWithExitCode(t, func() int {
		return Get([]string{"cache://expired_key"}, cfg)
	})
	if exitCode == 0 {
		t.Errorf("expected expired_key to not be found after prune")
	}

	// 6. Running prune again reports 0 expired entries
	exitCode, stdout, stderr = captureOutputWithExitCode(t, func() int {
		return Cache([]string{"prune"}, cfg)
	})
	if exitCode != 0 {
		t.Fatalf("Second cache prune failed: %s", stderr)
	}
	if !strings.Contains(stdout, "0 expired entries removed (0 B reclaimed)") {
		t.Errorf("expected '0 expired entries removed (0 B reclaimed)' in stdout, got %q", stdout)
	}
}

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		bytes    int64
		expected string
	}{
		{0, "0 B"},
		{500, "500 B"},
		{1023, "1023 B"},
		{1024, "1.00 KB"},
		{1536, "1.50 KB"},
		{1024 * 1024, "1.00 MB"},
		{1024 * 1024 * 1024, "1.00 GB"},
		{1024 * 1024 * 1024 * 1024, "1.00 TB"},
		{1024 * 1024 * 1024 * 1024 * 1024, "1024.00 TB"},
		{5 * 1024 * 1024 * 1024 * 1024 * 1024, "5120.00 TB"},
	}

	for _, tt := range tests {
		got := formatBytes(tt.bytes)
		if got != tt.expected {
			t.Errorf("formatBytes(%d) = %q, want %q", tt.bytes, got, tt.expected)
		}
	}
}

func TestGet_Help(t *testing.T) {
	args := []string{"--help"}
	cfg := &config.Config{}

	exitCode, stdout, _ := captureOutputWithExitCode(t, func() int {
		return Get(args, cfg)
	})

	if exitCode != 0 {
		t.Errorf("expected exit code 0, got %d", exitCode)
	}

	if !strings.Contains(stdout, "Usage:") {
		t.Errorf("expected help output, got %q", stdout)
	}
}

func TestGet_InvalidArgs(t *testing.T) {
	cfg := &config.Config{}

	tests := []struct {
		name string
		args []string
	}{
		{"no args", []string{}},
		{"too many args", []string{"env://FOO", "extra"}},
		{"flag arg", []string{"-invalid"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			exitCode, _, stderr := captureOutputWithExitCode(t, func() int {
				return Get(tt.args, cfg)
			})

			if exitCode != 1 {
				t.Errorf("expected exit code 1, got %d", exitCode)
			}
			if !strings.Contains(stderr, "Usage: cloakenv get <uri>") {
				t.Errorf("expected usage output, got %q", stderr)
			}
		})
	}
}

func TestGet_Success(t *testing.T) {
	t.Setenv("GET_TEST_VAR", "test_value")

	args := []string{"env://GET_TEST_VAR"}
	cfg := &config.Config{}

	exitCode, stdout, _ := captureOutputWithExitCode(t, func() int {
		return Get(args, cfg)
	})

	if exitCode != 0 {
		t.Errorf("expected exit code 0, got %d", exitCode)
	}

	if stdout != "test_value" {
		t.Errorf("expected %q, got %q", "test_value", stdout)
	}
}

func TestGet_ResolutionError(t *testing.T) {
	args := []string{"env://NON_EXISTENT_VAR_FOR_TEST"}
	cfg := &config.Config{}

	exitCode, _, stderr := captureOutputWithExitCode(t, func() int {
		return Get(args, cfg)
	})

	if exitCode != 1 {
		t.Errorf("expected exit code 1, got %d", exitCode)
	}

	if !strings.Contains(stderr, "Resolution failed:") {
		t.Errorf("expected resolution failure message, got %q", stderr)
	}
}

func mockStdin(t *testing.T, content string) {
	t.Helper()
	oldStdin := os.Stdin
	rIn, wIn, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create stdin pipe: %v", err)
	}

	os.Stdin = rIn

	t.Cleanup(func() {
		os.Stdin = oldStdin
		if closeErr := rIn.Close(); closeErr != nil && !errors.Is(closeErr, os.ErrClosed) {
			t.Errorf("failed to close stdin reader pipe: %v", closeErr)
		}
	})

	defer func() {
		if closeErr := wIn.Close(); closeErr != nil && !errors.Is(closeErr, os.ErrClosed) {
			t.Errorf("failed to close stdin writer pipe: %v", closeErr)
		}
	}()

	if _, writeErr := wIn.Write([]byte(content)); writeErr != nil {
		t.Fatalf("failed to write to stdin pipe: %v", writeErr)
	}
}

// captureOutputWithExitCode captures stdout and stderr for the given function
func captureOutputWithExitCode(t *testing.T, f func() int) (int, string, string) {
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
	defer func() {
		if closeErr := wOut.Close(); closeErr != nil && !errors.Is(closeErr, os.ErrClosed) {
			t.Errorf("failed to close stdout write pipe: %v", closeErr)
		}
	}()

	rErr, wErr, err := os.Pipe()
	if err != nil {
		if closeErr := rOut.Close(); closeErr != nil && !errors.Is(closeErr, os.ErrClosed) {
			t.Errorf("failed to close stdout read pipe: %v", closeErr)
		}
		t.Fatalf("failed to create stderr pipe: %v", err)
	}
	defer func() {
		if closeErr := wErr.Close(); closeErr != nil && !errors.Is(closeErr, os.ErrClosed) {
			t.Errorf("failed to close stderr write pipe: %v", closeErr)
		}
	}()

	os.Stdout = wOut
	os.Stderr = wErr

	var outBuf, errBuf bytes.Buffer
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		if _, copyErr := io.Copy(&outBuf, rOut); copyErr != nil {
			t.Errorf("failed to read from stdout pipe: %v", copyErr)
		}
		if closeErr := rOut.Close(); closeErr != nil && !errors.Is(closeErr, os.ErrClosed) {
			t.Errorf("failed to close stdout read pipe: %v", closeErr)
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		if _, copyErr := io.Copy(&errBuf, rErr); copyErr != nil {
			t.Errorf("failed to read from stderr pipe: %v", copyErr)
		}
		if closeErr := rErr.Close(); closeErr != nil && !errors.Is(closeErr, os.ErrClosed) {
			t.Errorf("failed to close stderr read pipe: %v", closeErr)
		}
	}()

	exitCode := f()

	if closeErr := wOut.Close(); closeErr != nil && !errors.Is(closeErr, os.ErrClosed) {
		t.Errorf("failed to close stdout write pipe: %v", closeErr)
	}
	if closeErr := wErr.Close(); closeErr != nil && !errors.Is(closeErr, os.ErrClosed) {
		t.Errorf("failed to close stderr write pipe: %v", closeErr)
	}

	wg.Wait()

	return exitCode, outBuf.String(), errBuf.String()
}

func TestSet(t *testing.T) {
	t.Run("HelpFlag", func(t *testing.T) {
		exitCode, stdout, _ := captureOutputWithExitCode(t, func() int {
			return Set([]string{"--help"}, &config.Config{})
		})

		if exitCode != 0 {
			t.Errorf("expected exit code 0 for help flag, got %d", exitCode)
		}

		if !strings.Contains(stdout, "Usage:") {
			t.Errorf("expected help text, got %q", stdout)
		}
	})

	t.Run("InvalidArgs", func(t *testing.T) {
		exitCode, _, stderr := captureOutputWithExitCode(t, func() int {
			return Set([]string{}, &config.Config{})
		})

		if exitCode != 1 {
			t.Errorf("expected exit code 1 for invalid args, got %d", exitCode)
		}

		if !strings.Contains(stderr, "Usage:") {
			t.Errorf("expected usage text, got %q", stderr)
		}
	})

	t.Run("MissingTTLValue", func(t *testing.T) {
		exitCode, _, stderr := captureOutputWithExitCode(t, func() int {
			return Set([]string{"cache://test", "--ttl"}, &config.Config{})
		})

		if exitCode != 1 {
			t.Errorf("expected exit code 1 for missing TTL value, got %d", exitCode)
		}

		if !strings.Contains(stderr, "missing value for --ttl flag") {
			t.Errorf("expected missing value error, got %q", stderr)
		}
	})

	t.Run("InvalidTTL", func(t *testing.T) {
		exitCode, _, stderr := captureOutputWithExitCode(t, func() int {
			return Set([]string{"cache://test", "--ttl", "invalid"}, &config.Config{})
		})

		if exitCode != 1 {
			t.Errorf("expected exit code 1 for invalid TTL format, got %d", exitCode)
		}

		if !strings.Contains(stderr, "invalid TTL duration format:") {
			t.Errorf("expected invalid TTL format error, got %q", stderr)
		}
	})

	t.Run("TooManyArgs", func(t *testing.T) {
		exitCode, _, stderr := captureOutputWithExitCode(t, func() int {
			return Set([]string{"cache://test", "extra"}, &config.Config{})
		})

		if exitCode != 1 {
			t.Errorf("expected exit code 1 for too many args, got %d", exitCode)
		}

		if !strings.Contains(stderr, "Usage:") {
			t.Errorf("expected usage text, got %q", stderr)
		}
	})

	t.Run("InvalidURI", func(t *testing.T) {
		mockStdin(t, "value")

		exitCode, _, stderr := captureOutputWithExitCode(t, func() int {
			return Set([]string{"invalid-uri"}, &config.Config{})
		})

		if exitCode != 1 {
			t.Errorf("expected exit code 1 for invalid URI, got %d", exitCode)
		}

		if !strings.Contains(stderr, "Invalid URI format:") {
			t.Errorf("expected Invalid URI format error, got %q", stderr)
		}
	})

	t.Run("TTLNotSupported", func(t *testing.T) {
		mockStdin(t, "value")

		exitCode, _, stderr := captureOutputWithExitCode(t, func() int {
			return Set([]string{"env://test", "--ttl", "1h"}, &config.Config{})
		})

		if exitCode != 1 {
			t.Errorf("expected exit code 1 for unsupported TTL scheme, got %d", exitCode)
		}

		if !strings.Contains(stderr, "flag --ttl is only supported by the 'cache' provider") {
			t.Errorf("expected unsupported TTL scheme error, got %q", stderr)
		}
	})

	t.Run("FallbackTTLInvalid", func(t *testing.T) {
		cfg := &config.Config{
			Cache: config.CacheConfig{DefaultTTL: "invalid"},
		}
		mockStdin(t, "value")

		exitCode, _, stderr := captureOutputWithExitCode(t, func() int {
			return Set([]string{"cache://test"}, cfg)
		})

		if exitCode != 1 {
			t.Errorf("expected exit code 1 for invalid default TTL, got %d", exitCode)
		}

		if !strings.Contains(stderr, "Invalid default_ttl in global config:") {
			t.Errorf("expected invalid default TTL config error, got %q", stderr)
		}
	})

	t.Run("ConfigError", func(t *testing.T) {
		cfg := &config.Config{
			Vaults: map[string]config.VaultConfig{
				"bad": {Provider: "invalid_provider"},
			},
		}
		mockStdin(t, "value")

		exitCode, _, stderr := captureOutputWithExitCode(t, func() int {
			return Set([]string{"cache://test"}, cfg)
		})

		if exitCode != 1 {
			t.Errorf("expected exit code 1 for config error, got %d", exitCode)
		}

		if !strings.Contains(stderr, "Config error:") {
			t.Errorf("expected Config error message, got %q", stderr)
		}
	})

	t.Run("Success_Stdin", func(t *testing.T) {
		keyring.MockInit()
		cfg := &config.Config{
			Vaults: make(map[string]config.VaultConfig),
		}

		cacheDir := t.TempDir()
		t.Setenv("XDG_CACHE_HOME", cacheDir)
		t.Setenv("CLOAKENV_ENCRYPTION_KEY", "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff")

		mockStdin(t, "stdin_value\n")

		exitCode, _, stderr := captureOutputWithExitCode(t, func() int {
			return Set([]string{"cache://test"}, cfg)
		})

		if exitCode != 0 {
			t.Errorf("expected exit code 0 for successful set via stdin, got %d (stderr: %s)", exitCode, stderr)
		}

		exitCode, stdout, _ := captureOutputWithExitCode(t, func() int {
			return Get([]string{"cache://test"}, cfg)
		})
		if exitCode != 0 || stdout != "stdin_value" {
			t.Errorf("expected value %q, got %q (exit code %d)", "stdin_value", stdout, exitCode)
		}
	})

	t.Run("Success_CRLF", func(t *testing.T) {
		keyring.MockInit()
		cfg := &config.Config{
			Vaults: make(map[string]config.VaultConfig),
		}

		cacheDir := t.TempDir()
		t.Setenv("XDG_CACHE_HOME", cacheDir)
		t.Setenv("CLOAKENV_ENCRYPTION_KEY", "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff")

		mockStdin(t, "crlf_value\r\n")

		exitCode, _, stderr := captureOutputWithExitCode(t, func() int {
			return Set([]string{"cache://test_crlf"}, cfg)
		})

		if exitCode != 0 {
			t.Errorf("expected exit code 0 for successful set with CRLF, got %d (stderr: %s)", exitCode, stderr)
		}

		exitCode, stdout, _ := captureOutputWithExitCode(t, func() int {
			return Get([]string{"cache://test_crlf"}, cfg)
		})
		if exitCode != 0 || stdout != "crlf_value" {
			t.Errorf("expected crlf_value without CRLF, got %q (exit: %d)", stdout, exitCode)
		}
	})

	t.Run("ExceedsMaxSize", func(t *testing.T) {
		keyring.MockInit()
		cfg := &config.Config{
			Vaults: make(map[string]config.VaultConfig),
		}

		oldStdin := os.Stdin
		rIn, wIn, err := os.Pipe()
		if err != nil {
			t.Fatalf("failed to create stdin pipe: %v", err)
		}
		os.Stdin = rIn
		t.Cleanup(func() {
			os.Stdin = oldStdin
			_ = rIn.Close()
		})

		go func() {
			defer func() { _ = wIn.Close() }()
			chunk := bytes.Repeat([]byte("A"), 64*1024)
			total := maxSecretInputSize + 1024
			written := 0
			for written < total {
				toWrite := len(chunk)
				if total-written < toWrite {
					toWrite = total - written
				}
				n, wErr := wIn.Write(chunk[:toWrite])
				if wErr != nil {
					return
				}
				written += n
			}
		}()

		exitCode, _, stderr := captureOutputWithExitCode(t, func() int {
			return Set([]string{"cache://test_oversized"}, cfg)
		})

		if exitCode != 1 {
			t.Errorf("expected exit code 1 for oversized input, got %d", exitCode)
		}

		if !strings.Contains(stderr, "Secret value exceeds maximum size limit") {
			t.Errorf("expected maximum size limit error message, got %q", stderr)
		}
	})

	t.Run("Interactive_Success", func(t *testing.T) {
		keyring.MockInit()
		cfg := &config.Config{
			Vaults: make(map[string]config.VaultConfig),
		}

		cacheDir := t.TempDir()
		t.Setenv("XDG_CACHE_HOME", cacheDir)
		t.Setenv("CLOAKENV_ENCRYPTION_KEY", "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff")

		origIsTerminal := isTerminal
		origReadPassword := readPassword
		t.Cleanup(func() {
			isTerminal = origIsTerminal
			readPassword = origReadPassword
		})

		isTerminal = func(fd int) bool { return true }
		passwords := [][]byte{[]byte("secret123"), []byte("secret123")}
		callCount := 0
		readPassword = func(fd int) ([]byte, error) {
			if callCount < len(passwords) {
				p := passwords[callCount]
				callCount++
				return p, nil
			}
			return nil, errors.New("unexpected readPassword call")
		}

		exitCode, _, stderr := captureOutputWithExitCode(t, func() int {
			return Set([]string{"cache://test_interactive"}, cfg)
		})

		if exitCode != 0 {
			t.Errorf("expected exit code 0 for matching interactive input, got %d (stderr: %s)", exitCode, stderr)
		}

		if !strings.Contains(stderr, "Enter secret value: ") {
			t.Errorf("expected 'Enter secret value: ' in stderr, got %q", stderr)
		}
		if !strings.Contains(stderr, "Confirm secret: ") {
			t.Errorf("expected 'Confirm secret: ' in stderr, got %q", stderr)
		}

		exitCode, stdout, _ := captureOutputWithExitCode(t, func() int {
			return Get([]string{"cache://test_interactive"}, cfg)
		})
		if exitCode != 0 || stdout != "secret123" {
			t.Errorf("expected stored secret 'secret123', got %q (exit: %d)", stdout, exitCode)
		}
	})

	t.Run("Interactive_Mismatch", func(t *testing.T) {
		keyring.MockInit()
		cfg := &config.Config{
			Vaults: make(map[string]config.VaultConfig),
		}

		cacheDir := t.TempDir()
		t.Setenv("XDG_CACHE_HOME", cacheDir)
		t.Setenv("CLOAKENV_ENCRYPTION_KEY", "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff")

		origIsTerminal := isTerminal
		origReadPassword := readPassword
		t.Cleanup(func() {
			isTerminal = origIsTerminal
			readPassword = origReadPassword
		})

		isTerminal = func(fd int) bool { return true }
		passwords := [][]byte{[]byte("secret123"), []byte("secret456")}
		callCount := 0
		readPassword = func(fd int) ([]byte, error) {
			if callCount < len(passwords) {
				p := passwords[callCount]
				callCount++
				return p, nil
			}
			return nil, errors.New("unexpected readPassword call")
		}

		exitCode, _, stderr := captureOutputWithExitCode(t, func() int {
			return Set([]string{"cache://test_mismatch"}, cfg)
		})

		if exitCode != 1 {
			t.Errorf("expected exit code 1 for mismatched confirmation, got %d", exitCode)
		}

		if !strings.Contains(stderr, "Enter secret value: ") {
			t.Errorf("expected 'Enter secret value: ' in stderr, got %q", stderr)
		}
		if !strings.Contains(stderr, "Confirm secret: ") {
			t.Errorf("expected 'Confirm secret: ' in stderr, got %q", stderr)
		}
		if !strings.Contains(stderr, "Error: secret confirmation does not match") {
			t.Errorf("expected mismatch error in stderr, got %q", stderr)
		}

		// Ensure nothing was written
		exitCode, _, _ = captureOutputWithExitCode(t, func() int {
			return Get([]string{"cache://test_mismatch"}, cfg)
		})
		if exitCode == 0 {
			t.Errorf("expected secret not to be written on confirmation mismatch")
		}
	})

	t.Run("Interactive_FirstPromptReadError", func(t *testing.T) {
		origIsTerminal := isTerminal
		origReadPassword := readPassword
		t.Cleanup(func() {
			isTerminal = origIsTerminal
			readPassword = origReadPassword
		})

		isTerminal = func(fd int) bool { return true }
		readPassword = func(fd int) ([]byte, error) {
			return nil, errors.New("read error on first prompt")
		}

		exitCode, _, stderr := captureOutputWithExitCode(t, func() int {
			return Set([]string{"cache://test_err1"}, &config.Config{})
		})

		if exitCode != 1 {
			t.Errorf("expected exit code 1 on first prompt error, got %d", exitCode)
		}
		if !strings.Contains(stderr, "Failed to read secret: read error on first prompt") {
			t.Errorf("expected error message in stderr, got %q", stderr)
		}
	})

	t.Run("Interactive_SecondPromptReadError", func(t *testing.T) {
		origIsTerminal := isTerminal
		origReadPassword := readPassword
		t.Cleanup(func() {
			isTerminal = origIsTerminal
			readPassword = origReadPassword
		})

		isTerminal = func(fd int) bool { return true }
		callCount := 0
		readPassword = func(fd int) ([]byte, error) {
			callCount++
			if callCount == 1 {
				return []byte("secret123"), nil
			}
			return nil, errors.New("read error on confirmation prompt")
		}

		exitCode, _, stderr := captureOutputWithExitCode(t, func() int {
			return Set([]string{"cache://test_err2"}, &config.Config{})
		})

		if exitCode != 1 {
			t.Errorf("expected exit code 1 on second prompt error, got %d", exitCode)
		}
		if !strings.Contains(stderr, "Failed to read confirmation: read error on confirmation prompt") {
			t.Errorf("expected confirmation error message in stderr, got %q", stderr)
		}
	})
}

func TestDelete(t *testing.T) {
	t.Run("HelpFlag", func(t *testing.T) {
		exitCode, stdout, _ := captureOutputWithExitCode(t, func() int {
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
		exitCode, _, stderr := captureOutputWithExitCode(t, func() int {
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
		exitCode, _, stderr := captureOutputWithExitCode(t, func() int {
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
		mockStdin(t, "testvalue")

		captureOutputWithExitCode(t, func() int {
			return Set([]string{"cache://test"}, cfg)
		})

		exitCode, stdout, _ := captureOutputWithExitCode(t, func() int {
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

		exitCode, _, stderr := captureOutputWithExitCode(t, func() int {
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

		exitCode, _, stderr := captureOutputWithExitCode(t, func() int {
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
