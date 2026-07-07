package cmd

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/warpcode/cloakenv/internal/config"
)

func TestShow_KeysFormat(t *testing.T) {
	// Keep original stdout
	oldStdout := os.Stdout
	defer func() { os.Stdout = oldStdout }()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create pipe: %v", err)
	}
	os.Stdout = w

	// Set up environment variables for testing env:// resolution
	t.Setenv("SHOW_TEST_VAR_A", "valA")
	t.Setenv("SHOW_TEST_VAR_B", "valB")

	// Call Show with keys format (with keys that should sort B before A)
	args := []string{"-e", "KEY_B=env://SHOW_TEST_VAR_B", "-e", "KEY_A=env://SHOW_TEST_VAR_A", "-o", "keys"}
	cfg := &config.Config{
		Vaults: make(map[string]config.VaultConfig),
	}

	exitCode := Show(args, cfg)
	w.Close()

	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatalf("failed to read from pipe: %v", err)
	}

	if exitCode != 0 {
		t.Errorf("expected exit code 0, got %d", exitCode)
	}

	actualOutput := buf.String()
	// Replace CRLF with LF to be cross-platform friendly
	actualOutput = strings.ReplaceAll(actualOutput, "\r\n", "\n")
	lines := strings.Split(strings.TrimSpace(actualOutput), "\n")

	expectedKeys := map[string]bool{
		"KEY_A": true,
		"KEY_B": true,
	}

	if len(lines) != len(expectedKeys) {
		t.Fatalf("expected %d keys, got %d. Output: %q", len(expectedKeys), len(lines), actualOutput)
	}

	for _, line := range lines {
		if !expectedKeys[line] {
			t.Errorf("unexpected key in output: %q", line)
		}
	}
}

func TestShow_KeysFormattingBehavior(t *testing.T) {
	oldStdout := os.Stdout
	defer func() { os.Stdout = oldStdout }()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create pipe: %v", err)
	}
	os.Stdout = w

	t.Setenv("SHOW_TEST_VAR_A", "valA")
	t.Setenv("SHOW_TEST_VAR_B", "valB")

	// Explicit keys with lowercase, hyphens, and multiple underscores
	args := []string{
		"-e", "db-user=env://SHOW_TEST_VAR_B",
		"-e", "api--key=env://SHOW_TEST_VAR_A",
		"-e", "multiple___underscores=env://SHOW_TEST_VAR_A",
		"-o", "keys",
	}
	cfg := &config.Config{
		Vaults: make(map[string]config.VaultConfig),
	}

	exitCode := Show(args, cfg)
	w.Close()

	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatalf("failed to read from pipe: %v", err)
	}

	if exitCode != 0 {
		t.Errorf("expected exit code 0, got %d", exitCode)
	}

	actualOutput := buf.String()
	actualOutput = strings.ReplaceAll(actualOutput, "\r\n", "\n")
	lines := strings.Split(strings.TrimSpace(actualOutput), "\n")

	expectedKeys := map[string]bool{
		"DB_USER":              true,
		"API_KEY":              true,
		"MULTIPLE_UNDERSCORES": true,
	}

	if len(lines) != len(expectedKeys) {
		t.Fatalf("expected %d keys, got %d. Output: %q", len(expectedKeys), len(lines), actualOutput)
	}

	for _, line := range lines {
		if !expectedKeys[line] {
			t.Errorf("unexpected formatted key in output: %q", line)
		}
	}
}
