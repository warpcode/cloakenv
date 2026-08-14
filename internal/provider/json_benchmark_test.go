package provider

import (
	"context"
	"fmt"
	"testing"
)

func BenchmarkJsonProvider_Search(b *testing.B) {
	provider := &JsonProvider{
		entries: make(map[string]Entry),
	}
	for i := 0; i < 1000; i++ {
		provider.entries[fmt.Sprintf("path/to/secret/%d", i)] = Entry{
			Title: fmt.Sprintf("Secret Title %d", i),
			Tags:  []string{"tag1", "tag2", "tag3"},
		}
	}

	query := SearchQuery{
		Title: "TITLE 500",
		Path:  "PATH/TO/SECRET/500",
		Tags:  []string{"TAG1", "TAG2"},
	}
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = provider.Search(ctx, query)
	}
}
