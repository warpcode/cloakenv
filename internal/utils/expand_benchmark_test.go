package utils

import "testing"

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
