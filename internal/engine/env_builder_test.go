package engine

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/warpcode/cloakenv/internal/config"
	"github.com/warpcode/cloakenv/internal/provider"
)

func TestBuildMergedEntry(t *testing.T) {
	t.Setenv("TEST_ENGINE_ENV_VAL", "resolved_env_val")

	cfg := &config.Config{
		Vaults: map[string]config.VaultConfig{
			"vaultA": {Provider: "env"},
			"vaultB": {Provider: "env"},
		},
	}

	orch, err := NewOrchestrator(cfg)
	if err != nil {
		t.Fatalf("failed to create orchestrator: %v", err)
	}
	orch.providerManager.vaultCache["vaultA"] = newTestMemoryProvider(map[string]provider.Entry{
		"entry1": {
			"user": "userA",
			"pass": "passA",
			"tags": []string{"tagA", "commonTag"},
		},
	})
	orch.providerManager.vaultCache["vaultB"] = newTestMemoryProvider(map[string]provider.Entry{
		"entry2": {
			"host": "hostB",
			"user": "userB_overridden",
			"tags": []string{"tagB", "commonTag"},
		},
	})

	ctx := t.Context()

	t.Run("MergeMultipleEntriesWithWhitelistAndExplicit", func(t *testing.T) {
		merges := []string{"vaultA://entry1", "vaultB://entry2"}
		whitelist := []string{"user", "host"}
		explicit := map[string]string{
			"EXTRA_KEY": `{{ get "env://TEST_ENGINE_ENV_VAL" }}`,
		}

		entry, err := orch.BuildMergedEntry(ctx, MergedEntryConfig{
			Merges:    merges,
			Whitelist: whitelist,
			Explicit:  explicit,
		})
		if err != nil {
			t.Fatalf("BuildMergedEntry failed: %v", err)
		}

		if got, want := entry["user"], "userB_overridden"; got != want {
			t.Errorf("entry[\"user\"] = %v; want %v", got, want)
		}
		if got, want := entry["host"], "hostB"; got != want {
			t.Errorf("entry[\"host\"] = %v; want %v", got, want)
		}
		if _, exists := entry["pass"]; exists {
			t.Errorf("entry[\"pass\"] should have been filtered out by whitelist")
		}
		if got, want := entry["EXTRA_KEY"], "resolved_env_val"; got != want {
			t.Errorf("entry[\"EXTRA_KEY\"] = %v; want %v", got, want)
		}

		tagSet := make(map[string]bool)
		for _, tag := range entry.Tags() {
			tagSet[tag] = true
		}
		if !tagSet["tagA"] || !tagSet["tagB"] || !tagSet["commonTag"] {
			t.Errorf("unexpected tags in merged entry: %v", entry.Tags())
		}
	})

	t.Run("MergeInvalidEntryError", func(t *testing.T) {
		merges := []string{"vaultA://nonexistent"}
		_, err := orch.BuildMergedEntry(ctx, MergedEntryConfig{Merges: merges})
		if err == nil {
			t.Errorf("expected error when retrieving nonexistent entry, got nil")
		}
	})

	t.Run("ExplicitMappingError", func(t *testing.T) {
		explicit := map[string]string{
			"BAD_KEY": `{{ get "invalid_scheme://foo" }}`,
		}
		_, err := orch.BuildMergedEntry(ctx, MergedEntryConfig{Explicit: explicit})
		if err == nil {
			t.Errorf("expected error when resolving invalid explicit mapping, got nil")
		}
	})
}

