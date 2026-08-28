package engine

import (
	"testing"

	"github.com/warpcode/cloakenv/internal/config"
)

func BenchmarkMatchRunAlias(b *testing.B) {
	cfg := &config.Config{
		Autoload: []config.AutoloadRule{
			{
				Match:   "^litellm\\s+(.*)$",
				Command: "uvx --with 'litellm[proxy]' litellm \\1",
			},
			{
				Match:  "^aws\\s+(.*)$",
				Vaults: []string{"aws_prod"},
			},
			{
				Match:  "^kubectl-.*$",
				Vaults: []string{"k8s_prod"},
			},
			{
				Match:  "git push",
				Vaults: []string{"git_prod"},
			},
		},
	}
	cfg.CompileAutoloadRules()

	cmdArgs := []string{"kubectl-prod", "get", "pods"}

	b.ResetTimer()
	for range b.N {
		_, _, _ = MatchRunAlias(cfg, cmdArgs)
	}
}

func BenchmarkMatchCommandRule(b *testing.B) {
	rule := config.AutoloadRule{
		Match:   "^litellm\\s+(.*)$",
		Command: "uvx --with 'litellm[proxy]' litellm \\1",
	}
	rule.Compile()

	cmdArgs := []string{"litellm", "--config", "config.yaml"}

	b.ResetTimer()
	for range b.N {
		_, _, _ = MatchCommandRule(rule, cmdArgs)
	}
}

func BenchmarkMatchRunAlias_PureMatch(b *testing.B) {
	cfg := &config.Config{
		Autoload: []config.AutoloadRule{
			{
				Match:  "^litellm\\s+(.*)$",
				Vaults: []string{"work_vault"},
			},
			{
				Match:  "^aws\\s+(.*)$",
				Vaults: []string{"aws_prod"},
			},
			{
				Match:  "^kubectl-.*$",
				Vaults: []string{"k8s_prod"},
			},
			{
				Match:  "git push",
				Vaults: []string{"git_prod"},
			},
		},
	}
	cfg.CompileAutoloadRules()

	cmdArgs := []string{"kubectl-prod", "get", "pods"}

	b.ResetTimer()
	for range b.N {
		_, _, _ = MatchRunAlias(cfg, cmdArgs)
	}
}

func BenchmarkMatchCommandRule_PureMatch(b *testing.B) {
	rule := config.AutoloadRule{
		Match:  "^litellm\\s+(.*)$",
		Vaults: []string{"work_vault"},
	}
	rule.Compile()

	cmdArgs := []string{"litellm", "--config", "config.yaml"}

	b.ResetTimer()
	for range b.N {
		_, _, _ = MatchCommandRule(rule, cmdArgs)
	}
}
