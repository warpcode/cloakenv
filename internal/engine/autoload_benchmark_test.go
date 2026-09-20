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
