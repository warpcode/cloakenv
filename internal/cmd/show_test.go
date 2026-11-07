package cmd

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"

	"github.com/warpcode/cloakenv/internal/config"
	"github.com/warpcode/cloakenv/internal/engine"
	"github.com/warpcode/cloakenv/internal/yaml"
)

func TestShow_EnvFormat(t *testing.T) {
	oldStdout := os.Stdout
	defer func() { os.Stdout = oldStdout }()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create pipe: %v", err)
	}
	os.Stdout = w

	t.Setenv("SHOW_TEST_VAR_A", "valA")
	t.Setenv("SHOW_TEST_VAR_B", "valB with space")
	t.Setenv("SHOW_TEST_VAR_C", "valC")

	args := []string{
		"-e", `KEY_C={{ get "env://SHOW_TEST_VAR_C" }}`,
		"-e", `KEY_A={{ get "env://SHOW_TEST_VAR_A" }}`,
		"-e", `KEY_B={{ get "env://SHOW_TEST_VAR_B" }}`,
		"-o", "env",
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

	wantLines := []string{
		"KEY_A=valA",
		`KEY_B="valB with space"`,
		"KEY_C=valC",
	}

	if len(lines) != len(wantLines) {
		t.Fatalf("expected %d lines, got %d. Output: %q", len(wantLines), len(lines), actualOutput)
	}

	for i, want := range wantLines {
		if lines[i] != want {
			t.Errorf("lines[%d] = %q; want %q", i, lines[i], want)
		}
	}
}

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

	wantKeys := []string{"KEY_A", "KEY_B"}
	if len(lines) != len(wantKeys) {
		t.Fatalf("expected %d keys, got %d. Output: %q", len(wantKeys), len(lines), actualOutput)
	}
	for i, want := range wantKeys {
		if lines[i] != want {
			t.Errorf("lines[%d] = %q; want %q", i, lines[i], want)
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

	wantKeys := []string{
		"api--key",
		"db-user",
		"multiple___underscores",
	}

	if len(lines) != len(wantKeys) {
		t.Fatalf("expected %d keys, got %d. Output: %q", len(wantKeys), len(lines), actualOutput)
	}

	for i, want := range wantKeys {
		if lines[i] != want {
			t.Errorf("lines[%d] = %q; want %q", i, lines[i], want)
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

	wantKeys := []string{
		"API_KEY",
		"DATABASE_USER_NAME",
		"MULTIPLE_UNDERSCORES",
	}

	if len(lines) != len(wantKeys) {
		t.Fatalf("expected %d keys, got %d. Output: %q", len(wantKeys), len(lines), actualOutput)
	}

	for i, want := range wantKeys {
		if lines[i] != want {
			t.Errorf("lines[%d] = %q; want %q", i, lines[i], want)
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

	wantKeys := []string{
		"TEST_LITERAL_VAL",
		"TEST_TEMPLATE_A",
		"TEST_TEMPLATE_B",
	}

	if len(lines) != len(wantKeys) {
		t.Fatalf("expected %d keys, got %d. Output: %q", len(wantKeys), len(lines), actualOutput)
	}

	for i, want := range wantKeys {
		if lines[i] != want {
			t.Errorf("lines[%d] = %q; want %q", i, lines[i], want)
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

		entry, err := orch.BuildMergedEntry(ctx, engine.MergedEntryConfig{
			Merges:    merges,
			Whitelist: whitelist,
			Explicit:  explicit,
		})
		if err != nil {
			t.Fatalf("BuildMergedEntry failed: %v", err)
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
		_, err := orch.BuildMergedEntry(ctx, engine.MergedEntryConfig{Merges: merges})
		if err == nil {
			t.Errorf("expected error when retrieving nonexistent entry, got nil")
		}
	})

	t.Run("Explicit mapping error", func(t *testing.T) {
		explicit := map[string]string{
			"BAD_KEY": `{{ get "invalid_scheme://foo" }}`,
		}
		_, err := orch.BuildMergedEntry(ctx, engine.MergedEntryConfig{Explicit: explicit})
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

func TestShow_InvalidKeyFormat(t *testing.T) {
	oldStderr := os.Stderr
	defer func() { os.Stderr = oldStderr }()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create pipe: %v", err)
	}
	os.Stderr = w

	args := []string{"-f", "camel", "work://my_entry"}
	cfg := &config.Config{
		Vaults: make(map[string]config.VaultConfig),
	}

	exitCode := Show(args, cfg)
	_ = w.Close()

	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatalf("failed to read from pipe: %v", err)
	}

	if exitCode != 1 {
		t.Errorf("expected exit code 1, got %d", exitCode)
	}

	if !strings.Contains(buf.String(), "invalid key format") {
		t.Errorf("expected output to contain 'invalid key format', got %q", buf.String())
	}
}

func TestShow_RuntimeKeyFormatOverride(t *testing.T) {
	oldStdout := os.Stdout
	defer func() { os.Stdout = oldStdout }()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create pipe: %v", err)
	}
	os.Stdout = w

	args := []string{"-f", "snake", "work://ssh_key_staging"}
	cfg := &config.Config{
		Vaults: map[string]config.VaultConfig{
			"work": {
				Provider:  "yaml",
				VaultPath: "../../testdata/test_entries.yaml",
				KeyFormat: "env", // overridden by snake
			},
		},
	}

	t.Setenv("CLOAKENV_MASTER_PASSWORD", "password123")
	exitCode := Show(args, cfg)
	if exitCode != 0 {
		t.Errorf("expected exit code 0, got %d", exitCode)
	}

	var buf bytes.Buffer
	_ = w.Close()
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatalf("failed to copy: %v", err)
	}
	output := buf.String()

	// test_entries.yaml has USERNAME
	if !strings.Contains(output, "username:") {
		t.Errorf("expected output to contain snake_case formatted keys 'username:', got %s", output)
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

func TestShow_TagsInMetaAndUnshadowed(t *testing.T) {
	cfg := &config.Config{
		Vaults: map[string]config.VaultConfig{
			"my_yaml": {
				Provider:  "yaml",
				VaultPath: "../../testdata/test_entries.yaml",
			},
			"custom_vault": {
				Provider: "custom_vault",
				Entities: map[string]config.CustomEntityConfig{
					"app": {
						Title:      "App Title",
						Tags:       []string{"meta_tag1", "meta_tag2"},
						IsComposed: true,
						Attributes: map[string]any{
							"tags":     "custom_tag_value",
							"password": "secret",
						},
					},
				},
			},
		},
	}

	t.Run("Show_WithShowMeta_IncludesTagsInMeta", func(t *testing.T) {
		oldStdout := os.Stdout
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatalf("failed to create pipe: %v", err)
		}
		os.Stdout = w

		exitCode := Show([]string{"my_yaml://ssh_key_prod", "-o", "json", "--show-meta"}, cfg)
		_ = w.Close()
		os.Stdout = oldStdout

		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)

		if exitCode != 0 {
			t.Fatalf("expected exit code 0, got %d", exitCode)
		}

		output := buf.String()
		if !strings.Contains(output, `"_meta_"`) {
			t.Fatalf("expected _meta_ in output, got %s", output)
		}
		if !strings.Contains(output, `"auth:ssh"`) || !strings.Contains(output, `"env:prod"`) {
			t.Errorf("expected tags inside _meta_, got %s", output)
		}
	})

	t.Run("Show_WithoutShowMeta_HidesTagsByDefault", func(t *testing.T) {
		oldStdout := os.Stdout
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatalf("failed to create pipe: %v", err)
		}
		os.Stdout = w

		exitCode := Show([]string{"my_yaml://ssh_key_prod", "-o", "json"}, cfg)
		_ = w.Close()
		os.Stdout = oldStdout

		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)

		if exitCode != 0 {
			t.Fatalf("expected exit code 0, got %d", exitCode)
		}

		output := buf.String()
		if strings.Contains(output, `"_meta_"`) {
			t.Errorf("expected _meta_ to be hidden by default, got %s", output)
		}
		if strings.Contains(output, `"auth:ssh"`) {
			t.Errorf("expected tags to be hidden by default, got %s", output)
		}
	})

	t.Run("Show_WithIncludeTags_DisplaysTagsAtTopLevel", func(t *testing.T) {
		oldStdout := os.Stdout
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatalf("failed to create pipe: %v", err)
		}
		os.Stdout = w

		exitCode := Show([]string{"my_yaml://ssh_key_prod", "-i", "tags", "-o", "json"}, cfg)
		_ = w.Close()
		os.Stdout = oldStdout

		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)

		if exitCode != 0 {
			t.Fatalf("expected exit code 0, got %d", exitCode)
		}

		output := buf.String()
		if !strings.Contains(output, `"tags"`) || !strings.Contains(output, `"auth:ssh"`) {
			t.Errorf("expected explicit -i tags to show tags, got %s", output)
		}
	})

	t.Run("Show_WithIncludeMetaTags_DisplaysMetaTags", func(t *testing.T) {
		oldStdout := os.Stdout
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatalf("failed to create pipe: %v", err)
		}
		os.Stdout = w

		exitCode := Show([]string{"my_yaml://ssh_key_prod", "-i", "_meta_.tags", "-o", "json"}, cfg)
		_ = w.Close()
		os.Stdout = oldStdout

		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)

		if exitCode != 0 {
			t.Fatalf("expected exit code 0, got %d", exitCode)
		}

		output := buf.String()
		if !strings.Contains(output, `"_meta_"`) || !strings.Contains(output, `"auth:ssh"`) {
			t.Errorf("expected explicit -i _meta_.tags to show _meta_ tags, got %s", output)
		}
	})

	t.Run("Show_CustomAttributeNamedTags_NotShadowedWhenMetaHidden", func(t *testing.T) {
		oldStdout := os.Stdout
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatalf("failed to create pipe: %v", err)
		}
		os.Stdout = w

		exitCode := Show([]string{"custom_vault://app", "-o", "json"}, cfg)
		_ = w.Close()
		os.Stdout = oldStdout

		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)

		if exitCode != 0 {
			t.Fatalf("expected exit code 0, got %d", exitCode)
		}

		output := buf.String()
		if strings.Contains(output, `"_meta_"`) {
			t.Errorf("expected _meta_ to be hidden, got %s", output)
		}
		if !strings.Contains(output, `"tags": "custom_tag_value"`) {
			t.Errorf("expected custom attribute 'tags' to be preserved, got %s", output)
		}
	})

	t.Run("Show_CustomAttributeNamedTags_BothCustomAndMetaTagsShownWithShowMeta", func(t *testing.T) {
		oldStdout := os.Stdout
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatalf("failed to create pipe: %v", err)
		}
		os.Stdout = w

		exitCode := Show([]string{"custom_vault://app", "-o", "json", "--show-meta"}, cfg)
		_ = w.Close()
		os.Stdout = oldStdout

		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)

		if exitCode != 0 {
			t.Fatalf("expected exit code 0, got %d", exitCode)
		}

		output := buf.String()
		if !strings.Contains(output, `"_meta_"`) {
			t.Errorf("expected _meta_ with --show-meta, got %s", output)
		}
		if !strings.Contains(output, `"meta_tag1"`) {
			t.Errorf("expected meta tags in _meta_, got %s", output)
		}
		if !strings.Contains(output, `"tags": "custom_tag_value"`) {
			t.Errorf("expected custom attribute 'tags' to be preserved alongside _meta_, got %s", output)
		}
	})
}

