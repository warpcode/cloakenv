package provider

import (
	"context"
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
}
