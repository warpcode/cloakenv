package utils

import (
	"testing"
)

func BenchmarkParseURI(b *testing.B) {
	uri := "keyring://service/account_name"
	b.ResetTimer()
	b.ReportAllocs()
	for range b.N {
		_, _, _ = ParseURI(uri)
	}
}