func TestShow_RawFlag(t *testing.T) {
	tempDir := t.TempDir()
	yamlFile := filepath.Join(tempDir, "vault.yaml")
	yamlContent := `entries:
  app:
    user_name: Alice
    secret_key: s3cr3t
    secret_old: deprecated
    notes: Some notes
`
	if err := os.WriteFile(yamlFile, []byte(yamlContent), 0600); err != nil {
		t.Fatalf("failed to write test yaml: %v", err)
	}

	cfg := &config.Config{
		Vaults: map[string]config.VaultConfig{
			"test_vault": {
				Provider:      "yaml",
				VaultPath:     yamlFile,
				IncludeFields: []string{"user*", "secret*"},
				ExcludeFields: []string{"secret_old"},
				Mapping: []config.MappingRule{
					{
						Match: "^user_(.*)$",
						Key:   "account_$1",
					},
				},
				KeyFormat: "env",
				Attributes: map[string]any{
					"COMPUTED_FIELD": "static_val",
				},
			},
		},
	}

	t.Run("StandardShow_AppliesAllPipelineStages", func(t *testing.T) {
		oldStdout := os.Stdout
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatalf("failed to create pipe: %v", err)
		}
		os.Stdout = w

		exitCode := Show([]string{"test_vault://app", "-o", "json"}, cfg)
		_ = w.Close()
		os.Stdout = oldStdout

		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)

		if exitCode != 0 {
			t.Fatalf("expected exit code 0, got %d", exitCode)
		}

		var result map[string]any
		if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
			t.Fatalf("failed to unmarshal JSON: %v, output: %s", err, buf.String())
		}

		if result["ACCOUNT_NAME"] != "Alice" {
			t.Errorf("expected ACCOUNT_NAME=Alice, got %v", result["ACCOUNT_NAME"])
		}
		if result["SECRET_KEY"] != "s3cr3t" {
			t.Errorf("expected SECRET_KEY=s3cr3t, got %v", result["SECRET_KEY"])
		}
		if result["COMPUTED_FIELD"] != "static_val" {
			t.Errorf("expected COMPUTED_FIELD=static_val, got %v", result["COMPUTED_FIELD"])
		}
		if _, exists := result["secret_old"]; exists {
			t.Errorf("expected secret_old to be excluded, but it was present")
		}
		if _, exists := result["notes"]; exists {
			t.Errorf("expected notes to be excluded by include_fields, but it was present")
		}
	})

	t.Run("RawShow_JSON_BypassesAllPipelineStages", func(t *testing.T) {
		oldStdout := os.Stdout
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatalf("failed to create pipe: %v", err)
		}
		os.Stdout = w

		exitCode := Show([]string{"test_vault://app", "--raw", "-o", "json"}, cfg)
		_ = w.Close()
		os.Stdout = oldStdout

		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)

		if exitCode != 0 {
			t.Fatalf("expected exit code 0, got %d", exitCode)
		}

		var result map[string]any
		if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
			t.Fatalf("failed to unmarshal JSON: %v, output: %s", err, buf.String())
		}

		// Raw keys and values must be preserved exactly
		if result["user_name"] != "Alice" {
			t.Errorf("expected raw user_name=Alice, got %v", result["user_name"])
		}
		if result["secret_key"] != "s3cr3t" {
			t.Errorf("expected raw secret_key=s3cr3t, got %v", result["secret_key"])
		}
		if result["secret_old"] != "deprecated" {
			t.Errorf("expected raw secret_old=deprecated (bypassing exclude_fields), got %v", result["secret_old"])
		}
		if result["notes"] != "Some notes" {
			t.Errorf("expected raw notes='Some notes' (bypassing include_fields), got %v", result["notes"])
		}
		if _, exists := result["COMPUTED_FIELD"]; exists {
			t.Errorf("expected computed attribute to be bypassed under --raw, but got %v", result["COMPUTED_FIELD"])
		}
		if _, exists := result["ACCOUNT_NAME"]; exists {
			t.Errorf("expected key mapping to be bypassed under --raw, but got %v", result["ACCOUNT_NAME"])
		}
	})

	t.Run("RawShow_YAML_BypassesAllPipelineStages", func(t *testing.T) {
		oldStdout := os.Stdout
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatalf("failed to create pipe: %v", err)
		}
		os.Stdout = w

		exitCode := Show([]string{"test_vault://app", "--raw", "-o", "yaml"}, cfg)
		_ = w.Close()
		os.Stdout = oldStdout

		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)

		if exitCode != 0 {
			t.Fatalf("expected exit code 0, got %d", exitCode)
		}

		var result map[string]any
		if err := yaml.Unmarshal(buf.Bytes(), &result); err != nil {
			t.Fatalf("failed to unmarshal YAML: %v, output: %s", err, buf.String())
		}

		if result["user_name"] != "Alice" || result["secret_old"] != "deprecated" || result["notes"] != "Some notes" {
			t.Errorf("expected raw fields in YAML, got %+v", result)
		}
	})

	t.Run("RawShow_KeysFormat", func(t *testing.T) {
		oldStdout := os.Stdout
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatalf("failed to create pipe: %v", err)
		}
		os.Stdout = w

		exitCode := Show([]string{"test_vault://app", "--raw", "-o", "keys"}, cfg)
		_ = w.Close()
		os.Stdout = oldStdout

		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)

		if exitCode != 0 {
			t.Fatalf("expected exit code 0, got %d", exitCode)
		}

		out := buf.String()
		for _, wantKey := range []string{"notes", "secret_key", "secret_old", "user_name"} {
			if !strings.Contains(out, wantKey) {
				t.Errorf("expected output to contain key %q, got %s", wantKey, out)
			}
		}
		if strings.Contains(out, "ACCOUNT_NAME") || strings.Contains(out, "COMPUTED_FIELD") {
			t.Errorf("expected raw keys output not to contain transformed keys, got %s", out)
		}
	})

	t.Run("RawShow_EnvFormat", func(t *testing.T) {
		oldStdout := os.Stdout
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatalf("failed to create pipe: %v", err)
		}
		os.Stdout = w

		exitCode := Show([]string{"test_vault://app", "--raw", "-o", "env"}, cfg)
		_ = w.Close()
		os.Stdout = oldStdout

		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)

		if exitCode != 0 {
			t.Fatalf("expected exit code 0, got %d", exitCode)
		}

		out := buf.String()
		if !strings.Contains(out, `notes="Some notes"`) || !strings.Contains(out, "user_name=Alice") {
			t.Errorf("expected raw env pairs, got %s", out)
		}
	})

	t.Run("RawShow_KeePassBackend", func(t *testing.T) {
		keyring.MockInit()
		if err := keyring.Set("cloakenv", "provider/rawdb", "password123"); err != nil {
			t.Fatalf("failed to set mock credentials: %v", err)
		}

		kpCfg := &config.Config{
			Vaults: map[string]config.VaultConfig{
				"rawdb": {
					Provider:      "keepass",
					VaultPath:     "../../testdata/testDB.kdbx",
					IncludeFields: []string{"UserName"},
					KeyFormat:     "env",
					Attributes: map[string]any{
						"EXTRA": "injected",
					},
				},
			},
		}

		oldStdout := os.Stdout
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatalf("failed to create pipe: %v", err)
		}
		os.Stdout = w

		exitCode := Show([]string{"rawdb://website/Test Website", "--raw", "-o", "json"}, kpCfg)
		_ = w.Close()
		os.Stdout = oldStdout

		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)

		if exitCode != 0 {
			t.Fatalf("expected exit code 0, got %d", exitCode)
		}

		var result map[string]any
		if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
			t.Fatalf("failed to unmarshal JSON: %v, output: %s", err, buf.String())
		}

		// Under --raw, all raw KeePass fields are present (UserName, Password, URL, Notes)
		if result["UserName"] == nil || result["Password"] == nil {
			t.Errorf("expected raw KeePass fields UserName and Password, got %+v", result)
		}
		if _, exists := result["EXTRA"]; exists {
			t.Errorf("expected EXTRA to be bypassed under --raw, got %+v", result)
		}
	})
}

