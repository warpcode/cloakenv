package provider

import (
	"context"
	"testing"
)

func TestCustomVaultProvider_MultipleEntities(t *testing.T) {
	ctx := context.Background()
	p := NewCustomVaultProvider()

	cfg := ProviderConfig{
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

	if p.Scheme() != "custom_vault" {
		t.Errorf("expected scheme custom_vault, got %q", p.Scheme())
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
