package engine

import (
	"context"
	"fmt"
	"testing"

	"github.com/warpcode/cloakenv/internal/config"
	"github.com/warpcode/cloakenv/internal/provider"
)

type dummyResolver struct{}

func (d *dummyResolver) Resolve(ctx context.Context, uri string) (string, error) {
	return uri, nil
}
func (d *dummyResolver) ResolveWithKey(ctx context.Context, uri string, configKey string) (string, error) {
	return uri, nil
}
func (d *dummyResolver) GetEntry(ctx context.Context, uri string) (provider.Entry, error) {
	return provider.Entry{Attributes: map[string]any{}}, nil
}

func BenchmarkMatchRunAlias(b *testing.B) {
	rules := make([]config.AutoloadRule, 50)
	for i := range 50 {
		rules[i] = config.AutoloadRule{
			Match: fmt.Sprintf("tool-%d", i),
			Env:   map[string]string{"FOO": "bar"},
		}
	}
	// Target command that matches the last rule
	rules[49].Match = "target-cmd"

	cfg := &config.Config{
		Autoload: rules,
	}
	cfg.CompileAutoloadRules()
	cmdArgs := []string{"target-cmd", "arg1", "arg2", "arg3"}

	b.ResetTimer()
	for range b.N {
		_, _, err := MatchRunAlias(cfg, cmdArgs)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkBuildEnvForCommand_Autoload(b *testing.B) {
	rules := make([]config.AutoloadRule, 50)
	for i := range 50 {
		rules[i] = config.AutoloadRule{
			Match: fmt.Sprintf("tool-%d", i),
			Env:   map[string]string{"FOO": "bar"},
		}
	}
	// Last rule matches
	rules[49].Match = "target-cmd"

	cfg := &config.Config{
		Autoload: rules,
	}
	cfg.CompileAutoloadRules()
	eb := NewEnvBuilder(cfg, &dummyResolver{})
	cmdArgs := []string{"target-cmd", "arg1", "arg2", "arg3"}
	ctx := context.Background()

	b.ResetTimer()
	for range b.N {
		_, _, err := eb.BuildEnvForCommand(ctx, EnvConfig{CmdArgs: cmdArgs, EmptyEnv: true})
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMatchRunAlias_Regex(b *testing.B) {
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
