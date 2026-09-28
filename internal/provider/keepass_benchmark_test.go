package provider

import "testing"

func BenchmarkMatchEntryTags(b *testing.B) {
	tagString := "Work, Personal, Important, Finance, Auto, Home"
	queryTagsLower := []string{"finance", "important"}

	b.ResetTimer()
	b.ReportAllocs()
	for range b.N {
		matchEntryTags(tagString, queryTagsLower)
	}
}
