package provider

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"
)

func TestIsFieldAllowed(t *testing.T) {
	tests := []struct {
		name          string
		fieldName     string
		includeFields []string
		excludeFields []string
		want          bool
	}{
		{
			name:          "no filters allows all",
			fieldName:     "anyField",
			includeFields: nil,
			excludeFields: nil,
			want:          true,
		},
		{
			name:          "include match",
			fieldName:     "env:PROD",
			includeFields: []string{"env:*", "UserName"},
			excludeFields: nil,
			want:          true,
		},
		{
			name:          "include no match",
			fieldName:     "Password",
			includeFields: []string{"env:*", "UserName"},
			excludeFields: nil,
			want:          false,
		},
		{
			name:          "exclude match",
			fieldName:     "db.notes",
			includeFields: nil,
			excludeFields: []string{"*.notes", "secret:*"},
			want:          false,
		},
		{
			name:          "exclude no match",
			fieldName:     "db.username",
			includeFields: nil,
			excludeFields: []string{"*.notes", "secret:*"},
			want:          true,
		},
		{
			name:          "both include match and exclude match drops field",
			fieldName:     "env:notes.notes",
			includeFields: []string{"env:*"},
			excludeFields: []string{"*.notes"},
			want:          false,
		},
		{
			name:          "both include match and exclude no match keeps field",
			fieldName:     "env:HOST",
			includeFields: []string{"env:*"},
			excludeFields: []string{"*.notes"},
			want:          true,
		},
		{
			name:          "exact match for include and exclude",
			fieldName:     "UserName",
			includeFields: []string{"UserName"},
			excludeFields: []string{"Password"},
			want:          true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsFieldAllowed(tt.fieldName, tt.includeFields, tt.excludeFields)
			if got != tt.want {
				t.Errorf("IsFieldAllowed(%q, %v, %v) = %v, want %v",
					tt.fieldName, tt.includeFields, tt.excludeFields, got, tt.want)
			}
		})
	}
}

func TestFilterAttributesAndEntry(t *testing.T) {
	attrs := map[string]any{
		"env:STAGE":    "prod",
		"UserName":     "alice",
		"Password":     "secret123",
		"db.notes":     "some notes",
		"secret:token": "bearerABC",
	}

	includeFields := []string{"env:*", "UserName"}
	excludeFields := []string{"*.notes", "secret:*"}

	filtered := FilterAttributes(attrs, includeFields, excludeFields)

	if _, ok := filtered["env:STAGE"]; !ok {
		t.Errorf("expected 'env:STAGE' to be kept")
	}
	if _, ok := filtered["UserName"]; !ok {
		t.Errorf("expected 'UserName' to be kept")
	}
	if _, ok := filtered["Password"]; ok {
		t.Errorf("expected 'Password' to be filtered out")
	}
	if _, ok := filtered["db.notes"]; ok {
		t.Errorf("expected 'db.notes' to be filtered out")
	}
	if _, ok := filtered["secret:token"]; ok {
		t.Errorf("expected 'secret:token' to be filtered out")
	}

	entry := Entry{
		Title:      "TestEntry",
		Tags:       []string{"tag1"},
		Attributes: attrs,
	}

	filteredEntry := FilterEntry(entry, includeFields, excludeFields)
	if filteredEntry.Title != "TestEntry" {
		t.Errorf("Title = %q, want %q", filteredEntry.Title, "TestEntry")
	}
	if len(filteredEntry.Attributes) != 2 {
		t.Errorf("Attributes length = %d, want 2", len(filteredEntry.Attributes))
	}
}

func TestFilteringProvider(t *testing.T) {
	ctx := context.Background()

	rawProvider := NewCustomVaultProvider()
	err := rawProvider.Initialize(ctx, ProviderConfig{
		Settings: map[string]string{
			"vault_name": "work",
		},
		Entities: map[string]map[string]any{
			"db_prod": {
				"UserName":     "admin",
				"Password":     "supersecret",
				"env:HOST":     "db.internal",
				"secret:token": "tok123",
				"db.notes":     "production database",
			},
		},
	})
	if err != nil {
		t.Fatalf("failed to initialize CustomVaultProvider: %v", err)
	}

	includeFields := []string{"env:*", "UserName"}
	excludeFields := []string{"*.notes", "secret:*"}

	fp := NewFilteringProvider(rawProvider, includeFields, excludeFields)

	if fp.Scheme() != "custom_vault" {
		t.Errorf("Scheme() = %q, want %q", fp.Scheme(), "custom_vault")
	}

	vr, ok := fp.(ValueResolvableProvider)
	if !ok || !vr.SupportsValueResolution() {
		t.Errorf("expected SupportsValueResolution() = true")
	}

	searchable, ok := fp.(SearchableProvider)
	if !ok {
		t.Fatalf("expected SearchableProvider implementation")
	}

	t.Run("GetEntry", func(t *testing.T) {
		entry, err := searchable.GetEntry(ctx, "db_prod")
		if err != nil {
			t.Fatalf("GetEntry failed: %v", err)
		}

		if len(entry.Attributes) != 2 {
			t.Fatalf("expected 2 attributes after filtering, got %d (%v)", len(entry.Attributes), entry.Attributes)
		}

		if entry.Attributes["UserName"] != "admin" {
			t.Errorf("UserName = %v, want 'admin'", entry.Attributes["UserName"])
		}

		if entry.Attributes["env:HOST"] != "db.internal" {
			t.Errorf("env:HOST = %v, want 'db.internal'", entry.Attributes["env:HOST"])
		}

		if _, ok := entry.Attributes["Password"]; ok {
			t.Errorf("Password should be filtered out")
		}
	})

	t.Run("Search", func(t *testing.T) {
		results, err := searchable.Search(ctx, SearchQuery{})
		if err != nil {
			t.Fatalf("Search failed: %v", err)
		}

		if len(results) != 1 {
			t.Fatalf("expected 1 result, got %d", len(results))
		}

		resEntry := results[0].Entry
		if len(resEntry.Attributes) != 2 {
			t.Fatalf("expected 2 attributes in search result, got %d", len(resEntry.Attributes))
		}
	})

	t.Run("GetSecret_AllowedFields", func(t *testing.T) {
		val, err := fp.GetSecret(ctx, "db_prod:UserName")
		if err != nil {
			t.Fatalf("GetSecret UserName failed: %v", err)
		}
		if val != "admin" {
			t.Errorf("GetSecret UserName = %q, want 'admin'", val)
		}

		valHost, err := fp.GetSecret(ctx, "db_prod:env:HOST")
		if err != nil {
			t.Fatalf("GetSecret env:HOST failed: %v", err)
		}
		if valHost != "db.internal" {
			t.Errorf("GetSecret env:HOST = %q, want 'db.internal'", valHost)
		}
	})

	t.Run("GetSecret_FilteredFields", func(t *testing.T) {
		_, err := fp.GetSecret(ctx, "db_prod:Password")
		if err == nil {
			t.Fatalf("expected error for filtered field Password, got nil")
		}
		if !strings.Contains(err.Error(), "excluded") && !strings.Contains(err.Error(), "not found") {
			t.Errorf("unexpected error message: %v", err)
		}

		_, errToken := fp.GetSecret(ctx, "db_prod:secret:token")
		if errToken == nil {
			t.Fatalf("expected error for filtered field secret:token, got nil")
		}

		_, errNotes := fp.GetSecret(ctx, "db_prod:db.notes")
		if errNotes == nil {
			t.Fatalf("expected error for filtered field db.notes, got nil")
		}
	})
}

func TestFilteringProvider_SingleEntityYAML(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	yamlPath := filepath.Join(tmpDir, "single.yaml")

	content := `
api_key: "my_api_key_val"
db_password: "my_db_password_val"
username: "app_user"
`
	if err := os.WriteFile(yamlPath, []byte(content), 0600); err != nil {
		t.Fatalf("failed to write yaml fixture: %v", err)
	}

	isSingle := true
	p := NewYamlProvider()
	if err := p.Initialize(ctx, ProviderConfig{
		Settings: map[string]string{
			"vault_path": yamlPath,
		},
		SingleEntity: &isSingle,
	}); err != nil {
		t.Fatalf("failed to init YAML provider: %v", err)
	}

	t.Run("AllowedAttributeWithoutColon", func(t *testing.T) {
		fp := NewFilteringProvider(p, []string{"api_key", "username"}, []string{"db_password"})
		val, err := fp.GetSecret(ctx, "api_key")
		if err != nil {
			t.Fatalf("GetSecret(api_key) failed: %v", err)
		}
		if val != "my_api_key_val" {
			t.Errorf("GetSecret(api_key) = %q, want 'my_api_key_val'", val)
		}
	})

	t.Run("ExcludedAttributeWithoutColon", func(t *testing.T) {
		fp := NewFilteringProvider(p, []string{"api_key", "username"}, []string{"db_password"})
		_, err := fp.GetSecret(ctx, "db_password")
		if err == nil {
			t.Fatalf("expected error for excluded db_password, got nil")
		}
		if !strings.Contains(err.Error(), "excluded") {
			t.Errorf("unexpected error message: %v", err)
		}
	})

	t.Run("ExplicitlyExcludedFieldWithoutColon", func(t *testing.T) {
		fp := NewFilteringProvider(p, nil, []string{"api_key"})
		_, err := fp.GetSecret(ctx, "api_key")
		if err == nil {
			t.Fatalf("expected error for excluded api_key, got nil")
		}
		if !strings.Contains(err.Error(), "excluded") {
			t.Errorf("unexpected error message: %v", err)
		}
	})
}

func TestFilteringProvider_MultiEntityYAML_DotPath(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	yamlPath := filepath.Join(tmpDir, "multi.yaml")

	content := `
entities:
  db:
    username: "db_admin"
    password: "db_secret_pw"
    notes: "db notes"
`
	if err := os.WriteFile(yamlPath, []byte(content), 0600); err != nil {
		t.Fatalf("failed to write yaml fixture: %v", err)
	}

	p := NewYamlProvider()
	if err := p.Initialize(ctx, ProviderConfig{
		Settings: map[string]string{
			"vault_path": yamlPath,
		},
		EntitiesRootKey: "entities",
	}); err != nil {
		t.Fatalf("failed to init YAML provider: %v", err)
	}

	fp := NewFilteringProvider(p, nil, []string{"password", "*.notes"})

	t.Run("ExcludedLeafDotPath", func(t *testing.T) {
		_, err := fp.GetSecret(ctx, "entities.db.password")
		if err == nil {
			t.Fatalf("expected error for excluded entities.db.password, got nil")
		}
		if !strings.Contains(err.Error(), "excluded") {
			t.Errorf("unexpected error message: %v", err)
		}
	})

	t.Run("ExcludedPatternDotPath", func(t *testing.T) {
		_, err := fp.GetSecret(ctx, "entities.db.notes")
		if err == nil {
			t.Fatalf("expected error for excluded entities.db.notes, got nil")
		}
		if !strings.Contains(err.Error(), "excluded") {
			t.Errorf("unexpected error message: %v", err)
		}
	})

	t.Run("AllowedDotPath", func(t *testing.T) {
		val, err := fp.GetSecret(ctx, "entities.db.username")
		if err != nil {
			t.Fatalf("GetSecret(entities.db.username) failed: %v", err)
		}
		if val != "db_admin" {
			t.Errorf("GetSecret(entities.db.username) = %q, want 'db_admin'", val)
		}
	})
}

