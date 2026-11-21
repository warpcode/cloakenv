package cmd

import (
	"bytes"
	"context"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/zalando/go-keyring"

	"github.com/warpcode/cloakenv/internal/config"
	"github.com/warpcode/cloakenv/internal/provider"
)

func captureOutputWithExitCode(t *testing.T, f func() int) (int, string, string) {
	t.Helper()
	oldStdout := os.Stdout
	oldStderr := os.Stderr

	rOut, wOut, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create stdout pipe: %v", err)
	}
	rErr, wErr, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create stderr pipe: %v", err)
	}

	os.Stdout = wOut
	os.Stderr = wErr

	outCh := make(chan string, 1)
	errCh := make(chan string, 1)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, rOut)
		outCh <- buf.String()
	}()
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, rErr)
		errCh <- buf.String()
	}()

	code := f()

	_ = wOut.Close()
	_ = wErr.Close()
	os.Stdout = oldStdout
	os.Stderr = oldStderr

	stdout := <-outCh
	stderr := <-errCh
	_ = rOut.Close()
	_ = rErr.Close()
	return code, stdout, stderr
}

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
		},
		{
			name:           "no args",
			args:           []string{},
			expectedCode:   1,
			expectedStderr: "Usage: cloakenv cache <clear|prune>",
		},
		{
			name:           "invalid subcommand",
			args:           []string{"invalid"},
			expectedCode:   1,
			expectedStderr: "Usage: cloakenv cache <clear|prune>",
		},
		{
			name:           "prune subcommand with help",
			args:           []string{"prune", "--help"},
			expectedCode:   0,
			expectedStdout: "Remove expired entries",
		},
		{
			name:           "prune subcommand extra args",
			args:           []string{"prune", "extra"},
			expectedCode:   1,
			expectedStderr: "Usage: cloakenv cache prune",
		},
		{
			name:           "prune subcommand valid empty config",
			args:           []string{"prune"},
			expectedCode:   0,
			expectedStdout: "Cache pruned successfully:",
		},
		{
			name:           "clear subcommand with help",
			args:           []string{"clear", "--help"},
			expectedCode:   0,
			expectedStdout: "Clear all entries in the local encrypted cache",
		},
		{
			name:           "clear subcommand extra args",
			args:           []string{"clear", "extra"},
			expectedCode:   1,
			expectedStderr: "Usage: cloakenv cache clear",
		},
		{
			name:           "clear subcommand valid empty config",
			args:           []string{"clear"},
			expectedCode:   0,
			expectedStdout: "Cache cleared successfully.",
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

	ctx := context.Background()
	cp := provider.NewCacheProvider()
	if err := cp.Initialize(ctx, provider.ProviderConfig{
		Settings: map[string]string{"keyring_prefix": cfg.KeyringPrefix()},
	}); err != nil {
		t.Fatalf("CacheProvider.Initialize failed: %v", err)
	}
	activeCtx := context.WithValue(ctx, provider.ContextKeyTTL, time.Hour)
	if err := cp.SetSecret(activeCtx, "active_key", "active_secret_val"); err != nil {
		t.Fatalf("SetSecret active_key failed: %v", err)
	}
	expiredCtx := context.WithValue(ctx, provider.ContextKeyTTL, time.Nanosecond)
	if err := cp.SetSecret(expiredCtx, "expired_key", "expired_secret_val"); err != nil {
		t.Fatalf("SetSecret expired_key failed: %v", err)
	}

	time.Sleep(10 * time.Millisecond)

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

	if val, err := cp.GetSecret(ctx, "active_key"); err != nil || val != "active_secret_val" {
		t.Errorf("active_key = (%q, %v), want ('active_secret_val', nil)", val, err)
	}
	if _, err := cp.GetSecret(ctx, "expired_key"); err == nil {
		t.Errorf("expected expired_key to be pruned")
	}

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
		if got := formatBytes(tt.bytes); got != tt.expected {
			t.Errorf("formatBytes(%d) = %q, want %q", tt.bytes, got, tt.expected)
		}
	}
}
