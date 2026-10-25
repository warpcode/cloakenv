package engine_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/warpcode/cloakenv/internal/config"
	"github.com/warpcode/cloakenv/internal/engine"
)

func TestTemplateEngine_FuncMap(t *testing.T) {
	ctx := context.Background()

	tmpDir := t.TempDir()
	incFile := filepath.Join(tmpDir, "test.txt")
	os.WriteFile(incFile, []byte("file content: {{ .Attributes.k }}"), 0644)

	cfg := &config.Config{
		ConfigPath: filepath.Join(tmpDir, "config.yaml"),
		Templates: map[string]config.TemplateConfig{
			"my_template": {
				Template: "included template: {{ .Attributes.k }}",
			},
		},
		Vaults: map[string]config.VaultConfig{
			"vault_a": {
				Provider: "custom_vault",
				Entities: config.ToCustomEntityMap(map[string]map[string]any{
					"myentity": {
						"Password": "secret_pass",
						"k":        "v",
						"tags":     []any{"test"},
					},
					"deep": {
						"val": `{{ get "vault_a://deep2:val" }}`,
					},
					"deep2": {
						"val": `{{ get "vault_a://deep3:val" }}`,
					},
					"deep3": {
						"val": `{{ get "vault_a://deep4:val" }}`,
					},
					"deep4": {
						"val": `{{ get "vault_a://deep5:val" }}`,
					},
					"deep5": {
						"val": `{{ get "vault_a://deep6:val" }}`,
					},
					"deep6": {
						"val": `{{ get "vault_a://deep7:val" }}`,
					},
					"deep7": {
						"val": "bottom",
					},
				}),
				ResolveValues: true,
			},
		},
	}

	orch, err := engine.NewOrchestrator(cfg)
	if err != nil {
		t.Fatalf("failed to create orchestrator: %v", err)
	}

	te := engine.NewTemplateEngine(orch.GetResolver())

	tests := []struct {
		name    string
		tmpl    string
		data    any
		want    string
		wantErr bool
	}{
		{
			name: "get",
			tmpl: `{{ get "vault_a://myentity:Password" }}`,
			want: "secret_pass",
		},
		{
			name: "entry",
			tmpl: `{{ (entry "vault_a://myentity").Attributes.Password }}`,
			want: "secret_pass",
		},
		{
			name: "search",
			tmpl: `{{ (index (search "\"test\" in tags") 0).Attributes.Password }}`,
			want: "secret_pass",
		},
		{
			name: "toEnv",
			data: map[string]any{"a": "1", "b": 2, "Title": "ignore"},
			tmpl: `{{ toEnv . }}`,
			want: "a=1\nb=2",
		},
		{
			name: "toKv",
			data: map[string]any{"a": "1", "b": 2, "uuid": "ignore"},
			tmpl: `{{ toKv . }}`,
			want: "a=1\nb=2",
		},
		{
			name: "toJson",
			data: map[string]any{"a": "1"},
			tmpl: `{{ toJson . }}`,
			want: `{"a":"1"}`,
		},
		{
			name: "toYaml",
			data: map[string]any{"a": "1"},
			tmpl: `{{ toYaml . }}`,
			want: "a: \"1\"",
		},
		{
			name: "upper",
			tmpl: `{{ upper "hello" }}`,
			want: "HELLO",
		},
		{
			name: "lower",
			tmpl: `{{ lower "HELLO" }}`,
			want: "hello",
		},
		{
			name: "env",
			tmpl: `{{ env "my.key-name" }}`,
			want: "MY_KEY_NAME",
		},
		{
			name: "snake",
			tmpl: `{{ snake "MyKeyName" }}`,
			want: "mykeyname",
		},
		{
			name: "kebab",
			tmpl: `{{ kebab "MyKeyName" }}`,
			want: "mykeyname",
		},
		{
			name: "keys",
			data: map[string]any{"b": 2, "a": 1, "c": 3},
			tmpl: `{{ join "," (keys .) }}`,
			want: "a,b,c",
		},
		{
			name: "values",
			data: map[string]any{"b": 2, "a": 1, "c": 3},
			tmpl: `{{ join "," (values .) }}`,
			want: "1,2,3",
		},
		{
			name: "include",
			data: engine.TemplateContext{Attributes: map[string]any{"k": "v"}},
			tmpl: `{{ include "my_template" . }}`,
			want: "included template: v",
		},
		{
			name: "includeFile",
			data: engine.TemplateContext{Attributes: map[string]any{"k": "v"}},
			tmpl: `{{ includeFile "test.txt" . }}`,
			want: "file content: v",
		},
		{
			name:    "missingkey",
			data: map[string]any{},
			tmpl:    `{{ .missing }}`,
			wantErr: true,
		},
		{
			name:    "recursion depth",
			tmpl:    `{{ get \"vault_a://deep6:val\" }}`,
			data: map[string]any{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := te.Render(ctx, tt.tmpl, tt.data, 0, "")
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if got != tt.want {
					t.Errorf("got %q, want %q", got, tt.want)
				}
			}
		})
	}
}
