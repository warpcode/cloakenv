package cmd

import (
	"bytes"
	"io"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/warpcode/cloakenv/internal/config"
	"github.com/zalando/go-keyring"
)

func captureOutput(t *testing.T, f func()) (string, string) {
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

	var outBuf, errBuf bytes.Buffer
	var wg sync.WaitGroup
	var errOut, errErr error

	wg.Add(1)
	go func() {
		defer wg.Done()
		_, errOut = io.Copy(&outBuf, rOut)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		_, errErr = io.Copy(&errBuf, rErr)
	}()

	f()

	// wOut and wErr will be closed by deferred calls above when captureOutput returns.
	// We need to close them here to unblock the readers in the goroutines.
	// The deferred calls will be no-ops if they are already closed.
	wOut.Close()
	wErr.Close()

	wg.Wait()
	rOut.Close()
	rErr.Close()

	if errOut != nil {
		t.Errorf("failed to copy stdout: %v", errOut)
	}
	if errErr != nil {
		t.Errorf("failed to copy stderr: %v", errErr)
	}

	return outBuf.String(), errBuf.String()
}

func TestAuth_Routing(t *testing.T) {
	tests := []struct {
		name           string
		args           []string
		expectedOutput string
		expectedError  string
		expectedCode   int
	}{
		{
			name:           "help flag",
			args:           []string{"--help"},
			expectedOutput: "Usage:",
			expectedCode:   0,
		},
		{
			name:          "no subcommand",
			args:          []string{},
			expectedError: "Usage:",
			expectedCode:  1,
		},
		{
			name:          "unknown subcommand",
			args:          []string{"unknown"},
			expectedError: "Unknown auth subcommand: unknown",
			expectedCode:  1,
		},
		{
			name:           "status no vaults",
			args:           []string{"status"},
			expectedOutput: "No vaults configured.",
			expectedCode:   0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.Config{
				Vaults: make(map[string]config.VaultConfig),
			}

			var exitCode int
			stdout, stderr := captureOutput(t, func() {
				exitCode = Auth(tc.args, cfg)
			})

			if exitCode != tc.expectedCode {
				t.Errorf("expected exit code %d, got %d", tc.expectedCode, exitCode)
			}

			if tc.expectedOutput != "" && !strings.Contains(stdout, tc.expectedOutput) {
				t.Errorf("expected stdout to contain %q, got %q", tc.expectedOutput, stdout)
			}

			if tc.expectedError != "" && !strings.Contains(stderr, tc.expectedError) {
				t.Errorf("expected stderr to contain %q, got %q", tc.expectedError, stderr)
			}
		})
	}
}

func TestAuth_Login(t *testing.T) {
	keyring.MockInit()

	cfg := &config.Config{
		Vaults: map[string]config.VaultConfig{
			"dummy": {
				Provider:  "json",
				VaultPath: "/fake/path",
			},
			"mykp": {
				Provider:  "keepass",
				VaultPath: "/fake/path.kdbx",
			},
		},
	}

	tests := []struct {
		name          string
		args          []string
		expectedError string
		expectedCode  int
	}{
		{
			name:          "missing vault argument",
			args:          []string{"login"},
			expectedError: "Usage: cloakenv auth login <scheme>",
			expectedCode:  1,
		},
		{
			name:          "unknown vault",
			args:          []string{"login", "unknown"},
			expectedError: "Authentication failed: unknown vault/scheme: \"unknown\"",
			expectedCode:  1,
		},
		{
			name:          "unsupported scheme",
			args:          []string{"login", "dummy"},
			expectedError: "Authentication failed: vault/scheme \"dummy\" of type \"json\" does not support authentication",
			expectedCode:  1,
		},
		{
			name:          "keepass scheme without terminal",
			args:          []string{"login", "mykp"},
			expectedError: "Authentication failed: keepass provider: no credentials found for remote \"mykp\" and stdin is not a terminal",
			expectedCode:  1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var exitCode int
			_, stderr := captureOutput(t, func() {
				exitCode = Auth(tc.args, cfg)
			})

			if exitCode != tc.expectedCode {
				t.Errorf("expected exit code %d, got %d", tc.expectedCode, exitCode)
			}

			if !strings.Contains(stderr, tc.expectedError) {
				t.Errorf("expected stderr to contain %q, got %q", tc.expectedError, stderr)
			}
		})
	}
}

