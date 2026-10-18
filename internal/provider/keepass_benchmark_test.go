package provider

import (
	"context"
	"fmt"
	"strings"
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
	for i := range numBinaries {
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
	for i := range numEntries {
		entry := gokeepasslib.Entry{
			Values: []gokeepasslib.ValueData{
				{Key: "Title", Value: gokeepasslib.V{Content: "Test Entry"}},
			},
		}

		// Add some binary references
		for j := range 5 {
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
	for range b.N {
		res, err := k.Search(ctx, query)
		if err != nil {
			b.Fatal(err)
		}
		benchmarkResult = res
	}
}

func BenchmarkKeePassTConvResolution(b *testing.B) {
	sizes := []int{1000, 2000, 4000, 8000}
	for _, n := range sizes {
		b.Run(fmt.Sprintf("Placeholders_%d", n), func(b *testing.B) {
			kp := NewKeePassProvider()
			kp.resolveReferences = true
			db := gokeepasslib.NewDatabase()
			rootGroup := gokeepasslib.NewGroup()
			rootGroup.Name = "Root"

			placeholder := "{T-CONV:/x/RAW/}"
			val := strings.Repeat(placeholder, n)

			entry := gokeepasslib.NewEntry()
			entry.Values = []gokeepasslib.ValueData{
				{Key: "Title", Value: gokeepasslib.V{Content: "BenchEntry"}},
				{Key: "Password", Value: gokeepasslib.V{Content: val}},
			}
			rootGroup.Entries = append(rootGroup.Entries, entry)
			db.Content.Root.Groups = []gokeepasslib.Group{rootGroup}
			kp.db = db

			ctx := context.Background()

			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				res, err := kp.GetSecret(ctx, "Root/BenchEntry:Password")
				if err != nil {
					b.Fatalf("GetSecret failed: %v", err)
				}
				if len(res) != n {
					b.Fatalf("expected length %d, got %d", n, len(res))
				}
			}
		})
	}
}

var benchmarkBool bool

func BenchmarkMatchEntryTags(b *testing.B) {
	tagString := "production, database, web, us-east-1, primary, v2, critical"
	queryTagsLower := []string{"production", "database", "critical"}

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		benchmarkBool = matchEntryTags(tagString, queryTagsLower)
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
	for range b.N {
		res, err := kp.Search(ctx, SearchQuery{Title: "Test Website"})
		if err != nil {
			b.Fatalf("Search failed: %v", err)
		}
		benchmarkResult = res
	}
}
