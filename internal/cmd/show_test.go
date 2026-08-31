package cmd

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/warpcode/cloakenv/internal/config"
	"github.com/warpcode/cloakenv/internal/engine"
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
	args := []string{"-e", "KEY_B=${env://SHOW_TEST_VAR_B}", "-e", "KEY_A=${env://SHOW_TEST_VAR_A}", "-o", "keys"}
	cfg := &config.Config{
		Vaults: make(map[string]config.VaultConfig),
	}

	exitCode := Show(args, cfg)
	_ = w.Close()

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
		"-e", "db-user=${env://SHOW_TEST_VAR_B}",
		"-e", "api--key=${env://SHOW_TEST_VAR_A}",
		"-e", "multiple___underscores=${env://SHOW_TEST_VAR_A}",
		"-o", "keys",
	}
	cfg := &config.Config{
		Vaults: make(map[string]config.VaultConfig),
	}

	exitCode := Show(args, cfg)
	_ = w.Close()

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

func TestShow_TemplateFlag(t *testing.T) {
	oldStdout := os.Stdout
	defer func() { os.Stdout = oldStdout }()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create pipe: %v", err)
	}
	os.Stdout = w

	t.Setenv("SHOW_TEST_VAR_A", "valA")
	t.Setenv("SHOW_TEST_VAR_B", "valB")

	args := []string{
		"-t", "../../testdata/test_template.env",
		"-o", "keys",
	}
	cfg := &config.Config{
		Vaults: make(map[string]config.VaultConfig),
	}

	exitCode := Show(args, cfg)
	_ = w.Close()

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
		"TEST_TEMPLATE_A":  true,
		"TEST_TEMPLATE_B":  true,
		"TEST_LITERAL_VAL": true,
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

func TestShouldQuoteDotenvValue(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{
			name:  "empty string",
			input: "",
			want:  false,
		},
		{
			name:  "no special characters",
			input: "hello",
			want:  false,
		},
		{
			name:  "with space",
			input: "hello world",
			want:  true,
		},
		{
			name:  "with newline",
			input: "hello\nworld",
			want:  true,
		},
		{
			name:  "with carriage return",
			input: "hello\rworld",
			want:  true,
		},
		{
			name:  "with hash",
			input: "hello#world",
			want:  true,
		},
		{
			name:  "with double quote",
			input: "hello\"world",
			want:  true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := shouldQuoteDotenvValue(tc.input)
			if got != tc.want {
				t.Errorf("shouldQuoteDotenvValue(%q) = %v; want %v", tc.input, got, tc.want)
			}
		})
	}
}

func TestBuildMergedEntry(t *testing.T) {
	t.Setenv("TEST_SHOW_ENV_VAL", "resolved_env_val")

	cfg := &config.Config{
		Vaults: map[string]config.VaultConfig{
			"vaultA": {
				Provider: "custom_vault",
				Entities: map[string]map[string]any{
					"entry1": {
						"user": "userA",
						"pass": "passA",
						"tags": []any{"tagA", "commonTag"},
					},
				},
			},
			"vaultB": {
				Provider: "custom_vault",
				Entities: map[string]map[string]any{
					"entry2": {
						"host": "hostB",
						"user": "userB_overridden",
						"tags": []any{"tagB", "commonTag"},
					},
				},
			},
		},
	}

	orch, err := engine.NewOrchestrator(cfg)
	if err != nil {
		t.Fatalf("failed to create orchestrator: %v", err)
	}

	ctx := t.Context()

	t.Run("Merge multiple entries with whitelist and explicit override", func(t *testing.T) {
		merges := []string{"vaultA://entry1", "vaultB://entry2"}
		whitelist := []string{"user", "host"}
		explicit := map[string]string{
			"EXTRA_KEY": "${env://TEST_SHOW_ENV_VAL}",
		}

		entry, err := buildMergedEntry(ctx, orch, merges, whitelist, explicit)
		if err != nil {
			t.Fatalf("buildMergedEntry failed: %v", err)
		}

		// Check attributes
		if got, want := entry.Attributes["user"], "userB_overridden"; got != want {
			t.Errorf("entry.Attributes[\"user\"] = %v; want %v", got, want)
		}
		if got, want := entry.Attributes["host"], "hostB"; got != want {
			t.Errorf("entry.Attributes[\"host\"] = %v; want %v", got, want)
		}
		// Whitelisted: pass should be excluded
		if _, exists := entry.Attributes["pass"]; exists {
			t.Errorf("entry.Attributes[\"pass\"] should have been filtered out by whitelist")
		}
		// Explicit mapping override
		if got, want := entry.Attributes["EXTRA_KEY"], "resolved_env_val"; got != want {
			t.Errorf("entry.Attributes[\"EXTRA_KEY\"] = %v; want %v", got, want)
		}

		// Check tags
		tagSet := make(map[string]bool)
		for _, tag := range entry.Tags {
			tagSet[tag] = true
		}
		if !tagSet["tagA"] || !tagSet["tagB"] || !tagSet["commonTag"] {
			t.Errorf("unexpected tags in merged entry: %v", entry.Tags)
		}
	})

	t.Run("Merge invalid entry error", func(t *testing.T) {
		merges := []string{"vaultA://nonexistent"}
		_, err := buildMergedEntry(ctx, orch, merges, nil, nil)
		if err == nil {
			t.Errorf("expected error when retrieving nonexistent entry, got nil")
		}
	})

	t.Run("Explicit mapping error", func(t *testing.T) {
		explicit := map[string]string{
			"BAD_KEY": "${invalid_scheme://foo}",
		}
		_, err := buildMergedEntry(ctx, orch, nil, nil, explicit)
		if err == nil {
			t.Errorf("expected error when resolving invalid explicit mapping, got nil")
		}
	})
}
