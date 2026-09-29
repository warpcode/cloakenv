package provider

import (
	"context"
	"fmt"
	"testing"
)

func BenchmarkCustomVaultProvider_Search(b *testing.B) {
	p := NewCustomVaultProvider()
	entities := make(map[string]map[string]any)

	// Create a large number of entities with tags
	for i := 0; i < 10000; i++ {
		name := fmt.Sprintf("entity_%d", i)
		entities[name] = map[string]any{
			"tags": []string{"Tag1", "Tag2", "Tag3", "Tag4", "Tag5"},
		}
	}

	p.entities = entities

	query := SearchQuery{
		Title: "Entity",
		Path:  "entity_",
		Tags:  []string{"TAG1", "TAG3", "TAG5", "TAG7", "TAG9"}, // some match, some don't
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = p.Search(context.Background(), query)
	}
}
