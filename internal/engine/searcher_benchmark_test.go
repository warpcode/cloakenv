package engine

import (
	"testing"
)

func BenchmarkParseSearchURI(b *testing.B) {
	uri := "tags=auth:ssh,env:prod,deprecated&title=bastion&path=servers/ssh/Password"
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _, err := parseSearchURI(uri)
		if err != nil {
			b.Fatal(err)
		}
	}
}
