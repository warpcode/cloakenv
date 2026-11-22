package cmd

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/warpcode/cloakenv/internal/config"
)

func TestInternalMatchAlias(t *testing.T) {
	cfg := &config.Config{
		Autoload: []config.AutoloadRule{
			{
				Match:   "^litellm\\s+(.*)$",
				Command: "uvx --with 'litellm[proxy]' litellm \\1",
				Envs: []config.AutoloadEnvItem{
					config.NamedEnvItem("litellm_env"),
					config.InlineEnvItem(map[string]string{"LOG_LEVEL": "INFO"}),
				},
				Templates: []config.AutoloadTemplateConfig{
					{Template: "litellm_cfg", EnvPath: "LITELLM_CONFIG_PATH"},
				},
			},
			{
				Match: "aws",
				Envs:  []config.AutoloadEnvItem{config.NamedEnvItem("aws_env")},
			},
		},
	}
	cfg.CompileAutoloadRules()

	captureStdout := func(t *testing.T, fn func()) string {
		t.Helper()
		oldStdout := os.Stdout
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatalf("failed to create stdout pipe: %v", err)
		}
		defer func() { _ = r.Close() }()
		defer func() { _ = w.Close() }()

		os.Stdout = w
		defer func() {
			os.Stdout = oldStdout
		}()

		fn()

		_ = w.Close()
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		return strings.TrimSpace(buf.String())
	}

	t.Run("Matched command plain output", func(t *testing.T) {
		out := captureStdout(t, func() {
			exitCode := Internal([]string{"match-alias", "--", "aws", "s3", "ls"}, cfg)
			if exitCode != 0 {
				t.Errorf("expected exit code 0, got %d", exitCode)
			}
		})
		if out != "aws" {
			t.Errorf("expected output 'aws', got %q", out)
		}
	})

	t.Run("Matched command JSON output", func(t *testing.T) {
		out := captureStdout(t, func() {
			exitCode := Internal([]string{"match-alias", "--json", "--", "litellm", "--config", "config.yaml"}, cfg)
			if exitCode != 0 {
				t.Errorf("expected exit code 0, got %d", exitCode)
			}
		})

		var res MatchAliasResult
		if err := json.Unmarshal([]byte(out), &res); err != nil {
			t.Fatalf("failed to unmarshal JSON output %q: %v", out, err)
		}

		if !res.Matched {
			t.Errorf("expected matched=true in JSON output")
		}
		if res.Match != "^litellm\\s+(.*)$" {
			t.Errorf("expected match '^litellm\\s+(.*)$', got %q", res.Match)
		}
		if res.Command != "uvx --with 'litellm[proxy]' litellm \\1" {
			t.Errorf("expected command replacement template, got %q", res.Command)
		}
		if len(res.Envs) != 2 || res.Envs[0].Name != "litellm_env" || res.Envs[1].Inline["LOG_LEVEL"] != "INFO" {
			t.Errorf("unexpected Envs in JSON result: %+v", res.Envs)
		}
		if len(res.Templates) != 1 || res.Templates[0].Template != "litellm_cfg" {
			t.Errorf("unexpected Templates in JSON result: %+v", res.Templates)
		}
	})

	t.Run("Unmatched command plain output", func(t *testing.T) {
		out := captureStdout(t, func() {
			exitCode := Internal([]string{"match-alias", "--", "helm", "status"}, cfg)
			if exitCode != 1 {
				t.Errorf("expected exit code 1 for no match, got %d", exitCode)
			}
		})
		if out != "" {
			t.Errorf("expected empty output, got %q", out)
		}
	})

	t.Run("Empty args plain output", func(t *testing.T) {
		out := captureStdout(t, func() {
			exitCode := Internal([]string{"match-alias", "--"}, cfg)
			if exitCode != 1 {
				t.Errorf("expected exit code 1 for empty args, got %d", exitCode)
			}
		})
		if out != "" {
			t.Errorf("expected empty output, got %q", out)
		}
	})

	t.Run("Unmatched command JSON output", func(t *testing.T) {
		out := captureStdout(t, func() {
			exitCode := Internal([]string{"match-alias", "--json", "--", "helm", "status"}, cfg)
			if exitCode != 1 {
				t.Errorf("expected exit code 1 for no match, got %d", exitCode)
			}
		})

		var res MatchAliasResult
		if err := json.Unmarshal([]byte(out), &res); err != nil {
			t.Fatalf("failed to unmarshal JSON output %q: %v", out, err)
		}

		if res.Matched {
			t.Errorf("expected matched=false in JSON output")
		}
	})

	t.Run("Help flag", func(t *testing.T) {
		out := captureStdout(t, func() {
			exitCode := Internal([]string{"match-alias", "--help"}, cfg)
			if exitCode != 0 {
				t.Errorf("expected exit code 0 for help, got %d", exitCode)
			}
		})
		if !strings.Contains(out, "Usage:") {
			t.Errorf("expected help text containing 'Usage:', got %q", out)
		}
	})

	t.Run("Internal help flag --help", func(t *testing.T) {
		out := captureStdout(t, func() {
			exitCode := Internal([]string{"--help"}, cfg)
			if exitCode != 0 {
				t.Errorf("expected exit code 0 for --help, got %d", exitCode)
			}
		})
		if !strings.Contains(out, "Usage:\n  cloakenv internal <subcommand> [args]") {
			t.Errorf("expected usage output, got %q", out)
		}
		if !strings.Contains(out, "match-alias") {
			t.Errorf("expected output to mention match-alias, got %q", out)
		}
	})

	t.Run("Internal help flag -h", func(t *testing.T) {
		out := captureStdout(t, func() {
			exitCode := Internal([]string{"-h"}, cfg)
			if exitCode != 0 {
				t.Errorf("expected exit code 0 for -h, got %d", exitCode)
			}
		})
		if !strings.Contains(out, "Usage:\n  cloakenv internal <subcommand> [args]") {
			t.Errorf("expected usage output, got %q", out)
		}
	})

	t.Run("Unknown internal subcommand", func(t *testing.T) {
		exitCode := Internal([]string{"unknown-cmd"}, cfg)
		if exitCode != 1 {
			t.Errorf("expected exit code 1 for unknown subcommand, got %d", exitCode)
		}
	})
}
