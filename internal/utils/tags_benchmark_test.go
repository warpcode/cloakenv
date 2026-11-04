package utils

import "testing"

func BenchmarkParseTagString(b *testing.B) {
	tagStr := "tag1, tag2, tag3, auth:ssh, env:prod"
	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		_ = ParseTagString(tagStr)
	}
}

func BenchmarkParseTagsSliceString(b *testing.B) {
	slice := []string{"tag1", "tag2", "tag3"}
	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		_ = ParseTags(slice)
	}
}

func BenchmarkParseTagsSliceAny(b *testing.B) {
	slice := []any{"tag1", "tag2", "tag3"}
	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		_ = ParseTags(slice)
	}
}

func BenchmarkParseTagsString(b *testing.B) {
	tagStr := "tag1, tag2, tag3"
	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		_ = ParseTags(tagStr)
	}
}
