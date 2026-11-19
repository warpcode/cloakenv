package provider

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/warpcode/cloakenv/internal/config"
	"github.com/warpcode/cloakenv/internal/utils"
)

type mockKeyFormatProvider struct {
	mockSearchableProvider
}

func newMockKeyFormatProvider(entries map[string]Entry) *mockKeyFormatProvider {
	results := make([]SearchResult, 0, len(entries))
	for path, e := range entries {
		results = append(results, SearchResult{
			Provider: "mock",
			Vault:    "mock",
			Path:     path,
			Entry:    e,
		})
	}
	return &mockKeyFormatProvider{
		mockSearchableProvider: mockSearchableProvider{
			entries: entries,
			results: results,
		},
	}
}

func (m *mockKeyFormatProvider) GetSecret(_ context.Context, location string) (string, error) {
	entityLoc, attr, hasColon := strings.Cut(location, ":")
	entry, ok := m.entries[entityLoc]
	if !ok {
		return "", fmt.Errorf("entry %q not found", entityLoc)
	}
	if !hasColon {
		attr = "Password"
	}
	v, exists := entry.Attributes[attr]
	if !exists {
		return "", fmt.Errorf("attribute %q not found in %q", attr, entityLoc)
	}
	return fmt.Sprintf("%v", v), nil
}

func TestKeyFormatProvider_InterfacePreservation(t *testing.T) {
	mockProv := newMockKeyFormatProvider(map[string]Entry{
		"app": {Title: "app", Attributes: map[string]any{"api-key": "secret"}},
	})

	p := NewKeyFormatProvider(mockProv, utils.KeyFormatSnake)

	if _, ok := p.(SearchableProvider); !ok {
		t.Fatal("expected KeyFormatProvider to implement SearchableProvider")
	}
}

func TestKeyFormatProvider_GetEntryFormats(t *testing.T) {
	ctx := context.Background()

	attrs := map[string]any{
		"api-key":               "key123",
		"database.user_name":    "admin",
		"UserName":              "alice",
		"multiple___underscore": "val",
	}

	tests := []struct {
		name         string
		format       string
		expectedKeys []string
	}{
		{
			name:   "none format passes through unmodified",
			format: utils.KeyFormatNone,
			expectedKeys: []string{
				"api-key",
				"database.user_name",
				"UserName",
				"multiple___underscore",
			},
		},
		{
			name:   "env format",
			format: utils.KeyFormatEnv,
			expectedKeys: []string{
				"API_KEY",
				"DATABASE_USER_NAME",
				"USERNAME",
				"MULTIPLE_UNDERSCORE",
			},
		},
		{
			name:   "snake format",
			format: utils.KeyFormatSnake,
			expectedKeys: []string{
				"api_key",
				"database_user_name",
				"username",
				"multiple_underscore",
			},
		},
		{
			name:   "kebab format",
			format: utils.KeyFormatKebab,
			expectedKeys: []string{
				"api-key",
				"database-user-name",
				"username",
				"multiple-underscore",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockProv := newMockKeyFormatProvider(map[string]Entry{
				"app": {Title: "app", Attributes: attrs},
			})
			p := NewKeyFormatProvider(mockProv, tt.format)
			sp, ok := p.(SearchableProvider)
			if !ok {
				t.Fatalf("expected SearchableProvider")
			}

			entry, err := sp.GetEntry(ctx, "app")
			if err != nil {
				t.Fatalf("GetEntry failed: %v", err)
			}

			for _, expectedKey := range tt.expectedKeys {
				if _, exists := entry.Attributes[expectedKey]; !exists {
					t.Errorf("expected attribute %q to exist in attributes: %v", expectedKey, entry.Attributes)
				}
			}
		})
	}
}

func TestKeyFormatProvider_SearchFormats(t *testing.T) {
	ctx := context.Background()
	mockProv := newMockKeyFormatProvider(map[string]Entry{
		"app": {
			Title:      "app",
			Attributes: map[string]any{"database.user_name": "dbadmin"},
		},
	})
	p := NewKeyFormatProvider(mockProv, utils.KeyFormatKebab)
	sp := p.(SearchableProvider)

	results, err := sp.Search(ctx, SearchQuery{})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if _, ok := results[0].Entry.Attributes["database-user-name"]; !ok {
		t.Errorf("expected kebab-case key 'database-user-name', got: %v", results[0].Entry.Attributes)
	}
}

func TestKeyFormatProvider_GetSecretResolution(t *testing.T) {
	ctx := context.Background()
	mockProv := newMockKeyFormatProvider(map[string]Entry{
		"app": {
			Title: "app",
			Attributes: map[string]any{
				"api-key":  "secret123",
				"UserName": "admin_user",
			},
		},
	})
	p := NewKeyFormatProvider(mockProv, utils.KeyFormatSnake)

	val, err := p.GetSecret(ctx, "app:api_key")
	if err != nil {
		t.Fatalf("GetSecret(app:api_key) failed: %v", err)
	}
	if val != "secret123" {
		t.Errorf("expected secret123, got %q", val)
	}

	valOrig, errOrig := p.GetSecret(ctx, "app:api-key")
	if errOrig != nil {
		t.Fatalf("GetSecret(app:api-key) failed: %v", errOrig)
	}
	if valOrig != "secret123" {
		t.Errorf("expected secret123, got %q", valOrig)
	}

	valUser, errUser := p.GetSecret(ctx, "app:username")
	if errUser != nil {
		t.Fatalf("GetSecret(app:username) failed: %v", errUser)
	}
	if valUser != "admin_user" {
		t.Errorf("expected admin_user, got %q", valUser)
	}
}

func TestKeyFormatProvider_CompositionWithFilterAndMapping(t *testing.T) {
	ctx := context.Background()
	mockProv := newMockKeyFormatProvider(map[string]Entry{
		"app": {
			Title: "app",
			Attributes: map[string]any{
				"raw_password": "pass",
				"client_token": "token123",
				"hidden_field": "hidden",
			},
		},
	})

	fp := NewFilteringProvider(mockProv, nil, []string{"hidden_field"})
	mp, err := NewMappingProvider(fp, []config.MappingRule{
		{Match: `^raw_password$`, Key: "client.secret_key"},
	})
	if err != nil {
		t.Fatalf("NewMappingProvider failed: %v", err)
	}
	kp := NewKeyFormatProvider(mp, utils.KeyFormatKebab)
	sp := kp.(SearchableProvider)

	entry, err := sp.GetEntry(ctx, "app")
	if err != nil {
		t.Fatalf("GetEntry failed: %v", err)
	}
	if _, ok := entry.Attributes["client-secret-key"]; !ok {
		t.Errorf("expected 'client-secret-key' in attributes, got: %v", entry.Attributes)
	}
	if _, ok := entry.Attributes["client-token"]; !ok {
		t.Errorf("expected 'client-token' in attributes, got: %v", entry.Attributes)
	}
	if _, ok := entry.Attributes["hidden-field"]; ok {
		t.Errorf("hidden-field should have been filtered out")
	}
}
