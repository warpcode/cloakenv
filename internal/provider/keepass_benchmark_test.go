package provider

import (
	"context"
	"testing"

	"github.com/tobischo/gokeepasslib/v3"
)

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

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := k.Search(ctx, query)
		if err != nil {
			b.Fatal(err)
		}
	}
}
