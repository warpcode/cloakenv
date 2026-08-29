package engine

import (
	"fmt"
	"sort"
	"testing"
)

func BenchmarkEnvFormattingBaseline(b *testing.B) {
	finalEnv := make(map[string]string, 100)
	for i := range 100 {
		finalEnv[fmt.Sprintf("ENV_VAR_KEY_%d", i)] = fmt.Sprintf("env_var_value_%d", i)
	}

	b.ResetTimer()
	b.ReportAllocs()
	for range b.N {
		keys := make([]string, 0, len(finalEnv))
		for k := range finalEnv {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		var result []string
		for _, k := range keys {
			result = append(result, fmt.Sprintf("%s=%s", k, finalEnv[k]))
		}
		_ = result
	}
}

func BenchmarkEnvFormattingOptimized(b *testing.B) {
	finalEnv := make(map[string]string, 100)
	for i := range 100 {
		finalEnv[fmt.Sprintf("ENV_VAR_KEY_%d", i)] = fmt.Sprintf("env_var_value_%d", i)
	}

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
}

func BenchmarkEnvFormattingOnlyBaseline(b *testing.B) {
	finalEnv := make(map[string]string, 100)
	for i := range 100 {
		finalEnv[fmt.Sprintf("ENV_VAR_KEY_%d", i)] = fmt.Sprintf("env_var_value_%d", i)
	}
	keys := make([]string, 0, len(finalEnv))
	for k := range finalEnv {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	b.ResetTimer()
	b.ReportAllocs()
	for range b.N {
		var result []string
		for _, k := range keys {
			result = append(result, fmt.Sprintf("%s=%s", k, finalEnv[k]))
		}
		_ = result
	}
}

func BenchmarkEnvFormattingOnlyOptimized(b *testing.B) {
	finalEnv := make(map[string]string, 100)
	for i := range 100 {
		finalEnv[fmt.Sprintf("ENV_VAR_KEY_%d", i)] = fmt.Sprintf("env_var_value_%d", i)
	}
	keys := make([]string, 0, len(finalEnv))
	for k := range finalEnv {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	b.ResetTimer()
	b.ReportAllocs()
	for range b.N {
		result := make([]string, 0, len(keys))
		for _, k := range keys {
			result = append(result, k+"="+finalEnv[k])
		}
		_ = result
	}
}
