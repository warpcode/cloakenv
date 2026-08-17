package cmd

import (
	"encoding/json"
	"bytes"
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
				Vaults:  []string{"litellm_vault"},
				Env: map[string]string{
					"LITELLM_KEY": "keyring://litellm/master_key",
				},
			},
			{
				Match:  "aws",
				Vaults: []string{"aws_vault"},
			},
		},
	}

	captureStdout := func(fn func()) string {
		oldStdout := os.Stdout
		r, w, _ := os.Pipe()
		os.Stdout = w

		fn()

		w.Close()
		os.Stdout = oldStdout

		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		return strings.TrimSpace(buf.String())
	}

	t.Run("Matched command plain output", func(t *testing.T) {
		out := captureStdout(func() {
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
		out := captureStdout(func() {
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
		if len(res.Vaults) != 1 || res.Vaults[0] != "litellm_vault" {
			t.Errorf("expected vaults [litellm_vault], got %v", res.Vaults)
		}
		if res.Env["LITELLM_KEY"] != "keyring://litellm/master_key" {
			t.Errorf("expected LITELLM_KEY in env map, got %v", res.Env)
		}
	})

	t.Run("Unmatched command plain output", func(t *testing.T) {
		out := captureStdout(func() {
			exitCode := Internal([]string{"match-alias", "--", "helm", "status"}, cfg)
			if exitCode != 1 {
				t.Errorf("expected exit code 1 for no match, got %d", exitCode)
			}
		})
		if out != "false" {
			t.Errorf("expected output 'false', got %q", out)
		}
	})

	t.Run("Unmatched command JSON output", func(t *testing.T) {
		out := captureStdout(func() {
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
		out := captureStdout(func() {
			exitCode := Internal([]string{"match-alias", "--help"}, cfg)
			if exitCode != 0 {
				t.Errorf("expected exit code 0 for help, got %d", exitCode)
			}
		})
		if !strings.Contains(out, "Usage:") {
			t.Errorf("expected help text containing 'Usage:', got %q", out)
		}
	})

	t.Run("Unknown internal subcommand", func(t *testing.T) {
		exitCode := Internal([]string{"unknown-cmd"}, cfg)
		if exitCode != 1 {
			t.Errorf("expected exit code 1 for unknown subcommand, got %d", exitCode)
		}
	})
}
