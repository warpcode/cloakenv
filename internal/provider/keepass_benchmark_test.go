package provider

import (
	"context"
	"fmt"
	"testing"

	"github.com/tobischo/gokeepasslib/v3"
)

func BenchmarkKeePassProvider_Search(b *testing.B) {
	kp := NewKeePassProvider()

	kp.db = gokeepasslib.NewDatabase()

	// Add 500 binaries
	numBinaries := 500
	for i := 0; i < numBinaries; i++ {
		kp.db.Content.Meta.Binaries = append(kp.db.Content.Meta.Binaries, gokeepasslib.Binary{
			ID:      i,
			Content: []byte(fmt.Sprintf("content for binary %d", i)),
		})
	}

	rootGroup := gokeepasslib.NewGroup()
	rootGroup.Name = "Root"

	// Add 2000 entries
	numEntries := 2000
	for i := 0; i < numEntries; i++ {
		entry := gokeepasslib.NewEntry()
		entry.Values = append(entry.Values, gokeepasslib.ValueData{Key: "Title", Value: gokeepasslib.V{Content: fmt.Sprintf("Entry %d", i)}})

		// Add 5 binaries to each entry
		for j := 0; j < 5; j++ {
			binID := (i + j) % numBinaries
			entry.Binaries = append(entry.Binaries, gokeepasslib.BinaryReference{
				Name: fmt.Sprintf("Attachment%d", j),
				Value: struct {
					ID int `xml:"Ref,attr"`
				}{ID: binID},
			})
		}

		rootGroup.Entries = append(rootGroup.Entries, entry)
	}

	kp.db.Content.Root = &gokeepasslib.RootData{
		Groups: []gokeepasslib.Group{rootGroup},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := kp.Search(context.Background(), SearchQuery{})
		if err != nil {
			b.Fatalf("search failed: %v", err)
		}
	}
}
