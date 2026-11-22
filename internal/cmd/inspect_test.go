package cmd

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/warpcode/cloakenv/internal/config"
	"github.com/warpcode/cloakenv/internal/runner"
)

func buildInspectTestConfig(t *testing.T) *config.Config {
	t.Helper()
	dbPath := writeTestKDBX(t, "secrets", "testpass", testKDBXEntry{
		Title: "OpenAI Prod",
		Tags:  "litellm,prod",
		Attributes: map[string]string{
			"api_key": "sk-super-secret-openai-99999",
			"org_id":  "org-secret-12345",
		},
	})
	cfg := &config.Config{
		Vaults: map[string]config.VaultConfig{
			"secrets": {
				Provider:  "keepass",
				VaultPath: dbPath,
			},
		},
		Envs: map[string][]config.VaultSourceConfig{
			"ai_env": {
				{
					Vault: "secrets",
					Query: `{{ hasTag "litellm" .tags }}`,
					Rules: []config.RuleConfig{{Type: "pass"}},
				},
			},
		},
		Templates: map[string]config.TemplateConfig{
			"ai_cfg": {
				Driver: "yaml",
				Vaults: []config.VaultSourceConfig{
					{
						Vault: "secrets",
						Query: `{{ hasTag "litellm" .tags }}`,
						Rules: []config.RuleConfig{{Type: "pass"}},
					},
				},
			},
		},
		Autoload: []config.AutoloadRule{
			{
				Match:   `^litellm\s+(.*)$`,
				Command: "litellm-bin $1",
				Envs: []config.AutoloadEnvItem{
					config.NamedEnvItem("ai_env"),
					config.InlineEnvItem(map[string]string{
						"APP_STAGE":  "staging",
						"INLINE_KEY": "inline-secret-value-8888",
					}),
				},
				Templates: []config.AutoloadTemplateConfig{
					{
						Template: "ai_cfg",
						EnvPath:  "LITELLM_CONFIG_PATH",
						Path:     "/tmp/litellm-{{ .APP_STAGE }}.yaml",
					},
				},
			},
		},
	}
	cfg.CompileAutoloadRules()
	return cfg
}

func TestInspect_RedactionAndModes(t *testing.T) {
	cfg := buildInspectTestConfig(t)
	allSecrets := []string{
		"sk-super-secret-openai-99999",
		"org-secret-12345",
		"inline-secret-value-8888",
		"staging",
	}

	assertNoLeaks := func(t *testing.T, out string) {
		t.Helper()
		for _, sec := range allSecrets {
			if strings.Contains(out, sec) {
				t.Fatalf("SECURITY LEAK: inspect output contained plaintext secret %q:\n%s", sec, out)
			}
		}
	}

	t.Run("overview text", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		if code := Inspect([]string{}, cfg, &stdout, &stderr); code != 0 {
			t.Fatalf("Inspect overview failed: %d stderr=%s", code, stderr.String())
		}
		out := stdout.String()
		assertNoLeaks(t, out)
		for _, want := range []string{"Vaults (1): secrets", "Envs (1): ai_env", "Templates (1): ai_cfg"} {
			if !strings.Contains(out, want) {
				t.Errorf("expected %q in output:\n%s", want, out)
			}
		}
	})

	t.Run("inspect env text and json", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		if code := Inspect([]string{"env", "ai_env"}, cfg, &stdout, &stderr); code != 0 {
			t.Fatalf("Inspect env failed: %d stderr=%s", code, stderr.String())
		}
		out := stdout.String()
		assertNoLeaks(t, out)
		for _, want := range []string{"OpenAI Prod", "litellm", "API_KEY=" + runner.RedactedPlaceholder, "ORG_ID=" + runner.RedactedPlaceholder} {
			if !strings.Contains(out, want) {
				t.Errorf("expected %q in output:\n%s", want, out)
			}
		}

		stdout.Reset()
		stderr.Reset()
		if code := Inspect([]string{"env", "ai_env", "-o", "json"}, cfg, &stdout, &stderr); code != 0 {
			t.Fatalf("Inspect env json failed: %d stderr=%s", code, stderr.String())
		}
		assertNoLeaks(t, stdout.String())
		var rep InspectEnvReport
		if err := json.Unmarshal(stdout.Bytes(), &rep); err != nil {
			t.Fatalf("invalid json: %v\n%s", err, stdout.String())
		}
		if rep.Env["API_KEY"] != runner.RedactedPlaceholder || rep.Env["ORG_ID"] != runner.RedactedPlaceholder {
			t.Errorf("unexpected redacted env map: %+v", rep.Env)
		}
	})

	t.Run("inspect template yaml", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		if code := Inspect([]string{"template", "ai_cfg", "-o", "yaml"}, cfg, &stdout, &stderr); code != 0 {
			t.Fatalf("Inspect template failed: %d stderr=%s", code, stderr.String())
		}
		out := stdout.String()
		assertNoLeaks(t, out)
		for _, want := range []string{"driver: yaml", "OpenAI Prod", runner.RedactedPlaceholder} {
			if !strings.Contains(out, want) {
				t.Errorf("expected %q in output:\n%s", want, out)
			}
		}
	})

	t.Run("inspect run json", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		if code := Inspect([]string{"run", "-o", "json", "--", "litellm", "--port", "4000"}, cfg, &stdout, &stderr); code != 0 {
			t.Fatalf("Inspect run failed: %d stderr=%s", code, stderr.String())
		}
		assertNoLeaks(t, stdout.String())
		var rep InspectRunReport
		if err := json.Unmarshal(stdout.Bytes(), &rep); err != nil {
			t.Fatalf("invalid json: %v\n%s", err, stdout.String())
		}
		if strings.Join(rep.Command, " ") != "litellm-bin --port 4000" {
			t.Errorf("Command = %v, want [litellm-bin --port 4000]", rep.Command)
		}
		if rep.Env["API_KEY"] != runner.RedactedPlaceholder || rep.Env["INLINE_KEY"] != runner.RedactedPlaceholder {
			t.Errorf("unexpected env redaction: %+v", rep.Env)
		}
		if len(rep.Templates) != 1 || rep.Templates[0].EnvPath != "LITELLM_CONFIG_PATH" || rep.Templates[0].Driver != "yaml" {
			t.Errorf("unexpected template delivery report: %+v", rep.Templates)
		}
		wantPath := "/tmp/litellm-" + runner.RedactedPlaceholder + ".yaml"
		if rep.Templates[0].Path != wantPath {
			t.Errorf("Template Path = %q, want %q", rep.Templates[0].Path, wantPath)
		}
	})

	t.Run("inspect -o json run without -- preserves child flags", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		if code := Inspect([]string{"-o", "json", "run", "litellm", "--port", "4000"}, cfg, &stdout, &stderr); code != 0 {
			t.Fatalf("Inspect -o json run failed: %d stderr=%s", code, stderr.String())
		}
		assertNoLeaks(t, stdout.String())
		var rep InspectRunReport
		if err := json.Unmarshal(stdout.Bytes(), &rep); err != nil {
			t.Fatalf("invalid json: %v\n%s", err, stdout.String())
		}
		if strings.Join(rep.Command, " ") != "litellm-bin --port 4000" {
			t.Errorf("Command = %v, want [litellm-bin --port 4000]", rep.Command)
		}
	})

	t.Run("shorthand target resolves uniquely", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		if code := Inspect([]string{"ai_env"}, cfg, &stdout, &stderr); code != 0 {
			t.Fatalf("Inspect shorthand failed: %d stderr=%s", code, stderr.String())
		}
		assertNoLeaks(t, stdout.String())
		if !strings.Contains(stdout.String(), "Env: ai_env") {
			t.Errorf("expected Env: ai_env, got:\n%s", stdout.String())
		}
	})

	t.Run("rejects --raw flag unconditionally", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		if code := Inspect([]string{"--raw", "env", "ai_env"}, cfg, &stdout, &stderr); code == 0 {
			t.Fatalf("expected non-zero exit for --raw flag, got 0 (stdout=%s)", stdout.String())
		}
		if !strings.Contains(stderr.String(), "unknown flag: --raw") {
			t.Errorf("expected unknown flag error for --raw, got stderr=%q", stderr.String())
		}
	})

	t.Run("help flag prints usage", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		if code := Inspect([]string{"--help"}, cfg, &stdout, &stderr); code != 0 {
			t.Fatalf("Inspect --help exit = %d, want 0", code)
		}
		if !strings.Contains(stdout.String(), "cloakenv inspect") || !strings.Contains(stdout.String(), "<REDACTED>") {
			t.Errorf("unexpected inspect help output:\n%s", stdout.String())
		}
	})
}