func TestOrchestratorBuildEnvMerges(t *testing.T) {
	cfg := &config.Config{
		Vaults: map[string]config.VaultConfig{
			"my_vault": {Provider: "env"},
		},
	}

	orch, err := NewOrchestrator(cfg)
	if err != nil {
		t.Fatalf("failed to create orchestrator: %v", err)
	}
	orch.providerManager.vaultCache["my_vault"] = newTestMemoryProvider(map[string]provider.Entry{
		"app_one": {
			"DB_USER": "user1",
			"DB_PASS": "pass1",
			"PORT":    "3000",
		},
		"app_two": {
			"PORT":    "5000",
			"DB_USER": "user2",
		},
	})
	ctx := context.Background()

	t.Run("MergeMultipleSourcesAndOverrides", func(t *testing.T) {
		merges := []string{
			"my_vault://app_one",
			"my_vault://app_two",
		}

		// Result expectations:
		// app_one attributes: DB_USER=user1, DB_PASS=pass1, PORT=3000
		// app_two updates: PORT=5000, DB_USER=user2 (overwriting user1 and 3000)
		// Explicit: DB_PASS=explicit_pass
		explicit := map[string]string{
			"DB_PASS": "explicit_pass",
		}

		res, err := orch.BuildEnv(ctx, EnvConfig{Explicit: explicit, Merges: merges})
		if err != nil {
			t.Fatalf("failed to build env: %v", err)
		}

		envMap := make(map[string]string)
		for _, item := range res {
			k, v, _ := strings.Cut(item, "=")
			envMap[k] = v
		}

		if envMap["DB_USER"] != "user2" {
			t.Errorf("expected DB_USER=user2, got %q", envMap["DB_USER"])
		}
		if envMap["DB_PASS"] != "explicit_pass" {
			t.Errorf("expected DB_PASS=explicit_pass (explicit override), got %q", envMap["DB_PASS"])
		}
		if envMap["PORT"] != "5000" {
			t.Errorf("expected PORT=5000 (app_two override), got %q", envMap["PORT"])
		}
	})

	t.Run("WhitelistFiltersMerges", func(t *testing.T) {
		merges := []string{
			"my_vault://app_one",
		}
		whitelist := []string{"DB_USER"}
		explicit := map[string]string{
			"DB_PASS": "explicit_pass", // Explicit is never filtered
		}

		res, err := orch.BuildEnv(ctx, EnvConfig{Explicit: explicit, Merges: merges, Whitelist: whitelist})
		if err != nil {
			t.Fatalf("failed to build env: %v", err)
		}

		envMap := make(map[string]string)
		for _, item := range res {
			k, v, _ := strings.Cut(item, "=")
			envMap[k] = v
		}

		if envMap["DB_USER"] != "user1" {
			t.Errorf("expected DB_USER=user1, got %q", envMap["DB_USER"])
		}
		if envMap["DB_PASS"] != "explicit_pass" {
			t.Errorf("expected DB_PASS=explicit_pass, got %q", envMap["DB_PASS"])
		}
		if _, exists := envMap["PORT"]; exists {
			t.Errorf("expected PORT to be filtered out by whitelist, but it exists")
		}
	})
}

func TestEnvBuilder_BareURICommandArgResolution(t *testing.T) {
	ctx := context.Background()
	t.Setenv("BARE_TEST_VAR", "resolved_bare_value")

	t.Setenv("API_KEY", "app-secret-xyz")
	cfg := &config.Config{
		Vaults: map[string]config.VaultConfig{
			"app_vault": {
				Provider: "env",
			},
		},
	}

	orch, err := NewOrchestrator(cfg)
	if err != nil {
		t.Fatalf("failed to create orchestrator: %v", err)
	}

	t.Run("resolves bare known URI arguments", func(t *testing.T) {
		cmdArgs := []string{
			"curl",
			"env://BARE_TEST_VAR",
			"app_vault://API_KEY",
			"http://example.com/api",
			"nonexistent_scheme://test",
			"plain-argument",
			`{{ exec "echo" "-n" "resolved-by-exec" }}`,
			`{{ shquote "unquoted value" }}`,
			`{{.State.Status}}`, // Foreign docker format string must pass through untouched
		}

		expectedArgs := []string{
			"curl",
			"resolved_bare_value",
			"app-secret-xyz",
			"http://example.com/api",
			"nonexistent_scheme://test",
			"plain-argument",
			"resolved-by-exec",
			"'unquoted value'",
			`{{.State.Status}}`,
		}

		newCmdArgs, _, err := orch.BuildEnvForCommand(ctx, EnvConfig{CmdArgs: cmdArgs})
		if err != nil {
			t.Fatalf("failed to build env for command: %v", err)
		}

		if !slices.Equal(newCmdArgs, expectedArgs) {
			t.Fatalf("expected args to match resolved arguments")
		}
	})
}

func TestEnvBuilder_IsKnownScheme(t *testing.T) {
	tests := []struct {
		name   string
		config *config.Config
		scheme string
		want   bool
	}{
		{
			name:   "builtin env scheme",
			config: nil,
			scheme: "env",
			want:   true,
		},
		{
			name:   "builtin cache scheme",
			config: nil,
			scheme: "cache",
			want:   true,
		},
		{
			name:   "builtin keyring scheme",
			config: nil,
			scheme: "keyring",
			want:   true,
		},
		{
			name:   "builtin pass scheme",
			config: nil,
			scheme: "pass",
			want:   true,
		},
		{
			name:   "builtin search scheme",
			config: nil,
			scheme: "search",
			want:   true,
		},
		{
			name:   "unknown scheme with nil config",
			config: nil,
			scheme: "http",
			want:   false,
		},
		{
			name:   "unknown scheme with nil vaults",
			config: &config.Config{Vaults: nil},
			scheme: "myvault",
			want:   false,
		},
		{
			name: "custom configured vault exists",
			config: &config.Config{
				Vaults: map[string]config.VaultConfig{
					"myvault": {Provider: "env"},
				},
			},
			scheme: "myvault",
			want:   true,
		},
		{
			name: "custom configured vault does not exist",
			config: &config.Config{
				Vaults: map[string]config.VaultConfig{
					"myvault": {Provider: "env"},
				},
			},
			scheme: "othervault",
			want:   false,
		},
		{
			name:   "empty string scheme with nil config",
			config: nil,
			scheme: "",
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			eb := NewEnvBuilder(tt.config, nil)
			got := eb.isKnownScheme(tt.scheme)
			if got != tt.want {
				t.Errorf("eb.isKnownScheme(%q) = %v, want %v", tt.scheme, got, tt.want)
			}
		})
	}
}

