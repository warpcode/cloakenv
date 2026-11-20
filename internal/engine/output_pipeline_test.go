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
	"github.com/warpcode/cloakenv/internal/provider"
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

type mockSearchableVaultProvider struct {
	scheme  string
	results []provider.Entry
}

func (m *mockSearchableVaultProvider) Scheme() string { return m.scheme }
func (m *mockSearchableVaultProvider) Initialize(_ context.Context, _ provider.ProviderConfig) error {
	return nil
}
func (m *mockSearchableVaultProvider) GetSecret(_ context.Context, _ string) (string, error) {
	return "", os.ErrNotExist
}
func (m *mockSearchableVaultProvider) Validate(_ map[string]string) error { return nil }
func (m *mockSearchableVaultProvider) Search(_ context.Context, _ provider.SearchQuery) ([]provider.Entry, error) {
	return m.results, nil
}
func (m *mockSearchableVaultProvider) GetEntry(_ context.Context, _ string) (provider.Entry, error) {
	return provider.Entry{}, os.ErrNotExist
}

func TestOutputPipeline_ReviewRegressionCoverage(t *testing.T) {
	ctx := context.Background()

	t.Run("FetchEntries populates empty tags on untagged entries so hasTag succeeds under missingkey=error", func(t *testing.T) {
		pm := NewProviderManager(&config.Config{}, map[string]provider.SecretProvider{
			"work": &mockSearchableVaultProvider{
				scheme: "keepass",
				results: []provider.Entry{
					{
						"path":     "db/untagged",
						"title":    "Untagged",
						"Password": "pw1",
					},
					{
						"path":     "db/tagged",
						"title":    "Tagged",
						"tags":     []string{"prod"},
						"Password": "pw2",
					},
				},
			},
		}, nil)
		fetcher := &providerManagerEntryFetcher{pm: pm}
		cfg := &config.Config{
			Envs: map[string][]config.VaultSourceConfig{
				"prod_env": {{
					Vault: "work",
					Query: `{{ hasTag "prod" .tags }}`,
					Rules: []config.RuleConfig{{Type: "pass", KeyFormat: "env"}},
				}},
			},
		}
		eb := NewEnvsBuilderWithFetcher(cfg, fetcher, NewRuleEngine())
		res, err := eb.BuildDetailed(ctx, "prod_env")
		if err != nil {
			t.Fatalf("BuildDetailed failed on vault containing untagged entry: %v", err)
		}
		if res.Env["PASSWORD"] != "pw2" {
			t.Fatalf("got PASSWORD=%q, want pw2", res.Env["PASSWORD"])
		}
	})

	t.Run("dotenv driver with from_env populates TemplateRenderResult.Entries", func(t *testing.T) {
		fetcher := &stubVaultFetcher{
			vaults: map[string][]SourceEntry{
				"work": {{
					Vault: "work",
					Path:  "app/a",
					Title: "A",
					Raw:   map[string]any{"title": "A", "tags": []string{}, "API_KEY": "sk-from-env-secret"},
				}},
			},
		}
		cfg := &config.Config{
			Envs: map[string][]config.VaultSourceConfig{
				"app_env": {{Vault: "work"}},
			},
			Templates: map[string]config.TemplateConfig{
				"dot": {Driver: "dotenv", FromEnv: "app_env"},
			},
		}
		eb := NewEnvsBuilderWithFetcher(cfg, fetcher, NewRuleEngine())
		tr := NewTemplatesRendererWithFetcher(cfg, fetcher, NewRuleEngine(), eb)
		res, err := tr.RenderDetailed(ctx, "dot")
		if err != nil {
			t.Fatalf("RenderDetailed failed: %v", err)
		}
		if len(res.Entries) != 1 || res.Entries[0]["API_KEY"] != "sk-from-env-secret" {
			t.Fatalf("expected Entries to contain from_env secrets for masking, got %#v", res.Entries)
		}
		if !strings.Contains(res.Content, `API_KEY="sk-from-env-secret"`) {
			t.Fatalf("unexpected dotenv content: %q", res.Content)
		}
	})

	t.Run("dotenv escapes special characters and rejects invalid keys without leaking secret values", func(t *testing.T) {
		fetcher := &stubVaultFetcher{
			vaults: map[string][]SourceEntry{
				"work": {{
					Vault: "work",
					Path:  "app/esc",
					Title: "Esc",
					Raw: map[string]any{
						"title": "Esc",
						"tags":  []string{},
						"VAL":   "a\\b\"c$HOME`id`\nd\re\tf",
					},
				}},
			},
		}
		cfg := &config.Config{
			Templates: map[string]config.TemplateConfig{
				"dot": {
					Driver: "dotenv",
					Vaults: []config.VaultSourceConfig{{
						Vault: "work",
						Rules: []config.RuleConfig{{Type: "pass", KeyFormat: "none"}},
					}},
				},
			},
		}
		eb := NewEnvsBuilderWithFetcher(cfg, fetcher, NewRuleEngine())
		tr := NewTemplatesRendererWithFetcher(cfg, fetcher, NewRuleEngine(), eb)
		res, err := tr.RenderDetailed(ctx, "dot")
		if err != nil {
			t.Fatalf("RenderDetailed failed: %v", err)
		}
		want := `VAL="a\\b\"c\$HOME\` + "`" + `id\` + "`" + `\nd\re\tf"`
		if res.Content != want {
			t.Fatalf("got:\n%s\nwant:\n%s", res.Content, want)
		}

		// Invalid key injection attempt
		badFetcher := &stubVaultFetcher{
			vaults: map[string][]SourceEntry{
				"work": {{
					Vault: "work",
					Path:  "app/bad",
					Title: "Bad",
					Raw:   map[string]any{"title": "Bad", "tags": []string{}, "GOOD\nEVIL=1": "TOP_SECRET_VAL"},
				}},
			},
		}
		trBad := NewTemplatesRendererWithFetcher(cfg, badFetcher, NewRuleEngine(), NewEnvsBuilderWithFetcher(cfg, badFetcher, NewRuleEngine()))
		_, err = trBad.RenderDetailed(ctx, "dot")
		if err == nil {
			t.Fatal("expected error for invalid dotenv key containing newline/=")
		}
		if strings.Contains(err.Error(), "TOP_SECRET_VAL") {
			t.Fatalf("error leaked secret value: %v", err)
		}
	})

	t.Run("EvaluateQueryPredicate truthiness variants and parse errors", func(t *testing.T) {
		raw := map[string]any{"title": "T", "tags": []string{"a"}}
		cases := []struct {
			query   string
			want    bool
			wantErr bool
		}{
			{" true \n", true, false},
			{"false", false, false},
			{"", true, false},
			{"   ", true, false},
			{"{{ \"\" }}", false, false},
			{"1", false, false},
			{"{{ if }}", false, true},
		}
		for _, tc := range cases {
			got, err := EvaluateQueryPredicate(tc.query, raw)
			if tc.wantErr {
				if err == nil {
					t.Errorf("query %q: expected error", tc.query)
				}
				continue
			}
			if err != nil {
				t.Errorf("query %q: unexpected error: %v", tc.query, err)
			}
			if got != tc.want {
				t.Errorf("query %q: got %v, want %v", tc.query, got, tc.want)
			}
		}
	})

	t.Run("Title tie-break sorting when Path matches", func(t *testing.T) {
		fetcher := &stubVaultFetcher{
			vaults: map[string][]SourceEntry{
				"work": {
					{Vault: "work", Path: "same/path", Title: "Zeta", Raw: map[string]any{"title": "Zeta", "tags": []string{}, "KEY": "from-zeta"}},
					{Vault: "work", Path: "same/path", Title: "Alpha", Raw: map[string]any{"title": "Alpha", "tags": []string{}, "KEY": "from-alpha"}},
				},
			},
		}
		cfg := &config.Config{
			Envs: map[string][]config.VaultSourceConfig{"e": {{Vault: "work"}}},
		}
		res, err := NewEnvsBuilderWithFetcher(cfg, fetcher, NewRuleEngine()).BuildDetailed(ctx, "e")
		if err != nil {
			t.Fatalf("BuildDetailed failed: %v", err)
		}
		if len(res.Sources) != 2 || res.Sources[0].Title != "Alpha" || res.Sources[1].Title != "Zeta" {
			t.Fatalf("expected Alpha before Zeta, got %#v", res.Sources)
		}
		if res.Env["KEY"] != "from-zeta" {
			t.Fatalf("expected later Title (Zeta) to win collision, got %q", res.Env["KEY"])
		}
	})

	t.Run("inline template missingkey=error and undefined env/template/driver errors", func(t *testing.T) {
		fetcher := &stubVaultFetcher{
			vaults: map[string][]SourceEntry{
				"work": {{Vault: "work", Path: "a", Title: "A", Raw: map[string]any{"title": "A", "tags": []string{}, "K": "v"}}},
			},
		}
		cfg := &config.Config{
			Templates: map[string]config.TemplateConfig{
				"inline_missing": {
					Driver:   "template",
					Template: `{{ range .entries }}{{ .MISSING_KEY }}{{ end }}`,
					Vaults:   []config.VaultSourceConfig{{Vault: "work"}},
				},
				"bad_driver": {
					Driver: "xml",
					Vaults: []config.VaultSourceConfig{{Vault: "work"}},
				},
			},
		}
		eb := NewEnvsBuilderWithFetcher(cfg, fetcher, NewRuleEngine())
		tr := NewTemplatesRendererWithFetcher(cfg, fetcher, NewRuleEngine(), eb)

		if _, err := eb.Build(ctx, "does_not_exist"); err == nil {
			t.Error("expected error for undefined env")
		}
		if _, err := tr.Render(ctx, "does_not_exist"); err == nil {
			t.Error("expected error for undefined template")
		}
		if _, err := tr.Render(ctx, "inline_missing"); err == nil {
			t.Error("expected missingkey=error on inline template")
		}
		if _, err := tr.Render(ctx, "bad_driver"); err == nil {
			t.Error("expected error for unsupported driver")
		}
	})
}