func TestFilteringProvider_BareLocation_Password(t *testing.T) {
	ctx := context.Background()
	rawProvider := NewCustomVaultProvider()
	err := rawProvider.Initialize(ctx, ProviderConfig{
		Settings: map[string]string{"vault_name": "work"},
		Entities: map[string]map[string]any{
			"db_prod": {
				"UserName": "admin",
				"Password": "supersecret",
			},
		},
	})
	if err != nil {
		t.Fatalf("failed to initialize CustomVaultProvider: %v", err)
	}

	t.Run("PasswordExcluded_BareLocationFails", func(t *testing.T) {
		fp := NewFilteringProvider(rawProvider, []string{"UserName"}, nil)
		_, err := fp.GetSecret(ctx, "db_prod")
		if err == nil {
			t.Fatalf("expected error when Password excluded by include list, got nil")
		}
		if !strings.Contains(err.Error(), "excluded") {
			t.Errorf("unexpected error message: %v", err)
		}
	})

	t.Run("PasswordAllowed_BareLocationSucceeds", func(t *testing.T) {
		fp := NewFilteringProvider(rawProvider, []string{"Password", "UserName"}, nil)
		val, err := fp.GetSecret(ctx, "db_prod")
		if err != nil {
			t.Fatalf("GetSecret(db_prod) failed: %v", err)
		}
		if val != "supersecret" {
			t.Errorf("GetSecret(db_prod) = %q, want 'supersecret'", val)
		}
	})
}

func TestFilteringProvider_SearchVault(t *testing.T) {
	ctx := context.Background()
	sp := NewSearchProvider()
	sp.SetSearchExecutor(func(ctx context.Context, query string, sourceVaults []string, depth int) ([]SearchResult, error) {
		return []SearchResult{
			{
				Vault: "source_vault",
				Path:  "app/config",
				Entry: Entry{
					Title: "app_config",
					Attributes: map[string]any{
						"api_key":  "search_api_key_123",
						"Password": "search_password_abc",
						"UserName": "search_user",
					},
				},
			},
		}, nil
	})

	if err := sp.Initialize(ctx, ProviderConfig{
		Settings: map[string]string{
			"vault_name": "filtered_search",
			"query":      "app",
		},
	}); err != nil {
		t.Fatalf("failed to init SearchProvider: %v", err)
	}

	fp := NewFilteringProvider(sp, []string{"api_key", "UserName"}, []string{"Password"})

	t.Run("SingleResult_AllowedAttribute", func(t *testing.T) {
		val, err := fp.GetSecret(ctx, "api_key")
		if err != nil {
			t.Fatalf("GetSecret(api_key) failed: %v", err)
		}
		if val != "search_api_key_123" {
			t.Errorf("GetSecret(api_key) = %q, want 'search_api_key_123'", val)
		}
	})

	t.Run("SingleResult_ExcludedPassword", func(t *testing.T) {
		_, err := fp.GetSecret(ctx, "Password")
		if err == nil {
			t.Fatalf("expected error for excluded Password, got nil")
		}
		if !strings.Contains(err.Error(), "excluded") {
			t.Errorf("unexpected error message: %v", err)
		}
	})

	t.Run("SingleResult_DefaultPasswordAccessBlocked", func(t *testing.T) {
		_, err := fp.GetSecret(ctx, "default")
		if err == nil {
			t.Fatalf("expected error for default password lookup when Password excluded, got nil")
		}
		if !strings.Contains(err.Error(), "excluded") {
			t.Errorf("unexpected error message: %v", err)
		}
	})
}

func TestFilteringProvider_InterfaceShape(t *testing.T) {
	ctx := context.Background()

	// 1. CustomVaultProvider implements ValueResolvableProvider -> wrapper must implement it
	cv := NewCustomVaultProvider()
	_ = cv.Initialize(ctx, ProviderConfig{
		Settings: map[string]string{"vault_name": "test"},
		Entities: map[string]map[string]any{"e": {"k": "v"}},
	})
	fpCV := NewFilteringProvider(cv, nil, nil)
	vrCV, ok := fpCV.(ValueResolvableProvider)
	if !ok {
		t.Fatalf("expected fpCV to implement ValueResolvableProvider")
	}
	if !vrCV.SupportsValueResolution() {
		t.Errorf("expected SupportsValueResolution() = true")
	}

	// 2. YamlProvider does NOT implement ValueResolvableProvider -> wrapper must NOT implement it
	yp := NewYamlProvider()
	fpYP := NewFilteringProvider(yp, nil, nil)
	if _, ok := fpYP.(ValueResolvableProvider); ok {
		t.Errorf("expected fpYP to NOT implement ValueResolvableProvider")
	}

	// 3. KeePassProvider does NOT implement ValueResolvableProvider -> wrapper must NOT implement it
	kp := NewKeePassProvider()
	fpKP := NewFilteringProvider(kp, nil, nil)
	if _, ok := fpKP.(ValueResolvableProvider); ok {
		t.Errorf("expected fpKP to NOT implement ValueResolvableProvider")
	}
}

func TestFilteringProvider_KeePass_ColonEmptySelector(t *testing.T) {
	keyring.MockInit()
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Setenv("LocalAppData", t.TempDir())

	ctx := context.Background()
	if err := keyring.Set("cloakenv", "provider/testdb", "password123"); err != nil {
		t.Fatalf("failed to set mock credentials: %v", err)
	}

	kp := NewKeePassProvider()
	err := kp.Initialize(ctx, ProviderConfig{
		Settings: map[string]string{
			"vault_path":  "../../testdata/testDB.kdbx",
			"remote_name": "testdb",
		},
	})
	if err != nil {
		t.Fatalf("failed to init KeePass provider: %v", err)
	}

	fp := NewFilteringProvider(kp, []string{"UserName"}, []string{"Password"})

	// Empty selector after colon "website/Test Website:" defaults to "Password"
	_, errEmpty := fp.GetSecret(ctx, "website/Test Website:")
	if errEmpty == nil {
		t.Fatalf("expected error for empty selector when Password excluded, got nil")
	}
	if !strings.Contains(errEmpty.Error(), "excluded") {
		t.Errorf("unexpected error message: %v", errEmpty)
	}

	// Explicit "website/Test Website:Password"
	_, errPass := fp.GetSecret(ctx, "website/Test Website:Password")
	if errPass == nil {
		t.Fatalf("expected error for Password, got nil")
	}

	// Bare location "website/Test Website" defaults to "Password"
	_, errBare := fp.GetSecret(ctx, "website/Test Website")
	if errBare == nil {
		t.Fatalf("expected error for bare location, got nil")
	}

	// Allowed selector "website/Test Website:UserName"
	valUser, errUser := fp.GetSecret(ctx, "website/Test Website:UserName")
	if errUser != nil {
		t.Fatalf("failed to get UserName: %v", errUser)
	}
	if valUser != "user@email.com" {
		t.Errorf("UserName = %q, want 'user@email.com'", valUser)
	}
}

func TestFilteringProvider_Search_CaseInsensitiveBypass(t *testing.T) {
	ctx := context.Background()
	sp := NewSearchProvider()
	sp.SetSearchExecutor(func(ctx context.Context, query string, sourceVaults []string, depth int) ([]SearchResult, error) {
		return []SearchResult{
			{
				Vault: "source_vault",
				Path:  "app/config",
				Entry: Entry{
					Title: "app_config",
					Attributes: map[string]any{
						"api_key":  "search_api_key_123",
						"Password": "super_secret_pw",
					},
				},
			},
		}, nil
	})

	if err := sp.Initialize(ctx, ProviderConfig{
		Settings: map[string]string{
			"vault_name": "filtered_search",
			"query":      "app",
		},
	}); err != nil {
		t.Fatalf("failed to init SearchProvider: %v", err)
	}

	fp := NewFilteringProvider(sp, nil, []string{"Password"})

	// Lowercase "password" should be matched against canonical "Password" and excluded
	_, errLower := fp.GetSecret(ctx, "app_config:password")
	if errLower == nil {
		t.Fatalf("expected error for lowercase password when Password excluded, got nil")
	}
	if !strings.Contains(errLower.Error(), "excluded") {
		t.Errorf("unexpected error message: %v", errLower)
	}

	// Uppercase "PASSWORD"
	_, errUpper := fp.GetSecret(ctx, "app_config:PASSWORD")
	if errUpper == nil {
		t.Fatalf("expected error for uppercase PASSWORD when Password excluded, got nil")
	}

	// Allowed attribute
	val, err := fp.GetSecret(ctx, "app_config:api_key")
	if err != nil {
		t.Fatalf("failed to get api_key: %v", err)
	}
	if val != "search_api_key_123" {
		t.Errorf("api_key = %q, want 'search_api_key_123'", val)
	}
}

func TestFilteringProvider_YAML_ColonInKey(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	yamlPath := filepath.Join(tmpDir, "colon_key.yaml")

	content := `
env:TOKEN: "token_value_abc"
Password: "secret_password"
`
	if err := os.WriteFile(yamlPath, []byte(content), 0600); err != nil {
		t.Fatalf("failed to write yaml fixture: %v", err)
	}

	isSingle := true
	p := NewYamlProvider()
	if err := p.Initialize(ctx, ProviderConfig{
		Settings: map[string]string{
			"vault_path": yamlPath,
		},
		SingleEntity: &isSingle,
	}); err != nil {
		t.Fatalf("failed to init YAML provider: %v", err)
	}

	fp := NewFilteringProvider(p, []string{"env:*"}, []string{"Password"})

	val, err := fp.GetSecret(ctx, "env:TOKEN")
	if err != nil {
		t.Fatalf("failed to get env:TOKEN: %v", err)
	}
	if val != "token_value_abc" {
		t.Errorf("env:TOKEN = %q, want 'token_value_abc'", val)
	}

	_, errPass := fp.GetSecret(ctx, "Password")
	if errPass == nil {
		t.Fatalf("expected error for excluded Password, got nil")
	}
}