func TestShow_ExplainFlag(t *testing.T) {
	tempDir := t.TempDir()
	yamlFile := filepath.Join(tempDir, "vault.yaml")
	yamlContent := `entries:
  app:
    user_name: Alice
    secret_key: s3cr3t
    secret_old: deprecated
    notes: Some notes
`
	if err := os.WriteFile(yamlFile, []byte(yamlContent), 0600); err != nil {
		t.Fatalf("failed to write test yaml: %v", err)
	}

	cfg := &config.Config{
		Vaults: map[string]config.VaultConfig{
			"test_vault": {
				Provider:      "yaml",
				VaultPath:     yamlFile,
				IncludeFields: []string{"user*", "secret*"},
				ExcludeFields: []string{"secret_old"},
				Mapping: []config.MappingRule{
					{
						Match: "^user_(.*)$",
						Key:   "account_$1",
					},
				},
				KeyFormat: "env",
				Attributes: map[string]any{
					"COMPUTED_FIELD": "static_val",
				},
			},
		},
	}

	t.Run("Explain_JSON_ShowsAllFiveStages", func(t *testing.T) {
		oldStdout := os.Stdout
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatalf("failed to create pipe: %v", err)
		}
		os.Stdout = w

		exitCode := Show([]string{"test_vault://app", "--explain", "-o", "json"}, cfg)
		_ = w.Close()
		os.Stdout = oldStdout

		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)

		if exitCode != 0 {
			t.Fatalf("expected exit code 0, got %d", exitCode)
		}

		var explanation struct {
			Original struct {
				Attributes map[string]any `json:"attributes"`
			} `json:"original"`
			Filtered struct {
				Attributes   map[string]any `json:"attributes"`
				ExcludedKeys []string       `json:"excluded_keys"`
			} `json:"filtered"`
			Mapped struct {
				Attributes  map[string]any    `json:"attributes"`
				RenamedKeys map[string]string `json:"renamed_keys"`
			} `json:"mapped"`
			KeyFormatted struct {
				Attributes    map[string]any    `json:"attributes"`
				FormattedKeys map[string]string `json:"formatted_keys"`
			} `json:"key_formatted"`
			FinalOutput struct {
				Attributes      map[string]any `json:"attributes"`
				AddedAttributes []string       `json:"added_attributes"`
			} `json:"final_output"`
		}

		if err := json.Unmarshal(buf.Bytes(), &explanation); err != nil {
			t.Fatalf("failed to unmarshal JSON: %v, output: %s", err, buf.String())
		}

		// Stage 1: Original
		origAttrs := explanation.Original.Attributes
		if origAttrs["user_name"] != "Alice" || origAttrs["secret_key"] != "s3cr3t" || origAttrs["secret_old"] != "deprecated" || origAttrs["notes"] != "Some notes" {
			t.Errorf("Stage 1 (original) attributes mismatch: %+v", origAttrs)
		}

		// Stage 2: Filtered
		filteredAttrs := explanation.Filtered.Attributes
		if filteredAttrs["user_name"] != "Alice" || filteredAttrs["secret_key"] != "s3cr3t" {
			t.Errorf("Stage 2 (filtered) attributes mismatch: %+v", filteredAttrs)
		}
		if _, exists := filteredAttrs["secret_old"]; exists {
			t.Errorf("Stage 2 should not contain secret_old: %+v", filteredAttrs)
		}
		if _, exists := filteredAttrs["notes"]; exists {
			t.Errorf("Stage 2 should not contain notes: %+v", filteredAttrs)
		}
		excluded := strings.Join(explanation.Filtered.ExcludedKeys, ",")
		if !strings.Contains(excluded, "notes") || !strings.Contains(excluded, "secret_old") {
			t.Errorf("Stage 2 excluded_keys mismatch: %v", explanation.Filtered.ExcludedKeys)
		}

		// Stage 3: Mapped
		mappedAttrs := explanation.Mapped.Attributes
		if mappedAttrs["account_name"] != "Alice" || mappedAttrs["secret_key"] != "s3cr3t" {
			t.Errorf("Stage 3 (mapped) attributes mismatch: %+v", mappedAttrs)
		}
		if explanation.Mapped.RenamedKeys["user_name"] != "account_name" {
			t.Errorf("Stage 3 renamed_keys mismatch: %+v", explanation.Mapped.RenamedKeys)
		}

		// Stage 4: Key Formatted
		formattedAttrs := explanation.KeyFormatted.Attributes
		if formattedAttrs["ACCOUNT_NAME"] != "Alice" || formattedAttrs["SECRET_KEY"] != "s3cr3t" {
			t.Errorf("Stage 4 (key_formatted) attributes mismatch: %+v", formattedAttrs)
		}
		if explanation.KeyFormatted.FormattedKeys["account_name"] != "ACCOUNT_NAME" || explanation.KeyFormatted.FormattedKeys["secret_key"] != "SECRET_KEY" {
			t.Errorf("Stage 4 formatted_keys mismatch: %+v", explanation.KeyFormatted.FormattedKeys)
		}

		// Stage 5: Final Output
		finalAttrs := explanation.FinalOutput.Attributes
		if finalAttrs["ACCOUNT_NAME"] != "Alice" || finalAttrs["SECRET_KEY"] != "s3cr3t" || finalAttrs["COMPUTED_FIELD"] != "static_val" {
			t.Errorf("Stage 5 (final_output) attributes mismatch: %+v", finalAttrs)
		}
		if len(explanation.FinalOutput.AddedAttributes) == 0 || explanation.FinalOutput.AddedAttributes[0] != "COMPUTED_FIELD" {
			t.Errorf("Stage 5 added_attributes mismatch: %v", explanation.FinalOutput.AddedAttributes)
		}
	})

	t.Run("Explain_YAML_DefaultFormat", func(t *testing.T) {
		oldStdout := os.Stdout
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatalf("failed to create pipe: %v", err)
		}
		os.Stdout = w

		exitCode := Show([]string{"test_vault://app", "--explain"}, cfg)
		_ = w.Close()
		os.Stdout = oldStdout

		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)

		if exitCode != 0 {
			t.Fatalf("expected exit code 0, got %d", exitCode)
		}

		var result map[string]any
		if err := yaml.Unmarshal(buf.Bytes(), &result); err != nil {
			t.Fatalf("failed to unmarshal YAML: %v, output: %s", err, buf.String())
		}

		for _, stage := range []string{"original", "filtered", "mapped", "key_formatted", "final_output"} {
			if _, exists := result[stage]; !exists {
				t.Errorf("expected stage %q in YAML output, got: %+v", stage, result)
			}
		}
	})

	t.Run("Explain_KeyFormatOverride", func(t *testing.T) {
		oldStdout := os.Stdout
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatalf("failed to create pipe: %v", err)
		}
		os.Stdout = w

		exitCode := Show([]string{"test_vault://app", "--explain", "-f", "kebab", "-o", "json"}, cfg)
		_ = w.Close()
		os.Stdout = oldStdout

		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)

		if exitCode != 0 {
			t.Fatalf("expected exit code 0, got %d", exitCode)
		}

		var explanation struct {
			KeyFormatted struct {
				Attributes    map[string]any    `json:"attributes"`
				FormattedKeys map[string]string `json:"formatted_keys"`
			} `json:"key_formatted"`
		}

		if err := json.Unmarshal(buf.Bytes(), &explanation); err != nil {
			t.Fatalf("failed to unmarshal JSON: %v", err)
		}

		if explanation.KeyFormatted.Attributes["account-name"] != "Alice" || explanation.KeyFormatted.Attributes["secret-key"] != "s3cr3t" {
			t.Errorf("expected kebab keys in Stage 4, got %+v", explanation.KeyFormatted.Attributes)
		}
		if explanation.KeyFormatted.FormattedKeys["account_name"] != "account-name" {
			t.Errorf("expected account_name -> account-name in formatted_keys, got %+v", explanation.KeyFormatted.FormattedKeys)
		}
	})
}

