package cmd

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"

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
	args := []string{"-e", `KEY_B={{ get "env://SHOW_TEST_VAR_B" }}`, "-e", `KEY_A={{ get "env://SHOW_TEST_VAR_A" }}`, "-o", "keys"}
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
		"-e", `db-user={{ get "env://SHOW_TEST_VAR_B" }}`,
		"-e", `api--key={{ get "env://SHOW_TEST_VAR_A" }}`,
		"-e", `multiple___underscores={{ get "env://SHOW_TEST_VAR_A" }}`,
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
		"db-user":                true,
		"api--key":               true,
		"multiple___underscores": true,
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

func TestShow_VaultKeyFormat(t *testing.T) {
	oldStdout := os.Stdout
	defer func() { os.Stdout = oldStdout }()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create pipe: %v", err)
	}
	os.Stdout = w

	// Custom vault with key_format: "env"
	cfg := &config.Config{
		Vaults: map[string]config.VaultConfig{
			"env_vault": {
				Provider:  "custom_vault",
				KeyFormat: "env",
				Entities: map[string]config.CustomEntityConfig{
					"item": {
						Attributes: map[string]any{
							"api-key":                "val1",
							"database.user_name":     "val2",
							"multiple___underscores": "val3",
						},
					},
				},
			},
		},
	}

	exitCode := Show([]string{"env_vault://item", "-o", "keys"}, cfg)
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
		"API_KEY":              true,
		"DATABASE_USER_NAME":   true,
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
				Entities: map[string]config.CustomEntityConfig{
					"entry1": {
						Attributes: map[string]any{
							"user": "userA",
							"pass": "passA",
							"tags": []any{"tagA", "commonTag"},
						},
					},
				},
			},
			"vaultB": {
				Provider: "custom_vault",
				Entities: map[string]config.CustomEntityConfig{
					"entry2": {
						Attributes: map[string]any{
							"host": "hostB",
							"user": "userB_overridden",
							"tags": []any{"tagB", "commonTag"},
						},
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
			"EXTRA_KEY": `{{ get "env://TEST_SHOW_ENV_VAL" }}`,
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
			"BAD_KEY": `{{ get "invalid_scheme://foo" }}`,
		}
		_, err := buildMergedEntry(ctx, orch, nil, nil, explicit)
		if err == nil {
			t.Errorf("expected error when resolving invalid explicit mapping, got nil")
		}
	})
}

func TestShow_KeePassMetadataOutput(t *testing.T) {
	keyring.MockInit()
	if err := keyring.Set("cloakenv", "provider/testdb", "password123"); err != nil {
		t.Fatalf("failed to set mock credentials: %v", err)
	}

	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)
	t.Setenv("XDG_CACHE_HOME", tempDir)
	t.Setenv("LocalAppData", tempDir)

	cfg := &config.Config{
		Vaults: map[string]config.VaultConfig{
			"testdb": {
				Provider:  "keepass",
				VaultPath: "../../testdata/testDB.kdbx",
			},
		},
	}

	oldStdout := os.Stdout
	defer func() { os.Stdout = oldStdout }()

	// Test -o json output includes uuid and group
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create pipe: %v", err)
	}
	os.Stdout = w

	exitCode := Show([]string{"testdb://website/Test Website", "-o", "json"}, cfg)
	_ = w.Close()

	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatalf("failed to read from pipe: %v", err)
	}

	if exitCode != 0 {
		t.Errorf("expected exit code 0, got %d", exitCode)
	}

	output := buf.String()
	if !strings.Contains(output, "uuid") || !strings.Contains(output, "group") {
		t.Errorf("expected json output to include uuid and group, got: %s", output)
	}

	// Test -o keys output does NOT include uuid or group
	r2, w2, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create pipe: %v", err)
	}
	os.Stdout = w2

	exitCode = Show([]string{"testdb://website/Test Website", "-o", "keys"}, cfg)
	_ = w2.Close()

	var buf2 bytes.Buffer
	if _, err := io.Copy(&buf2, r2); err != nil {
		t.Fatalf("failed to read from pipe: %v", err)
	}

	if exitCode != 0 {
		t.Errorf("expected exit code 0, got %d", exitCode)
	}

	keysOutput := buf2.String()
	if strings.Contains(keysOutput, "uuid") || strings.Contains(keysOutput, "group") {
		t.Errorf("expected keys output to NOT include uuid or group, got: %s", keysOutput)
	}
}

func TestShow_ShowMeta(t *testing.T) {
	keyring.MockInit()
	if err := keyring.Set("cloakenv", "provider/testdb", "password123"); err != nil {
		t.Fatalf("failed to set mock credentials: %v", err)
	}

	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)
	t.Setenv("XDG_CACHE_HOME", tempDir)
	t.Setenv("LocalAppData", tempDir)

	cfg := &config.Config{
		Vaults: map[string]config.VaultConfig{
			"testdb": {
				Provider:  "keepass",
				VaultPath: "../../testdata/testDB.kdbx",
			},
		},
	}

	t.Run("JSON_WithMeta", func(t *testing.T) {
		oldStdout := os.Stdout
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatalf("failed to create pipe: %v", err)
		}
		os.Stdout = w

		exitCode := Show([]string{"testdb://website/Test Website", "-o", "json", "--show-meta"}, cfg)
		_ = w.Close()
		os.Stdout = oldStdout

		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)

		if exitCode != 0 {
			t.Fatalf("expected exit code 0, got %d", exitCode)
		}

		output := buf.String()
		if !strings.Contains(output, "_meta_") {
			t.Errorf("expected json output to include _meta_, got: %s", output)
		}
		if !strings.Contains(output, `"vault": "testdb"`) || !strings.Contains(output, `"provider": "keepass"`) {
			t.Errorf("expected _meta_ to include vault and provider, got: %s", output)
		}
	})

	t.Run("JSON_NilConfigDoesNotPanic", func(t *testing.T) {
		oldStdout := os.Stdout
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatalf("failed to create pipe: %v", err)
		}
		os.Stdout = w

		exitCode := Show([]string{"testdb://website/Test Website", "-o", "json", "--show-meta"}, nil)
		_ = w.Close()
		os.Stdout = oldStdout

		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)

		// Even with nil config, should not panic
		_ = exitCode
	})

	t.Run("EnvFormat_OmitsMeta", func(t *testing.T) {
		oldStdout := os.Stdout
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatalf("failed to create pipe: %v", err)
		}
		os.Stdout = w

		exitCode := Show([]string{"testdb://website/Test Website", "-o", "env", "--show-meta"}, cfg)
		_ = w.Close()
		os.Stdout = oldStdout

		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)

		if exitCode != 0 {
			t.Fatalf("expected exit code 0, got %d", exitCode)
		}

		output := buf.String()
		if strings.Contains(output, "_meta_") {
			t.Errorf("expected env format to omit _meta_, got: %s", output)
		}
	})

	t.Run("KeysFormat_OmitsMeta", func(t *testing.T) {
		oldStdout := os.Stdout
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatalf("failed to create pipe: %v", err)
		}
		os.Stdout = w

		exitCode := Show([]string{"testdb://website/Test Website", "-o", "keys", "--show-meta"}, cfg)
		_ = w.Close()
		os.Stdout = oldStdout

		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)

		if exitCode != 0 {
			t.Fatalf("expected exit code 0, got %d", exitCode)
		}

		output := buf.String()
		if strings.Contains(output, "_meta_") {
			t.Errorf("expected keys format to omit _meta_, got: %s", output)
		}
	})
}
