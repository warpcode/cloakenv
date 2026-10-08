package utils

import "testing"

func BenchmarkParseURI(b *testing.B) {
	for range b.N {
		_, _, _ = ParseURI("keyring://service/account")
	}
}