func TestBuildEnvForCommand_Autoload(t *testing.T) {
	ctx := context.Background()
	t.Setenv("TEST_REGION", "us-east-1")
	t.Setenv("KUBECONFIG_VAL", "/path/to/staging.conf")

	cfg := &config.Config{
		Envs: map[string][]config.VaultSourceConfig{
			"aws_dev": {
				{
					Vault: "env",
					Rules: []config.RuleConfig{
						{Type: "pass", Include: []string{"TEST_REGION"}},
						{
							Type: "set",
							Values: map[string]string{
								"AWS_ACCESS_KEY_ID":     "AKIA1111",
								"AWS_SECRET_ACCESS_KEY": "secret1111",
								"AWS_DEFAULT_REGION":    "{{ .TEST_REGION }}",
							},
						},
					},
				},
			},
			"k8s_staging": {
				{
					Vault: "env",
					Rules: []config.RuleConfig{
						{Type: "pass", Include: []string{"KUBECONFIG_VAL"}},
						{
							Type: "set",
							Values: map[string]string{
								"KUBECONFIG": "{{ .KUBECONFIG_VAL }}",
							},
						},
					},
				},
			},
			"litellm_env": {
				{
					Vault: "env",
					Rules: []config.RuleConfig{
						{
							Type: "set",
							Values: map[string]string{
								"LITELLM_KEY": "sk-12345",
							},
						},
					},
				},
			},
		},
		Autoload: []config.AutoloadRule{
			{
				Match: "aws",
				Envs: []config.AutoloadEnvItem{
					config.NamedEnvItem("aws_dev"),
					config.InlineEnvItem(map[string]string{
						"LOG_LEVEL":  "INFO",
						"AWS_BANNER": "region={{ .AWS_DEFAULT_REGION }}",
					}),
				},
			},
			{
				Match: "kubectl*",
				Envs: []config.AutoloadEnvItem{
					config.NamedEnvItem("k8s_staging"),
					config.InlineEnvItem(map[string]string{"K8S_ENV": "staging"}),
				},
			},
		},
	}

	orch, err := NewOrchestrator(cfg)
	if err != nil {
		t.Fatalf("failed to create orchestrator: %v", err)
	}

	t.Run("Matching command autoloads named envs and inline template maps", func(t *testing.T) {
		cmdArgs := []string{"aws", "s3", "ls"}
		_, res, err := orch.BuildEnvForCommand(ctx, EnvConfig{CmdArgs: cmdArgs})
		if err != nil {
			t.Fatalf("failed to build env: %v", err)
		}

		envMap := make(map[string]string)
		for _, item := range res {
			k, v, _ := strings.Cut(item, "=")
			envMap[k] = v
		}

		if envMap["AWS_ACCESS_KEY_ID"] != "AKIA1111" {
			t.Errorf("expected AWS_ACCESS_KEY_ID=AKIA1111, got %q", envMap["AWS_ACCESS_KEY_ID"])
		}
		if envMap["AWS_SECRET_ACCESS_KEY"] != "secret1111" {
			t.Errorf("expected AWS_SECRET_ACCESS_KEY=secret1111, got %q", envMap["AWS_SECRET_ACCESS_KEY"])
		}
		if envMap["AWS_DEFAULT_REGION"] != "us-east-1" {
			t.Errorf("expected AWS_DEFAULT_REGION=us-east-1, got %q", envMap["AWS_DEFAULT_REGION"])
		}
		if envMap["LOG_LEVEL"] != "INFO" || envMap["AWS_BANNER"] != "region=us-east-1" {
			t.Errorf("unexpected inline env vars: LOG_LEVEL=%q AWS_BANNER=%q", envMap["LOG_LEVEL"], envMap["AWS_BANNER"])
		}
	})

	t.Run("CLI explicit flag overrides autoload env", func(t *testing.T) {
		cmdArgs := []string{"aws", "s3", "ls"}
		explicit := map[string]string{
			"AWS_DEFAULT_REGION": "us-west-2",
		}
		_, res, err := orch.BuildEnvForCommand(ctx, EnvConfig{CmdArgs: cmdArgs, Explicit: explicit})
		if err != nil {
			t.Fatalf("failed to build env: %v", err)
		}

		envMap := make(map[string]string)
		for _, item := range res {
			k, v, _ := strings.Cut(item, "=")
			envMap[k] = v
		}

		if envMap["AWS_DEFAULT_REGION"] != "us-west-2" {
			t.Errorf("expected CLI explicit flag us-west-2 to override autoload, got %q", envMap["AWS_DEFAULT_REGION"])
		}
	})

	t.Run("Matching glob command autoloads env and inline override", func(t *testing.T) {
		cmdArgs := []string{"kubectl-prod", "get", "pods"}
		_, res, err := orch.BuildEnvForCommand(ctx, EnvConfig{CmdArgs: cmdArgs})
		if err != nil {
			t.Fatalf("failed to build env: %v", err)
		}

		envMap := make(map[string]string)
		for _, item := range res {
			k, v, _ := strings.Cut(item, "=")
			envMap[k] = v
		}

		if envMap["KUBECONFIG"] != "/path/to/staging.conf" {
			t.Errorf("expected KUBECONFIG=/path/to/staging.conf, got %q", envMap["KUBECONFIG"])
		}
		if envMap["K8S_ENV"] != "staging" {
			t.Errorf("expected K8S_ENV=staging, got %q", envMap["K8S_ENV"])
		}
	})

	t.Run("Non matching command does not apply autoload rules", func(t *testing.T) {
		cmdArgs := []string{"helm", "install"}
		_, res, err := orch.BuildEnvForCommand(ctx, EnvConfig{CmdArgs: cmdArgs})
		if err != nil {
			t.Fatalf("failed to build env: %v", err)
		}

		envMap := make(map[string]string)
		for _, item := range res {
			k, v, _ := strings.Cut(item, "=")
			envMap[k] = v
		}

		if _, exists := envMap["AWS_ACCESS_KEY_ID"]; exists {
			t.Errorf("did not expect AWS_ACCESS_KEY_ID for helm command")
		}
		if _, exists := envMap["KUBECONFIG"]; exists {
			t.Errorf("did not expect KUBECONFIG for helm command")
		}
	})

	t.Run("Regex match and command transformation substitution", func(t *testing.T) {
		regexCfg := &config.Config{
			Envs: cfg.Envs,
			Autoload: []config.AutoloadRule{
				{
					Match:   `^litellm\s+(.*)$`,
					Command: `uvx --with 'litellm[proxy]' --with 'fastapi<0.116' litellm \1`,
					Envs:    []config.AutoloadEnvItem{config.NamedEnvItem("litellm_env")},
				},
			},
		}
		regexCfg.CompileAutoloadRules()

		orchRegex, err := NewOrchestrator(regexCfg)
		if err != nil {
			t.Fatalf("failed to create orchestrator: %v", err)
		}

		cmdArgs := []string{"litellm", "--config", "~/.config/litellm/config.yaml"}
		newCmdArgs, res, err := orchRegex.BuildEnvForCommand(ctx, EnvConfig{CmdArgs: cmdArgs})
		if err != nil {
			t.Fatalf("failed to build env for command: %v", err)
		}

		expectedArgs := []string{"uvx", "--with", "litellm[proxy]", "--with", "fastapi<0.116", "litellm", "--config", "~/.config/litellm/config.yaml"}
		if len(newCmdArgs) != len(expectedArgs) {
			t.Fatalf("expected %d args, got %d (%v)", len(expectedArgs), len(newCmdArgs), newCmdArgs)
		}
		for idx, arg := range newCmdArgs {
			if arg != expectedArgs[idx] {
				t.Errorf("arg[%d]: expected %q, got %q", idx, expectedArgs[idx], arg)
			}
		}

		envMap := make(map[string]string)
		for _, item := range res {
			k, v, _ := strings.Cut(item, "=")
			envMap[k] = v
		}
		if envMap["LITELLM_KEY"] != "sk-12345" {
			t.Errorf("expected LITELLM_KEY=sk-12345, got %q", envMap["LITELLM_KEY"])
		}
	})
}

