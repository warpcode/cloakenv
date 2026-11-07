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