func TestFilteringProvider_JSON_RootKeyAndDotPath(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	jsonPath := filepath.Join(tmpDir, "multi.json")

	content := `{
  "entities": {
    "db": {
      "username": "db_admin",
      "password": "json_secret_pw"
    }
  }
}`
	if err := os.WriteFile(jsonPath, []byte(content), 0600); err != nil {
		t.Fatalf("failed to write json fixture: %v", err)
	}

	p := NewJsonProvider()
	if err := p.Initialize(ctx, ProviderConfig{
		Settings: map[string]string{
			"vault_path": jsonPath,
		},
		EntitiesRootKey: "entities",
	}); err != nil {
		t.Fatalf("failed to init JSON provider: %v", err)
	}

	fp := NewFilteringProvider(p, nil, []string{"password"})

	// Excluded path with root key
	_, err := fp.GetSecret(ctx, "entities.db.password")
	if err == nil {
		t.Fatalf("expected error for excluded entities.db.password, got nil")
	}
	if !strings.Contains(err.Error(), "excluded") {
		t.Errorf("unexpected error message: %v", err)
	}

	// Excluded path without root key
	_, errNoRoot := fp.GetSecret(ctx, "db.password")
	if errNoRoot == nil {
		t.Fatalf("expected error for excluded db.password, got nil")
	}

	// Allowed path with root key
	val, err := fp.GetSecret(ctx, "entities.db.username")
	if err != nil {
		t.Fatalf("failed to get entities.db.username: %v", err)
	}
	if val != "db_admin" {
		t.Errorf("username = %q, want 'db_admin'", val)
	}

	// Entire entity JSON serialized without password
	valEntity, err := fp.GetSecret(ctx, "entities.db")
	if err != nil {
		t.Fatalf("failed to get entities.db: %v", err)
	}
	if strings.Contains(valEntity, "json_secret_pw") {
		t.Errorf("entities.db exposed excluded password: %s", valEntity)
	}
	if !strings.Contains(valEntity, "db_admin") {
		t.Errorf("entities.db missing username: %s", valEntity)
	}
}

func TestFilteringProvider_Search_DuplicatePath_MultiResult_MixedCase(t *testing.T) {
	ctx := context.Background()

	t.Run("DuplicatePath_FieldExclusionOnCanonicalKey", func(t *testing.T) {
		// Two results with identical Path "app/config".
		// First result lacks the requested field, second result supplies it.
		sp := NewSearchProvider()
		sp.searchExecutor = func(ctx context.Context, query string, sourceVaults []string, depth int) ([]SearchResult, error) {
			return []SearchResult{
				{
					Vault: "source_vault",
					Path:  "app/config",
					Entry: Entry{
						Title: "first_entry",
						Attributes: map[string]any{
							"other_field": "val1",
						},
					},
				},
				{
					Vault: "source_vault",
					Path:  "app/config",
					Entry: Entry{
						Title: "second_entry",
						Attributes: map[string]any{
							"api_key":  "secret_api_key",
							"Password": "secret_password",
						},
					},
				},
			}, nil
		}

		if err := sp.Initialize(ctx, ProviderConfig{
			Settings: map[string]string{
				"vault_name": "filtered_search",
				"query":      "app",
			},
		}); err != nil {
			t.Fatalf("failed to init SearchProvider: %v", err)
		}

		fp := NewFilteringProvider(sp, nil, []string{"Password"})

		// 1. Bare location defaults to Password; second result has Password, which must be excluded
		_, errBare := fp.GetSecret(ctx, "app/config")
		if errBare == nil {
			t.Fatalf("expected error for excluded Password on bare location, got nil")
		}
		if !strings.Contains(errBare.Error(), "excluded") {
			t.Errorf("unexpected error: %v", errBare)
		}

		// 2. Case-insensitive attribute query "password" resolves to second result's canonical key "Password"
		_, errCase := fp.GetSecret(ctx, "app/config:password")
		if errCase == nil {
			t.Fatalf("expected error for excluded Password via case-insensitive query, got nil")
		}
		if !strings.Contains(errCase.Error(), "excluded") {
			t.Errorf("unexpected error: %v", errCase)
		}

		// 3. Allowed field in second result is returned
		val, err := fp.GetSecret(ctx, "app/config:api_key")
		if err != nil {
			t.Fatalf("failed to get api_key: %v", err)
		}
		if val != "secret_api_key" {
			t.Errorf("api_key = %q, want 'secret_api_key'", val)
		}
	})

	t.Run("MultiResult_DifferentPaths", func(t *testing.T) {
		sp := NewSearchProvider()
		sp.searchExecutor = func(ctx context.Context, query string, sourceVaults []string, depth int) ([]SearchResult, error) {
			return []SearchResult{
				{
					Vault: "source_vault",
					Path:  "services/auth",
					Entry: Entry{
						Title: "auth_service",
						Attributes: map[string]any{
							"token": "auth_token_123",
						},
					},
				},
				{
					Vault: "source_vault",
					Path:  "services/db",
					Entry: Entry{
						Title: "db_service",
						Attributes: map[string]any{
							"secret": "db_secret_456",
						},
					},
				},
			}, nil
		}

		if err := sp.Initialize(ctx, ProviderConfig{
			Settings: map[string]string{
				"vault_name": "filtered_search",
				"query":      "services",
			},
		}); err != nil {
			t.Fatalf("failed to init SearchProvider: %v", err)
		}

		fp := NewFilteringProvider(sp, nil, []string{"secret"})

		// Allowed in first result
		val, err := fp.GetSecret(ctx, "services/auth:token")
		if err != nil {
			t.Fatalf("failed to get token: %v", err)
		}
		if val != "auth_token_123" {
			t.Errorf("token = %q, want 'auth_token_123'", val)
		}

		// Excluded in second result
		_, errSec := fp.GetSecret(ctx, "services/db:secret")
		if errSec == nil {
			t.Fatalf("expected error for excluded secret, got nil")
		}
	})

	t.Run("MixedCase_DeterministicResolution", func(t *testing.T) {
		sp := NewSearchProvider()
		sp.searchExecutor = func(ctx context.Context, query string, sourceVaults []string, depth int) ([]SearchResult, error) {
			return []SearchResult{
				{
					Vault: "source_vault",
					Path:  "app/config",
					Entry: Entry{
						Title: "app_config",
						Attributes: map[string]any{
							"API_KEY": "val_upper",
							"api_key": "val_lower",
						},
					},
				},
			}, nil
		}

		if err := sp.Initialize(ctx, ProviderConfig{
			Settings: map[string]string{
				"vault_name": "filtered_search",
				"query":      "app",
			},
		}); err != nil {
			t.Fatalf("failed to init SearchProvider: %v", err)
		}

		// Exact match takes precedence
		fp := NewFilteringProvider(sp, []string{"api_key"}, nil)
		valExact, err := fp.GetSecret(ctx, "app/config:api_key")
		if err != nil {
			t.Fatalf("failed to get exact api_key: %v", err)
		}
		if valExact != "val_lower" {
			t.Errorf("expected 'val_lower', got %q", valExact)
		}

		// Case-insensitive query resolves deterministically to the sorted key
		_, errUpper := fp.GetSecret(ctx, "app/config:API_KEY")
		if errUpper == nil {
			t.Errorf("API_KEY should be rejected when only api_key is included")
		}
	})
}

