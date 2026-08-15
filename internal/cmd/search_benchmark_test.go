package cmd

import (
	"fmt"
	"testing"

	"github.com/warpcode/cloakenv/internal/provider"
)

var benchmarkRes []map[string]any

func BenchmarkFlattenSearchResults(b *testing.B) {
	// Setup dummy search results
	var results []provider.SearchResult
	for i := 0; i < 1000; i++ {
		results = append(results, provider.SearchResult{
			Provider: "dummy",
			Vault:    "test-vault",
			Path:     fmt.Sprintf("path/to/secret/%d", i),
			Entry: provider.Entry{
				Title: "Test Secret",
				Tags:  []string{"tag1", "tag2"},
				Attributes: map[string]any{
					"Username": "testuser",
					"Password": "testpassword",
					"URL":      "https://example.com",
					"Notes":    "Some test notes",
				},
			},
		})
	}

	selectedKeys := []string{"provider", "vault", "path", "Title", "TAGS", "username", "PASSWORD"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchmarkRes = flattenSearchResults(results, selectedKeys)
	}
}
