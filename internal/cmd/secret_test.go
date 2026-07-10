package cmd

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/warpcode/cloakenv/internal/config"
)

func TestGet_Help(t *testing.T) {
	args := []string{"--help"}
	cfg := &config.Config{}

	oldStdout := os.Stdout
	defer func() { os.Stdout = oldStdout }()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w

	exitCode := Get(args, cfg)
	w.Close()

	if exitCode != 0 {
		t.Errorf("expected exit code 0, got %d", exitCode)
	}

	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil { t.Fatalf("failed to read from pipe: %v", err) }
	if !strings.Contains(buf.String(), "Usage:") {
		t.Errorf("expected help output, got %q", buf.String())
	}
}

func TestGet_InvalidArgs(t *testing.T) {
	cfg := &config.Config{}
	oldStderr := os.Stderr
	defer func() { os.Stderr = oldStderr }()

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
			r, w, _ := os.Pipe()
			os.Stderr = w

			exitCode := Get(tt.args, cfg)

			w.Close()
			var buf bytes.Buffer
			if _, err := io.Copy(&buf, r); err != nil { t.Fatalf("failed to read from pipe: %v", err) }

			if exitCode != 1 {
				t.Errorf("expected exit code 1, got %d", exitCode)
			}
			if !strings.Contains(buf.String(), "Usage: cloakenv get <uri>") {
				t.Errorf("expected usage output, got %q", buf.String())
			}
		})
	}
}

func TestGet_Success(t *testing.T) {
	oldStdout := os.Stdout
	defer func() { os.Stdout = oldStdout }()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create pipe: %v", err)
	}
	os.Stdout = w

	t.Setenv("GET_TEST_VAR", "test_value")

	args := []string{"env://GET_TEST_VAR"}
	cfg := &config.Config{}

	exitCode := Get(args, cfg)
	w.Close()

	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatalf("failed to read from pipe: %v", err)
	}

	if exitCode != 0 {
		t.Errorf("expected exit code 0, got %d", exitCode)
	}

	actualOutput := buf.String()
	if actualOutput != "test_value" {
		t.Errorf("expected %q, got %q", "test_value", actualOutput)
	}
}

func TestGet_ResolutionError(t *testing.T) {
	oldStderr := os.Stderr
	defer func() { os.Stderr = oldStderr }()

	r, w, _ := os.Pipe()
	os.Stderr = w

	args := []string{"env://NON_EXISTENT_VAR_FOR_TEST"}
	cfg := &config.Config{}

	exitCode := Get(args, cfg)
	w.Close()

	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil { t.Fatalf("failed to read from pipe: %v", err) }

	if exitCode != 1 {
		t.Errorf("expected exit code 1, got %d", exitCode)
	}

	if !strings.Contains(buf.String(), "Resolution failed:") {
		t.Errorf("expected resolution failure message, got %q", buf.String())
	}
}