func TestFilteringProvider_Static_FullPath_RootPrefixedAndRootless(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	jsonPath := filepath.Join(tmpDir, "db.json")

	content := `{
  "entities": {
    "db": {
      "username": "db_admin",
      "password": "json_secret_pw",
      "port": 5432
    }
  }
}`
	if err := os.WriteFile(jsonPath, []byte(content), 0600); err != nil {
		t.Fatalf("failed to write json fixture: %v", err)
	}

	initProvider := func() SecretProvider {
		p := NewJsonProvider()
		if err := p.Initialize(ctx, ProviderConfig{
			Settings: map[string]string{
				"vault_path": jsonPath,
			},
			EntitiesRootKey: "entities",
		}); err != nil {
			t.Fatalf("failed to init JSON provider: %v", err)
		}
		return p
	}

	t.Run("Exclude_FullPath_RootPrefixedRule", func(t *testing.T) {
		// exclude_fields has the full canonical path "entities.db.password"
		p := initProvider()
		fp := NewFilteringProvider(p, nil, []string{"entities.db.password"})

		// Root-prefixed URI must be excluded
		_, errPrefixed := fp.GetSecret(ctx, "entities.db.password")
		if errPrefixed == nil {
			t.Fatalf("expected error for excluded entities.db.password, got nil")
		}

		// Rootless URI alias must also be excluded (bypassing via rootless alias is blocked)
		_, errRootless := fp.GetSecret(ctx, "db.password")
		if errRootless == nil {
			t.Fatalf("expected error for excluded db.password, got nil")
		}

		// Allowed field accessible via both forms
		val1, err := fp.GetSecret(ctx, "entities.db.username")
		if err != nil {
			t.Fatalf("failed to get entities.db.username: %v", err)
		}
		if val1 != "db_admin" {
			t.Errorf("username = %q, want 'db_admin'", val1)
		}

		val2, err := fp.GetSecret(ctx, "db.username")
		if err != nil {
			t.Fatalf("failed to get db.username: %v", err)
		}
		if val2 != "db_admin" {
			t.Errorf("username = %q, want 'db_admin'", val2)
		}
	})

	t.Run("Include_FullPath_RootPrefixedRule", func(t *testing.T) {
		// include_fields has the full canonical path "entities.db.username"
		p := initProvider()
		fp := NewFilteringProvider(p, []string{"entities.db.username"}, nil)

		// Root-prefixed URI is allowed
		val1, err := fp.GetSecret(ctx, "entities.db.username")
		if err != nil {
			t.Fatalf("failed to get entities.db.username: %v", err)
		}
		if val1 != "db_admin" {
			t.Errorf("username = %q, want 'db_admin'", val1)
		}

		// Rootless alias must also be allowed (not falsely rejected)
		val2, err := fp.GetSecret(ctx, "db.username")
		if err != nil {
			t.Fatalf("failed to get db.username: %v", err)
		}
		if val2 != "db_admin" {
			t.Errorf("username = %q, want 'db_admin'", val2)
		}

		// Other fields excluded via both forms
		if _, err := fp.GetSecret(ctx, "entities.db.password"); err == nil {
			t.Fatalf("expected error for password, got nil")
		}
		if _, err := fp.GetSecret(ctx, "db.password"); err == nil {
			t.Fatalf("expected error for password, got nil")
		}
	})

	t.Run("Exclude_RootlessFullPathRule", func(t *testing.T) {
		// exclude_fields has the rootless path "db.password"
		p := initProvider()
		fp := NewFilteringProvider(p, nil, []string{"db.password"})

		// Both forms must be excluded
		if _, err := fp.GetSecret(ctx, "entities.db.password"); err == nil {
			t.Fatalf("expected error for entities.db.password, got nil")
		}
		if _, err := fp.GetSecret(ctx, "db.password"); err == nil {
			t.Fatalf("expected error for db.password, got nil")
		}
	})

	t.Run("NormalizeEmptyPathComponents", func(t *testing.T) {
		p := initProvider()
		fp := NewFilteringProvider(p, nil, []string{"entities.db.password"})

		// Consecutive dots or leading/trailing dots must not bypass exclusion
		if _, err := fp.GetSecret(ctx, "entities..db.password"); err == nil {
			t.Fatalf("expected error for entities..db.password, got nil")
		}
		if _, err := fp.GetSecret(ctx, ".db.password"); err == nil {
			t.Fatalf("expected error for .db.password, got nil")
		}

		// Allowed fields with redundant dots are normalized and resolve properly
		val1, err := fp.GetSecret(ctx, "entities..db.username")
		if err != nil {
			t.Fatalf("failed to get entities..db.username: %v", err)
		}
		if val1 != "db_admin" {
			t.Errorf("username = %q, want 'db_admin'", val1)
		}

		val2, err := fp.GetSecret(ctx, ".db.username.")
		if err != nil {
			t.Fatalf("failed to get .db.username.: %v", err)
		}
		if val2 != "db_admin" {
			t.Errorf("username = %q, want 'db_admin'", val2)
		}
	})

	t.Run("EntityMap_FullPathFiltering", func(t *testing.T) {
		// exclude_fields has full path "entities.db.password"
		p := initProvider()
		fp := NewFilteringProvider(p, nil, []string{"entities.db.password"})

		// Fetching entities.db or db should omit password from the serialized map
		valEntity1, err := fp.GetSecret(ctx, "entities.db")
		if err != nil {
			t.Fatalf("failed to get entities.db: %v", err)
		}
		if strings.Contains(valEntity1, "json_secret_pw") {
			t.Errorf("entities.db exposed excluded password: %s", valEntity1)
		}
		if !strings.Contains(valEntity1, "db_admin") {
			t.Errorf("entities.db missing username: %s", valEntity1)
		}

		valEntity2, err := fp.GetSecret(ctx, "db")
		if err != nil {
			t.Fatalf("failed to get db: %v", err)
		}
		if strings.Contains(valEntity2, "json_secret_pw") {
			t.Errorf("db exposed excluded password: %s", valEntity2)
		}
		if !strings.Contains(valEntity2, "db_admin") {
			t.Errorf("db missing username: %s", valEntity2)
		}
	})

	t.Run("EntityMap_AggregateInclude_RootPrefixedAndRootless", func(t *testing.T) {
		// include_fields has leaf "username"
		p := initProvider()
		fp := NewFilteringProvider(p, []string{"username"}, nil)

		// Root-prefixed container entities.db must include username and exclude password
		valEntity1, err := fp.GetSecret(ctx, "entities.db")
		if err != nil {
			t.Fatalf("failed to get entities.db: %v", err)
		}
		if !strings.Contains(valEntity1, "db_admin") {
			t.Errorf("entities.db missing username: %s", valEntity1)
		}
		if strings.Contains(valEntity1, "json_secret_pw") {
			t.Errorf("entities.db exposed non-included password: %s", valEntity1)
		}

		// Rootless container db must also include username and exclude password
		valEntity2, err := fp.GetSecret(ctx, "db")
		if err != nil {
			t.Fatalf("failed to get db: %v", err)
		}
		if !strings.Contains(valEntity2, "db_admin") {
			t.Errorf("db missing username: %s", valEntity2)
		}
		if strings.Contains(valEntity2, "json_secret_pw") {
			t.Errorf("db exposed non-included password: %s", valEntity2)
		}

		// Full-path include rules: "entities.db.username" and "db.username"
		fpFullPath := NewFilteringProvider(p, []string{"entities.db.username"}, nil)
		valFullPath1, err := fpFullPath.GetSecret(ctx, "entities.db")
		if err != nil {
			t.Fatalf("failed to get entities.db with full-path include: %v", err)
		}
		if !strings.Contains(valFullPath1, "db_admin") || strings.Contains(valFullPath1, "json_secret_pw") {
			t.Errorf("unexpected output with full-path include: %s", valFullPath1)
		}

		valFullPath2, err := fpFullPath.GetSecret(ctx, "db")
		if err != nil {
			t.Fatalf("failed to get db with full-path include: %v", err)
		}
		if !strings.Contains(valFullPath2, "db_admin") || strings.Contains(valFullPath2, "json_secret_pw") {
			t.Errorf("unexpected output with full-path include: %s", valFullPath2)
		}
	})
}

func TestFilteringProvider_Search_SinglePassConsistency(t *testing.T) {
	ctx := context.Background()

	// Stateful search executor that returns different results on each successive call
	callCount := 0
	sp := NewSearchProvider()
	sp.SetSearchExecutor(func(ctx context.Context, query string, sourceVaults []string, depth int) ([]SearchResult, error) {
		callCount++
		return []SearchResult{
			{
				Vault: "src",
				Path:  "app/config",
				Entry: Entry{
					Title: "app_config",
					Attributes: map[string]any{
						"Password": fmt.Sprintf("pw_snapshot_%d", callCount),
					},
				},
			},
		}, nil
	})

	fp := NewFilteringProvider(sp, nil, []string{"other_field"})

	val, err := fp.GetSecret(ctx, "app/config:Password")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Must only call search executor once per GetSecret
	if callCount != 1 {
		t.Errorf("search executor called %d times, want exactly 1 (single-pass consistency)", callCount)
	}
	if val != "pw_snapshot_1" {
		t.Errorf("val = %q, want 'pw_snapshot_1'", val)
	}
}

func TestFilteringProvider_StructuredOutput_RecursiveCanonicalProjection(t *testing.T) {
	ctx := context.Background()

	rawContent := []byte(`
entities:
  db:
    credentials:
      token: "secret_token_123"
      user: "db_admin"
    accounts:
      - token: "acct_token_1"
        name: "acct1"
      - token: "acct_token_2"
        name: "acct2"
    flat.dotted.secret: "flat_secret_val"
    flat.dotted.safe: "flat_safe_val"
`)
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "vault.yaml")
	if err := os.WriteFile(filePath, rawContent, 0600); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	initProvider := func() SecretProvider {
		p := NewYamlProvider()
		err := p.Initialize(ctx, ProviderConfig{
			Settings: map[string]string{
				"vault_path": filePath,
				"vault_name": "test_yaml",
			},
			EntitiesRootKey: "entities",
			Searchable:      true,
		})
		if err != nil {
			t.Fatalf("failed to initialize yaml provider: %v", err)
		}
		return p
	}

	t.Run("NestedMap_ExcludeLeaf", func(t *testing.T) {
		p := initProvider()
		fp := NewFilteringProvider(p, nil, []string{"token"})

		searchable, ok := fp.(SearchableProvider)
		if !ok {
			t.Fatalf("expected SearchableProvider")
		}

		entry, err := searchable.GetEntry(ctx, "db")
		if err != nil {
			t.Fatalf("GetEntry failed: %v", err)
		}

		creds, ok := entry.Attributes["credentials"].(map[string]any)
		if !ok {
			t.Fatalf("expected credentials to be map[string]any, got %T", entry.Attributes["credentials"])
		}
		if _, hasToken := creds["token"]; hasToken {
			t.Errorf("expected token to be excluded from credentials map, got %v", creds)
		}
		if creds["user"] != "db_admin" {
			t.Errorf("expected user 'db_admin', got %v", creds["user"])
		}
	})

	t.Run("NestedMap_RootedRuleExclusion", func(t *testing.T) {
		// exclude_fields has canonical full path "entities.db.credentials.token"
		p := initProvider()
		fp := NewFilteringProvider(p, nil, []string{"entities.db.credentials.token"})

		searchable := fp.(SearchableProvider)
		entry, err := searchable.GetEntry(ctx, "db")
		if err != nil {
			t.Fatalf("GetEntry failed: %v", err)
		}

		creds, ok := entry.Attributes["credentials"].(map[string]any)
		if !ok {
			t.Fatalf("expected credentials map")
		}
		if _, hasToken := creds["token"]; hasToken {
			t.Errorf("expected token excluded under rooted rule, got %v", creds)
		}
		if creds["user"] != "db_admin" {
			t.Errorf("expected user preserved, got %v", creds["user"])
		}
	})

	t.Run("MapsInsideArray_Exclusion", func(t *testing.T) {
		p := initProvider()
		fp := NewFilteringProvider(p, nil, []string{"token"})

		searchable := fp.(SearchableProvider)
		entry, err := searchable.GetEntry(ctx, "db")
		if err != nil {
			t.Fatalf("GetEntry failed: %v", err)
		}

		accounts, ok := entry.Attributes["accounts"].([]any)
		if !ok {
			t.Fatalf("expected accounts to be []any, got %T", entry.Attributes["accounts"])
		}
		if len(accounts) != 2 {
			t.Fatalf("expected 2 accounts, got %d", len(accounts))
		}
		for i, acctRaw := range accounts {
			acct, ok := acctRaw.(map[string]any)
			if !ok {
				t.Fatalf("account %d is not map[string]any", i)
			}
			if _, hasToken := acct["token"]; hasToken {
				t.Errorf("account %d has excluded token: %v", i, acct)
			}
			if acct["name"] == "" {
				t.Errorf("account %d missing name: %v", i, acct)
			}
		}
	})

	t.Run("DottedAttributeKeys_Exclusion", func(t *testing.T) {
		p := initProvider()
		fp := NewFilteringProvider(p, nil, []string{"*.secret"})

		searchable := fp.(SearchableProvider)
		entry, err := searchable.GetEntry(ctx, "db")
		if err != nil {
			t.Fatalf("GetEntry failed: %v", err)
		}

		if _, hasSecret := entry.Attributes["flat.dotted.secret"]; hasSecret {
			t.Errorf("flat.dotted.secret was not excluded: %v", entry.Attributes)
		}
		if entry.Attributes["flat.dotted.safe"] != "flat_safe_val" {
			t.Errorf("flat.dotted.safe missing or altered: %v", entry.Attributes)
		}
	})

	t.Run("Search_StructuredOutput_Filtering", func(t *testing.T) {
		p := initProvider()
		fp := NewFilteringProvider(p, nil, []string{"token"})

		searchable := fp.(SearchableProvider)
		results, err := searchable.Search(ctx, SearchQuery{})
		if err != nil {
			t.Fatalf("Search failed: %v", err)
		}
		if len(results) == 0 {
			t.Fatalf("expected search results")
		}

		for _, r := range results {
			if creds, ok := r.Entry.Attributes["credentials"].(map[string]any); ok {
				if _, hasToken := creds["token"]; hasToken {
					t.Errorf("search result exposed excluded token in credentials: %v", creds)
				}
			}
		}
	})
}

