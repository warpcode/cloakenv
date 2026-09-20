package engine

import (
	"context"
	"fmt"
	"sort"
	"testing"

	"github.com/warpcode/cloakenv/internal/config"
)

func BenchmarkFormatEnvMap(b *testing.B) {
	finalEnv := make(map[string]string, 100)
	for i := range 100 {
		finalEnv[fmt.Sprintf("ENV_VAR_KEY_%d", i)] = fmt.Sprintf("env_var_value_%d", i)
	}

	b.ResetTimer()
	b.ReportAllocs()
	for range b.N {
		_ = formatEnvMap(finalEnv)
	}
}

func benchmarkFormatSizes(b *testing.B, size int) {
	finalEnv := make(map[string]string, size)
	for i := range size {
		finalEnv[fmt.Sprintf("ENV_VAR_KEY_%d", i)] = fmt.Sprintf("env_var_value_%d", i)
	}

	b.Run("Legacy_Baseline", func(b *testing.B) {
		b.ResetTimer()
		b.ReportAllocs()
		for range b.N {
			keys := make([]string, 0, len(finalEnv))
			for k := range finalEnv {
				keys = append(keys, k)
			}
			sort.Strings(keys)

			result := make([]string, 0, len(keys))
			for _, k := range keys {
				result = append(result, k+"="+finalEnv[k])
			}
			_ = result
		}
	})

	b.Run("FormatEnvMap", func(b *testing.B) {
		b.ResetTimer()
		b.ReportAllocs()
		for range b.N {
			_ = formatEnvMap(finalEnv)
		}
	})
}

func BenchmarkFormatEnvMap_Size10(b *testing.B) {
	benchmarkFormatSizes(b, 10)
}

func BenchmarkFormatEnvMap_Size50(b *testing.B) {
	benchmarkFormatSizes(b, 50)
}

func BenchmarkFormatEnvMap_Size100(b *testing.B) {
	benchmarkFormatSizes(b, 100)
}

func BenchmarkBuildEnvMerges(b *testing.B) {
	b.ReportAllocs()
	ctx := context.Background()

	entities := make(map[string]map[string]any)
	var merges []string
	for i := range 10 {
		entityName := fmt.Sprintf("app_%d", i)
		attrs := make(map[string]any)
		for j := range 20 {
			attrs[fmt.Sprintf("VAR_%d_%d", i, j)] = fmt.Sprintf("val_%d_%d", i, j)
		}
		entities[entityName] = attrs
		merges = append(merges, fmt.Sprintf("bench_vault://%s", entityName))
	}

	cfg := &config.Config{
		Vaults: map[string]config.VaultConfig{
			"bench_vault": {
				Provider: "custom_vault",
				Entities: entities,
			},
		},
	}

	orch, err := NewOrchestrator(cfg)
	if err != nil {
		b.Fatalf("failed to create orchestrator: %v", err)
	}

	b.ResetTimer()
	for range b.N {
		_, err := orch.BuildEnv(ctx, EnvConfig{Merges: merges, EmptyEnv: true})
		if err != nil {
			b.Fatal(err)
		}
	}
}
