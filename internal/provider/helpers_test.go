package provider

import (
	"testing"
)

func TestSerializeVal(t *testing.T) {
	got, err := serializeVal("hello")
	if err != nil || got != "hello" {
		t.Fatalf("serializeVal(string) = (%q, %v), want (%q, nil)", got, err, "hello")
	}

	gotMap, err := serializeVal(map[string]any{"k": "v"})
	if err != nil || gotMap == "" {
		t.Fatalf("serializeVal(map) = (%q, %v), want non-empty string", gotMap, err)
	}
}

func TestResolveDotPath(t *testing.T) {
	data := map[string]any{
		"db": map[any]any{
			"host": "localhost",
			1:      "numeric_key",
		},
		"items": []any{"first", map[string]any{"name": "second"}},
	}

	if got, err := resolveDotPath(data, ""); err != nil || got == nil {
		t.Fatalf("empty path failed: %v", err)
	}
	if got, err := resolveDotPath(data, "db.host"); err != nil || got != "localhost" {
		t.Fatalf("resolveDotPath(db.host) = (%v, %v), want localhost", got, err)
	}
	if got, err := resolveDotPath(data, "db.1"); err != nil || got != "numeric_key" {
		t.Fatalf("resolveDotPath(db.1) = (%v, %v), want numeric_key", got, err)
	}
	if got, err := resolveDotPath(data, "items.1.name"); err != nil || got != "second" {
		t.Fatalf("resolveDotPath(items.1.name) = (%v, %v), want second", got, err)
	}
	if _, err := resolveDotPath(data, "items.99"); err == nil {
		t.Fatal("expected out of bounds error")
	}
	if _, err := resolveDotPath(data, "items.bad"); err == nil {
		t.Fatal("expected non-integer slice index error")
	}
	if _, err := resolveDotPath(data, "db.missing"); err == nil {
		t.Fatal("expected missing key error")
	}
}

func TestNormalizeEntryMapAndMatchEntry(t *testing.T) {
	if m, ok := normalizeEntryMap(map[any]any{"a": 1, 2: "two"}); !ok || m["a"] != 1 || m["2"] != "two" {
		t.Fatalf("unexpected normalizeEntryMap result: %v (ok=%v)", m, ok)
	}
	if _, ok := normalizeEntryMap("not a map"); ok {
		t.Fatal("expected normalizeEntryMap to return false for string")
	}

	entry := Entry{
		Title: "Prod DB",
		Tags:  []string{"Env:Prod", "DB"},
	}
	if !matchEntry(entry, "infra/prod-db", SearchQuery{Title: "prod", Path: "infra", Tags: []string{"env:prod"}}, "prod", "infra", []string{"env:prod"}) {
		t.Fatal("expected matchEntry to succeed case-insensitively")
	}
	if matchEntry(entry, "infra/prod-db", SearchQuery{Tags: []string{"staging"}}, "", "", []string{"staging"}) {
		t.Fatal("expected matchEntry to reject missing tag")
	}
}
