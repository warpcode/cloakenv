package provider

import (
	"context"
	"testing"
)

func TestCustomVaultProvider_SingleEntity(t *testing.T) {
	ctx := context.Background()
	p := NewCustomVaultProvider()

	isTrue := true
	cfg := ProviderConfig{
		SingleEntity: &isTrue,
		EntityName:   "Test Static Vault",
		Tags:         []string{"test", "static"},
		Attributes: map[string]any{
			"key1": "value1",
			"key2": 42,
			"key3": []any{"a", "b"},
		},
	}

	if err := p.Initialize(ctx, cfg); err != nil {
		t.Fatalf("failed to initialize: %v", err)
	}

	if p.Scheme() != "custom_vault" {
		t.Errorf("expected scheme custom_vault, got %q", p.Scheme())
	}

	// 1. GetSecret
	val, err := p.GetSecret(ctx, "key1")
	if err != nil {
		t.Fatalf("GetSecret failed: %v", err)
	}
	if val != "value1" {
		t.Errorf("expected value1, got %q", val)
	}

	val, err = p.GetSecret(ctx, "key2")
	if err != nil {
		t.Fatalf("GetSecret failed: %v", err)
	}
	if val != "42" {
		t.Errorf("expected 42, got %q", val)
	}

	val, err = p.GetSecret(ctx, "key3")
	if err != nil {
		t.Fatalf("GetSecret failed: %v", err)
	}
	expectedSerialized := "- a\n- b"
	if val != expectedSerialized {
		t.Errorf("expected serialised array, got %q", val)
	}

	_, err = p.GetSecret(ctx, "nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent key, got nil")
	}

	// 2. GetEntry
	entry, err := p.GetEntry(ctx, "")
	if err != nil {
		t.Fatalf("GetEntry failed: %v", err)
	}
	if entry.Title != "Test Static Vault" {
		t.Errorf("expected title 'Test Static Vault', got %q", entry.Title)
	}
	if len(entry.Tags) != 2 || entry.Tags[0] != "test" || entry.Tags[1] != "static" {
		t.Errorf("unexpected tags: %v", entry.Tags)
	}

	// 3. Search
	results, err := p.Search(ctx, SearchQuery{})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Path != "" {
		t.Errorf("expected empty path for single-entity, got %q", results[0].Path)
	}
	if results[0].Entry.Title != "Test Static Vault" {
		t.Errorf("expected title 'Test Static Vault', got %q", results[0].Entry.Title)
	}

	// Search matching title query
	results, err = p.Search(ctx, SearchQuery{Title: "Static"})
	if err != nil || len(results) != 1 {
		t.Errorf("expected match for title query, got results: %d, err: %v", len(results), err)
	}

	// Search mismatching title query
	results, err = p.Search(ctx, SearchQuery{Title: "Mismatch"})
	if err != nil || len(results) != 0 {
		t.Errorf("expected no match, got results: %d, err: %v", len(results), err)
	}

	// Search matching tag
	results, err = p.Search(ctx, SearchQuery{Tags: []string{"test"}})
	if err != nil || len(results) != 1 {
		t.Errorf("expected match for tag, got results: %d, err: %v", len(results), err)
	}

	// Search mismatching tag
	results, err = p.Search(ctx, SearchQuery{Tags: []string{"mismatch"}})
	if err != nil || len(results) != 0 {
		t.Errorf("expected no match for tag, got results: %d, err: %v", len(results), err)
	}
}

func TestCustomVaultProvider_MultipleEntities(t *testing.T) {
	ctx := context.Background()
	p := NewCustomVaultProvider()

	isFalse := false
	cfg := ProviderConfig{
		SingleEntity: &isFalse,
		Entities: map[string]map[string]any{
			"entity1": {
				"Password": "pass1",
				"username": "user1",
				"tags":     "env:dev, type:db",
			},
			"entity2": {
				"Password": "pass2",
				"username": "user2",
				"tags":     []any{"env:prod"},
			},
		},
	}

	if err := p.Initialize(ctx, cfg); err != nil {
		t.Fatalf("failed to initialize: %v", err)
	}

	// 1. GetSecret
	val, err := p.GetSecret(ctx, "entity1:username")
	if err != nil {
		t.Fatalf("GetSecret failed: %v", err)
	}
	if val != "user1" {
		t.Errorf("expected user1, got %q", val)
	}

	// Default attribute "Password"
	val, err = p.GetSecret(ctx, "entity2")
	if err != nil {
		t.Fatalf("GetSecret failed: %v", err)
	}
	if val != "pass2" {
		t.Errorf("expected pass2, got %q", val)
	}

	_, err = p.GetSecret(ctx, "nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent entity, got nil")
	}

	_, err = p.GetSecret(ctx, "entity1:nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent attribute, got nil")
	}

	// 2. GetEntry
	entry, err := p.GetEntry(ctx, "entity1")
	if err != nil {
		t.Fatalf("GetEntry failed: %v", err)
	}
	if entry.Title != "entity1" {
		t.Errorf("expected title 'entity1', got %q", entry.Title)
	}
	if len(entry.Tags) != 2 || entry.Tags[0] != "env:dev" || entry.Tags[1] != "type:db" {
		t.Errorf("unexpected tags parsed: %v", entry.Tags)
	}

	// 3. Search
	results, err := p.Search(ctx, SearchQuery{})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	// Search matching title query
	results, err = p.Search(ctx, SearchQuery{Title: "entity1"})
	if err != nil || len(results) != 1 {
		t.Errorf("expected 1 result, got %d, err: %v", len(results), err)
	}

	// Search matching tag
	results, err = p.Search(ctx, SearchQuery{Tags: []string{"env:prod"}})
	if err != nil || len(results) != 1 {
		t.Errorf("expected 1 result for tag query, got %d, err: %v", len(results), err)
	}
}