func TestFilteringProvider_Static_SlicePath(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	yamlPath := filepath.Join(tmpDir, "slice.yaml")

	content := `
entities:
  db:
    accounts:
      - token: "tok_1"
        name: "acct1"
      - token: "tok_2"
        name: "acct2"
`
	if err := os.WriteFile(yamlPath, []byte(content), 0600); err != nil {
		t.Fatalf("failed to write yaml fixture: %v", err)
	}

	p := NewYamlProvider()
	if err := p.Initialize(ctx, ProviderConfig{
		Settings: map[string]string{
			"vault_path": yamlPath,
		},
		EntitiesRootKey: "entities",
	}); err != nil {
		t.Fatalf("failed to init YAML provider: %v", err)
	}

	fp := NewFilteringProvider(p, nil, []string{"token"})

	val, err := fp.GetSecret(ctx, "entities.db.accounts")
	if err != nil {
		t.Fatalf("GetSecret(entities.db.accounts) failed: %v", err)
	}
	if strings.Contains(val, "tok_") {
		t.Errorf("serialized slice exposed excluded token fields: %s", val)
	}
	if !strings.Contains(val, "acct1") || !strings.Contains(val, "acct2") {
		t.Errorf("serialized slice is missing expected name fields: %s", val)
	}
}

func TestFilteringProvider_Static_AncestorExclusion(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	jsonPath := filepath.Join(tmpDir, "ancestor.json")

	content := `{
  "entities": {
    "db": {
      "username": "db_admin",
      "port": 5432
    },
    "cache": {
      "host": "redis.internal"
    }
  }
}`
	if err := os.WriteFile(jsonPath, []byte(content), 0600); err != nil {
		t.Fatalf("failed to write json fixture: %v", err)
	}

	p := NewJsonProvider()
	if err := p.Initialize(ctx, ProviderConfig{
		Settings: map[string]string{
			"vault_path": jsonPath,
		},
		EntitiesRootKey: "entities",
	}); err != nil {
		t.Fatalf("failed to init JSON provider: %v", err)
	}

	// exclude_fields: ["db"] — should block access to any nested path under "db"
	fp := NewFilteringProvider(p, nil, []string{"db"})

	// Accessing db.username must be blocked because ancestor "db" is excluded
	_, err := fp.GetSecret(ctx, "db.username")
	if err == nil {
		t.Fatalf("expected error for db.username when ancestor 'db' is excluded, got nil")
	}
	if !strings.Contains(err.Error(), "excluded") {
		t.Errorf("unexpected error message: %v", err)
	}

	// Sibling entity "cache" must still be accessible
	val, err := fp.GetSecret(ctx, "cache.host")
	if err != nil {
		t.Fatalf("GetSecret(cache.host) should succeed but failed: %v", err)
	}
	if val != "redis.internal" {
		t.Errorf("cache.host = %q, want 'redis.internal'", val)
	}
}

func TestFilteringProvider_Static_AncestorExclusion_Structured(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	jsonPath := filepath.Join(tmpDir, "ancestor_struct.json")

	content := `{
  "entities": {
    "db": {
      "username": "db_admin",
      "port": 5432
    },
    "cache": {
      "host": "redis.internal"
    }
  }
}`
	if err := os.WriteFile(jsonPath, []byte(content), 0600); err != nil {
		t.Fatalf("failed to write json fixture: %v", err)
	}

	p := NewJsonProvider()
	if err := p.Initialize(ctx, ProviderConfig{
		Settings: map[string]string{
			"vault_path": jsonPath,
		},
		EntitiesRootKey: "entities",
	}); err != nil {
		t.Fatalf("failed to init JSON provider: %v", err)
	}

	// 1. exclude_fields: ["db"] — structured GetEntry and Search on "db" must have attributes excluded
	fp := NewFilteringProvider(p, nil, []string{"db"})
	sfp, ok := fp.(SearchableProvider)
	if !ok {
		t.Fatalf("FilteringProvider should implement SearchableProvider")
	}

	entryDB, err := sfp.GetEntry(ctx, "db")
	if err != nil {
		t.Fatalf("GetEntry(db) failed: %v", err)
	}
	if len(entryDB.Attributes) != 0 {
		t.Errorf("GetEntry(db) exposed attributes despite ancestor exclude: %v", entryDB.Attributes)
	}

	entryCache, err := sfp.GetEntry(ctx, "cache")
	if err != nil {
		t.Fatalf("GetEntry(cache) failed: %v", err)
	}
	if entryCache.Attributes["host"] != "redis.internal" {
		t.Errorf("GetEntry(cache) host = %v, want 'redis.internal'", entryCache.Attributes["host"])
	}

	// Search
	results, err := sfp.Search(ctx, SearchQuery{})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	for _, r := range results {
		if r.Path == "db" && len(r.Entry.Attributes) != 0 {
			t.Errorf("Search result for 'db' exposed attributes: %v", r.Entry.Attributes)
		}
		if r.Path == "cache" && r.Entry.Attributes["host"] != "redis.internal" {
			t.Errorf("Search result for 'cache' missing host: %v", r.Entry.Attributes)
		}
	}

	// 2. exclude_fields: ["entities.db"] — root-prefixed ancestor exclusion
	fpRoot := NewFilteringProvider(p, nil, []string{"entities.db"})
	sfpRoot := fpRoot.(SearchableProvider)
	entryDBRoot, err := sfpRoot.GetEntry(ctx, "db")
	if err != nil {
		t.Fatalf("GetEntry(db) with entities.db exclude failed: %v", err)
	}
	if len(entryDBRoot.Attributes) != 0 {
		t.Errorf("GetEntry(db) exposed attributes with entities.db exclude: %v", entryDBRoot.Attributes)
	}

	// 3. include_fields: ["db"] — subtree inclusion
	fpInc := NewFilteringProvider(p, []string{"db"}, nil)
	sfpInc := fpInc.(SearchableProvider)
	entryDBInc, err := sfpInc.GetEntry(ctx, "db")
	if err != nil {
		t.Fatalf("GetEntry(db) with include 'db' failed: %v", err)
	}
	if entryDBInc.Attributes["username"] != "db_admin" {
		t.Errorf("GetEntry(db) username = %v, want 'db_admin'", entryDBInc.Attributes["username"])
	}
	entryCacheInc, err := sfpInc.GetEntry(ctx, "cache")
	if err != nil {
		t.Fatalf("GetEntry(cache) with include 'db' failed: %v", err)
	}
	if len(entryCacheInc.Attributes) != 0 {
		t.Errorf("GetEntry(cache) should have empty attributes when only 'db' is included, got: %v", entryCacheInc.Attributes)
	}
}

func TestFilteringProvider_KeePass_EntryPathCandidates(t *testing.T) {
	keyring.MockInit()
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()

	if err := keyring.Set("cloakenv", "provider/testkp", "password123"); err != nil {
		t.Fatalf("failed to set mock credentials: %v", err)
	}

	kp := NewKeePassProvider()
	if err := kp.Initialize(ctx, ProviderConfig{
		Settings: map[string]string{
			"vault_path":  "../../testdata/testDB.kdbx",
			"remote_name": "testkp",
		},
	}); err != nil {
		t.Fatalf("failed to initialize KeePass provider: %v", err)
	}

	// 1. Path-qualified exclusion: exclude_fields: ["website/Test Website.Password"]
	fp := NewFilteringProvider(kp, nil, []string{"website/Test Website.Password"})

	// GetSecret with explicit attribute
	_, err := fp.GetSecret(ctx, "website/Test Website:Password")
	if err == nil {
		t.Fatalf("expected error for website/Test Website:Password, got nil")
	}
	if !strings.Contains(err.Error(), "excluded") {
		t.Errorf("unexpected error message: %v", err)
	}

	// GetSecret with bare entry path (defaults to Password)
	_, err = fp.GetSecret(ctx, "website/Test Website")
	if err == nil {
		t.Fatalf("expected error for bare website/Test Website, got nil")
	}
	if !strings.Contains(err.Error(), "excluded") {
		t.Errorf("unexpected error message: %v", err)
	}

	// UserName should still be accessible
	user, err := fp.GetSecret(ctx, "website/Test Website:UserName")
	if err != nil {
		t.Fatalf("GetSecret(website/Test Website:UserName) failed: %v", err)
	}
	if user != "user@email.com" {
		t.Errorf("UserName = %q, want 'user@email.com'", user)
	}

	// Structured GetEntry should also drop Password
	sfp := fp.(SearchableProvider)
	entry, err := sfp.GetEntry(ctx, "website/Test Website")
	if err != nil {
		t.Fatalf("GetEntry failed: %v", err)
	}
	if _, ok := entry.Attributes["Password"]; ok {
		t.Errorf("GetEntry exposed Password when website/Test Website.Password excluded")
	}
	if entry.Attributes["UserName"] != "user@email.com" {
		t.Errorf("GetEntry UserName = %v, want 'user@email.com'", entry.Attributes["UserName"])
	}

	// 2. Full entry exclusion: exclude_fields: ["website/Test Website"]
	fpAll := NewFilteringProvider(kp, nil, []string{"website/Test Website"})
	_, err = fpAll.GetSecret(ctx, "website/Test Website:Password")
	if err == nil {
		t.Fatalf("expected error when entire entry is excluded, got nil")
	}
	_, err = fpAll.GetSecret(ctx, "website/Test Website:UserName")
	if err == nil {
		t.Fatalf("expected error when entire entry is excluded, got nil")
	}

	sfpAll := fpAll.(SearchableProvider)
	entryAll, err := sfpAll.GetEntry(ctx, "website/Test Website")
	if err != nil {
		t.Fatalf("GetEntry failed: %v", err)
	}
	if len(entryAll.Attributes) != 0 {
		t.Errorf("expected 0 attributes when entry is excluded, got %v", entryAll.Attributes)
	}
}

