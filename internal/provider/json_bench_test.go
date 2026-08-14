package provider

import (
	"context"
	"testing"
)

func BenchmarkJSONSearchTags(b *testing.B) {
	p := NewJsonProvider()
	p.entries = make(map[string]Entry)
	for i := 0; i < 1000; i++ {
		p.entries[string(rune(i))] = Entry{
			Title: "test",
			Tags:  []string{"tag1", "tag2", "tag3"},
		}
	}

	q := SearchQuery{
		Tags: []string{"tag2", "tag3"},
	}
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = p.Search(ctx, q)
	}
}
