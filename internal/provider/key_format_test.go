package provider

import (
	"context"
	"testing"

	"github.com/warpcode/cloakenv/internal/config"
	"github.com/warpcode/cloakenv/internal/utils"
)

func TestKeyFormatProvider_InterfacePreservation(t *testing.T) {
	cp := NewCustomVaultProvider()
	_ = cp.Initialize(context.Background(), ProviderConfig{
		Entities: config.ToCustomEntityMap(map[string]map[string]any{
			"app": {"api-key": "secret"},
		}),
	})

	// CustomVault implements both SearchableProvider and ValueResolvableProvider
	p := NewKeyFormatProvider(cp, utils.KeyFormatSnake)

	if _, ok := p.(SearchableProvider); !ok {
		t.Fatal("expected KeyFormatProvider to implement SearchableProvider")
	}
	if _, ok := p.(ValueResolvableProvider); !ok {
		t.Fatal("expected KeyFormatProvider to implement ValueResolvableProvider")
	}
}

func TestKeyFormatProvider_GetEntryFormats(t *testing.T) {
	ctx := context.Background()

	rawEntities := map[string]map[string]any{
		"app": {
			"api-key":               "key123",
			"database.user_name":    "admin",
			"UserName":              "alice",
			"multiple___underscore": "val",
		},
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
			cp := NewCustomVaultProvider()
			_ = cp.Initialize(ctx, ProviderConfig{
				Entities: config.ToCustomEntityMap(rawEntities),
			})

			p := NewKeyFormatProvider(cp, tt.format)
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
	cp := NewCustomVaultProvider()
	_ = cp.Initialize(ctx, ProviderConfig{
		Entities: config.ToCustomEntityMap(map[string]map[string]any{
			"app": {
				"database.user_name": "dbadmin",
			},
		}),
	})

	p := NewKeyFormatProvider(cp, utils.KeyFormatKebab)
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
	cp := NewCustomVaultProvider()
	_ = cp.Initialize(ctx, ProviderConfig{
		Entities: config.ToCustomEntityMap(map[string]map[string]any{
			"app": {
				"api-key":  "secret123",
				"UserName": "admin_user",
			},
		}),
	})

	p := NewKeyFormatProvider(cp, utils.KeyFormatSnake)

	// Direct lookup using formatted name
	val, err := p.GetSecret(ctx, "app:api_key")
	if err != nil {
		t.Fatalf("GetSecret(app:api_key) failed: %v", err)
	}
	if val != "secret123" {
		t.Errorf("expected secret123, got %q", val)
	}

	// Lookup using original name
	valOrig, errOrig := p.GetSecret(ctx, "app:api-key")
	if errOrig != nil {
		t.Fatalf("GetSecret(app:api-key) failed: %v", errOrig)
	}
	if valOrig != "secret123" {
		t.Errorf("expected secret123, got %q", valOrig)
	}

	// Lookup UserName via snake name username
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
	cp := NewCustomVaultProvider()
	_ = cp.Initialize(ctx, ProviderConfig{
		Entities: config.ToCustomEntityMap(map[string]map[string]any{
			"app": {
				"raw_password": "pass",
				"client_token": "token123",
				"hidden_field": "hidden",
			},
		}),
	})

	// 1. Filtering: exclude hidden_field
	fp := NewFilteringProvider(cp, nil, []string{"hidden_field"})

	// 2. Mapping: rename raw_password -> client.secret_key
	mp, err := NewMappingProvider(fp, []config.MappingRule{
		{Match: `^raw_password$`, Key: "client.secret_key"},
	})
	if err != nil {
		t.Fatalf("NewMappingProvider failed: %v", err)
	}

	// 3. Key formatting: kebab
	kp := NewKeyFormatProvider(mp, utils.KeyFormatKebab)

	sp := kp.(SearchableProvider)
	entry, err := sp.GetEntry(ctx, "app")
	if err != nil {
		t.Fatalf("GetEntry failed: %v", err)
	}

	// client.secret_key formatted as kebab -> client-secret-key
	if _, ok := entry.Attributes["client-secret-key"]; !ok {
		t.Errorf("expected 'client-secret-key' in attributes, got: %v", entry.Attributes)
	}
	// client_token formatted as kebab -> client-token
	if _, ok := entry.Attributes["client-token"]; !ok {
		t.Errorf("expected 'client-token' in attributes, got: %v", entry.Attributes)
	}
	// hidden_field was excluded by filter
	if _, ok := entry.Attributes["hidden-field"]; ok {
		t.Errorf("hidden-field should have been filtered out")
	}
}
