package engine

import (
	"context"
	"strings"
	"testing"

	"github.com/warpcode/cloakenv/internal/config"
)

func TestOrchestratorBuildEnvMerges(t *testing.T) {
	cfg := &config.Config{
		Vaults: map[string]config.VaultConfig{
			"my_vault": {
				Provider: "custom_vault",
				Entities: map[string]map[string]any{
					"app_one": {
						"DB_USER": "user1",
						"DB_PASS": "pass1",
						"PORT":    "3000",
					},
					"app_two": {
						"PORT":    "5000",
						"DB_USER": "user2",
					},
				},
			},
		},
	}

	orch, err := NewOrchestrator(cfg)
	if err != nil {
		t.Fatalf("failed to create orchestrator: %v", err)
	}
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

		res, err := orch.BuildEnv(ctx, explicit, merges, nil, false)
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

		res, err := orch.BuildEnv(ctx, explicit, merges, whitelist, false)
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

func TestBuildEnvForCommand_Autoload(t *testing.T) {
	ctx := context.Background()
	t.Setenv("TEST_REGION", "us-east-1")

	cfg := &config.Config{
		Vaults: map[string]config.VaultConfig{
			"aws_dev": {
				Provider:     "custom_vault",
				SingleEntity: boolPtr(true),
				Attributes: map[string]any{
					"AWS_ACCESS_KEY_ID":     "AKIA1111",
					"AWS_SECRET_ACCESS_KEY": "secret1111",
					"EXTRA_VAR":             "should_be_filtered",
				},
			},
			"k8s_vault": {
				Provider: "custom_vault",
				Entities: map[string]map[string]any{
					"staging": {
						"KUBECONFIG": "/path/to/staging.conf",
					},
				},
			},
		},
		Autoload: []config.AutoloadRule{
			{
				Match:  "aws",
				Vaults: []string{"aws_dev"},
				Env: map[string]string{
					"AWS_DEFAULT_REGION": "env://TEST_REGION",
				},
				Whitelist: []string{"AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "AWS_DEFAULT_REGION"},
			},
			{
				Match: "kubectl*",
				Merge: []string{"k8s_vault://staging"},
				Env: map[string]string{
					"K8S_ENV": "staging",
				},
			},
		},
	}

	orch, err := NewOrchestrator(cfg)
	if err != nil {
		t.Fatalf("failed to create orchestrator: %v", err)
	}

	t.Run("Matching command autoloads vaults, env, and applies whitelist", func(t *testing.T) {
		cmdArgs := []string{"aws", "s3", "ls"}
		_, res, err := orch.BuildEnvForCommand(ctx, cmdArgs, nil, nil, nil, false)
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
		if _, exists := envMap["EXTRA_VAR"]; exists {
			t.Errorf("expected EXTRA_VAR to be filtered out by autoload whitelist")
		}
	})

	t.Run("CLI explicit flag overrides autoload env", func(t *testing.T) {
		cmdArgs := []string{"aws", "s3", "ls"}
		explicit := map[string]string{
			"AWS_DEFAULT_REGION": "us-west-2",
		}
		_, res, err := orch.BuildEnvForCommand(ctx, cmdArgs, explicit, nil, nil, false)
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

	t.Run("Matching glob command autoloads merge URI and env", func(t *testing.T) {
		cmdArgs := []string{"kubectl-prod", "get", "pods"}
		_, res, err := orch.BuildEnvForCommand(ctx, cmdArgs, nil, nil, nil, false)
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
		_, res, err := orch.BuildEnvForCommand(ctx, cmdArgs, nil, nil, nil, false)
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
			Vaults: map[string]config.VaultConfig{
				"litellm_vault": {
					Provider:     "custom_vault",
					SingleEntity: boolPtr(true),
					Attributes: map[string]any{
						"LITELLM_KEY": "sk-12345",
					},
				},
			},
			Autoload: []config.AutoloadRule{
				{
					Match:   `^litellm\s+(.*)$`,
					Command: `uvx --with 'litellm[proxy]' --with 'fastapi<0.116' litellm \1`,
					Vaults:  []string{"litellm_vault"},
				},
			},
		}

		orchRegex, err := NewOrchestrator(regexCfg)
		if err != nil {
			t.Fatalf("failed to create orchestrator: %v", err)
		}

		cmdArgs := []string{"litellm", "--config", "~/.config/litellm/config.yaml"}
		newCmdArgs, res, err := orchRegex.BuildEnvForCommand(ctx, cmdArgs, nil, nil, nil, false)
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

	t.Run("Autoload command substitution with secret URIs", func(t *testing.T) {
		t.Setenv("EXP_USER", "alice")
		uriCfg := &config.Config{
			Autoload: []config.AutoloadRule{
				{
					Match:   `^testest(.*)$`,
					Command: `echo "${env://EXP_USER}"`,
				},
			},
		}
		orchURI, err := NewOrchestrator(uriCfg)
		if err != nil {
			t.Fatalf("failed to create orchestrator: %v", err)
		}

		cmdArgs := []string{"testest"}
		newCmdArgs, _, err := orchURI.BuildEnvForCommand(ctx, cmdArgs, nil, nil, nil, false)
		if err != nil {
			t.Fatalf("failed to build env for command: %v", err)
		}

		expectedArgs := []string{"echo", "alice"}
		if len(newCmdArgs) != len(expectedArgs) {
			t.Fatalf("expected %d args, got %d (%v)", len(expectedArgs), len(newCmdArgs), newCmdArgs)
		}
		for idx, arg := range newCmdArgs {
			if arg != expectedArgs[idx] {
				t.Errorf("arg[%d]: expected %q, got %q", idx, expectedArgs[idx], arg)
			}
		}
	})

	t.Run("Direct command arguments with secret URIs and escaped tokens", func(t *testing.T) {
		t.Setenv("CLI_TOKEN", "secret-token-123")
		directCfg := &config.Config{}
		orchDirect, err := NewOrchestrator(directCfg)
		if err != nil {
			t.Fatalf("failed to create orchestrator: %v", err)
		}

		cmdArgs := []string{"mycli", "--token=${env://CLI_TOKEN}", "--raw=$${env://CLI_TOKEN}", "plain-arg"}
		newCmdArgs, _, err := orchDirect.BuildEnvForCommand(ctx, cmdArgs, nil, nil, nil, false)
		if err != nil {
			t.Fatalf("failed to build env for command: %v", err)
		}

		expectedArgs := []string{"mycli", "--token=secret-token-123", "--raw=${env://CLI_TOKEN}", "plain-arg"}
		if len(newCmdArgs) != len(expectedArgs) {
			t.Fatalf("expected %d args, got %d (%v)", len(expectedArgs), len(newCmdArgs), newCmdArgs)
		}
		for idx, arg := range newCmdArgs {
			if arg != expectedArgs[idx] {
				t.Errorf("arg[%d]: expected %q, got %q", idx, expectedArgs[idx], arg)
			}
		}
	})

	t.Run("Command argument with invalid secret URI returns error", func(t *testing.T) {
		failCfg := &config.Config{}
		orchFail, err := NewOrchestrator(failCfg)
		if err != nil {
			t.Fatalf("failed to create orchestrator: %v", err)
		}

		cmdArgs := []string{"mycli", "--token=${nonexistent_vault://missing}"}
		_, _, err = orchFail.BuildEnvForCommand(ctx, cmdArgs, nil, nil, nil, false)
		if err == nil {
			t.Fatal("expected error for unresolvable secret URI in command arg, got nil")
		}
	})

	t.Run("Validation rejects autoload rule with empty match", func(t *testing.T) {
		invalidCfg := &config.Config{
			Autoload: []config.AutoloadRule{
				{Match: "", Command: "echo 1"},
			},
		}
		_, err := NewOrchestrator(invalidCfg)
		if err == nil {
			t.Fatal("expected error for empty autoload match, got nil")
		}
	})
}

func TestOrchestratorBuildEnvEmptyEnv(t *testing.T) {
	cfg := &config.Config{
		Vaults: map[string]config.VaultConfig{
			"my_vault": {
				Provider: "custom_vault",
				Entities: map[string]map[string]any{
					"app_one": {
						"DB_USER": "user1",
						"DB_PASS": "pass1",
						"PORT":    "3000",
					},
				},
			},
		},
	}
	orch, _ := NewOrchestrator(cfg)
	ctx := context.Background()

	merges := []string{
		"my_vault://app_one",
	}
	explicit := map[string]string{
		"DB_PASS": "explicit_pass",
	}

	t.Setenv("TEST_ENV_VAR", "TEST_VALUE")

	res, err := orch.BuildEnv(ctx, explicit, merges, nil, true)
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