func TestOrchestratorBuildEnvEmptyEnv(t *testing.T) {
	cfg := &config.Config{
		Vaults: map[string]config.VaultConfig{
			"my_vault": {Provider: "env"},
		},
	}
	orch, _ := NewOrchestrator(cfg)
	orch.providerManager.vaultCache["my_vault"] = newTestMemoryProvider(map[string]provider.Entry{
		"app_one": {
			"DB_USER": "user1",
			"DB_PASS": "pass1",
			"PORT":    "3000",
		},
	})
	ctx := context.Background()

	merges := []string{
		"my_vault://app_one",
	}
	explicit := map[string]string{
		"DB_PASS": "explicit_pass",
	}

	t.Setenv("TEST_ENV_VAR", "TEST_VALUE")

	res, err := orch.BuildEnv(ctx, EnvConfig{Explicit: explicit, Merges: merges, EmptyEnv: true})
	if err != nil {
		t.Fatalf("failed to build env: %v", err)
	}

	envMap := make(map[string]string)
	for _, item := range res {
		k, v, _ := strings.Cut(item, "=")
		envMap[k] = v
	}

	if len(res) != 3 {
		t.Errorf("expected 3 env variables, got %d", len(res))
	}
	if _, ok := envMap["TEST_ENV_VAR"]; ok {
		t.Errorf("expected TEST_ENV_VAR to not be present")
	}
}

