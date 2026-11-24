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

func TestBuildEnvForCommand_Autoload(t *testing.T) {
	t.Setenv("AWS_ACCESS_KEY_ID_SRC", "AKIAIOSFODNN7EXAMPLE")
	t.Setenv("AWS_SECRET_ACCESS_KEY_SRC", "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY")
	t.Setenv("KUBE_ENV_SRC", "staging")
	t.Setenv("LITELLM_KEY_SRC", "sk-12345")

	cfg := &config.Config{
		Envs: map[string][]config.VaultSourceConfig{
			"aws_env": {
				{
					Vault: "env",
					Rules: []config.RuleConfig{
						{Type: "match", Match: `^AWS_ACCESS_KEY_ID_SRC$`, Key: "AWS_ACCESS_KEY_ID"},
						{Type: "match", Match: `^AWS_SECRET_ACCESS_KEY_SRC$`, Key: "AWS_SECRET_ACCESS_KEY"},
						{Type: "set", Values: map[string]string{"AWS_DEFAULT_REGION": "us-east-1"}},
					},
				},
			},
			"kube_env": {
				{
					Vault: "env",
					Rules: []config.RuleConfig{
						{Type: "match", Match: `^KUBE_ENV_SRC$`, Key: "K8S_ENV"},
					},
				},
			},
			"litellm_env": {
				{
					Vault: "env",
					Rules: []config.RuleConfig{
						{Type: "match", Match: `^LITELLM_KEY_SRC$`, Key: "LITELLM_KEY"},
					},
				},
			},
		},
		Autoload: []config.AutoloadRule{
			{
				Match: "aws",
				Envs:  []config.AutoloadEnvItem{config.NamedEnvItem("aws_env")},
			},
			{
				Match: "kubectl-*",
				Envs: []config.AutoloadEnvItem{
					config.NamedEnvItem("kube_env"),
					config.InlineEnvItem(map[string]string{
						"KUBECONFIG": "/path/to/{{ .K8S_ENV }}.conf",
					}),
				},
			},
		},
	}
	cfg.CompileAutoloadRules()

	orch, err := NewOrchestrator(cfg)
	if err != nil {
		t.Fatalf("failed to create orchestrator: %v", err)
	}
	ctx := context.Background()

	t.Run("Matching exact command autoloads named env", func(t *testing.T) {
		cmdArgs := []string{"aws", "s3", "ls"}
		_, res, err := orch.BuildEnvForCommand(ctx, EnvConfig{CmdArgs: cmdArgs, EmptyEnv: true})
		if err != nil {
			t.Fatalf("failed to build env: %v", err)
		}

		envMap := make(map[string]string)
		for _, item := range res {
			k, v, _ := strings.Cut(item, "=")
			envMap[k] = v
		}

		if envMap["AWS_ACCESS_KEY_ID"] != "AKIAIOSFODNN7EXAMPLE" {
			t.Errorf("expected AWS_ACCESS_KEY_ID, got %q", envMap["AWS_ACCESS_KEY_ID"])
		}
		if envMap["AWS_DEFAULT_REGION"] != "us-east-1" {
			t.Errorf("expected AWS_DEFAULT_REGION=us-east-1, got %q", envMap["AWS_DEFAULT_REGION"])
		}
	})

	t.Run("Matching glob command autoloads env and inline template override", func(t *testing.T) {
		cmdArgs := []string{"kubectl-prod", "get", "pods"}
		_, res, err := orch.BuildEnvForCommand(ctx, EnvConfig{CmdArgs: cmdArgs, EmptyEnv: true})
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
		_, res, err := orch.BuildEnvForCommand(ctx, EnvConfig{CmdArgs: cmdArgs, EmptyEnv: true})
		if err != nil {
			t.Fatalf("failed to build env: %v", err)
		}
		if len(res) != 0 {
			t.Errorf("expected empty env for unmatched command with EmptyEnv=true, got %v", res)
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
		newCmdArgs, res, err := orchRegex.BuildEnvForCommand(ctx, EnvConfig{CmdArgs: cmdArgs, EmptyEnv: true})
		if err != nil {
			t.Fatalf("failed to build env for command: %v", err)
		}

		expectedArgs := []string{"uvx", "--with", "litellm[proxy]", "--with", "fastapi<0.116", "litellm", "--config", "~/.config/litellm/config.yaml"}
		if !slices.Equal(newCmdArgs, expectedArgs) {
			t.Fatalf("newCmdArgs = %v, want %v", newCmdArgs, expectedArgs)
		}
		if !slices.Contains(res, "LITELLM_KEY=sk-12345") {
			t.Errorf("expected LITELLM_KEY=sk-12345 in %v", res)
		}
	})
}

func TestBuildEnvForCommand_ChildArgsPassThroughVerbatim(t *testing.T) {
	t.Setenv("SENSITIVE_VAR", "never-expand-into-argv")
	cfg := &config.Config{}
	orch, err := NewOrchestrator(cfg)
	if err != nil {
		t.Fatalf("NewOrchestrator failed: %v", err)
	}

	// Child command arguments must never have URIs or Go templates evaluated.
	rawArgs := []string{
		"docker",
		"ps",
		"--format={{.State.Status}}:{{.Id}}",
		"env://SENSITIVE_VAR",
		`{{ get "env://SENSITIVE_VAR" }}`,
		`{{ exec "echo" "nope" }}`,
	}

	gotArgs, _, err := orch.BuildEnvForCommand(context.Background(), EnvConfig{
		CmdArgs:  rawArgs,
		EmptyEnv: true,
	})
	if err != nil {
		t.Fatalf("BuildEnvForCommand failed: %v", err)
	}
	if !slices.Equal(gotArgs, rawArgs) {
		t.Fatalf("child command args were altered:\ngot:  %v\nwant: %v", gotArgs, rawArgs)
	}
}

func TestBuildEnvForCommand_EmptyEnv(t *testing.T) {
	t.Setenv("PARENT_ONLY_VAR", "parent_value")
	cfg := &config.Config{
		Vaults: map[string]config.VaultConfig{
			"my_vault": {Provider: "env"},
		},
		Envs: map[string][]config.VaultSourceConfig{
			"app_env": {
				{
					Vault: "my_vault",
					Rules: []config.RuleConfig{{Type: "pass"}},
				},
			},
		},
		Autoload: []config.AutoloadRule{
			{
				Match: "myapp",
				Envs:  []config.AutoloadEnvItem{config.NamedEnvItem("app_env")},
			},
		},
	}
	cfg.CompileAutoloadRules()

	orch, err := NewOrchestrator(cfg)
	if err != nil {
		t.Fatalf("NewOrchestrator failed: %v", err)
	}
	orch.providerManager.vaultCache["my_vault"] = newTestMemoryProvider(map[string]provider.Entry{
		"app_one": {
			"DB_USER": "user1",
			"DB_PASS": "pass1",
			"PORT":    "3000",
		},
	})

	_, res, err := orch.BuildEnvForCommand(context.Background(), EnvConfig{
		CmdArgs:  []string{"myapp"},
		EmptyEnv: true,
	})
	if err != nil {
		t.Fatalf("BuildEnvForCommand failed: %v", err)
	}
	if len(res) != 3 {
		t.Fatalf("expected 3 env vars, got %d: %v", len(res), res)
	}
	for _, kv := range res {
		if strings.HasPrefix(kv, "PARENT_ONLY_VAR=") {
			t.Errorf("PARENT_ONLY_VAR leaked despite EmptyEnv=true")
		}
	}
}

func TestBuildEnvDeterministicOrder(t *testing.T) {
	got := formatEnvMap(map[string]string{
		"Z_KEY": "z",
		"A_KEY": "a",
		"M_KEY": "m",
	})
	want := []string{"A_KEY=a", "M_KEY=m", "Z_KEY=z"}
	if !slices.Equal(got, want) {
		t.Errorf("formatEnvMap = %v, want %v", got, want)
	}
}

func TestGetParentEnv(t *testing.T) {
	t.Setenv("TEST_PARENT_KEY", "test_val")
	t.Setenv("EMPTY_VAL_KEY", "")
	m := getParentEnv()
	if m["TEST_PARENT_KEY"] != "test_val" {
		t.Errorf("expected TEST_PARENT_KEY=test_val, got %q", m["TEST_PARENT_KEY"])
	}
	if val, ok := m["EMPTY_VAL_KEY"]; !ok || val != "" {
		t.Errorf("expected EMPTY_VAL_KEY=\"\", got %q (ok: %v)", val, ok)
	}
}

func TestParseParentEnv_SkipsInvalidEntries(t *testing.T) {
	environ := []string{
		"VALID_KEY=val",
		"EMPTY=",
		"A=b=c d",
		"export MISE_SHELL=zsh",
		"BAD\tKEY=1",
		"BAD\nKEY=1",
		"BAD\rKEY=1",
		"BAD\x00KEY=1",
		"GOOD=bad\x00val",
		"=empty_key",
		"MISSING_EQUALS",
	}

	got := parseParentEnv(environ)
	want := map[string]string{
		"VALID_KEY": "val",
		"EMPTY":     "",
		"A":         "b=c d",
	}

	if len(got) != len(want) {
		t.Fatalf("parseParentEnv() returned %d entries (%v), want %d (%v)", len(got), got, len(want), want)
	}
	for k, wantV := range want {
		gotV, ok := got[k]
		if !ok || gotV != wantV {
			t.Errorf("parseParentEnv()[%q] = (%q, %v), want (%q, true)", k, gotV, ok, wantV)
		}
	}
}

func TestBuildEnvForCommandDetails(t *testing.T) {
	ctx := context.Background()
	t.Setenv("SRC_AUTO", "resolved_auto_val")
	cfg := &config.Config{
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
				Envs: []config.AutoloadEnvItem{
					config.NamedEnvItem("auto_env"),
					config.InlineEnvItem(map[string]string{"INLINE_KEY": "inline_val"}),
				},
			},
		},
	}
	cfg.CompileAutoloadRules()

	orch, err := NewOrchestrator(cfg)
	if err != nil {
		t.Fatalf("NewOrchestrator failed: %v", err)
	}

	details, err := orch.BuildEnvForCommandDetails(ctx, EnvConfig{
		CmdArgs:  []string{"test-alias", "arg1", "arg2"},
		EmptyEnv: true,
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

	expectedInjected := []string{"AUTO_KEY", "INLINE_KEY", "SRC_AUTO"}
	if !slices.Equal(details.InjectedKeys, expectedInjected) {
		t.Errorf("expected InjectedKeys = %v, got %v", expectedInjected, details.InjectedKeys)
	}

	envMap := make(map[string]string)
	for _, kv := range details.Env {
		k, v, _ := strings.Cut(kv, "=")
		envMap[k] = v
	}
	if envMap["AUTO_KEY"] != "resolved_auto_val" || envMap["INLINE_KEY"] != "inline_val" {
		t.Errorf("unexpected envMap: %+v", envMap)
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
		envsB := NewEnvsBuilderWithFetcher(cfg, fetcher, re)
		tr := NewTemplatesRendererWithFetcher(cfg, fetcher, re, envsB)
		eb := NewEnvBuilder(cfg, envsB, tr, re)
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
		ebNil := NewEnvBuilder(cfg, nil, nil, re)
		if _, err := ebNil.BuildEnvForCommandDetails(ctx, EnvConfig{CmdArgs: []string{"deploy"}, EmptyEnv: true}); err == nil || !strings.Contains(err.Error(), "envs builder is not configured") {
			t.Errorf("expected nil envsBuilder error, got: %v", err)
		}

		// Error path 2: unknown named env
		cfgUnknown := &config.Config{
			Autoload: []config.AutoloadRule{{Match: "^deploy", Envs: []config.AutoloadEnvItem{config.NamedEnvItem("missing_env")}}},
		}
		cfgUnknown.CompileAutoloadRules()
		ebUnknown := NewEnvBuilder(cfgUnknown, NewEnvsBuilderWithFetcher(cfgUnknown, fetcher, re), nil, re)
		if _, err := ebUnknown.BuildEnvForCommandDetails(ctx, EnvConfig{CmdArgs: []string{"deploy"}, EmptyEnv: true}); err == nil {
			t.Error("expected error for unknown named env")
		}

		// Error path 3: inline template missingkey=error
		cfgMissing := &config.Config{
			Autoload: []config.AutoloadRule{{Match: "^deploy", Envs: []config.AutoloadEnvItem{config.InlineEnvItem(map[string]string{"BAD": "{{ .DOES_NOT_EXIST }}"})}}},
		}
		cfgMissing.CompileAutoloadRules()
		ebMissing := NewEnvBuilder(cfgMissing, NewEnvsBuilderWithFetcher(cfgMissing, fetcher, re), nil, re)
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
	envsB := NewEnvsBuilderWithFetcher(cfg, fetcher, re)
	tr := NewTemplatesRendererWithFetcher(cfg, fetcher, re, envsB)
	eb := NewEnvBuilder(cfg, envsB, tr, re)

	if _, err := eb.BuildEnvForCommandDetails(ctx, EnvConfig{CmdArgs: []string{"app"}, EmptyEnv: true}); err == nil || !strings.Contains(err.Error(), "evaluated path is empty") {
		t.Fatalf("expected empty evaluated path error, got: %v", err)
	}
}
