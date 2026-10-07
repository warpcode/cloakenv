package utils

import (
	"strings"
	"testing"
)

func BenchmarkExpandString_NoExpansion(b *testing.B) {
	for range b.N {
		_, _ = ExpandString("plain string without expansion", "", nil)
	}
}

func BenchmarkExpandString_WithExpansion(b *testing.B) {
	fn := func(u string) (string, error) { return "earth", nil }
	for range b.N {
		_, _ = ExpandString("hello ${world}", "", fn)
	}
}

func BenchmarkExpandString_LargeInput(b *testing.B) {
	fn := func(u string) (string, error) { return "resolved", nil }
	input := "${a}" + strings.Repeat("x", 100_000)
	b.ResetTimer()
	for range b.N {
		_, _ = ExpandString(input, "", fn)
	}
}
