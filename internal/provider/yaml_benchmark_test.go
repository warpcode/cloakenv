package provider

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"testing"

	"gopkg.in/yaml.v3"
)

func BenchmarkYamlProviderInitialize(b *testing.B) {
	// Create a large dummy yaml file
	f, err := os.CreateTemp(b.TempDir(), "benchmark-yaml-*.yml")
	if err != nil {
		b.Fatal(err)
	}

	data := make(map[string]any)
	for i := range 1000 {
		entry := make(map[any]any)
		for j := range 50 {
			entry[fmt.Sprintf("key%d", j)] = j
		}
		data[fmt.Sprintf("entry%d", i)] = entry
	}
	raw, err := yaml.Marshal(data)
	if err != nil {
		b.Fatal(err)
	}
	if _, err := f.Write(raw); err != nil {
		b.Fatal(err)
	}
	if err := f.Close(); err != nil {
		b.Fatal(err)
	}

	cfg := ProviderConfig{
		Settings: map[string]string{
			"vault_path": f.Name(),
		},
	}
	ctx := context.Background()

	b.ResetTimer()
	for range b.N {
		p := NewYamlProvider()
		_ = p.Initialize(ctx, cfg)
	}
}

var dummy = make(map[any]any)

func init() {
	for i := range 1000 {
		dummy[fmt.Sprintf("key%d", i)] = i
	}
	// Add some int and bool keys for testing
	dummy[42] = "forty-two"
	dummy[true] = "true"
}

func BenchmarkFmtSprintf(b *testing.B) {
	for range b.N {
		converted := make(map[string]any)
		for k, v := range dummy {
			converted[fmt.Sprintf("%v", k)] = v
		}
	}
}

func BenchmarkAnyToString(b *testing.B) {
	for range b.N {
		converted := make(map[string]any)
		for k, v := range dummy {
			converted[anyToString(k)] = v
		}
	}
}

func BenchmarkYamlProvider_Search(b *testing.B) {
	// Create a large number of dummy entries
	entries := make(map[string]Entry)
	for i := range 10000 {
		entries[fmt.Sprintf("entry_%d", i)] = Entry{
			Title: fmt.Sprintf("Entry %d", i),
			Tags:  []string{"tagA", "tagB", "tagC", "tagD"},
		}
	}

	provider := NewYamlProvider()
	provider.entries = entries

	query := SearchQuery{
		Title: "entry",
		Path:  "entry",
		Tags:  []string{"tagC", "tagD"},
	}

	ctx := context.Background()

	b.ResetTimer()
	for range b.N {
		_, err := provider.Search(ctx, query)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkYamlSearchTags(b *testing.B) {
	y := NewYamlProvider()
	for i := range 1000 {
		y.entries[strconv.Itoa(i)] = Entry{
			Title: "Test",
			Tags:  []string{"tag1", "tag2", "tag3"},
		}
	}

	query := SearchQuery{
		Tags: []string{"tag1", "tag2"},
	}

	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		_, _ = y.Search(ctx, query)
	}
}