func TestFilteringProvider_CustomVault_EntryPathCandidates(t *testing.T) {
	ctx := context.Background()
	cv := NewCustomVaultProvider()
	if err := cv.Initialize(ctx, ProviderConfig{
		Settings: map[string]string{},
		Entities: map[string]map[string]any{
			"my_entity": {
				"Password": "my_secret_pass",
				"UserName": "my_user",
			},
		},
	}); err != nil {
		t.Fatalf("failed to initialize custom vault: %v", err)
	}

	// 1. Path-qualified exclusion: exclude_fields: ["my_entity.Password"]
	fp := NewFilteringProvider(cv, nil, []string{"my_entity.Password"})

	// GetSecret with explicit attribute
	_, err := fp.GetSecret(ctx, "my_entity:Password")
	if err == nil {
		t.Fatalf("expected error for my_entity:Password, got nil")
	}
	if !strings.Contains(err.Error(), "excluded") {
		t.Errorf("unexpected error message: %v", err)
	}

	// GetSecret with bare entity (defaults to Password)
	_, err = fp.GetSecret(ctx, "my_entity")
	if err == nil {
		t.Fatalf("expected error for bare my_entity, got nil")
	}

	// UserName should succeed
	val, err := fp.GetSecret(ctx, "my_entity:UserName")
	if err != nil {
		t.Fatalf("GetSecret(my_entity:UserName) failed: %v", err)
	}
	if val != "my_user" {
		t.Errorf("UserName = %q, want 'my_user'", val)
	}

	// Structured GetEntry
	sfp := fp.(SearchableProvider)
	entry, err := sfp.GetEntry(ctx, "my_entity")
	if err != nil {
		t.Fatalf("GetEntry failed: %v", err)
	}
	if _, ok := entry.Attributes["Password"]; ok {
		t.Errorf("GetEntry exposed Password when my_entity.Password excluded")
	}
	if entry.Attributes["UserName"] != "my_user" {
		t.Errorf("GetEntry UserName = %v, want 'my_user'", entry.Attributes["UserName"])
	}

	// 2. Full entity exclusion: exclude_fields: ["my_entity"]
	fpAll := NewFilteringProvider(cv, nil, []string{"my_entity"})
	_, err = fpAll.GetSecret(ctx, "my_entity:Password")
	if err == nil {
		t.Fatalf("expected error when entire entity is excluded, got nil")
	}
	_, err = fpAll.GetSecret(ctx, "my_entity:UserName")
	if err == nil {
		t.Fatalf("expected error when entire entity is excluded, got nil")
	}
}

func TestFilteringProvider_Search_PathQualifiedAndSnapshot(t *testing.T) {
	ctx := context.Background()

	sp := NewSearchProvider()
	sp.vaultName = "search_vault"
	sp.searchExecutor = func(_ context.Context, _ string, _ []string, _ int) ([]SearchResult, error) {
		return []SearchResult{
			{
				Provider: "custom_vault",
				Vault:    "src",
				Path:     "db/main",
				Entry: Entry{
					Title: "db/main",
					Attributes: map[string]any{
						"Password": "pass_value",
						"Username": "user_value",
					},
				},
			},
		}, nil
	}

	// 1. Path-qualified exclusion: exclude_fields: ["db/main.Password"]
	fp := NewFilteringProvider(sp, nil, []string{"db/main.Password"})

	// GetSecret explicit attribute
	_, err := fp.GetSecret(ctx, "db/main:Password")
	if err == nil {
		t.Fatalf("expected error for db/main:Password when db/main.Password is excluded, got nil")
	}
	if !strings.Contains(err.Error(), "excluded") {
		t.Errorf("unexpected error message: %v", err)
	}

	// GetSecret bare path (defaults to Password)
	_, err = fp.GetSecret(ctx, "db/main")
	if err == nil {
		t.Fatalf("expected error for bare db/main, got nil")
	}

	// Username succeeds
	user, err := fp.GetSecret(ctx, "db/main:Username")
	if err != nil {
		t.Fatalf("GetSecret(db/main:Username) failed: %v", err)
	}
	if user != "user_value" {
		t.Errorf("Username = %q, want 'user_value'", user)
	}

	// 2. Full path exclusion: exclude_fields: ["db/main"]
	fpAll := NewFilteringProvider(sp, nil, []string{"db/main"})
	_, err = fpAll.GetSecret(ctx, "db/main:Password")
	if err == nil {
		t.Fatalf("expected error when entire path is excluded, got nil")
	}
	_, err = fpAll.GetSecret(ctx, "db/main:Username")
	if err == nil {
		t.Fatalf("expected error when entire path is excluded, got nil")
	}
}

func TestFilteringProvider_Static_TopLevelSlice_ExactContainerInclude(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	// 1. Single-entity / rootless static provider with top-level slices
	jsonSinglePath := filepath.Join(tmpDir, "slices_single.json")
	contentSingle := `{
  "accounts": [
    {"name": "acct1", "token": "tok1"},
    {"name": "acct2", "token": "tok2"}
  ],
  "regions": ["prod", "us-east"]
}`
	if err := os.WriteFile(jsonSinglePath, []byte(contentSingle), 0600); err != nil {
		t.Fatalf("failed to write json fixture: %v", err)
	}

	p := NewJsonProvider()
	if err := p.Initialize(ctx, ProviderConfig{
		Settings: map[string]string{
			"vault_path": jsonSinglePath,
		},
	}); err != nil {
		t.Fatalf("failed to init single-entity JSON provider: %v", err)
	}

	// include_fields: ["accounts"] — should keep entire list of maps
	fpAccts := NewFilteringProvider(p, []string{"accounts"}, nil)
	valAccts, err := fpAccts.GetSecret(ctx, "accounts")
	if err != nil {
		t.Fatalf("GetSecret(accounts) failed: %v", err)
	}
	if !strings.Contains(valAccts, "acct1") || !strings.Contains(valAccts, "tok1") {
		t.Errorf("GetSecret(accounts) missing expected content: %s", valAccts)
	}

	// include_fields: ["regions"] — should keep primitive array
	fpRegions := NewFilteringProvider(p, []string{"regions"}, nil)
	valRegions, err := fpRegions.GetSecret(ctx, "regions")
	if err != nil {
		t.Fatalf("GetSecret(regions) failed: %v", err)
	}
	if !strings.Contains(valRegions, "prod") || !strings.Contains(valRegions, "us-east") {
		t.Errorf("GetSecret(regions) missing expected array items: %s", valRegions)
	}

	// False rejection: include_fields: ["other"] should reject accounts and regions
	fpOther := NewFilteringProvider(p, []string{"other"}, nil)
	_, err = fpOther.GetSecret(ctx, "accounts")
	if err == nil {
		t.Fatalf("expected error for accounts with include_fields: ['other'], got nil")
	}
	if !strings.Contains(err.Error(), "excluded") {
		t.Errorf("unexpected error message: %v", err)
	}
	_, err = fpOther.GetSecret(ctx, "regions")
	if err == nil {
		t.Fatalf("expected error for regions with include_fields: ['other'], got nil")
	}

	// 2. Rooted multi-entity static provider (entities_root_key: "entities")
	jsonRootedPath := filepath.Join(tmpDir, "slices_rooted.json")
	contentRooted := `{
  "entities": {
    "db": {
      "accounts": [
        {"name": "acct3", "token": "tok3"}
      ],
      "tags": ["internal", "v2"]
    }
  }
}`
	if err := os.WriteFile(jsonRootedPath, []byte(contentRooted), 0600); err != nil {
		t.Fatalf("failed to write rooted json fixture: %v", err)
	}

	pRoot := NewJsonProvider()
	if err := pRoot.Initialize(ctx, ProviderConfig{
		Settings: map[string]string{
			"vault_path": jsonRootedPath,
		},
		EntitiesRootKey: "entities",
	}); err != nil {
		t.Fatalf("failed to init rooted JSON provider: %v", err)
	}

	// include_fields: ["entities.db.accounts"] — should allow both rootless and root-prefixed forms
	fpRootAccts := NewFilteringProvider(pRoot, []string{"entities.db.accounts"}, nil)
	valRoot1, err := fpRootAccts.GetSecret(ctx, "db.accounts")
	if err != nil {
		t.Fatalf("GetSecret(db.accounts) with entities.db.accounts include failed: %v", err)
	}
	if !strings.Contains(valRoot1, "acct3") {
		t.Errorf("GetSecret(db.accounts) missing acct3: %s", valRoot1)
	}
	valRoot2, err := fpRootAccts.GetSecret(ctx, "entities.db.accounts")
	if err != nil {
		t.Fatalf("GetSecret(entities.db.accounts) with entities.db.accounts include failed: %v", err)
	}
	if !strings.Contains(valRoot2, "acct3") {
		t.Errorf("GetSecret(entities.db.accounts) missing acct3: %s", valRoot2)
	}

	// include_fields: ["entities.db.tags"] — primitive list under root key
	fpRootTags := NewFilteringProvider(pRoot, []string{"entities.db.tags"}, nil)
	valRootTags, err := fpRootTags.GetSecret(ctx, "db.tags")
	if err != nil {
		t.Fatalf("GetSecret(db.tags) with entities.db.tags include failed: %v", err)
	}
	if !strings.Contains(valRootTags, "internal") || !strings.Contains(valRootTags, "v2") {
		t.Errorf("GetSecret(db.tags) missing expected items: %s", valRootTags)
	}
}

func TestFilteringProvider_CustomVault_StructuredProjection(t *testing.T) {
	ctx := context.Background()
	cvp := NewCustomVaultProvider()

	cfg := ProviderConfig{
		Entities: map[string]map[string]any{
			"app": {
				"data": map[string]any{
					"user":  "alice",
					"token": "secret123",
				},
				"items": []any{
					map[string]any{"id": 1, "secret": "s1"},
					map[string]any{"id": 2, "secret": "s2"},
				},
			},
		},
	}
	if err := cvp.Initialize(ctx, cfg); err != nil {
		t.Fatalf("failed to init custom_vault: %v", err)
	}

	// 1. Exclude nested field in map: exclude_fields: ["*.token"]
	fpMap := NewFilteringProvider(cvp, nil, []string{"*.token"})
	resMap, err := fpMap.GetSecret(ctx, "app:data")
	if err != nil {
		t.Fatalf("GetSecret(app:data) failed: %v", err)
	}
	if strings.Contains(resMap, "secret123") {
		t.Errorf("expected token 'secret123' to be excluded from serialized map, got: %s", resMap)
	}
	if !strings.Contains(resMap, "alice") {
		t.Errorf("expected user 'alice' to be present in serialized map, got: %s", resMap)
	}

	// 2. Exclude nested field in slice: exclude_fields: ["*.secret"]
	fpSlice := NewFilteringProvider(cvp, nil, []string{"*.secret"})
	resSlice, err := fpSlice.GetSecret(ctx, "app:items")
	if err != nil {
		t.Fatalf("GetSecret(app:items) failed: %v", err)
	}
	if strings.Contains(resSlice, "s1") || strings.Contains(resSlice, "s2") {
		t.Errorf("expected secrets to be excluded from serialized slice, got: %s", resSlice)
	}
	if !strings.Contains(resSlice, "id: 1") && !strings.Contains(resSlice, "id: 2") {
		t.Errorf("expected ids to be retained in serialized slice, got: %s", resSlice)
	}
}

func TestFilteringProvider_Search_StructuredProjection(t *testing.T) {
	ctx := context.Background()
	sp := NewSearchProvider()
	sp.SetSearchExecutor(func(_ context.Context, _ string, _ []string, _ int) ([]SearchResult, error) {
		return []SearchResult{
			{
				Vault: "source",
				Path:  "services/api",
				Entry: Entry{
					Title: "api_service",
					Attributes: map[string]any{
						"config": map[string]any{
							"endpoint": "https://api.internal",
							"token":    "tok_raw_secret",
						},
					},
				},
			},
		}, nil
	})

	// Exclude config.token via glob
	fp := NewFilteringProvider(sp, nil, []string{"*.token"})
	res, err := fp.GetSecret(ctx, "config")
	if err != nil {
		t.Fatalf("GetSecret(config) failed: %v", err)
	}
	if strings.Contains(res, "tok_raw_secret") {
		t.Errorf("expected token to be excluded, got: %s", res)
	}
	if !strings.Contains(res, "https://api.internal") {
		t.Errorf("expected endpoint to be preserved, got: %s", res)
	}
}

