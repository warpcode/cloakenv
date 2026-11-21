package utils

import (
	"bytes"
	"errors"
	"testing"
)

type failMarshaler struct{}

func (failMarshaler) MarshalYAML() (any, error) {
	return nil, errors.New("marshal error")
}

func TestUnmarshalYAML(t *testing.T) {
	data := []byte("key: value")
	var m map[string]string
	if err := UnmarshalYAML(data, &m); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m["key"] != "value" {
		t.Errorf("expected 'value', got %q", m["key"])
	}

	badData := []byte("invalid: yaml: :")
	if err := UnmarshalYAML(badData, &m); err == nil {
		t.Error("expected error, got nil")
	}
}

func TestMarshalYAML(t *testing.T) {
	tests := []struct {
		name    string
		input   any
		want    string
		wantErr bool
	}{
		{
			name:  "valid map",
			input: map[string]string{"key": "value"},
			want:  "key: value\n",
		},
		{
			name:  "nested map indentation",
			input: map[string]any{"a": map[string]int{"b": 1}},
			want:  "a:\n  b: 1\n",
		},
		{
			name:    "failing marshaler",
			input:   failMarshaler{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := MarshalYAML(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("MarshalYAML() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && string(got) != tt.want {
				t.Errorf("MarshalYAML() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMarshalYAMLString(t *testing.T) {
	tests := []struct {
		name    string
		input   any
		want    string
		wantErr bool
	}{
		{
			name:  "valid map",
			input: map[string]string{"key": "value"},
			want:  "key: value",
		},
		{
			name:  "nested map indentation",
			input: map[string]any{"a": map[string]int{"b": 1}},
			want:  "a:\n  b: 1",
		},
		{
			name:    "failing marshaler",
			input:   failMarshaler{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := MarshalYAMLString(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("MarshalYAMLString() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("MarshalYAMLString() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSerializeYAMLValue(t *testing.T) {
	tests := []struct {
		name     string
		input    any
		expected string
	}{
		{"string", "hello", "hello"},
		{"int", 42, "42"},
		{"int64", int64(100), "100"},
		{"int32", int32(50), "50"},
		{"uint", uint(10), "10"},
		{"uint64", uint64(999), "999"},
		{"float64", 3.14159, "3.14159"},
		{"float32", float32(2.5), "2.5"},
		{"bool true", true, "true"},
		{"bool false", false, "false"},
		{"nil", nil, ""},
		{"slice", []any{"a", "b"}, "- a\n- b"},
		{"map", map[string]any{"foo": "bar"}, "foo: bar"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := SerializeYAMLValue(tc.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.expected {
				t.Errorf("SerializeYAMLValue(%v) = %q, expected %q", tc.input, got, tc.expected)
			}
		})
	}
}

func TestYAMLEncoder(t *testing.T) {
	var buf bytes.Buffer
	enc := NewYAMLEncoder(&buf)
	enc.SetIndent(2)
	m := map[string]string{"key": "value"}
	if err := enc.Encode(m); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if buf.String() != "key: value\n" {
		t.Errorf("unexpected output: %q", buf.String())
	}
}
