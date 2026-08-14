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
		{"slice of string", []any{"tag1", "tag2"}, []string{"tag1", "tag2"}},
		{"slice with non-string", []any{"tag1", 123, "tag2"}, []string{"tag1", "tag2"}},
		{"other type", 123, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ParseTags(tt.v); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ParseTags() = %v, want %v", got, tt.want)
			}
		})
	}
}