func TestBuildEnvDeterministicOrder(t *testing.T) {
	cfg := &config.Config{}
	orch, err := NewOrchestrator(cfg)
	if err != nil {
		t.Fatalf("failed to create orchestrator: %v", err)
	}

	explicit := map[string]string{
		"ZEBRA":   "1",
		"ALPHA":   "2",
		"MANGO":   "3",
		"BETA":    "4",
		"DELTA":   "5",
		"CHARLIE": "6",
	}

	expected := []string{
		"ALPHA=2",
		"BETA=4",
		"CHARLIE=6",
		"DELTA=5",
		"MANGO=3",
		"ZEBRA=1",
	}

	for i := range 10 {
		res, err := orch.BuildEnv(context.Background(), EnvConfig{Explicit: explicit, EmptyEnv: true})
		if err != nil {
			t.Fatalf("failed to build env: %v", err)
		}
		if len(res) != len(expected) {
			t.Fatalf("expected %d items, got %d", len(expected), len(res))
		}
		for j, item := range res {
			if item != expected[j] {
				t.Fatalf("run %d index %d: expected %q, got %q", i, j, expected[j], item)
			}
		}
	}
}

func TestNormalizeURIs(t *testing.T) {
	tests := []struct {
		name string
		uris []string
		want []string
	}{
		{
			name: "empty list",
			uris: []string{},
			want: []string{},
		},
		{
			name: "nil list",
			uris: nil,
			want: []string{},
		},
		{
			name: "list with empty strings and spaces",
			uris: []string{"", "   ", "\t"},
			want: []string{},
		},
		{
			name: "list without schemes",
			uris: []string{"my_vault", "another_vault"},
			want: []string{"my_vault://", "another_vault://"},
		},
		{
			name: "list with schemes",
			uris: []string{"env://", "keyring://my-service"},
			want: []string{"env://", "keyring://my-service"},
		},
		{
			name: "mixed list",
			uris: []string{"env://", "my_vault", "  ", "keyring://"},
			want: []string{"env://", "my_vault://", "keyring://"},
		},
		{
			name: "needs trimming",
			uris: []string{"  env://  ", " my_vault\t"},
			want: []string{"env://", "my_vault://"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeURIs(tt.uris)
			if !slices.Equal(got, tt.want) {
				t.Errorf("normalizeURIs() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGetParentEnv(t *testing.T) {
	// Do not use t.Parallel() here because t.Setenv() is incompatible with parallel subtests,
	// and getParentEnv() reads the full os.Environ() which would cause data races.
	t.Setenv("NORMAL_KEY", "value")
	t.Setenv("MULTI_EQ_KEY", "value=with=equals")
	t.Setenv("EMPTY_VAL_KEY", "")

	envMap := getParentEnv()

	if val, ok := envMap["NORMAL_KEY"]; !ok || val != "value" {
		t.Errorf("expected NORMAL_KEY=value, got %q (ok: %v)", val, ok)
	}

	if val, ok := envMap["MULTI_EQ_KEY"]; !ok || val != "value=with=equals" {
		t.Errorf("expected MULTI_EQ_KEY=value=with=equals, got %q (ok: %v)", val, ok)
	}

	if val, ok := envMap["EMPTY_VAL_KEY"]; !ok || val != "" {
		t.Errorf("expected EMPTY_VAL_KEY=\"\", got %q (ok: %v)", val, ok)
	}
}

func TestBuildEnvForCommandDetails(t *testing.T) {
	ctx := context.Background()
	t.Setenv("SRC_AUTO", "resolved_auto_val")
	cfg := &config.Config{
		Vaults: map[string]config.VaultConfig{
			"test_vault": {
				Provider: "env",
			},
		},
		Envs: map[string][]config.VaultSourceConfig{
			"auto_env": {
				{
					Vault: "env",
					Rules: []config.RuleConfig{
						{Type: "pass", Include: []string{"SRC_AUTO"}},
						{Type: "set", Values: map[string]string{"AUTO_KEY": "{{ .SRC_AUTO }}"}},
					},
				},
			},
		},
		Autoload: []config.AutoloadRule{
			{
				Match:   `^test-alias\s+(.*)$`,
				Command: "real-binary $1",
				Envs:    []config.AutoloadEnvItem{config.NamedEnvItem("auto_env")},
			},
		},
	}
	cfg.CompileAutoloadRules()

	orch, err := NewOrchestrator(cfg)
	if err != nil {
		t.Fatalf("NewOrchestrator failed: %v", err)
	}
	orch.providerManager.vaultCache["test_vault"] = newTestMemoryProvider(map[string]provider.Entry{
		"default": {
			"VAR_Z": "val_z",
			"VAR_A": "val_a",
			"VAR_M": "val_m",
		},
	})

	details, err := orch.BuildEnvForCommandDetails(ctx, EnvConfig{
		CmdArgs: []string{"test-alias", "arg1", "arg2"},
		Explicit: map[string]string{
			"EXPLICIT_KEY": "explicit_val",
		},
		Merges:    []string{"test_vault://"},
		Whitelist: []string{"VAR_Z", "VAR_A"},
		EmptyEnv:  true,
	})
	if err != nil {
		t.Fatalf("BuildEnvForCommandDetails failed: %v", err)
	}

	expectedCmd := []string{"real-binary", "arg1", "arg2"}
	if !slices.Equal(details.Command, expectedCmd) {
		t.Errorf("expected Command = %v, got %v", expectedCmd, details.Command)
	}
	if !slices.Equal(details.RawCommand, expectedCmd) {
		t.Errorf("expected RawCommand = %v, got %v", expectedCmd, details.RawCommand)
	}

	expectedInjected := []string{"AUTO_KEY", "EXPLICIT_KEY", "SRC_AUTO", "VAR_A", "VAR_Z"}
	if !slices.Equal(details.InjectedKeys, expectedInjected) {
		t.Errorf("expected InjectedKeys = %v, got %v", expectedInjected, details.InjectedKeys)
	}

	envMap := make(map[string]string)
	for _, kv := range details.Env {
		k, v, _ := strings.Cut(kv, "=")
		envMap[k] = v
	}
	if envMap["AUTO_KEY"] != "resolved_auto_val" {
		t.Errorf("expected AUTO_KEY=resolved_auto_val, got %q", envMap["AUTO_KEY"])
	}
	if envMap["EXPLICIT_KEY"] != "explicit_val" {
		t.Errorf("expected EXPLICIT_KEY=explicit_val, got %q", envMap["EXPLICIT_KEY"])
	}
	if envMap["VAR_A"] != "val_a" || envMap["VAR_Z"] != "val_z" {
		t.Errorf("expected VAR_A and VAR_Z merged, got %v", envMap)
	}
	if _, ok := envMap["VAR_M"]; ok {
		t.Errorf("expected VAR_M to be filtered out by whitelist")
	}
}

func TestHasCloakenvInvocation(t *testing.T) {
	tests := []struct {
		name string
		arg  string
		want bool
	}{
		{
			name: "plain argument without braces",
			arg:  "plain-arg",
			want: false,
		},
		{
			name: "docker format foreign template",
			arg:  "'{{.State.Status}}'",
			want: false,
		},
		{
			name: "kubernetes jsonpath foreign template",
			arg:  `{"id":"{{.Id}}"}`,
			want: false,
		},
		{
			name: "unclosed opening brace without invocation",
			arg:  "prefix-{{-foo",
			want: false,
		},
		{
			name: "cloakenv get function",
			arg:  `--token={{ get "env://CLI_TOKEN" }}`,
			want: true,
		},
		{
			name: "cloakenv tryGet function",
			arg:  `--fallback={{ tryGet "env://NONEXISTENT" "env://CLI_TOKEN" }}`,
			want: true,
		},
		{
			name: "cloakenv entry function",
			arg:  `--entry={{ entry "vault://path/to/entry" }}`,
			want: true,
		},
		{
			name: "cloakenv search function",
			arg:  `--query={{ search "tag:prod" "password" }}`,
			want: true,
		},
		{
			name: "cloakenv include function",
			arg:  `--tmpl={{ include "my_template" . }}`,
			want: true,
		},
		{
			name: "cloakenv includeFile function",
			arg:  `--file={{ includeFile "path/file.txt" . }}`,
			want: true,
		},
		{
			name: "cloakenv exec function",
			arg:  `--exec={{ exec "echo" "hello" }}`,
			want: true,
		},
		{
			name: "cloakenv shquote function",
			arg:  `--quote={{ shquote "arg with spaces" }}`,
			want: true,
		},
		{
			name: "cloakenv shellQuote function",
			arg:  `--quote={{ shellQuote "arg with spaces" }}`,
			want: true,
		},
		{
			name: "pipeline execution with cloakenv function",
			arg:  `--token={{ .val | exec "sh" "-c" }}`,
			want: true,
		},
		{
			name: "grouped invocation with cloakenv function",
			arg:  `--token={{ (get "vault://sec:key") }}`,
			want: true,
		},
		{
			name: "multiple template blocks with second having invocation",
			arg:  `format={{.State.Status}}&secret={{ get "vault://sec:token" }}`,
			want: true,
		},
		{
			name: "multiple template blocks with neither having invocation",
			arg:  `format={{.State.Status}}&id={{.Id}}`,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hasCloakenvInvocation(tt.arg)
			if got != tt.want {
				t.Errorf("hasCloakenvInvocation(%q) = %v, want %v", tt.arg, got, tt.want)
			}
		})
	}
}

func TestBuildEnvForCommand_PR487ReviewRegressionCoverage(t *testing.T) {
	ctx := context.Background()

	t.Run("Autoload command expansion escapes user input against argument injection", func(t *testing.T) {
		secCfg := &config.Config{
			Autoload: []config.AutoloadRule{
				{
					Match:   `^mytool\s+(.*)$`,
					Command: `myrealtool $1`,
				},
			},
		}
		secCfg.CompileAutoloadRules()
		orchSec, err := NewOrchestrator(secCfg)
		if err != nil {
			t.Fatalf("failed to create orchestrator: %v", err)
		}
		cmdArgs := []string{"mytool", "arg1; injected_cmd --flag"}
		newCmdArgs, _, err := orchSec.BuildEnvForCommand(ctx, EnvConfig{CmdArgs: cmdArgs})
		if err != nil {
			t.Fatalf("failed to build env for command: %v", err)
		}
		expectedArgs := []string{"myrealtool", "arg1;", "injected_cmd", "--flag"}
		if len(newCmdArgs) != len(expectedArgs) {
			t.Fatalf("expected %d args, got %d (%v)", len(expectedArgs), len(newCmdArgs), newCmdArgs)
		}
		for idx, arg := range newCmdArgs {
			if arg != expectedArgs[idx] {
				t.Errorf("arg[%d]: expected %q, got %q", idx, expectedArgs[idx], arg)
			}
		}
	})

	t.Run("Validation rejects autoload rule with empty match", func(t *testing.T) {
		invalidCfg := &config.Config{
			Autoload: []config.AutoloadRule{
				{Match: "", Command: "echo 1"},
			},
		}
		if _, err := NewOrchestrator(invalidCfg); err == nil {
			t.Fatal("expected error for empty autoload match from NewOrchestrator, got nil")
		}
		tmp := filepath.Join(t.TempDir(), "empty_match.yaml")
		yamlData := "autoload:\n  - match: \"\"\n    envs:\n      - FOO: bar\n"
		if err := os.WriteFile(tmp, []byte(yamlData), 0o600); err != nil {
			t.Fatal(err)
		}
		res, err := config.Validate(tmp, true)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.IsValid() {
			t.Fatal("expected validation failure for empty match pattern")
		}
	})

	t.Run("CommandEnvDetails exposes ResolvedEnvs, Templates, and resolveAutoloadEnvs precedence and error paths", func(t *testing.T) {
		fetcher := &stubVaultFetcher{
			vaults: map[string][]SourceEntry{
				"work": {{
					Vault: "work",
					Path:  "app",
					Title: "App",
					Raw:   map[string]any{"title": "App", "tags": []string{}, "BASE_KEY": "from-vault", "SHARED": "vault-val"},
				}},
			},
		}
		cfg := &config.Config{
			Envs: map[string][]config.VaultSourceConfig{
				"base_env": {{Vault: "work"}},
			},
			Templates: map[string]config.TemplateConfig{
				"app_cfg": {Driver: "json", Vaults: []config.VaultSourceConfig{{Vault: "work"}}},
			},
			Autoload: []config.AutoloadRule{
				{
					Match: `^deploy`,
					Envs: []config.AutoloadEnvItem{
						config.NamedEnvItem("base_env"),
						config.InlineEnvItem(map[string]string{
							"SHARED":   "inline-override",
							"COMPUTED": `{{ .BASE_KEY }}-suffix`,
						}),
					},
					Templates: []config.AutoloadTemplateConfig{
						{Template: "app_cfg", EnvPath: "APP_CFG_PATH"},
					},
				},
			},
		}
		cfg.CompileAutoloadRules()
		re := NewRuleEngine()
		eb := NewEnvBuilder(cfg, nil)
		envsB := NewEnvsBuilderWithFetcher(cfg, fetcher, re)
		eb.SetEnvsBuilder(envsB)
		eb.SetTemplatesRenderer(NewTemplatesRendererWithFetcher(cfg, fetcher, re, envsB))
		eb.SetRuleEngine(re)
		details, err := eb.BuildEnvForCommandDetails(ctx, EnvConfig{
			CmdArgs:  []string{"deploy", "now"},
			EmptyEnv: true,
		})
		if err != nil {
			t.Fatalf("BuildEnvForCommandDetails failed: %v", err)
		}
		if details.ResolvedEnvs["BASE_KEY"] != "from-vault" || details.ResolvedEnvs["SHARED"] != "inline-override" || details.ResolvedEnvs["COMPUTED"] != "from-vault-suffix" {
			t.Fatalf("unexpected ResolvedEnvs: %#v", details.ResolvedEnvs)
		}
		if len(details.Templates) != 1 || details.Templates[0].Template != "app_cfg" || details.Templates[0].EnvPath != "APP_CFG_PATH" {
			t.Fatalf("unexpected Templates: %#v", details.Templates)
		}

		// Error path 1: envsBuilder == nil when named env referenced
		ebNil := NewEnvBuilder(cfg, nil)
		ebNil.SetRuleEngine(re)
		if _, err := ebNil.BuildEnvForCommandDetails(ctx, EnvConfig{CmdArgs: []string{"deploy"}, EmptyEnv: true}); err == nil || !strings.Contains(err.Error(), "envs builder is not configured") {
			t.Errorf("expected nil envsBuilder error, got: %v", err)
		}

		// Error path 2: unknown named env
		cfgUnknown := &config.Config{
			Autoload: []config.AutoloadRule{{Match: "^deploy", Envs: []config.AutoloadEnvItem{config.NamedEnvItem("missing_env")}}},
		}
		cfgUnknown.CompileAutoloadRules()
		ebUnknown := NewEnvBuilder(cfgUnknown, nil)
		ebUnknown.SetEnvsBuilder(NewEnvsBuilderWithFetcher(cfgUnknown, fetcher, re))
		ebUnknown.SetRuleEngine(re)
		if _, err := ebUnknown.BuildEnvForCommandDetails(ctx, EnvConfig{CmdArgs: []string{"deploy"}, EmptyEnv: true}); err == nil {
			t.Error("expected error for unknown named env")
		}

		// Error path 3: inline template missingkey=error
		cfgMissing := &config.Config{
			Autoload: []config.AutoloadRule{{Match: "^deploy", Envs: []config.AutoloadEnvItem{config.InlineEnvItem(map[string]string{"BAD": "{{ .DOES_NOT_EXIST }}"})}}},
		}
		cfgMissing.CompileAutoloadRules()
		ebMissing := NewEnvBuilder(cfgMissing, nil)
		ebMissing.SetEnvsBuilder(NewEnvsBuilderWithFetcher(cfgMissing, fetcher, re))
		ebMissing.SetRuleEngine(re)
		if _, err := ebMissing.BuildEnvForCommandDetails(ctx, EnvConfig{CmdArgs: []string{"deploy"}, EmptyEnv: true}); err == nil {
			t.Error("expected missingkey=error from inline env template")
		}
	})
}

func TestResolveAutoloadTemplates_RejectsEmptyEvaluatedPath(t *testing.T) {
	ctx := context.Background()
	fetcher := &stubVaultFetcher{
		vaults: map[string][]SourceEntry{
			"work": {{
				Vault: "work",
				Path:  "a",
				Title: "A",
				Raw:   map[string]any{"title": "A", "tags": []string{}, "K": "v"},
			}},
		},
	}
	cfg := &config.Config{
		Templates: map[string]config.TemplateConfig{
			"t1": {Driver: "json", Vaults: []config.VaultSourceConfig{{Vault: "work"}}},
		},
		Autoload: []config.AutoloadRule{
			{
				Match: `^app`,
				Envs:  []config.AutoloadEnvItem{config.InlineEnvItem(map[string]string{"EMPTY_PATH": "   "})},
				Templates: []config.AutoloadTemplateConfig{
					{Template: "t1", Path: `{{ .EMPTY_PATH }}`},
				},
			},
		},
	}
	cfg.CompileAutoloadRules()
	re := NewRuleEngine()
	eb := NewEnvBuilder(cfg, nil)
	eb.SetRuleEngine(re)
	eb.SetEnvsBuilder(NewEnvsBuilderWithFetcher(cfg, fetcher, re))
	eb.SetTemplatesRenderer(NewTemplatesRendererWithFetcher(cfg, fetcher, re, NewEnvsBuilderWithFetcher(cfg, fetcher, re)))

	if _, err := eb.BuildEnvForCommandDetails(ctx, EnvConfig{CmdArgs: []string{"app"}, EmptyEnv: true}); err == nil || !strings.Contains(err.Error(), "evaluated path is empty") {
		t.Fatalf("expected empty evaluated path error, got: %v", err)
	}
}
