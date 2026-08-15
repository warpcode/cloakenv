package provider

import (
	"context"
	"fmt"
	"os"
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
	for i := 0; i < 1000; i++ {
		entry := make(map[any]any)
		for j := 0; j < 50; j++ {
			entry[fmt.Sprintf("key%d", j)] = j
		}
		data[fmt.Sprintf("entry%d", i)] = entry
	}
	raw, err := yaml.Marshal(data)
	if err != nil {
		b.Fatal(err)
	}
	f.Write(raw)
	f.Close()

	cfg := ProviderConfig{
		Settings: map[string]string{
			"vault_path": f.Name(),
		},
	}
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p := NewYamlProvider()
		_ = p.Initialize(ctx, cfg)
	}
}

var dummy = make(map[any]any)

func init() {
	for i := 0; i < 1000; i++ {
		dummy[fmt.Sprintf("key%d", i)] = i
	}
	// Add some int and bool keys for testing
	dummy[42] = "forty-two"
	dummy[true] = "true"
}

func BenchmarkFmtSprintf(b *testing.B) {
	for i := 0; i < b.N; i++ {
		converted := make(map[string]any)
		for k, v := range dummy {
			converted[fmt.Sprintf("%v", k)] = v
		}
	}
}

func BenchmarkAnyToString(b *testing.B) {
	for i := 0; i < b.N; i++ {
		converted := make(map[string]any)
		for k, v := range dummy {
			converted[anyToString(k)] = v
		}
	}
}

func BenchmarkYamlProvider_Search(b *testing.B) {
	// Create a large number of dummy entries
	entries := make(map[string]Entry)
	for i := 0; i < 10000; i++ {
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
	for i := 0; i < b.N; i++ {
		_, err := provider.Search(ctx, query)
		if err != nil {
			b.Fatal(err)
		}
	}
}