func TestShow_RawAndExplainErrors(t *testing.T) {
	cfg := &config.Config{}

	t.Run("RawAndExplainTogether_ReturnsError", func(t *testing.T) {
		oldStderr := os.Stderr
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatalf("failed to create pipe: %v", err)
		}
		os.Stderr = w

		exitCode := Show([]string{"test://entry", "--raw", "--explain"}, cfg)
		_ = w.Close()
		os.Stderr = oldStderr

		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)

		if exitCode == 0 {
			t.Errorf("expected non-zero exit code, got 0")
		}
		if !strings.Contains(buf.String(), "cannot use --raw and --explain together") {
			t.Errorf("expected mutual exclusion error message, got %s", buf.String())
		}
	})

	t.Run("ExplainWithInvalidFormat_ReturnsError", func(t *testing.T) {
		for _, invalidFormat := range []string{"env", "keys"} {
			oldStderr := os.Stderr
			r, w, err := os.Pipe()
			if err != nil {
				t.Fatalf("failed to create pipe: %v", err)
			}
			os.Stderr = w

			exitCode := Show([]string{"test://entry", "--explain", "-o", invalidFormat}, cfg)
			_ = w.Close()
			os.Stderr = oldStderr

			var buf bytes.Buffer
			_, _ = io.Copy(&buf, r)

			if exitCode == 0 {
				t.Errorf("expected non-zero exit code for format %q, got 0", invalidFormat)
			}
			if !strings.Contains(buf.String(), "invalid output format") {
				t.Errorf("expected invalid output format error message, got %s", buf.String())
			}
		}
	})

	t.Run("ExplainWithMultipleMerges_ReturnsError", func(t *testing.T) {
		oldStderr := os.Stderr
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatalf("failed to create pipe: %v", err)
		}
		os.Stderr = w

		exitCode := Show([]string{"--explain", "-m", "test://a", "-m", "test://b"}, cfg)
		_ = w.Close()
		os.Stderr = oldStderr

		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)

		if exitCode == 0 {
			t.Errorf("expected non-zero exit code for multiple merges with --explain, got 0")
		}
		if !strings.Contains(buf.String(), "--explain can only be used with a single entry URI") {
			t.Errorf("expected single entry URI error message, got %s", buf.String())
		}
	})
}