func TestFilteringProvider_SingleEntity_RootSymmetry(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	jsonPath := filepath.Join(tmpDir, "single_entity_symmetry.json")

	content := `{
  "entities": {
    "db": {
      "host": "localhost",
      "password": "secret_password"
    }
  }
}`
	if err := os.WriteFile(jsonPath, []byte(content), 0600); err != nil {
		t.Fatalf("failed to write json fixture: %v", err)
	}

	singleEntity := true
	p := NewJsonProvider()
	if err := p.Initialize(ctx, ProviderConfig{
		Settings: map[string]string{
			"vault_path": jsonPath,
		},
		EntitiesRootKey: "entities",
		SingleEntity:    &singleEntity,
	}); err != nil {
		t.Fatalf("failed to init single-entity JSON provider: %v", err)
	}

	// Test 1: Exclude with rooted path "entities.db.password"
	fpRooted := NewFilteringProvider(p, nil, []string{"entities.db.password"})

	// Rootless query db.password must be blocked
	_, err := fpRooted.GetSecret(ctx, "db.password")
	if err == nil {
		t.Fatalf("expected error for db.password with exclude_fields: ['entities.db.password'], got nil")
	}

	// Root-prefixed query entities.db.password must also be blocked
	_, err = fpRooted.GetSecret(ctx, "entities.db.password")
	if err == nil {
		t.Fatalf("expected error for entities.db.password with exclude_fields: ['entities.db.password'], got nil")
	}

	// Allowed field must succeed for both forms
	valRootless, err := fpRooted.GetSecret(ctx, "db.host")
	if err != nil {
		t.Fatalf("GetSecret(db.host) failed: %v", err)
	}
	if valRootless != "localhost" {
		t.Errorf("expected localhost, got %q", valRootless)
	}

	valRooted, err := fpRooted.GetSecret(ctx, "entities.db.host")
	if err != nil {
		t.Fatalf("GetSecret(entities.db.host) failed: %v", err)
	}
	if valRooted != "localhost" {
		t.Errorf("expected localhost, got %q", valRooted)
	}

	// Test 2: Exclude with rootless path "db.password"
	fpRootless := NewFilteringProvider(p, nil, []string{"db.password"})

	// Both forms must be blocked symmetrically
	_, err = fpRootless.GetSecret(ctx, "db.password")
	if err == nil {
		t.Fatalf("expected error for db.password with exclude_fields: ['db.password'], got nil")
	}
	_, err = fpRootless.GetSecret(ctx, "entities.db.password")
	if err == nil {
		t.Fatalf("expected error for entities.db.password with exclude_fields: ['db.password'], got nil")
	}
}

func TestSearchableFilteringProvider_GetEntry_AuthoritativePathVsCallerAlias(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	// 1. Single-entity static provider:
	// The entry's authoritative Title is "guest_user".
	// The allowlist includes only "admin.*".
	// A caller injecting location "admin.super" must NOT inherit inclusion for the entry's attributes.
	yamlPath := filepath.Join(tmpDir, "single_entity_alias.yaml")
	yamlContent := `
title: "guest_user"
token: "secret_token_123"
name: "guest"
`
	if err := os.WriteFile(yamlPath, []byte(yamlContent), 0600); err != nil {
		t.Fatalf("failed to write yaml: %v", err)
	}

	singleEntity := true
	yp := NewYamlProvider()
	if err := yp.Initialize(ctx, ProviderConfig{
		Settings: map[string]string{
			"vault_path": yamlPath,
		},
		SingleEntity: &singleEntity,
	}); err != nil {
		t.Fatalf("failed to init YAML provider: %v", err)
	}

	fp := NewFilteringProvider(yp, []string{"admin.*"}, nil)
	sfp, ok := fp.(SearchableProvider)
	if !ok {
		t.Fatal("expected SearchableProvider wrapper")
	}

	// Caller attempts alias injection by asking for "admin.super"
	entry, err := sfp.GetEntry(ctx, "admin.super")
	if err != nil {
		t.Fatalf("GetEntry failed: %v", err)
	}
	if len(entry.Attributes) != 0 {
		t.Errorf("expected all attributes to be excluded under authoritative title 'guest_user', but got: %v", entry.Attributes)
	}

	// 2. Single-result SearchProvider:
	// Result path is "guest/account". Allowlist includes only "admin/*".
	// Caller requesting "admin/account" must NOT inherit inclusion.
	sp := NewSearchProvider()
	sp.SetSearchExecutor(func(_ context.Context, _ string, _ []string, _ int) ([]SearchResult, error) {
		return []SearchResult{
			{
				Vault: "src",
				Path:  "guest/account",
				Entry: Entry{
					Title: "guest_account",
					Attributes: map[string]any{
						"token": "tok_guest",
					},
				},
			},
		}, nil
	})

	sfpSearch := NewFilteringProvider(sp, []string{"admin/*"}, nil).(SearchableProvider)
	entrySearch, err := sfpSearch.GetEntry(ctx, "admin/account")
	if err != nil {
		t.Fatalf("GetEntry on search provider failed: %v", err)
	}
	if len(entrySearch.Attributes) != 0 {
		t.Errorf("expected attributes to be excluded under authoritative path 'guest/account', but got: %v", entrySearch.Attributes)
	}
}

func TestFilteringProvider_SliceIndexing(t *testing.T) {
	rawEntry := Entry{
		Title: "users_entry",
		Attributes: map[string]any{
			"users": []any{
				map[string]any{
					"name":  "alice",
					"token": "tok_alice",
				},
				map[string]any{
					"name":  "bob",
					"token": "tok_bob",
				},
			},
			"tags_list": []any{"public", "secret_tag", "internal"},
			"matrix": []any{
				[]any{"m00", "m01"},
				[]any{"m10", "m11"},
			},
		},
	}

	t.Run("ExcludeSpecificElementFieldByIndex", func(t *testing.T) {
		// exclude_fields: ["users.0.token"]
		// Element 0 loses token, element 1 keeps token
		filtered := FilterEntry(rawEntry, nil, []string{"users.0.token"})
		users, ok := filtered.Attributes["users"].([]any)
		if !ok || len(users) != 2 {
			t.Fatalf("expected 2 users, got: %v", users)
		}
		u0 := users[0].(map[string]any)
		if _, hasToken := u0["token"]; hasToken {
			t.Errorf("expected users.0.token to be excluded, but present: %v", u0)
		}
		if u0["name"] != "alice" {
			t.Errorf("expected users.0.name to be alice, got: %v", u0["name"])
		}
		u1 := users[1].(map[string]any)
		if u1["token"] != "tok_bob" {
			t.Errorf("expected users.1.token to be tok_bob, got: %v", u1["token"])
		}
	})

	t.Run("ExcludeEntireSliceElementByIndex", func(t *testing.T) {
		// exclude_fields: ["users.1"]
		filtered := FilterEntry(rawEntry, nil, []string{"users.1"})
		users, ok := filtered.Attributes["users"].([]any)
		if !ok || len(users) != 1 {
			t.Fatalf("expected 1 user after excluding index 1, got: %v", users)
		}
		u0 := users[0].(map[string]any)
		if u0["name"] != "alice" {
			t.Errorf("expected alice, got: %v", u0["name"])
		}
	})

	t.Run("IncludeSpecificElementFieldByIndex", func(t *testing.T) {
		// include_fields: ["users.0.name"]
		filtered := FilterEntry(rawEntry, []string{"users.0.name"}, nil)
		users, ok := filtered.Attributes["users"].([]any)
		if !ok || len(users) != 1 {
			t.Fatalf("expected 1 user element matching include_fields, got: %v", users)
		}
		u0 := users[0].(map[string]any)
		if u0["name"] != "alice" {
			t.Errorf("expected alice, got: %v", u0["name"])
		}
		if _, hasToken := u0["token"]; hasToken {
			t.Errorf("token should not be included: %v", u0)
		}
	})

	t.Run("ScalarSliceElementExclusion", func(t *testing.T) {
		// exclude_fields: ["tags_list.1"]
		filtered := FilterEntry(rawEntry, nil, []string{"tags_list.1"})
		tagsList, ok := filtered.Attributes["tags_list"].([]any)
		if !ok || len(tagsList) != 2 {
			t.Fatalf("expected 2 tags, got: %v", tagsList)
		}
		if tagsList[0] != "public" || tagsList[1] != "internal" {
			t.Errorf("unexpected tagsList: %v", tagsList)
		}
	})

	t.Run("NestedSliceElementExclusion", func(t *testing.T) {
		// exclude_fields: ["matrix.0.1"]
		filtered := FilterEntry(rawEntry, nil, []string{"matrix.0.1"})
		matrix, ok := filtered.Attributes["matrix"].([]any)
		if !ok || len(matrix) != 2 {
			t.Fatalf("expected 2 matrix rows, got: %v", matrix)
		}
		row0 := matrix[0].([]any)
		if len(row0) != 1 || row0[0] != "m00" {
			t.Errorf("expected row0 to have only m00, got: %v", row0)
		}
		row1 := matrix[1].([]any)
		if len(row1) != 2 {
			t.Errorf("expected row1 to have 2 elements, got: %v", row1)
		}
	})
}

