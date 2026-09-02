package yaml

import (
	"bytes"
	"testing"
)

func TestUnmarshal(t *testing.T) {
	data := []byte("key: value")
	var m map[string]string
	if err := Unmarshal(data, &m); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m["key"] != "value" {
		t.Errorf("expected 'value', got %q", m["key"])
	}

	badData := []byte("invalid: yaml: :")
	if err := Unmarshal(badData, &m); err == nil {
		t.Error("expected error, got nil")
	}
}

func TestMarshal(t *testing.T) {
	m := map[string]string{"key": "value"}
	data, err := Marshal(m)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(data) != "key: value\n" {
		t.Errorf("unexpected output: %q", data)
	}
}

func TestMarshalString(t *testing.T) {
	m := map[string]string{"key": "value"}
	str, err := MarshalString(m)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if str != "key: value" {
		t.Errorf("expected 'key: value', got %q", str)
	}
}

func TestSerializeValue(t *testing.T) {
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
			got, err := SerializeValue(tc.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.expected {
				t.Errorf("SerializeValue(%v) = %q, expected %q", tc.input, got, tc.expected)
			}
		})
	}
}

func TestEncoder(t *testing.T) {
	var buf bytes.Buffer
	enc := NewEncoder(&buf)
	enc.SetIndent(2)
	m := map[string]string{"key": "value"}
	if err := enc.Encode(m); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if buf.String() != "key: value\n" {
		t.Errorf("unexpected output: %q", buf.String())
	}
}