func TestAuth_Forget(t *testing.T) {
	keyring.MockInit()
	// Set a dummy password to forget
	_ = keyring.Set("cloakenv", "provider/mykp", "secret123")

	cfg := &config.Config{
		Vaults: map[string]config.VaultConfig{
			"mykp": {
				Provider:  "keepass",
				VaultPath: "/fake/path.kdbx",
			},
			"dummy": {
				Provider:  "json",
				VaultPath: "/fake/path",
			},
			"mykp_forgotten": {
				Provider:  "keepass",
				VaultPath: "/fake/path.kdbx",
			},
		},
	}

	tests := []struct {
		name           string
		args           []string
		expectedOutput string
		expectedError  string
		expectedCode   int
		checkKeyring   bool
		keyringScheme  string
	}{
		{
			name:          "missing vault argument",
			args:          []string{"forget"},
			expectedError: "Usage: cloakenv auth forget <scheme>",
			expectedCode:  1,
		},
		{
			name:          "unknown vault",
			args:          []string{"forget", "unknown"},
			expectedError: "Failed to clear credentials: unknown vault/scheme: \"unknown\"",
			expectedCode:  1,
		},
		{
			name:          "unsupported scheme",
			args:          []string{"forget", "dummy"},
			expectedError: "Failed to clear credentials: vault/scheme \"dummy\" of type \"json\" does not support authentication",
			expectedCode:  1,
		},
		{
			name:           "forget existing credentials",
			args:           []string{"forget", "mykp"},
			expectedOutput: "Successfully cleared credentials for scheme \"mykp\"",
			expectedCode:   0,
			checkKeyring:   true,
			keyringScheme:  "mykp",
		},
		{
			name:          "forget non-existent credentials",
			args:          []string{"forget", "mykp_forgotten"},
			expectedError: "Failed to clear credentials: secret not found in keyring", // It returns an error if keyring.Delete fails
			expectedCode:  1,                                                          // So the exit code should be 1
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var exitCode int
			stdout, stderr := captureOutput(t, func() {
				exitCode = Auth(tc.args, cfg)
			})

			if exitCode != tc.expectedCode {
				t.Errorf("expected exit code %d, got %d", tc.expectedCode, exitCode)
			}

			if tc.expectedOutput != "" && !strings.Contains(stdout, tc.expectedOutput) {
				t.Errorf("expected stdout to contain %q, got %q", tc.expectedOutput, stdout)
			}

			if tc.expectedError != "" && !strings.Contains(stderr, tc.expectedError) {
				t.Errorf("expected stderr to contain %q, got %q", tc.expectedError, stderr)
			}

			if tc.checkKeyring {
				_, err := keyring.Get("cloakenv", "provider/"+tc.keyringScheme)
				if err == nil {
					t.Errorf("expected credentials for %q to be deleted, but they were still found", tc.keyringScheme)
				}
			}
		})
	}
}

func TestAuth_Status(t *testing.T) {
	keyring.MockInit()
	// Provide valid credentials for one of the vaults
	_ = keyring.Set("cloakenv", "provider/mykp_auth", "secret123")

	cfg := &config.Config{
		Vaults: map[string]config.VaultConfig{
			"mykp_noauth": {
				Provider:  "keepass",
				VaultPath: "/fake/path1.kdbx",
			},
			"mykp_auth": {
				Provider:  "keepass",
				VaultPath: "/fake/path2.kdbx",
			},
			"dummy": {
				Provider:  "json",
				VaultPath: "/fake/path3.json",
			},
		},
	}

	tests := []struct {
		name           string
		args           []string
		expectedOutput string
		expectedCode   int
	}{
		{
			name:           "status specific vault no auth required",
			args:           []string{"status", "dummy"},
			expectedOutput: "dummy: ACTIVE",
			expectedCode:   0,
		},
		{
			name:           "status specific vault missing auth",
			args:           []string{"status", "mykp_noauth"},
			expectedOutput: "mykp_noauth: ERROR: failed to initialize vault \"mykp_noauth\": no credentials found for remote \"mykp_noauth\"; please log in first using 'cloakenv auth login mykp_noauth'",
			expectedCode:   1,
		},
		{
			name: "status specific vault with auth but bad path",
			args: []string{"status", "mykp_auth"},
			// Will fail to open the file because /fake/path2.kdbx does not exist, but we got past the credential check
			expectedOutput: "mykp_auth: ERROR: failed to initialize vault \"mykp_auth\": decryption failed using credentials from keyring. The stored password may be incorrect. Please log in again using 'cloakenv auth login mykp_auth'",
			expectedCode:   1,
		},
		{
			name:           "status all vaults",
			args:           []string{"status"},
			expectedOutput: "mykp_noauth: ERROR:",
			expectedCode:   1, // Overall command should fail if any vault fails
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var exitCode int
			stdout, _ := captureOutput(t, func() {
				exitCode = Auth(tc.args, cfg)
			})

			if exitCode != tc.expectedCode {
				t.Errorf("expected exit code %d, got %d", tc.expectedCode, exitCode)
			}

			if !strings.Contains(stdout, tc.expectedOutput) {
				t.Errorf("expected stdout to contain %q, got %q", tc.expectedOutput, stdout)
			}
		})
	}
}