func TestFilteringProvider_TitleQualifiedExclusion(t *testing.T) {
	ctx := context.Background()

	t.Run("StaticProvider_TitleQualifiedExclusion", func(t *testing.T) {
		tmpDir := t.TempDir()
		yamlPath := filepath.Join(tmpDir, "title_excl.yaml")
		yamlContent := `
entities:
  entry1:
    title: "Database Admin"
    token: "secret_admin_tok"
    host: "db.internal"
`
		if err := os.WriteFile(yamlPath, []byte(yamlContent), 0600); err != nil {
			t.Fatalf("failed to write yaml: %v", err)
		}

		yp := NewYamlProvider()
		if err := yp.Initialize(ctx, ProviderConfig{
			Settings: map[string]string{
				"vault_path": yamlPath,
			},
			EntitiesRootKey: "entities",
		}); err != nil {
			t.Fatalf("failed to init YAML: %v", err)
		}

		// exclude_fields: ["Database Admin.token"]
		fp := NewFilteringProvider(yp, nil, []string{"Database Admin.token"})

		// Scalar GetSecret must be blocked using title-prefixed exclusion
		_, err := fp.GetSecret(ctx, "entry1.token")
		if err == nil {
			t.Errorf("expected GetSecret(entry1.token) to be blocked by title-qualified exclusion, got nil")
		}

		// Non-excluded field must succeed
		val, err := fp.GetSecret(ctx, "entry1.host")
		if err != nil {
			t.Fatalf("GetSecret(entry1.host) failed: %v", err)
		}
		if val != "db.internal" {
			t.Errorf("expected 'db.internal', got %q", val)
		}

		// Structured GetEntry must also exclude token
		sfp := fp.(SearchableProvider)
		entry, err := sfp.GetEntry(ctx, "entry1")
		if err != nil {
			t.Fatalf("GetEntry failed: %v", err)
		}
		if _, hasToken := entry.Attributes["token"]; hasToken {
			t.Errorf("expected token to be excluded in GetEntry, but got: %v", entry.Attributes)
		}
	})

	t.Run("CustomVault_TitleQualifiedExclusion", func(t *testing.T) {
		cp := NewCustomVaultProvider()
		if err := cp.Initialize(ctx, ProviderConfig{
			Entities: map[string]map[string]any{
				"e1": {
					"title":    "My App Secret",
					"Password": "top_secret_pass",
					"username": "admin",
				},
			},
		}); err != nil {
			t.Fatalf("failed to init CustomVault: %v", err)
		}

		// exclude_fields: ["My App Secret.Password"]
		fp := NewFilteringProvider(cp, nil, []string{"My App Secret.Password"})

		// Scalar GetSecret with default Password
		_, err := fp.GetSecret(ctx, "e1")
		if err == nil {
			t.Errorf("expected GetSecret(e1) to be blocked by title-qualified exclusion, got nil")
		}

		// Scalar GetSecret with explicit attribute
		_, err = fp.GetSecret(ctx, "e1:Password")
		if err == nil {
			t.Errorf("expected GetSecret(e1:Password) to be blocked by title-qualified exclusion, got nil")
		}

		// Non-excluded field succeeds
		val, err := fp.GetSecret(ctx, "e1:username")
		if err != nil {
			t.Fatalf("GetSecret(e1:username) failed: %v", err)
		}
		if val != "admin" {
			t.Errorf("expected admin, got %q", val)
		}

		// Structured GetEntry must also exclude Password
		sfp := fp.(SearchableProvider)
		entry, err := sfp.GetEntry(ctx, "e1")
		if err != nil {
			t.Fatalf("GetEntry failed: %v", err)
		}
		if _, hasPass := entry.Attributes["Password"]; hasPass {
			t.Errorf("expected Password to be excluded in GetEntry, got: %v", entry.Attributes)
		}
	})

	t.Run("SearchProvider_TitleQualifiedExclusion", func(t *testing.T) {
		sp := NewSearchProvider()
		sp.SetSearchExecutor(func(_ context.Context, _ string, _ []string, _ int) ([]SearchResult, error) {
			return []SearchResult{
				{
					Vault: "src",
					Path:  "services/backend",
					Entry: Entry{
						Title: "Backend Service",
						Attributes: map[string]any{
							"api_key":  "secret_key_999",
							"endpoint": "https://service.internal",
						},
					},
				},
			}, nil
		})

		// exclude_fields: ["Backend Service.api_key"]
		fp := NewFilteringProvider(sp, nil, []string{"Backend Service.api_key"})

		_, err := fp.GetSecret(ctx, "services/backend:api_key")
		if err == nil {
			t.Errorf("expected GetSecret to be blocked by title-qualified exclusion, got nil")
		}

		val, err := fp.GetSecret(ctx, "services/backend:endpoint")
		if err != nil {
			t.Fatalf("GetSecret for endpoint failed: %v", err)
		}
		if val != "https://service.internal" {
			t.Errorf("expected endpoint, got %q", val)
		}

		sfp := fp.(SearchableProvider)
		entry, err := sfp.GetEntry(ctx, "services/backend")
		if err != nil {
			t.Fatalf("GetEntry failed: %v", err)
		}
		if _, hasKey := entry.Attributes["api_key"]; hasKey {
			t.Errorf("expected api_key to be excluded in GetEntry, got: %v", entry.Attributes)
		}
	})

	t.Run("StaticProvider_SingleEntity_TitleQualified", func(t *testing.T) {
		tmpDir := t.TempDir()
		yamlPath := filepath.Join(tmpDir, "single_entity_title.yaml")
		yamlContent := `
token: "secret_admin_tok"
username: "app_user"
host: "app.internal"
`
		if err := os.WriteFile(yamlPath, []byte(yamlContent), 0600); err != nil {
			t.Fatalf("failed to write yaml: %v", err)
		}

		yp := NewYamlProvider()
		if err := yp.Initialize(ctx, ProviderConfig{
			Settings: map[string]string{
				"vault_path":  yamlPath,
				"entity_name": "app",
			},
		}); err != nil {
			t.Fatalf("failed to init single-entity YAML: %v", err)
		}

		// 1. Title-qualified exclusion: exclude_fields: ["app.token"]
		fpExcl := NewFilteringProvider(yp, nil, []string{"app.token"})

		// Scalar GetSecret("token") must be blocked
		_, err := fpExcl.GetSecret(ctx, "token")
		if err == nil {
			t.Errorf("expected GetSecret(token) to be blocked by title-qualified exclusion, got nil")
		}

		// Non-excluded field must succeed
		val, err := fpExcl.GetSecret(ctx, "username")
		if err != nil {
			t.Fatalf("GetSecret(username) failed: %v", err)
		}
		if val != "app_user" {
			t.Errorf("expected 'app_user', got %q", val)
		}

		// Structured GetEntry("") must also exclude token
		sfpExcl := fpExcl.(SearchableProvider)
		entryExcl, err := sfpExcl.GetEntry(ctx, "")
		if err != nil {
			t.Fatalf("GetEntry failed: %v", err)
		}
		if _, hasToken := entryExcl.Attributes["token"]; hasToken {
			t.Errorf("expected token to be excluded in GetEntry, got: %v", entryExcl.Attributes)
		}

		// 2. Title-qualified inclusion: include_fields: ["app.username"]
		fpInc := NewFilteringProvider(yp, []string{"app.username"}, nil)

		// Scalar GetSecret("username") must be allowed
		valInc, err := fpInc.GetSecret(ctx, "username")
		if err != nil {
			t.Fatalf("GetSecret(username) failed with title-qualified include: %v", err)
		}
		if valInc != "app_user" {
			t.Errorf("expected 'app_user', got %q", valInc)
		}

		// Scalar GetSecret("token") must be rejected by include
		_, err = fpInc.GetSecret(ctx, "token")
		if err == nil {
			t.Errorf("expected GetSecret(token) to be rejected by title-qualified include, got nil")
		}

		// Structured GetEntry("") must only include username
		sfpInc := fpInc.(SearchableProvider)
		entryInc, err := sfpInc.GetEntry(ctx, "")
		if err != nil {
			t.Fatalf("GetEntry failed: %v", err)
		}
		if entryInc.Attributes["username"] != "app_user" {
			t.Errorf("expected username in GetEntry, got: %v", entryInc.Attributes)
		}
		if _, hasToken := entryInc.Attributes["token"]; hasToken {
			t.Errorf("expected token to not be included in GetEntry, got: %v", entryInc.Attributes)
		}
	})
}

func TestFilteringProvider_TitleResolutionError(t *testing.T) {
	ctx := context.Background()

	// Custom vault: lookup for nonexistent entity title must return error
	cp := NewCustomVaultProvider()
	if err := cp.Initialize(ctx, ProviderConfig{
		Entities: map[string]map[string]any{
			"valid_entity": {
				"Password": "secret",
				"title":    "Valid Title",
			},
		},
	}); err != nil {
		t.Fatalf("failed to initialize custom vault: %v", err)
	}

	fp := NewFilteringProvider(cp, nil, []string{"Valid Title.Password"})

	// Looking up a nonexistent entity must surface the lookup error, not silently drop candidate title
	_, err := fp.GetSecret(ctx, "missing_entity:Password")
	if err == nil {
		t.Errorf("expected error when resolving nonexistent entity, got nil")
	}
}

func TestFilteringProvider_AncestorInclusionIsolation(t *testing.T) {
	ctx := context.Background()

	tmpDir := t.TempDir()
	yamlPath := filepath.Join(tmpDir, "ancestor_inc.yaml")
	yamlContent := `
entities:
  db:
    username: "db_user"
    password: "db_password"
`
	if err := os.WriteFile(yamlPath, []byte(yamlContent), 0600); err != nil {
		t.Fatalf("failed to write yaml fixture: %v", err)
	}

	yp := NewYamlProvider()
	if err := yp.Initialize(ctx, ProviderConfig{
		Settings: map[string]string{
			"vault_path": yamlPath,
		},
		EntitiesRootKey: "entities",
	}); err != nil {
		t.Fatalf("failed to init YAML: %v", err)
	}

	// include_fields: ["entities"] must NOT act as a no-op allowlist that grants all children
	fp := NewFilteringProvider(yp, []string{"entities"}, nil)

	// Scalar GetSecret for "db.username" must NOT be granted by the ancestor "entities"
	_, err := fp.GetSecret(ctx, "db.username")
	if err == nil {
		t.Errorf("expected GetSecret(db.username) to be rejected by include_fields: ['entities'], got nil")
	}

	// Structured GetEntry for "db" must not expose username or password when only ancestor "entities" is included
	sfp := fp.(SearchableProvider)
	entry, err := sfp.GetEntry(ctx, "db")
	if err != nil {
		t.Fatalf("GetEntry failed: %v", err)
	}
	if len(entry.Attributes) != 0 {
		t.Errorf("expected empty attributes with include_fields: ['entities'], got: %v", entry.Attributes)
	}
}

func TestFilteringProvider_UnsupportedSchemeDenial(t *testing.T) {
	ctx := context.Background()

	// A mock provider with an unsupported scheme (e.g. "unknown_scheme")
	mock := &mockGenericProvider{scheme: "unknown_scheme", val: "my_secret"}
	fp := NewFilteringProvider(mock, []string{"my_secret"}, nil)

	_, err := fp.GetSecret(ctx, "my_secret")
	if err == nil {
		t.Errorf("expected error denying unsupported scheme with field filtering, got nil")
	}
}

type mockGenericProvider struct {
	scheme string
	val    string
}

func (m *mockGenericProvider) Scheme() string                                       { return m.scheme }
func (m *mockGenericProvider) Initialize(_ context.Context, _ ProviderConfig) error { return nil }
func (m *mockGenericProvider) GetSecret(_ context.Context, _ string) (string, error) {
	return m.val, nil
}
func (m *mockGenericProvider) SetSecret(_ context.Context, _, _ string) error { return nil }
func (m *mockGenericProvider) DeleteSecret(_ context.Context, _ string) error { return nil }
func (m *mockGenericProvider) Validate(_ map[string]string) error             { return nil }
