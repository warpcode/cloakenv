package utils_test

import (
	"testing"

	"github.com/warpcode/cloakenv/internal/utils"
)

func BenchmarkExpandString_NoExpansion(b *testing.B) {
	input := "plain_secret_value_without_any_dollar"
	b.ResetTimer()
	b.ReportAllocs()
	for range b.N {
		_, _ = utils.ExpandString(input, "", nil)
	}
}

func BenchmarkExpandString_WithExpansion(b *testing.B) {
	input := "prefix_${env:SECRET}_suffix"
	resolve := func(u string) (string, error) {
		return "resolved_value", nil
	}
	b.ResetTimer()
	b.ReportAllocs()
	for range b.N {
		_, _ = utils.ExpandString(input, "", resolve)
	}
}