func TestInspectRunRedactsShortAndTransformedSecretsInPath(t *testing.T) {
	t.Setenv("TINY_SECRET", "xy")
	t.Setenv("LONG_SECRET", "s3cr3tTokenVal")

	cfg := &config.Config{
		Envs: map[string][]config.VaultSourceConfig{
			"app": {
				{
					Vault: "env",
					Rules: []config.RuleConfig{
						{Type: "pass", Include: []string{"TINY_SECRET", "LONG_SECRET"}},
					},
				},
			},
		},
		Templates: map[string]config.TemplateConfig{
			"out": {
				Driver: "json",
				Vaults: []config.VaultSourceConfig{
					{
						Vault: "env",
						Rules: []config.RuleConfig{
							{Type: "pass", Include: []string{"TINY_SECRET", "LONG_SECRET"}},
						},
					},
				},
			},
		},
		Autoload: []config.AutoloadRule{
			{
				Match: "mytool",
				Envs:  []config.AutoloadEnvItem{{Name: "app"}},
				Templates: []config.AutoloadTemplateConfig{
					{
						Template: "out",
						Path:     `/tmp/{{ .TINY_SECRET }}-{{ upper .LONG_SECRET }}-{{ printf "%.6s" .LONG_SECRET }}.json`,
					},
				},
			},
		},
	}

	for _, format := range []string{"text", "json", "yaml"} {
		t.Run(format, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := Inspect([]string{"run", "-o", format, "--", "mytool"}, cfg, &stdout, &stderr)
			if code != 0 {
				t.Fatalf("Inspect failed (code=%d): %s", code, stderr.String())
			}
			out := stdout.String()
			for _, forbidden := range []string{"xy", "s3cr3t", "S3CR3T", "TokenVal", "TOKENVAL"} {
				if strings.Contains(out, forbidden) {
					t.Errorf("format=%s leaked secret fragment %q in output:\n%s", format, forbidden, out)
				}
			}
			if !strings.Contains(out, runner.RedactedPlaceholder) {
				t.Errorf("format=%s expected %q in output:\n%s", format, runner.RedactedPlaceholder, out)
			}
		})
	}
}
