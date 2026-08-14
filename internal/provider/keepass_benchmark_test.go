package provider

import (
	"testing"
	"fmt"
	"github.com/tobischo/gokeepasslib/v3"
)

func BenchmarkKeePassToEntry(b *testing.B) {
	kp := NewKeePassProvider()

	kp.db = gokeepasslib.NewDatabase()

	// Add 100 binaries
	kp.binaries = make(map[int]string)
	for i := 0; i < 100; i++ {
		kp.db.Content.Meta.Binaries = append(kp.db.Content.Meta.Binaries, gokeepasslib.Binary{
			ID:      i,
			Content: []byte("some binary content data here to simulate real data"),
		})
		kp.binaries[i] = "some binary content data here to simulate real data"
	}

	// Create an entry with 10 binary references
	entry := gokeepasslib.NewEntry()
	entry.Values = append(entry.Values, gokeepasslib.ValueData{
		Key: "Title",
		Value: gokeepasslib.V{
			Content: "My Entry",
		},
	})

	for i := 0; i < 10; i++ {
		entry.Binaries = append(entry.Binaries, gokeepasslib.NewBinaryReference(fmt.Sprintf("attachment%d", i), i))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		kp.toEntry(&entry)
	}
}
