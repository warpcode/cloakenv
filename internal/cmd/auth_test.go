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

func captureOutput(f func()) (string, string) {
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

	var wg sync.WaitGroup
	var bufOut bytes.Buffer
	var bufErr bytes.Buffer

	wg.Add(2)
	go func() {
		defer wg.Done()
		io.Copy(&bufOut, rOut)
	}()

	go func() {
		defer wg.Done()
		io.Copy(&bufErr, rErr)
	}()

	f()

	wOut.Close()
	wErr.Close()

	wg.Wait()

	return bufOut.String(), bufErr.String()
}

func TestAuth_RoutingAndHelp(t *testing.T) {
	tests := []struct {
		name           string
		args           []string
		expectedOutput string
		expectedError  string
		expectedCode   int
	}{
		{
			name:          "no args",
			args:          []string{},
			expectedError: "Usage: cloakenv auth <login|forget|status> [vault]",
			expectedCode:  1,
		},
		{
			name:           "help flag on auth",
			args:           []string{"--help"},
			expectedOutput: "cloakenv auth",
			expectedCode:   0,
		},
		{
			name:           "help flag on login",
			args:           []string{"login", "--help"},
			expectedOutput: "cloakenv auth login",
			expectedCode:   0,
		},
		{
			name:          "login missing scheme",
			args:          []string{"login"},
			expectedError: "Usage: cloakenv auth login <scheme>",
			expectedCode:  1,
		},
		{
			name:           "help flag on forget",
			args:           []string{"forget", "--help"},
			expectedOutput: "cloakenv auth forget",
			expectedCode:   0,
		},
		{
			name:          "forget missing scheme",
			args:          []string{"forget"},
			expectedError: "Usage: cloakenv auth forget <scheme>",
			expectedCode:  1,
		},
		{
			name:           "help flag on status",
			args:           []string{"status", "--help"},
			expectedOutput: "cloakenv auth status",
			expectedCode:   0,
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
			stdout, stderr := captureOutput(func() {
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
				Provider:  "json", // Valid built-in to avoid 'Config error: unsupported provider type'
				VaultPath: "/fake/path.json",
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
			name:          "unknown scheme",
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
			_, stderr := captureOutput(func() {
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
	keyring.Set("cloakenv", "provider/mykp", "secret123")
	keyring.Set("cloakenv", "provider/mykp_forgotten", "secret123")

	cfg := &config.Config{
		Vaults: map[string]config.VaultConfig{
			"dummy": {
				Provider:  "json", // Valid built-in
				VaultPath: "/fake/path.json",
			},
			"mykp": {
				Provider:  "keepass",
				VaultPath: "/fake/path.kdbx",
			},
			"mykp_forgotten": {
				Provider:  "keepass",
				VaultPath: "/fake/path.kdbx",
			},
		},
	}

	// Delete it directly from the keyring so it's "non-existent"
	keyring.Delete("cloakenv", "provider/mykp_forgotten")

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
			name:          "unknown scheme",
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
			stdout, stderr := captureOutput(func() {
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
	keyring.Set("cloakenv", "provider/mykp_auth", "secret123")

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
		},
	}

	tests := []struct {
		name           string
		args           []string
		expectedOutput string
		expectedCode   int
	}{
		{
			name:           "status specific vault without auth",
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
			stdout, _ := captureOutput(func() {
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
