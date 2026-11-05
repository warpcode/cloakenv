package utils

import (
	"reflect"
	"testing"
)

func TestParseTagString(t *testing.T) {
	tests := []struct {
		name    string
		tagsStr string
		want    []string
	}{
		{"empty", "", nil},
		{"single", "tag1", []string{"tag1"}},
		{"multiple", "tag1,tag2,tag3", []string{"tag1", "tag2", "tag3"}},
		{"with spaces", "tag1 , tag2, tag3 ", []string{"tag1", "tag2", "tag3"}},
		{"empty parts", "tag1,,tag2", []string{"tag1", "tag2"}},
		{"only spaces", " ,  , ", nil},
		{"leading trailing commas", ",tag1,tag2,", []string{"tag1", "tag2"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ParseTagString(tt.tagsStr); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ParseTagString() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseTags(t *testing.T) {
	tests := []struct {
		name string
		v    any
		want []string
	}{
		{"nil", nil, nil},
		{"string", "tag1,tag2", []string{"tag1", "tag2"}},
		{"string with spaces", "tag1 , tag2 ", []string{"tag1", "tag2"}},
		{"slice of string direct", []string{"tag1", "tag2"}, []string{"tag1", "tag2"}},
		{"slice of any", []any{"tag1", "tag2"}, []string{"tag1", "tag2"}},
		{"slice with non-string", []any{"tag1", 123, "tag2"}, []string{"tag1", "tag2"}},
		{"other type", 123, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseTags(tt.v)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ParseTags() = %v, want %v", got, tt.want)
			}
			// Verify immutability/cloning for []string input
			if inSlice, ok := tt.v.([]string); ok && len(inSlice) > 0 {
				got[0] = "mutated"
				if inSlice[0] == "mutated" {
					t.Errorf("ParseTags modified original slice backing array")
				}
			}
		})
	}
}

func BenchmarkParseTagString(b *testing.B) {
	tagStr := "tag1, tag2, tag3, auth:ssh, env:prod"
	b.ResetTimer()
	b.ReportAllocs()
	for range b.N {
		_ = ParseTagString(tagStr)
	}
}

func BenchmarkParseTags_SliceString(b *testing.B) {
	tagsSlice := []string{"tag1", "tag2", "tag3", "auth:ssh", "env:prod"}
	b.ResetTimer()
	b.ReportAllocs()
	for range b.N {
		_ = ParseTags(tagsSlice)
	}
}

func BenchmarkParseTags_SliceAny(b *testing.B) {
	tagsSlice := []any{"tag1", "tag2", "tag3", "auth:ssh", "env:prod"}
	b.ResetTimer()
	b.ReportAllocs()
	for range b.N {
		_ = ParseTags(tagsSlice)
	}
}
