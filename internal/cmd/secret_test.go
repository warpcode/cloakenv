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
			expectedStderr: "Usage: cloakenv cache clear",
		},
		{
			name:           "invalid subcommand",
			args:           []string{"invalid"},
			expectedCode:   1,
			expectedStdout: "",
			expectedStderr: "Usage: cloakenv cache clear",
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
			oldStdout := os.Stdout
			oldStderr := os.Stderr

			rOut, wOut, errOut := os.Pipe()
			if errOut != nil {
				t.Fatalf("failed to create stdout pipe: %v", errOut)
			}
			defer rOut.Close()
			defer wOut.Close()

			rErr, wErr, errErr := os.Pipe()
			if errErr != nil {
				t.Fatalf("failed to create stderr pipe: %v", errErr)
			}
			defer rErr.Close()
			defer wErr.Close()

			os.Stdout = wOut
			os.Stderr = wErr

			defer func() {
				os.Stdout = oldStdout
				os.Stderr = oldStderr
			}()

			cfg := &config.Config{}

			exitCode := Cache(tt.args, cfg)

			wOut.Close()
			wErr.Close()

			var bufOut, bufErr bytes.Buffer
			if _, err := io.Copy(&bufOut, rOut); err != nil {
				t.Fatalf("failed to read from stdout pipe: %v", err)
			}
			if _, err := io.Copy(&bufErr, rErr); err != nil {
				t.Fatalf("failed to read from stderr pipe: %v", err)
			}

			if exitCode != tt.expectedCode {
				t.Errorf("Cache() exit code = %d, want %d", exitCode, tt.expectedCode)
			}

			if tt.expectedStdout != "" && !strings.Contains(bufOut.String(), tt.expectedStdout) {
				t.Errorf("Cache() stdout = %q, want substring %q", bufOut.String(), tt.expectedStdout)
			}

			if tt.expectedStderr != "" && !strings.Contains(bufErr.String(), tt.expectedStderr) {
				t.Errorf("Cache() stderr = %q, want substring %q", bufErr.String(), tt.expectedStderr)
			}
		})
	}
}

func TestGet_Help(t *testing.T) {
	args := []string{"--help"}
	cfg := &config.Config{}

	oldStdout := os.Stdout
	defer func() { os.Stdout = oldStdout }()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
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
			r, w, err := os.Pipe()
			if err != nil {
				t.Fatalf("failed to create pipe: %v", err)
			}
			defer r.Close()
			os.Stderr = w
			defer func() { os.Stderr = oldStderr }()

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
	defer r.Close()
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

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create pipe: %v", err)
	}
	defer r.Close()
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
