package provider

import (
	"context"
	"testing"

	"github.com/tobischo/gokeepasslib/v3"
	"github.com/zalando/go-keyring"
)

var benchmarkResult []SearchResult

func BenchmarkKeePassProvider_Search_Binaries(b *testing.B) {
	k := NewKeePassProvider()
	k.db = gokeepasslib.NewDatabase()

	// Create many binaries in metadata
	numBinaries := 5000
	for i := 0; i < numBinaries; i++ {
		k.db.Content.Meta.Binaries = append(k.db.Content.Meta.Binaries, gokeepasslib.Binary{
			ID:      i,
			Content: []byte("test content"),
		})
	}

	// Create a group with entries that reference some binaries
	group := gokeepasslib.Group{
		Name: "TestGroup",
	}

	numEntries := 100
	for i := 0; i < numEntries; i++ {
		entry := gokeepasslib.Entry{
			Values: []gokeepasslib.ValueData{
				{Key: "Title", Value: gokeepasslib.V{Content: "Test Entry"}},
			},
		}

		// Add some binary references
		for j := 0; j < 5; j++ {
			entry.Binaries = append(entry.Binaries, gokeepasslib.BinaryReference{
				Name: "Attachment",
				Value: struct {
					ID int `xml:"Ref,attr"`
				}{ID: (i*5 + j) % numBinaries},
			})
		}

		group.Entries = append(group.Entries, entry)
	}

	k.db.Content.Root.Groups = []gokeepasslib.Group{group}

	ctx := context.Background()
	query := SearchQuery{}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		res, err := k.Search(ctx, query)
		if err != nil {
			b.Fatal(err)
		}
		benchmarkResult = res
	}
}

func BenchmarkKeePassProviderSearch(b *testing.B) {
	keyring.MockInit()
	ctx := context.Background()

	// Setup mock credentials
	if err := keyring.Set("cloakenv", "provider/testdb", "password123"); err != nil {
		b.Fatalf("failed to set mock credentials: %v", err)
	}

	kp := NewKeePassProvider()
	cfg := ProviderConfig{
		Settings: map[string]string{
			"vault_path":  "../../testdata/testDB.kdbx",
			"remote_name": "testdb",
		},
	}
	if err := kp.Initialize(ctx, cfg); err != nil {
		b.Fatalf("Initialize failed: %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		res, err := kp.Search(ctx, SearchQuery{Title: "Test Website"})
		if err != nil {
			b.Fatalf("Search failed: %v", err)
		}
		benchmarkResult = res
	}
}
