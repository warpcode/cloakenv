package utils_test

import (
	"testing"

	"github.com/warpcode/cloakenv/internal/utils"
)

func BenchmarkParseURI(b *testing.B) {
	uri := "keyring://service/account_name"
	b.ResetTimer()
	b.ReportAllocs()
	for range b.N {
		_, _, _ = utils.ParseURI(uri)
	}
}
