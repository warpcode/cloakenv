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
