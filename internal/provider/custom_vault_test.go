package provider

import (
	"context"
	"strings"
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

func TestCustomVaultProvider_Search(t *testing.T) {
	ctx := context.Background()
	p := NewCustomVaultProvider()

	cfg := ProviderConfig{
		Entities: map[string]map[string]any{
			"projectA/db": {
				"Password": "pass1",
				"Title":    "Database Prod",
				"tags":     "env:prod, type:db",
			},
			"projectA/web": {
				"Password": "pass2",
				"Title":    "Web Server Prod",
				"tags":     []any{"env:prod", "type:web"},
			},
			"projectB/db": {
				"Password": "pass3",
				"Title":    "Database Staging",
				"tags":     []string{"env:staging", "type:db"},
			},
		},
	}

	if err := p.Initialize(ctx, cfg); err != nil {
		t.Fatalf("failed to initialize: %v", err)
	}

	tests := []struct {
		name        string
		query       SearchQuery
		wantResults int
	}{
		{
			name:        "Empty query returns all",
			query:       SearchQuery{},
			wantResults: 3,
		},
		{
			name:        "Title exact match",
			query:       SearchQuery{Title: "Database Prod"},
			wantResults: 1,
		},
		{
			name:        "Title partial match case-insensitive",
			query:       SearchQuery{Title: "database"},
			wantResults: 2,
		},
		{
			name:        "Title mismatch",
			query:       SearchQuery{Title: "Cache"},
			wantResults: 0,
		},
		{
			name:        "Path exact match",
			query:       SearchQuery{Path: "projectA/db"},
			wantResults: 1,
		},
		{
			name:        "Path partial match case-insensitive",
			query:       SearchQuery{Path: "projecta"},
			wantResults: 2,
		},
		{
			name:        "Path mismatch",
			query:       SearchQuery{Path: "projectC"},
			wantResults: 0,
		},
		{
			name:        "Tags single match",
			query:       SearchQuery{Tags: []string{"env:prod"}},
			wantResults: 2,
		},
		{
			name:        "Tags multiple match",
			query:       SearchQuery{Tags: []string{"env:prod", "type:db"}},
			wantResults: 1,
		},
		{
			name:        "Tags partial mismatch",
			query:       SearchQuery{Tags: []string{"env:prod", "type:cache"}},
			wantResults: 0,
		},
		{
			name:        "Tags case-insensitive match",
			query:       SearchQuery{Tags: []string{"ENV:PROD"}},
			wantResults: 2,
		},
		{
			name:        "Combination match",
			query:       SearchQuery{Title: "database", Path: "projectb", Tags: []string{"env:staging"}},
			wantResults: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results, err := p.Search(ctx, tt.query)
			if err != nil {
				t.Fatalf("Search failed: %v", err)
			}
			if len(results) != tt.wantResults {
				t.Errorf("expected %d results, got %d", tt.wantResults, len(results))
			}
		})
	}
}

func TestCustomVaultProvider_SetSecret(t *testing.T) {
	p := NewCustomVaultProvider()
	err := p.SetSecret(context.Background(), "entity1:key", "val")
	if err == nil {
		t.Error("expected error for SetSecret, got nil")
	}
	if err != nil && !strings.Contains(err.Error(), "read-only") {
		t.Errorf("expected error to contain 'read-only', got %q", err.Error())
	}
}

func TestCustomVaultProvider_DeleteSecret(t *testing.T) {
	p := NewCustomVaultProvider()
	err := p.DeleteSecret(context.Background(), "entity1:key")
	if err == nil {
		t.Error("expected error for DeleteSecret, got nil")
	}
	if err != nil && !strings.Contains(err.Error(), "read-only") {
		t.Errorf("expected error to contain 'read-only', got %q", err.Error())
	}
}
