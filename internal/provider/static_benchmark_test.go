package provider

import (
	"testing"
)

var benchmarkDotPathRes any

func BenchmarkResolveDotPath(b *testing.B) {
	data := map[string]any{
		"server": map[string]any{
			"database": map[string]any{
				"credentials": map[string]any{
					"password": "secretPassword123",
				},
				"hosts": []any{"10.0.0.1", "10.0.0.2"},
			},
		},
	}

	b.Run("MapTraversal", func(b *testing.B) {
		path := "server.database.credentials.password"
		b.ReportAllocs()
		b.ResetTimer()
		for range b.N {
			benchmarkDotPathRes, _ = resolveDotPath(data, path)
		}
	})

	b.Run("ArrayTraversal", func(b *testing.B) {
		path := "server.database.hosts.1"
		b.ReportAllocs()
		b.ResetTimer()
		for range b.N {
			benchmarkDotPathRes, _ = resolveDotPath(data, path)
		}
	})
}
