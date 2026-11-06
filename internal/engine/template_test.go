package engine_test

import (
	"context"
	"fmt"
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
	if err := os.WriteFile(incFile, []byte("file content: {{ .Attributes.k }}"), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		ConfigPath: filepath.Join(tmpDir, "config.yaml"),
		Templates: map[string]config.TemplateConfig{
			"my_template": {
				Template: "included template: {{ .Attributes.k }}",
			},
			"my_path_template": {
				Path: "test.txt",
			},
		},
		Vaults: map[string]config.VaultConfig{
			"vault_a": {
				Provider: "custom_vault",
				Entities: config.ToCustomEntityMap(map[string]map[string]any{
					"myentity": {
						"Password":     "secret_pass",
						"k":            "v",
						"tags":         []any{"test"},
						"json_payload": `{"token":"tok_secret_123"}`,
						"yaml_payload": "api_key: key_xyz_789\nenv: prod",
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
			name: "toJSON",
			data: map[string]any{"a": "1"},
			tmpl: `{{ toJSON . }}`,
			want: `{"a":"1"}`,
		},
		{
			name: "json",
			data: map[string]any{"a": "1"},
			tmpl: `{{ json . }}`,
			want: `{"a":"1"}`,
		},
		{
			name: "toYaml",
			data: map[string]any{"a": "1"},
			tmpl: `{{ toYaml . }}`,
			want: "a: \"1\"",
		},
		{
			name: "toYAML",
			data: map[string]any{"a": "1"},
			tmpl: `{{ toYAML . }}`,
			want: "a: \"1\"",
		},
		{
			name: "yaml",
			data: map[string]any{"a": "1"},
			tmpl: `{{ yaml . }}`,
			want: "a: \"1\"",
		},
		{
			name: "fromJSON object field extraction",
			tmpl: `{{ (fromJSON "{\"login\":\"octocat\"}").login }}`,
			want: "octocat",
		},
		{
			name: "fromJson alias object field extraction",
			tmpl: `{{ (fromJson "{\"login\":\"octocat\"}").login }}`,
			want: "octocat",
		},
		{
			name: "fromJSON array indexing",
			tmpl: `{{ index (fromJSON "[\"first\",\"second\"]") 1 }}`,
			want: "second",
		},
		{
			name: "fromJSON nested map and slice navigation",
			tmpl: `{{ (index (fromJSON "{\"users\":[{\"id\":42}]}").users 0).id }}`,
			want: "42",
		},
		{
			name: "fromYAML object field extraction",
			tmpl: `{{ (fromYAML "access_key_id: AKIA123").access_key_id }}`,
			want: "AKIA123",
		},
		{
			name: "fromYaml alias object field extraction",
			tmpl: `{{ (fromYaml "access_key_id: AKIA123").access_key_id }}`,
			want: "AKIA123",
		},
		{
			name: "fromYAML array indexing",
			tmpl: `{{ index (fromYAML "- apple\n- banana") 0 }}`,
			want: "apple",
		},
		{
			name: "fromYAML nested map and slice navigation",
			tmpl: `{{ (index (fromYAML "services:\n  - name: api\n    port: 8080").services 0).port }}`,
			want: "8080",
		},
		{
			name: "pipeline get fromJSON",
			tmpl: `{{ (get "vault_a://myentity:json_payload" | fromJSON).token }}`,
			want: "tok_secret_123",
		},
		{
			name: "pipeline get fromYAML",
			tmpl: `{{ (get "vault_a://myentity:yaml_payload" | fromYAML).api_key }}`,
			want: "key_xyz_789",
		},
		{
			name: "pipeline simulated cli output fromJSON",
			data: map[string]any{
				"cli_output": `{"session":{"token":"sess_abc","expires_in":3600}}`,
			},
			tmpl: `{{ (.cli_output | fromJSON).session.token }}`,
			want: "sess_abc",
		},
		{
			name:    "fromJSON malformed",
			tmpl:    `{{ fromJSON "{invalid-json" }}`,
			wantErr: true,
		},
		{
			name:    "fromYAML malformed",
			tmpl:    `{{ fromYAML "invalid: yaml: :" }}`,
			wantErr: true,
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
			name: "include_path_template",
			data: engine.TemplateContext{Attributes: map[string]any{"k": "v"}},
			tmpl: `{{ include "my_path_template" . }}`,
			want: "file content: v",
		},
		{
			name:    "missingkey",
			data:    map[string]any{},
			tmpl:    `{{ .missing }}`,
			wantErr: true,
		},
		{
			name:    "recursion depth",
			tmpl:    `{{ get "vault_a://deep:val" }}`,
			data:    map[string]any{},
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

func TestTemplateEngine_LowercaseDotMetaAndAttributes(t *testing.T) {
	ctx := context.Background()
	te := engine.NewTemplateEngine(nil)

	tmplCtx := engine.TemplateContext{
		Meta: map[string]any{
			"Title": "My Title",
			"Group": map[string]any{
				"Name": "My Group",
			},
		},
		Attributes: map[string]any{
			"key": "val",
		},
	}

	tmpl := `{{ .meta.title }} / {{ .meta.group.name }} = {{ index .attributes "key" }}`
	got, err := te.Render(ctx, tmpl, tmplCtx, 0, "")
	if err != nil {
		t.Fatalf("unexpected render error: %v", err)
	}
	want := "My Title / My Group = val"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestTemplateEngine_RenderBufferPool(t *testing.T) {
	ctx := context.Background()
	te := engine.NewTemplateEngine(nil)

	// Verify sequential calls with differing output sizes do not bleed state.
	for i := range 50 {
		data := map[string]any{"val": i}
		got, err := te.Render(ctx, `value={{ .val }}`, data, 0, "")
		if err != nil {
			t.Fatalf("unexpected error at iteration %d: %v", i, err)
		}
		want := "value=" + string(rune('0'+i%10))
		if i >= 10 {
			want = "value=" + string([]byte{byte('0' + i/10), byte('0' + i%10)})
		}
		if got != want {
			t.Errorf("iteration %d: got %q, want %q", i, got, want)
		}
	}

	// Verify concurrent calls do not race or corrupt data.
	t.Run("concurrent renders", func(t *testing.T) {
		t.Parallel()
		const goroutines = 20
		const iters = 50
		errCh := make(chan error, goroutines)

		for g := range goroutines {
			go func(gid int) {
				var workerErr error
				for iterIndex := range iters {
					data := map[string]any{"id": gid, "iter": iterIndex}
					res, err := te.Render(ctx, `id={{ .id }}-iter={{ .iter }}`, data, 0, "")
					if err != nil {
						workerErr = err
						break
					}
					want := fmt.Sprintf("id=%d-iter=%d", gid, iterIndex)
					if res != want {
						workerErr = fmt.Errorf("unexpected output format: %q", res)
						break
					}
				}
				errCh <- workerErr
			}(g)
		}

		for range goroutines {
			if err := <-errCh; err != nil {
				t.Fatalf("concurrent render failed: %v", err)
			}
		}
	})
}

func BenchmarkTemplateEngine_Render(b *testing.B) {
	ctx := context.Background()
	te := engine.NewTemplateEngine(nil)
	data := engine.TemplateContext{
		Meta: map[string]any{
			"Title": "Benchmark Title",
		},
		Attributes: map[string]any{
			"Username": "admin",
			"Password": "supersecretpassword",
		},
	}
	tmpl := `user={{ index .Attributes "Username" }} pass={{ index .Attributes "Password" }} title={{ .Meta.Title }}`

	b.ResetTimer()
	b.ReportAllocs()
	for range b.N {
		_, err := te.Render(ctx, tmpl, data, 0, "")
		if err != nil {
			b.Fatalf("render failed: %v", err)
		}
	}
}
