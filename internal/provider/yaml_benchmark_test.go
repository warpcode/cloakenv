package provider

import (
	"context"
	"fmt"
	"testing"
)

func BenchmarkYamlProviderSearch(b *testing.B) {
	yp := NewYamlProvider()

	// Create a large number of dummy entries
	yp.entries = make(map[string]Entry)
	for i := 0; i < 10000; i++ {
		yp.entries[fmt.Sprintf("path_%d", i)] = Entry{
			Title: fmt.Sprintf("Title %d", i),
			Tags:  []string{"tag1", "tag2"},
		}
	}
	yp.singleEntity = false

	ctx := context.Background()
	query := SearchQuery{
		Title: "TITLE 999", // To hit the loop and run lowercasing
		Path:  "PATH_999",
		Tags:  []string{"TAG1"},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = yp.Search(ctx, query)
	}
}
