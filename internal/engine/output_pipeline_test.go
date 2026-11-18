package engine

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/warpcode/cloakenv/internal/config"
	"github.com/warpcode/cloakenv/internal/utils"
)

type stubVaultFetcher struct {
	vaults map[string][]SourceEntry
}

func (f *stubVaultFetcher) FetchEntries(_ context.Context, vaultName string) ([]SourceEntry, error) {
	entries, ok := f.vaults[vaultName]
	if !ok {
		return nil, os.ErrNotExist
	}
	return entries, nil
}

func TestEnvsBuilder_DeterministicSortAndLastEntryWins(t *testing.T) {
	fetcher := &stubVaultFetcher{
		vaults: map[string][]SourceEntry{
			"work": {
				{
					Vault: "work",
					Path:  "z/second",
					Title: "Second",
					Tags:  []string{"litellm"},
					Raw: map[string]any{
						"title":   "Second",
						"path":    "z/second",
						"tags":    []string{"litellm"},
						"API_KEY": "from-second",
						"EXTRA":   "yes",
					},
				},
				{
					Vault: "work",
					Path:  "a/first",
					Title: "First",
					Tags:  []string{"litellm"},
					Raw: map[string]any{
						"title":   "First",
						"path":    "a/first",
						"tags":    []string{"litellm"},
						"API_KEY": "from-first",
						"FIRST":   "1",
					},
				},
				{
					Vault: "work",
					Path:  "b/skipped",
					Title: "Skipped",
					Tags:  []string{"other"},
					Raw: map[string]any{
						"title":   "Skipped",
						"path":    "b/skipped",
						"tags":    []string{"other"},
						"API_KEY": "must-not-appear",
					},
				},
			},
			"override": {
				{
					Vault: "override",
					Path:  "override",
					Title: "Override",
					Raw: map[string]any{
						"title": "Override",
						"EXTRA": "overridden-by-second-source",
					},
				},
			},
		},
	}

	cfg := &config.Config{
		Envs: map[string][]config.VaultSourceConfig{
			"litellm_env": {
				{
					Vault: "work",
					Query: `{{ hasTag "litellm" .tags }}`,
				},
				{
					Vault: "override",
				},
			},
		},
	}

	builder := NewEnvsBuilderWithFetcher(cfg, fetcher, NewRuleEngine())
	got, err := builder.Build(context.Background(), "litellm_env")
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	want := map[string]string{
		"FIRST":   "1",
		"API_KEY": "from-second",
		"EXTRA":   "overridden-by-second-source",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestTemplatesRenderer_AllDriversAndPostRulesOnly(t *testing.T) {
	tmpDir := t.TempDir()
	tmplFile := filepath.Join(tmpDir, "models.tmpl")
	if err := os.WriteFile(tmplFile, []byte("{{- range .entries }}{{ .model }}={{ .api_key }}\n{{ end -}}"), 0600); err != nil {
		t.Fatalf("failed to write template file: %v", err)
	}

	fetcher := &stubVaultFetcher{
		vaults: map[string][]SourceEntry{
			"work": {
				{
					Vault: "work",
					Path:  "ai/b",
					Title: "Claude",
					Tags:  []string{"llm"},
					Raw: map[string]any{
						"title":   "Claude",
						"path":    "ai/b",
						"tags":    []string{"llm"},
						"model":   "claude-3",
						"api_key": "sk-ant",
						"secret":  "unfiltered-must-not-leak",
					},
				},
				{
					Vault: "work",
					Path:  "ai/a",
					Title: "OpenAI",
					Tags:  []string{"llm"},
					Raw: map[string]any{
						"title":   "OpenAI",
						"path":    "ai/a",
						"tags":    []string{"llm"},
						"model":   "gpt-4o",
						"api_key": "sk-open",
						"secret":  "unfiltered-must-not-leak",
					},
				},
			},
		},
	}

	rules := []config.RuleConfig{
		{
			Type:      "pass",
			Include:   []string{"model", "api_key"},
			KeyFormat: utils.KeyFormatNone,
		},
	}

	cfg := &config.Config{
		ConfigPath: filepath.Join(tmpDir, "config.yaml"),
		Envs: map[string][]config.VaultSourceConfig{
			"llm_env": {
				{
					Vault: "work",
					Rules: []config.RuleConfig{
						{Type: "pass", Include: []string{"model", "api_key"}},
					},
				},
			},
		},
		Templates: map[string]config.TemplateConfig{
			"from_file": {
				Driver: "template",
				Path:   "models.tmpl",
				Vaults: []config.VaultSourceConfig{{Vault: "work", Rules: rules}},
			},
			"as_json": {
				Driver: "json",
				Vaults: []config.VaultSourceConfig{{Vault: "work", Rules: rules}},
			},
			"as_yaml": {
				Driver: "yaml",
				Vaults: []config.VaultSourceConfig{{Vault: "work", Rules: rules}},
			},
			"as_dotenv": {
				Driver:  "dotenv",
				FromEnv: "llm_env",
				Vaults: []config.VaultSourceConfig{
					{
						Vault: "work",
						Rules: []config.RuleConfig{
							{Type: "set", Values: map[string]string{"EXTRA": "line\nwith\"quotes"}},
						},
					},
				},
			},
		},
	}

	re := NewRuleEngine()
	envs := NewEnvsBuilderWithFetcher(cfg, fetcher, re)
	renderer := NewTemplatesRendererWithFetcher(cfg, fetcher, re, envs)

	ctx := context.Background()

	// 1. template driver
	tmplOut, err := renderer.Render(ctx, "from_file")
	if err != nil {
		t.Fatalf("template driver failed: %v", err)
	}
	wantTmpl := "gpt-4o=sk-open\nclaude-3=sk-ant\n"
	if tmplOut != wantTmpl {
		t.Fatalf("template output = %q, want %q", tmplOut, wantTmpl)
	}

	// 2. json driver (post-rules .entries only)
	jsonOut, err := renderer.Render(ctx, "as_json")
	if err != nil {
		t.Fatalf("json driver failed: %v", err)
	}
	if strings.Contains(jsonOut, "unfiltered-must-not-leak") {
		t.Fatalf("json output leaked unfiltered field: %s", jsonOut)
	}
	var parsedJSON []map[string]string
	if err := json.Unmarshal([]byte(jsonOut), &parsedJSON); err != nil {
		t.Fatalf("json output invalid: %v\n%s", err, jsonOut)
	}
	if len(parsedJSON) != 2 || parsedJSON[0]["model"] != "gpt-4o" || parsedJSON[1]["model"] != "claude-3" {
		t.Fatalf("unexpected json entries: %+v", parsedJSON)
	}

	// 3. yaml driver (post-rules .entries only)
	yamlOut, err := renderer.Render(ctx, "as_yaml")
	if err != nil {
		t.Fatalf("yaml driver failed: %v", err)
	}
	if strings.Contains(yamlOut, "unfiltered-must-not-leak") {
		t.Fatalf("yaml output leaked unfiltered field: %s", yamlOut)
	}
	var parsedYAML []map[string]string
	if err := utils.UnmarshalYAML([]byte(yamlOut), &parsedYAML); err != nil {
		t.Fatalf("yaml output invalid: %v\n%s", err, yamlOut)
	}
	if len(parsedYAML) != 2 || parsedYAML[0]["model"] != "gpt-4o" {
		t.Fatalf("unexpected yaml entries: %+v", parsedYAML)
	}

	// 4. dotenv driver (from_env + vaults override + sorted escaping)
	dotenvOut, err := renderer.Render(ctx, "as_dotenv")
	if err != nil {
		t.Fatalf("dotenv driver failed: %v", err)
	}
	wantDotenv := "API_KEY=\"sk-ant\"\nEXTRA=\"line\\nwith\\\"quotes\"\nMODEL=\"claude-3\""
	if dotenvOut != wantDotenv {
		t.Fatalf("dotenv output =\n%s\nwant:\n%s", dotenvOut, wantDotenv)
	}
}

func TestTemplatesRenderer_QueryErrorFailsRun(t *testing.T) {
	fetcher := &stubVaultFetcher{
		vaults: map[string][]SourceEntry{
			"work": {
				{Vault: "work", Path: "a", Title: "A", Raw: map[string]any{"title": "A"}},
			},
		},
	}
	cfg := &config.Config{
		Envs: map[string][]config.VaultSourceConfig{
			"bad": {{Vault: "work", Query: `{{ .nonexistent }}`}},
		},
	}
	b := NewEnvsBuilderWithFetcher(cfg, fetcher, NewRuleEngine())
	_, err := b.Build(context.Background(), "bad")
	if err == nil || !strings.Contains(err.Error(), "query execution failed") {
		t.Fatalf("expected query execution error, got %v", err)
	}
}

func TestOrchestrator_PipelineIntegrationWithEnvProvider(t *testing.T) {
	t.Setenv("CLOAKENV_PIPE_A", "valA")
	t.Setenv("CLOAKENV_PIPE_B", "valB")

	cfg := &config.Config{
		Vaults: map[string]config.VaultConfig{},
		Envs: map[string][]config.VaultSourceConfig{
			"app_env": {
				{
					Vault: "env",
					Rules: []config.RuleConfig{
						{Type: "pass", Include: []string{"CLOAKENV_PIPE_*"}},
					},
				},
			},
		},
		Templates: map[string]config.TemplateConfig{
			"app_dotenv": {
				Driver:  "dotenv",
				FromEnv: "app_env",
			},
		},
	}

	orch, err := NewOrchestrator(cfg)
	if err != nil {
		t.Fatalf("NewOrchestrator failed: %v", err)
	}

	envMap, err := orch.EnvsBuilder().Build(context.Background(), "app_env")
	if err != nil {
		t.Fatalf("EnvsBuilder.Build failed: %v", err)
	}
	if envMap["CLOAKENV_PIPE_A"] != "valA" || envMap["CLOAKENV_PIPE_B"] != "valB" {
		t.Fatalf("unexpected envMap: %v", envMap)
	}

	rendered, err := orch.TemplatesRenderer().Render(context.Background(), "app_dotenv")
	if err != nil {
		t.Fatalf("TemplatesRenderer.Render failed: %v", err)
	}
	want := "CLOAKENV_PIPE_A=\"valA\"\nCLOAKENV_PIPE_B=\"valB\""
	if rendered != want {
		t.Fatalf("got %q, want %q", rendered, want)
	}
}