func TestEnvsBuilderDeterministicSortUUIDTieBreaker(t *testing.T) {
	ctx := context.Background()
	fetcher := &stubVaultFetcher{
		vaults: map[string][]SourceEntry{
			"work": {
				{
					Vault: "work",
					Path:  "prod/db",
					Title: "db",
					Raw:   map[string]any{"title": "db", "path": "prod/db", "uuid": "uuid-b", "HOST": "second.example"},
				},
				{
					Vault: "work",
					Path:  "prod/db",
					Title: "db",
					Raw:   map[string]any{"title": "db", "path": "prod/db", "uuid": "uuid-a", "HOST": "first.example"},
				},
			},
		},
	}
	cfg := &config.Config{
		Envs: map[string][]config.VaultSourceConfig{
			"app": {{Vault: "work"}},
		},
	}
	builder := NewEnvsBuilderWithFetcher(cfg, fetcher, NewRuleEngine())
	env, err := builder.Build(ctx, "app")
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	// uuid-a sorts before uuid-b, so last-entry-wins resolves to uuid-b regardless of input slice order.
	if env["HOST"] != "second.example" {
		t.Fatalf("expected HOST=second.example (uuid-b wins after uuid-a), got %q", env["HOST"])
	}
}

func TestExamplesConfigCustomEnvFileRendersCleanly(t *testing.T) {
	examplesPath := filepath.Join("..", "..", "examples", "config.yaml")
	valRes, err := config.Validate(examplesPath, false)
	if err != nil {
		t.Fatalf("config.Validate(examples/config.yaml) error: %v", err)
	}
	for _, e := range valRes.Errors {
		if !strings.Contains(e, "vault_path does not exist") {
			t.Fatalf("examples/config.yaml has non-path validation error: %s (all: %v)", e, valRes.Errors)
		}
	}
	if len(valRes.Warnings) > 0 {
		t.Fatalf("examples/config.yaml produced unexpected validation warnings: %v", valRes.Warnings)
	}
	cfg, err := config.Load(examplesPath)
	if err != nil {
		t.Fatalf("config.Load(examples/config.yaml) failed: %v", err)
	}

	fetcher := &stubVaultFetcher{
		vaults: map[string][]SourceEntry{
			"work": {
				{
					Vault: "work",
					Path:  "apps/web",
					Title: "Web App",
					Raw: map[string]any{
						"title":    "Web App",
						"path":     "apps/web",
						"tags":     []string{"app"},
						"username": "svc-user",
						"password": "s3cr3t-value",
					},
				},
				{
					Vault: "work",
					Path:  "apps/worker",
					Title: "Worker",
					Raw: map[string]any{
						"title":    "Worker",
						"path":     "apps/worker",
						"tags":     []string{"app"},
						"password": "worker-secret",
					},
				},
			},
		},
	}

	re := NewRuleEngine()
	eb := NewEnvsBuilderWithFetcher(cfg, fetcher, re)
	tr := NewTemplatesRendererWithFetcher(cfg, fetcher, re, eb)

	res, err := tr.RenderDetailed(context.Background(), "custom_env_file")
	if err != nil {
		t.Fatalf("RenderDetailed(custom_env_file) failed: %v", err)
	}
	for _, want := range []string{
		"# Entry: Web App (apps/web)",
		"APP_USER=svc-user",
		"APP_PASSWORD=s3cr3t-value",
		"# Entry: Worker (apps/worker)",
		"APP_PASSWORD=worker-secret",
	} {
		if !strings.Contains(res.Content, want) {
			t.Errorf("rendered template missing %q:\n%s", want, res.Content)
		}
	}
}
