package engine_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
			name: "shquote",
			tmpl: `{{ shquote "foo bar" }}`,
			want: "'foo bar'",
		},
		{
			name: "shellQuote_alias",
			tmpl: `{{ shellQuote "foo'bar" }}`,
			want: `'foo'\''bar'`,
		},
		{
			name: "exec_echo",
			tmpl: `{{ exec "echo" "hello" "world" }}`,
			want: "hello world",
		},
		{
			name: "exec_with_nested_get",
			tmpl: `{{ exec "echo" "-n" (get "vault_a://myentity:Password") }}`,
			want: "secret_pass",
		},
		{
			name: "exec_piped_with_fromJSON",
			tmpl: `{{ (exec "echo" "{\"token\":\"tok_from_exec\"}" | fromJSON).token }}`,
			want: "tok_from_exec",
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
			name: "tryGet_existing",
			tmpl: `{{ tryGet "vault_a://myentity:Password" }}`,
			want: "secret_pass",
		},
		{
			name: "tryGet_missing",
			tmpl: `{{ tryGet "vault_a://missing:Password" }}`,
			want: "",
		},
		{
			name: "tryGet_invalid_scheme",
			tmpl: `{{ tryGet "nonexistent_scheme://foo" }}`,
			want: "",
		},
		{
			name: "tryGet_variadic_fallback_first_match",
			tmpl: `{{ tryGet "vault_a://missing:val" "vault_a://myentity:Password" "vault_a://myentity:k" }}`,
			want: "secret_pass",
		},
		{
			name: "tryGet_variadic_all_missing",
			tmpl: `{{ tryGet "vault_a://missing:a" "vault_a://missing:b" }}`,
			want: "",
		},
		{
			name: "tryGet_empty_args",
			tmpl: `{{ tryGet }}`,
			want: "",
		},
		{
			name: "tryGet_depth_limit",
			tmpl: `{{ tryGet "vault_a://deep:val" }}`,
			want: "",
		},
		{
			name: "default_pipeline_with_tryGet_fallback",
			tmpl: `{{ tryGet "vault_a://missing:Password" | default "default_secret" }}`,
			want: "default_secret",
		},
		{
			name: "default_pipeline_with_tryGet_found",
			tmpl: `{{ tryGet "vault_a://myentity:Password" | default "default_secret" }}`,
			want: "secret_pass",
		},
		{
			name: "default_direct_invocation_empty",
			tmpl: `{{ default "my_default" "" }}`,
			want: "my_default",
		},
		{
			name: "default_direct_invocation_nonempty",
			tmpl: `{{ default "my_default" "given_val" }}`,
			want: "given_val",
		},
		{
			name: "default_types_empty_int_and_nil",
			tmpl: `{{ default "def" 0 }} / {{ default "def" nil }}`,
			want: "def / def",
		},
		{
			name: "coalesce_first_nonempty",
			tmpl: `{{ coalesce "" (tryGet "vault_a://missing:k") "first_valid" "second" }}`,
			want: "first_valid",
		},
		{
			name: "coalesce_pipeline",
			tmpl: `{{ "pipeline_val" | coalesce }}`,
			want: "pipeline_val",
		},
		{
			name: "coalesce_all_empty",
			tmpl: `{{ coalesce "" nil "" }}`,
			want: "",
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

func TestTemplateEngine_ExecComprehensive(t *testing.T) {
	ctx := context.Background()
	te := engine.NewTemplateEngine(nil)

	t.Run("preserves adversarial metacharacters without shell splitting or injection", func(t *testing.T) {
		// Pass characters like ;, |, &, $(), `, quotes, spaces into args
		adversarialArg := `hello; rm -rf / | cat && $(whoami) ` + "`id`" + ` 'single' "double"`
		tmpl := `{{ exec "sh" "-c" "for a in \"$@\"; do echo \"ARG:$a\"; done" "_" .arg }}`
		data := map[string]any{"arg": adversarialArg}
		got, err := te.Render(ctx, tmpl, data, 0, "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := "ARG:" + adversarialArg
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("trims trailing CRLF and LF newlines", func(t *testing.T) {
		tmpl := `{{ exec "printf" "foo\r\n\r\n" }}`
		got, err := te.Render(ctx, tmpl, nil, 0, "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "foo" {
			t.Errorf("got %q, want %q", got, "foo")
		}
	})

	t.Run("empty executable name error", func(t *testing.T) {
		tmpl := `{{ exec "" }}`
		_, err := te.Render(ctx, tmpl, nil, 0, "")
		if err == nil {
			t.Fatalf("expected error for empty exec name, got nil")
		}
	})

	t.Run("command not found error", func(t *testing.T) {
		tmpl := `{{ exec "non_existent_binary_xyz_123" }}`
		_, err := te.Render(ctx, tmpl, nil, 0, "")
		if err == nil {
			t.Fatalf("expected error for missing command, got nil")
		}
	})

	t.Run("command exit failure without leaking secret arguments in error", func(t *testing.T) {
		secretVal := "super_secret_token_12345"
		// Run sh -c 'exit 2' with the secret as argument
		tmpl := fmt.Sprintf(`{{ exec "sh" "-c" "echo standard_err_msg >&2; exit 2" "%s" }}`, secretVal)
		_, err := te.Render(ctx, tmpl, nil, 0, "")
		if err == nil {
			t.Fatalf("expected error on non-zero exit, got nil")
		}
		errMsg := err.Error()
		if strings.Contains(errMsg, secretVal) {
			t.Errorf("error message leaked secret argument: %s", errMsg)
		}
		if !strings.Contains(errMsg, "standard_err_msg") {
			t.Errorf("expected stderr message in error, got: %s", errMsg)
		}
	})

	t.Run("context cancellation terminates command", func(t *testing.T) {
		cancelledCtx, cancel := context.WithCancel(ctx)
		cancel() // immediately cancelled

		tmpl := `{{ exec "sleep" "5" }}`
		_, err := te.Render(cancelledCtx, tmpl, nil, 0, "")
		if err == nil {
			t.Fatalf("expected error on cancelled context, got nil")
		}
	})

	t.Run("exec.ErrDot relative path execution blocked", func(t *testing.T) {
		dir := t.TempDir()
		cmdName := "relative-binary-test"
		binPath := filepath.Join(dir, cmdName)
		if err := os.WriteFile(binPath, []byte("#!/bin/sh\necho relative_ran\n"), 0755); err != nil {
			t.Fatalf("failed to create relative executable: %v", err)
		}

		t.Chdir(dir)
		t.Setenv("PATH", "."+string(os.PathListSeparator)+os.Getenv("PATH"))

		tmpl := fmt.Sprintf(`{{ exec %q }}`, cmdName)
		_, err := te.Render(ctx, tmpl, nil, 0, "")
		if err == nil {
			t.Fatalf("expected error for relative PATH execution, got nil")
		}
		if !strings.Contains(err.Error(), "blocked due to security risks") {
			t.Errorf("expected security risk error message, got: %v", err)
		}
	})
}

func TestTemplateEngine_Shquote(t *testing.T) {
	ctx := context.Background()
	te := engine.NewTemplateEngine(nil)

	tests := []struct {
		name string
		tmpl string
		want string
	}{
		{
			name: "empty string",
			tmpl: `{{ shquote "" }}`,
			want: "''",
		},
		{
			name: "simple string",
			tmpl: `{{ shquote "hello" }}`,
			want: "'hello'",
		},
		{
			name: "string with spaces and metacharacters",
			tmpl: `{{ shquote "hello world; rm -rf /" }}`,
			want: "'hello world; rm -rf /'",
		},
		{
			name: "string with single quotes",
			tmpl: `{{ shquote "don't do that" }}`,
			want: `'don'\''t do that'`,
		},
		{
			name: "shellQuote alias",
			tmpl: `{{ shellQuote "it's 'quoted'" }}`,
			want: `'it'\''s '\''quoted'\'''`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := te.Render(ctx, tt.tmpl, nil, 0, "")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
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
