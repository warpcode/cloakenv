package engine

import (
	"context"
	"fmt"
	"testing"

	"github.com/warpcode/cloakenv/internal/config"
)

func BenchmarkMatchRunAlias(b *testing.B) {
	rules := make([]config.AutoloadRule, 50)
	for i := range 50 {
		rules[i] = config.AutoloadRule{
			Match: fmt.Sprintf("tool-%d", i),
			Envs:  []config.AutoloadEnvItem{config.InlineEnvItem(map[string]string{"FOO": "bar"})},
		}
	}
	rules[49].Match = "target-cmd"

	cfg := &config.Config{
		Autoload: rules,
	}
	cfg.CompileAutoloadRules()
	cmdArgs := []string{"target-cmd", "arg1", "arg2", "arg3"}

	b.ResetTimer()
	for range b.N {
		_, _, _ = MatchRunAlias(cfg, cmdArgs)
	}
}

func BenchmarkMatchRunAlias_NoMatch(b *testing.B) {
	rules := make([]config.AutoloadRule, 50)
	for i := range 50 {
		rules[i] = config.AutoloadRule{
			Match: fmt.Sprintf("^tool-%d\\s+(.*)$", i),
			Envs:  []config.AutoloadEnvItem{config.InlineEnvItem(map[string]string{"FOO": "bar"})},
		}
	}
	cfg := &config.Config{
		Autoload: rules,
	}
	cfg.CompileAutoloadRules()
	cmdArgs := []string{"kubectl", "get", "pods"}

	b.ResetTimer()
	for range b.N {
		_, _, _ = MatchRunAlias(cfg, cmdArgs)
	}
}

func BenchmarkMatchCommandRule_PureMatch(b *testing.B) {
	rule := config.AutoloadRule{
		Match: "^litellm\\s+(.*)$",
		Envs:  []config.AutoloadEnvItem{config.NamedEnvItem("work_vault")},
	}
	rule.Compile()

	cmdArgs := []string{"litellm", "--config", "config.yaml"}

	b.ResetTimer()
	for range b.N {
		_, _, _ = MatchCommandRule(rule, cmdArgs)
	}
}

func BenchmarkMatchCommand(b *testing.B) {
	ruleMatch := "^git (checkout|branch) "
	cmdArgs := []string{"git", "checkout", "main"}
	b.ResetTimer()
	for range b.N {
		MatchCommand(ruleMatch, cmdArgs)
	}
}

func BenchmarkBuildEnvForCommand_AutoloadInline(b *testing.B) {
	cfg := &config.Config{
		Autoload: []config.AutoloadRule{
			{
				Match: "^deploy\\s+(.*)$",
				Envs: []config.AutoloadEnvItem{
					config.InlineEnvItem(map[string]string{"STAGE": "prod", "REGION": "us-east-1"}),
				},
			},
		},
	}
	cfg.CompileAutoloadRules()
	eb := NewEnvBuilder(cfg, nil, nil, NewRuleEngine())
	ctx := context.Background()
	cmdArgs := []string{"deploy", "--apply"}

	b.ResetTimer()
	for range b.N {
		if _, _, err := eb.BuildEnvForCommand(ctx, EnvConfig{CmdArgs: cmdArgs, EmptyEnv: true}); err != nil {
			b.Fatal(err)
		}
	}
}
