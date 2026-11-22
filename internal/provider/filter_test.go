package provider

import (
	"context"
	"encoding/json"
	"fmt"
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
		{
			name:          "include with exclude all wildcard keeps included field",
			fieldName:     "UserName",
			includeFields: []string{"UserName"},
			excludeFields: []string{"*"},
			want:          true,
		},
		{
			name:          "include with exclude all wildcard drops unincluded field",
			fieldName:     "Password",
			includeFields: []string{"UserName"},
			excludeFields: []string{"*"},
			want:          false,
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

func TestIsDefaultFieldAllowed(t *testing.T) {
	tests := []struct {
		name          string
		fieldName     string
		entryPath     string
		entryTitle    string
		includeFields []string
		excludeFields []string
		want          bool
	}{
		{
			name:          "include title overrides exclude all wildcard",
			fieldName:     "title",
			entryPath:     "services/api",
			entryTitle:    "API Service",
			includeFields: []string{"title"},
			excludeFields: []string{"*"},
			want:          true,
		},
		{
			name:          "exclude all wildcard drops unincluded tags",
			fieldName:     "tags",
			entryPath:     "services/api",
			entryTitle:    "API Service",
			includeFields: []string{"title"},
			excludeFields: []string{"*"},
			want:          false,
		},
		{
			name:          "exclude all wildcard drops provider",
			fieldName:     "provider",
			entryPath:     "services/api",
			entryTitle:    "API Service",
			includeFields: []string{"title"},
			excludeFields: []string{"*"},
			want:          false,
		},
		{
			name:          "exclude all wildcard drops vault",
			fieldName:     "vault",
			entryPath:     "services/api",
			entryTitle:    "API Service",
			includeFields: []string{"title"},
			excludeFields: []string{"*"},
			want:          false,
		},
		{
			name:          "no filters allows default fields",
			fieldName:     "title",
			entryPath:     "services/api",
			entryTitle:    "API Service",
			includeFields: nil,
			excludeFields: nil,
			want:          true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsDefaultFieldAllowed(tt.fieldName, tt.entryPath, nil, tt.entryTitle, tt.includeFields, tt.excludeFields)
			if got != tt.want {
				t.Errorf("IsDefaultFieldAllowed(%q, %v, %v) = %v, want %v",
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

	includeFields := []string{"env:*", "UserName", "title"}
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

type mockResultsProvider struct {
	mockSecretProvider
	vaultName      string
	results        []SearchResult
	searchExecutor func(context.Context, string, []string, int) ([]SearchResult, error)
}

func (m *mockResultsProvider) Scheme() string { return "search" }

func (m *mockResultsProvider) SetSearchExecutor(exec func(context.Context, string, []string, int) ([]SearchResult, error)) {
	m.searchExecutor = exec
	if res, err := exec(context.Background(), "", nil, 0); err == nil {
		m.results = res
	}
}

func (m *mockResultsProvider) Initialize(ctx context.Context, _ ProviderConfig) error {
	if m.searchExecutor != nil {
		res, err := m.searchExecutor(ctx, "", nil, 0)
		if err != nil {
			return err
		}
		m.results = res
	}
	return nil
}

func (m *mockResultsProvider) Search(ctx context.Context, _ SearchQuery) ([]SearchResult, error) {
	if m.searchExecutor != nil {
		return m.searchExecutor(ctx, "", nil, 0)
	}
	return m.results, nil
}

func (m *mockResultsProvider) currentResults(ctx context.Context) []SearchResult {
	if len(m.results) == 0 && m.searchExecutor != nil {
		if res, err := m.searchExecutor(ctx, "", nil, 0); err == nil {
			m.results = res
		}
	}
	return m.results
}

func (m *mockResultsProvider) GetEntryWithPath(ctx context.Context, location string) (Entry, string, error) {
	res := m.currentResults(ctx)
	for _, r := range res {
		if r.Path == location || r.Entry.Title == location {
			return r.Entry, r.Path, nil
		}
	}
	if len(res) == 1 {
		return res[0].Entry, res[0].Path, nil
	}
	return Entry{}, "", fmt.Errorf("entry %q not found", location)
}

func (m *mockResultsProvider) GetEntry(ctx context.Context, location string) (Entry, error) {
	e, _, err := m.GetEntryWithPath(ctx, location)
	return e, err
}

func (m *mockResultsProvider) lookupAttr(r SearchResult, attr string) (string, any, bool) {
	if attr == "" {
		attr = "Password"
	}
	switch strings.ToLower(attr) {
	case "title":
		return "title", r.Entry.Title, true
	case "tags":
		return "tags", strings.Join(r.Entry.Tags, ","), true
	case "path":
		return "path", r.Path, true
	case "vault":
		return "vault", r.Vault, true
	case "provider":
		return "provider", r.Provider, true
	case "uuid":
		return "uuid", r.Entry.UUID, true
	case "group":
		if r.Entry.Group != nil {
			return "group", r.Entry.Group.Name, true
		}
	case "group_uuid":
		if r.Entry.Group != nil {
			return "group_uuid", r.Entry.Group.UUID, true
		}
	case "group_path":
		if r.Entry.Group != nil {
			return "group_path", r.Entry.Group.Path, true
		}
	case "group_name":
		if r.Entry.Group != nil {
			return "group_name", r.Entry.Group.Name, true
		}
	}
	if v, ok := r.Entry.Attributes[attr]; ok {
		return attr, v, true
	}
	for k, v := range r.Entry.Attributes {
		if strings.EqualFold(k, attr) {
			return k, v, true
		}
	}
	return "", nil, false
}

func (m *mockResultsProvider) GetSecretWithRaw(ctx context.Context, location string) (string, any, string, string, error) {
	res := m.currentResults(ctx)
	if entryLoc, attr, ok := strings.Cut(location, ":"); ok {
		var matches []SearchResult
		for _, r := range res {
			if r.Path == entryLoc || r.Entry.Title == entryLoc {
				matches = append(matches, r)
			}
		}
		if len(matches) == 0 && len(res) == 1 && (entryLoc == "" || entryLoc == "default") {
			matches = res
		}
		for _, r := range matches {
			if ck, v, found := m.lookupAttr(r, attr); found {
				return ck, v, r.Path, r.Entry.Title, nil
			}
		}
		if len(matches) == 0 {
			return "", nil, "", "", fmt.Errorf("entry %q not found", entryLoc)
		}
		return "", nil, "", "", fmt.Errorf("attribute %q not found in %q", attr, entryLoc)
	}
	if len(res) == 1 {
		attr := location
		if location == "default" || location == res[0].Path || location == res[0].Entry.Title {
			attr = "Password"
		}
		if ck, v, found := m.lookupAttr(res[0], attr); found {
			return ck, v, res[0].Path, res[0].Entry.Title, nil
		}
	}
	for _, r := range res {
		if r.Path == location || r.Entry.Title == location {
			if ck, v, found := m.lookupAttr(r, "Password"); found {
				return ck, v, r.Path, r.Entry.Title, nil
			}
		}
	}
	return "", nil, "", "", fmt.Errorf("secret %q not found", location)
}

func (m *mockResultsProvider) GetSecret(ctx context.Context, location string) (string, error) {
	_, v, _, _, err := m.GetSecretWithRaw(ctx, location)
	if err != nil {
		return "", err
	}
	return serializeVal(v)
}

func newTestSearchProvider() *mockResultsProvider {
	return &mockResultsProvider{}
}

func newFilterTestSearchProvider(results ...SearchResult) *mockResultsProvider {
	return &mockResultsProvider{results: results}
}

func TestFilteringProvider(t *testing.T) {
	ctx := context.Background()

	rawProvider := newFilterTestSearchProvider(SearchResult{
		Provider: "mock",
		Vault:    "work",
		Path:     "db_prod",
		Entry: Entry{
			Title: "db_prod",
			Attributes: map[string]any{
				"UserName":     "admin",
				"Password":     "supersecret",
				"env:HOST":     "db.internal",
				"secret:token": "tok123",
				"db.notes":     "production database",
			},
		},
	})

	includeFields := []string{"env:*", "UserName"}
	excludeFields := []string{"*.notes", "secret:*"}

	fp := NewFilteringProvider(rawProvider, includeFields, excludeFields)

	if fp.Scheme() != "search" {
		t.Errorf("Scheme() = %q, want %q", fp.Scheme(), "search")
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

func TestFilteringProvider_BareLocation_Password(t *testing.T) {
	ctx := context.Background()
	rawProvider := newFilterTestSearchProvider(SearchResult{
		Provider: "mock",
		Vault:    "work",
		Path:     "db_prod",
		Entry: Entry{
			Title: "db_prod",
			Attributes: map[string]any{
				"UserName": "admin",
				"Password": "supersecret",
			},
		},
	})

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
	sp := newTestSearchProvider()
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
	sp := newTestSearchProvider()
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

func TestFilteringProvider_Search_DuplicatePath_MultiResult_MixedCase(t *testing.T) {
	ctx := context.Background()

	t.Run("DuplicatePath_FieldExclusionOnCanonicalKey", func(t *testing.T) {
		// Two results with identical Path "app/config".
		// First result lacks the requested field, second result supplies it.
		sp := newTestSearchProvider()
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
		sp := newTestSearchProvider()
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
		sp := newTestSearchProvider()
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

		// Case-mismatched query is rejected because include filter is case-sensitive
		_, errUpper := fp.GetSecret(ctx, "app/config:API_KEY")
		if errUpper == nil {
			t.Fatalf("expected error for API_KEY under case-sensitive include, got nil")
		}
	})
}

func TestFilteringProvider_Search_SinglePassConsistency(t *testing.T) {
	ctx := context.Background()

	// Stateful search executor that returns different results on each successive call
	callCount := 0
	sp := newTestSearchProvider()
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

func TestFilteringProvider_Search_EntryPathCandidates(t *testing.T) {
	ctx := context.Background()
	sp := newFilterTestSearchProvider(SearchResult{
		Provider: "mock",
		Vault:    "work",
		Path:     "my_entity",
		Entry: Entry{
			Title: "my_entity",
			Attributes: map[string]any{
				"Password": "my_secret_pass",
				"UserName": "my_user",
			},
		},
	})

	fp := NewFilteringProvider(sp, nil, []string{"my_entity.Password"})

	_, err := fp.GetSecret(ctx, "my_entity:Password")
	if err == nil {
		t.Fatalf("expected error for my_entity:Password, got nil")
	}
	if !strings.Contains(err.Error(), "excluded") {
		t.Errorf("unexpected error message: %v", err)
	}

	_, err = fp.GetSecret(ctx, "my_entity")
	if err == nil {
		t.Fatalf("expected error for bare my_entity, got nil")
	}

	val, err := fp.GetSecret(ctx, "my_entity:UserName")
	if err != nil {
		t.Fatalf("GetSecret(my_entity:UserName) failed: %v", err)
	}
	if val != "my_user" {
		t.Errorf("UserName = %q, want 'my_user'", val)
	}

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

	fpAll := NewFilteringProvider(sp, nil, []string{"my_entity"})
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

	sp := newTestSearchProvider()
	sp.vaultName = "search_vault"
	sp.searchExecutor = func(_ context.Context, _ string, _ []string, _ int) ([]SearchResult, error) {
		return []SearchResult{
			{
				Provider: "mock",
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

func TestFilteringProvider_Search_StructuredProjection(t *testing.T) {
	ctx := context.Background()
	sp := newTestSearchProvider()
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

func TestSearchableFilteringProvider_GetEntry_AuthoritativePathVsCallerAlias(t *testing.T) {
	ctx := context.Background()

	// Single-result SearchProvider:
	// Result path is "guest/account". Allowlist includes only "admin/*".
	// Caller requesting "admin/account" must NOT inherit inclusion.
	sp := newTestSearchProvider()
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

	sp := newFilterTestSearchProvider(SearchResult{
		Provider: "keepass",
		Vault:    "work",
		Path:     "services/backend",
		Entry: Entry{
			Title: "Backend Service",
			Attributes: map[string]any{
				"api_key":  "secret_key_999",
				"endpoint": "https://service.internal",
			},
		},
	})

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
}

func TestFilteringProvider_ColonQualifiedCandidateInclusionAndExclusion(t *testing.T) {
	ctx := context.Background()

	sp := newFilterTestSearchProvider(SearchResult{
		Provider: "mock",
		Vault:    "work",
		Path:     "app",
		Entry: Entry{
			Title: "app",
			Attributes: map[string]any{
				"Password": "secret_password",
				"UserName": "test_user",
			},
		},
	})

	// 1. Colon-qualified exclude_fields: ["app:*"]
	fpExcl := NewFilteringProvider(sp, nil, []string{"app:*"})
	sfpExcl := fpExcl.(SearchableProvider)

	entryExcl, err := sfpExcl.GetEntry(ctx, "app")
	if err != nil {
		t.Fatalf("GetEntry failed: %v", err)
	}
	if len(entryExcl.Attributes) != 0 {
		t.Errorf("expected 0 attributes when colon-qualified exclude_fields ['app:*'] is configured, got %v", entryExcl.Attributes)
	}

	resultsExcl, err := sfpExcl.Search(ctx, SearchQuery{})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(resultsExcl) != 1 {
		t.Fatalf("expected 1 search result, got %d", len(resultsExcl))
	}
	if len(resultsExcl[0].Entry.Attributes) != 0 {
		t.Errorf("expected 0 search result attributes when 'app:*' is excluded, got %v", resultsExcl[0].Entry.Attributes)
	}

	// 2. Colon-qualified include_fields: ["app:UserName"]
	fpInc := NewFilteringProvider(sp, []string{"app:UserName"}, nil)
	sfpInc := fpInc.(SearchableProvider)

	entryInc, err := sfpInc.GetEntry(ctx, "app")
	if err != nil {
		t.Fatalf("GetEntry failed: %v", err)
	}
	if entryInc.Attributes["UserName"] != "test_user" {
		t.Errorf("expected UserName 'test_user' in GetEntry, got %v", entryInc.Attributes)
	}
	if _, ok := entryInc.Attributes["Password"]; ok {
		t.Errorf("expected Password to be filtered out by colon-qualified include_fields ['app:UserName']")
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

func TestDefaultFieldsFiltering(t *testing.T) {
	ctx := context.Background()

	t.Run("ExcludeFields_DefaultMetadataFields", func(t *testing.T) {
		sp := newTestSearchProvider()
		sp.SetSearchExecutor(func(ctx context.Context, query string, sourceVaults []string, depth int) ([]SearchResult, error) {
			return []SearchResult{
				{
					Provider: "keepass",
					Vault:    "main_vault",
					Path:     "services/api",
					Entry: Entry{
						Title: "API Key Service",
						Tags:  []string{"prod", "backend"},
						Attributes: map[string]any{
							"api_key": "secret_key_123",
						},
					},
				},
			}, nil
		})

		// exclude_fields: ["tags", "title", "path", "vault", "provider"]
		fp := NewFilteringProvider(sp, nil, []string{"tags", "Title", "*path", "vault", "PROVIDER"})
		sfp := fp.(SearchableProvider)

		results, err := sfp.Search(ctx, SearchQuery{})
		if err != nil {
			t.Fatalf("Search failed: %v", err)
		}
		if len(results) != 1 {
			t.Fatalf("expected 1 result, got %d", len(results))
		}

		res := results[0]
		if res.Provider != "" {
			t.Errorf("expected Provider to be excluded, got %q", res.Provider)
		}
		if res.Vault != "" {
			t.Errorf("expected Vault to be excluded, got %q", res.Vault)
		}
		if res.Path != "" {
			t.Errorf("expected Path to be excluded, got %q", res.Path)
		}
		if res.Entry.Title != "" {
			t.Errorf("expected Title to be excluded, got %q", res.Entry.Title)
		}
		if res.Entry.Tags != nil {
			t.Errorf("expected Tags to be excluded, got %v", res.Entry.Tags)
		}
		if res.Entry.Attributes["api_key"] != "secret_key_123" {
			t.Errorf("expected api_key to be preserved, got %v", res.Entry.Attributes["api_key"])
		}
	})

	t.Run("IncludeFields_StripsDefaultFieldsIfNotMatched", func(t *testing.T) {
		sp := newTestSearchProvider()
		sp.SetSearchExecutor(func(ctx context.Context, query string, sourceVaults []string, depth int) ([]SearchResult, error) {
			return []SearchResult{
				{
					Provider: "keepass",
					Vault:    "main_vault",
					Path:     "services/api",
					Entry: Entry{
						Title: "API Key Service",
						Tags:  []string{"prod"},
						Attributes: map[string]any{
							"api_key": "secret_key_123",
						},
					},
				},
			}, nil
		})

		// include_fields: ["api_key"] -> title, tags, path, vault, provider must be stripped
		fp := NewFilteringProvider(sp, []string{"api_key"}, nil)
		sfp := fp.(SearchableProvider)

		results, err := sfp.Search(ctx, SearchQuery{})
		if err != nil {
			t.Fatalf("Search failed: %v", err)
		}

		res := results[0]
		if res.Provider != "" || res.Vault != "" || res.Path != "" || res.Entry.Title != "" || res.Entry.Tags != nil {
			t.Errorf("expected default fields stripped, got Provider=%q Vault=%q Path=%q Title=%q Tags=%v",
				res.Provider, res.Vault, res.Path, res.Entry.Title, res.Entry.Tags)
		}

		// include_fields: ["title", "api_key"] -> title kept, others stripped
		fp2 := NewFilteringProvider(sp, []string{"Title", "api_key"}, nil)
		sfp2 := fp2.(SearchableProvider)

		results2, err := sfp2.Search(ctx, SearchQuery{})
		if err != nil {
			t.Fatalf("Search failed: %v", err)
		}

		res2 := results2[0]
		if res2.Entry.Title != "API Key Service" {
			t.Errorf("expected Title 'API Key Service', got %q", res2.Entry.Title)
		}
		if res2.Entry.Tags != nil {
			t.Errorf("expected Tags stripped, got %v", res2.Entry.Tags)
		}
	})

	t.Run("GetSecret_RejectsExcludedTitleAndTags", func(t *testing.T) {
		sp := newTestSearchProvider()
		sp.SetSearchExecutor(func(ctx context.Context, query string, sourceVaults []string, depth int) ([]SearchResult, error) {
			return []SearchResult{
				{
					Vault: "main_vault",
					Path:  "services/api",
					Entry: Entry{
						Title: "API Key Service",
						Tags:  []string{"prod"},
						Attributes: map[string]any{
							"api_key": "secret_key_123",
						},
					},
				},
			}, nil
		})

		fp := NewFilteringProvider(sp, nil, []string{"*.tags", "title"})

		_, errTitle := fp.GetSecret(ctx, "services/api:title")
		if errTitle == nil {
			t.Errorf("expected error for excluded title, got nil")
		}

		_, errTags := fp.GetSecret(ctx, "services/api:tags")
		if errTags == nil {
			t.Errorf("expected error for excluded tags, got nil")
		}
	})
}

func TestFilterEntryWithPath_UUIDAndGroup(t *testing.T) {
	baseEntry := Entry{
		Title: "Test Entry",
		Tags:  []string{"prod", "web"},
		UUID:  "12345678123456781234567812345678",
		Group: &GroupInfo{
			Name: "mygroup",
			UUID: "87654321876543218765432187654321",
			Path: "mygroup",
		},
		Attributes: map[string]any{
			"username": "admin",
		},
	}

	t.Run("exclude_fields uuid case-insensitive", func(t *testing.T) {
		res := FilterEntryWithPath(baseEntry, "folder/entry", nil, nil, []string{"uuid"})
		if res.UUID != "" {
			t.Errorf("expected UUID stripped, got %q", res.UUID)
		}
		if res.Group == nil || res.Group.Name != "mygroup" {
			t.Errorf("expected Group preserved, got %+v", res.Group)
		}

		resUpper := FilterEntryWithPath(baseEntry, "folder/entry", nil, nil, []string{"UUID"})
		if resUpper.UUID != "" {
			t.Errorf("expected UUID stripped by uppercase pattern, got %q", resUpper.UUID)
		}
	})

	t.Run("exclude_fields group wildcard", func(t *testing.T) {
		res := FilterEntryWithPath(baseEntry, "folder/entry", nil, nil, []string{"group*"})
		if res.Group != nil {
			t.Errorf("expected Group stripped, got %+v", res.Group)
		}
		if res.UUID != baseEntry.UUID {
			t.Errorf("expected UUID preserved, got %q", res.UUID)
		}
	})

	t.Run("include_fields uuid", func(t *testing.T) {
		res := FilterEntryWithPath(baseEntry, "folder/entry", nil, []string{"uuid"}, nil)
		if res.UUID != baseEntry.UUID {
			t.Errorf("expected UUID preserved, got %q", res.UUID)
		}
		if res.Group != nil {
			t.Errorf("expected Group stripped, got %+v", res.Group)
		}
		if res.Title != "" {
			t.Errorf("expected Title stripped, got %q", res.Title)
		}
	})

	t.Run("include_fields group", func(t *testing.T) {
		res := FilterEntryWithPath(baseEntry, "folder/entry", nil, []string{"group"}, nil)
		if res.Group == nil || res.Group.Name != "mygroup" {
			t.Errorf("expected Group preserved, got %+v", res.Group)
		}
		if res.UUID != "" {
			t.Errorf("expected UUID stripped, got %q", res.UUID)
		}
	})

	t.Run("include takes precedence over wildcard exclude", func(t *testing.T) {
		res := FilterEntryWithPath(baseEntry, "folder/entry", nil, []string{"uuid"}, []string{"*"})
		if res.UUID != baseEntry.UUID {
			t.Errorf("expected UUID preserved when included alongside '*', got %q", res.UUID)
		}
		if res.Group != nil {
			t.Errorf("expected Group stripped by '*', got %+v", res.Group)
		}
	})
}

func TestFilteringProvider_KeePass_UUIDAndGroup(t *testing.T) {
	keyring.MockInit()
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Setenv("LocalAppData", t.TempDir())

	ctx := context.Background()
	if err := keyring.Set("cloakenv", "provider/testkp_meta", "password123"); err != nil {
		t.Fatalf("failed to set mock credentials: %v", err)
	}

	kp := NewKeePassProvider()
	if err := kp.Initialize(ctx, ProviderConfig{
		Settings: map[string]string{
			"vault_path":  "../../testdata/testDB.kdbx",
			"remote_name": "testkp_meta",
		},
	}); err != nil {
		t.Fatalf("failed to initialize KeePass provider: %v", err)
	}

	t.Run("exclude_fields uuid blocks both :UUID and :uuid", func(t *testing.T) {
		fp := NewFilteringProvider(kp, nil, []string{"uuid"})

		_, errUpper := fp.GetSecret(ctx, "website/Test Website:UUID")
		if errUpper == nil || !strings.Contains(errUpper.Error(), "excluded") {
			t.Errorf("expected error for :UUID when uuid excluded, got: %v", errUpper)
		}

		_, errLower := fp.GetSecret(ctx, "website/Test Website:uuid")
		if errLower == nil || !strings.Contains(errLower.Error(), "excluded") {
			t.Errorf("expected error for :uuid when uuid excluded, got: %v", errLower)
		}

		grp, err := fp.GetSecret(ctx, "website/Test Website:group")
		if err != nil || grp != "website" {
			t.Errorf("expected group 'website', got %q, err: %v", grp, err)
		}

		sfp := fp.(SearchableProvider)
		entry, err := sfp.GetEntry(ctx, "website/Test Website")
		if err != nil {
			t.Fatalf("GetEntry failed: %v", err)
		}
		if entry.UUID != "" {
			t.Errorf("expected UUID stripped in GetEntry, got %q", entry.UUID)
		}
		if entry.Group == nil || entry.Group.Name != "website" {
			t.Errorf("expected Group preserved in GetEntry, got %+v", entry.Group)
		}

		results, err := sfp.Search(ctx, SearchQuery{Title: "Test Website"})
		if err != nil || len(results) == 0 {
			t.Fatalf("Search failed: %v", err)
		}
		if results[0].Entry.UUID != "" {
			t.Errorf("expected UUID stripped in Search, got %q", results[0].Entry.UUID)
		}
		if results[0].Entry.Group == nil || results[0].Entry.Group.Name != "website" {
			t.Errorf("expected Group preserved in Search, got %+v", results[0].Entry.Group)
		}
	})

	t.Run("exclude_fields group wildcard blocks group selectors and strips Group", func(t *testing.T) {
		fp := NewFilteringProvider(kp, nil, []string{"group*"})

		selectors := []string{"group", "group_name", "group_uuid", "group_path"}
		for _, sel := range selectors {
			_, err := fp.GetSecret(ctx, "website/Test Website:"+sel)
			if err == nil || !strings.Contains(err.Error(), "excluded") {
				t.Errorf("expected error for :%s when group* excluded, got: %v", sel, err)
			}
		}

		uVal, err := fp.GetSecret(ctx, "website/Test Website:uuid")
		if err != nil || len(uVal) != 32 {
			t.Errorf("expected 32-char hex UUID, got %q, err: %v", uVal, err)
		}

		sfp := fp.(SearchableProvider)
		entry, err := sfp.GetEntry(ctx, "website/Test Website")
		if err != nil {
			t.Fatalf("GetEntry failed: %v", err)
		}
		if entry.Group != nil {
			t.Errorf("expected Group stripped in GetEntry, got %+v", entry.Group)
		}
		if len(entry.UUID) != 32 {
			t.Errorf("expected UUID preserved in GetEntry, got %q", entry.UUID)
		}

		results, err := sfp.Search(ctx, SearchQuery{Title: "Test Website"})
		if err != nil || len(results) == 0 {
			t.Fatalf("Search failed: %v", err)
		}
		if results[0].Entry.Group != nil {
			t.Errorf("expected Group stripped in Search, got %+v", results[0].Entry.Group)
		}
		if len(results[0].Entry.UUID) != 32 {
			t.Errorf("expected UUID preserved in Search, got %q", results[0].Entry.UUID)
		}
	})

	t.Run("include_fields uuid allows :UUID and :uuid, blocks group", func(t *testing.T) {
		fp := NewFilteringProvider(kp, []string{"uuid"}, nil)

		uVal, err := fp.GetSecret(ctx, "website/Test Website:UUID")
		if err != nil || len(uVal) != 32 {
			t.Errorf("expected :UUID allowed, got %q, err: %v", uVal, err)
		}

		_, errGrp := fp.GetSecret(ctx, "website/Test Website:group")
		if errGrp == nil || !strings.Contains(errGrp.Error(), "excluded") {
			t.Errorf("expected group excluded when only uuid included, got: %v", errGrp)
		}

		sfp := fp.(SearchableProvider)
		entry, err := sfp.GetEntry(ctx, "website/Test Website")
		if err != nil {
			t.Fatalf("GetEntry failed: %v", err)
		}
		if len(entry.UUID) != 32 {
			t.Errorf("expected UUID preserved, got %q", entry.UUID)
		}
		if entry.Group != nil {
			t.Errorf("expected Group stripped, got %+v", entry.Group)
		}
	})

	t.Run("include_fields group_name allows :group_name", func(t *testing.T) {
		fp := NewFilteringProvider(kp, []string{"group_name"}, nil)

		grp, err := fp.GetSecret(ctx, "website/Test Website:group_name")
		if err != nil || grp != "website" {
			t.Errorf("expected group_name 'website', got %q, err: %v", grp, err)
		}

		_, errUUID := fp.GetSecret(ctx, "website/Test Website:uuid")
		if errUUID == nil || !strings.Contains(errUUID.Error(), "excluded") {
			t.Errorf("expected uuid excluded when only group_name included, got: %v", errUUID)
		}
	})
}

func TestFilteringProvider_Search_UUIDAndGroup(t *testing.T) {
	ctx := context.Background()

	newMockSearchProvider := func() *mockResultsProvider {
		sp := newTestSearchProvider()
		sp.SetSearchExecutor(func(ctx context.Context, query string, sourceVaults []string, depth int) ([]SearchResult, error) {
			return []SearchResult{
				{
					Provider: "mock",
					Vault:    "main_vault",
					Path:     "services/api",
					Entry: Entry{
						Title: "API Service",
						Tags:  []string{"prod"},
						UUID:  "mock-entry-uuid-1234",
						Group: &GroupInfo{
							Name: "infra",
							UUID: "mock-group-uuid-5678",
							Path: "infra",
						},
						Attributes: map[string]any{
							"api_key": "secret123",
						},
					},
				},
			}, nil
		})
		return sp
	}

	t.Run("exclude_fields uuid blocks :UUID and :uuid", func(t *testing.T) {
		sp := newMockSearchProvider()
		fp := NewFilteringProvider(sp, nil, []string{"uuid"})

		_, errUpper := fp.GetSecret(ctx, "services/api:UUID")
		if errUpper == nil || !strings.Contains(errUpper.Error(), "excluded") {
			t.Errorf("expected error for :UUID when uuid excluded, got: %v", errUpper)
		}

		_, errLower := fp.GetSecret(ctx, "services/api:uuid")
		if errLower == nil || !strings.Contains(errLower.Error(), "excluded") {
			t.Errorf("expected error for :uuid when uuid excluded, got: %v", errLower)
		}

		grp, err := fp.GetSecret(ctx, "services/api:group")
		if err != nil || grp != "infra" {
			t.Errorf("expected group 'infra', got %q, err: %v", grp, err)
		}

		sfp := fp.(SearchableProvider)
		entry, err := sfp.GetEntry(ctx, "services/api")
		if err != nil {
			t.Fatalf("GetEntry failed: %v", err)
		}
		if entry.UUID != "" {
			t.Errorf("expected UUID stripped in GetEntry, got %q", entry.UUID)
		}
		if entry.Group == nil || entry.Group.Name != "infra" {
			t.Errorf("expected Group preserved in GetEntry, got %+v", entry.Group)
		}

		results, err := sfp.Search(ctx, SearchQuery{})
		if err != nil || len(results) == 0 {
			t.Fatalf("Search failed: %v", err)
		}
		if results[0].Entry.UUID != "" {
			t.Errorf("expected UUID stripped in Search, got %q", results[0].Entry.UUID)
		}
		if results[0].Entry.Group == nil || results[0].Entry.Group.Name != "infra" {
			t.Errorf("expected Group preserved in Search, got %+v", results[0].Entry.Group)
		}
	})

	t.Run("exclude_fields group wildcard blocks group selectors and strips Group", func(t *testing.T) {
		sp := newMockSearchProvider()
		fp := NewFilteringProvider(sp, nil, []string{"group*"})

		selectors := []string{"group", "group_name", "group_uuid", "group_path"}
		for _, sel := range selectors {
			_, err := fp.GetSecret(ctx, "services/api:"+sel)
			if err == nil || !strings.Contains(err.Error(), "excluded") {
				t.Errorf("expected error for :%s when group* excluded, got: %v", sel, err)
			}
		}

		uVal, err := fp.GetSecret(ctx, "services/api:uuid")
		if err != nil || uVal != "mock-entry-uuid-1234" {
			t.Errorf("expected UUID allowed, got %q, err: %v", uVal, err)
		}

		sfp := fp.(SearchableProvider)
		entry, err := sfp.GetEntry(ctx, "services/api")
		if err != nil {
			t.Fatalf("GetEntry failed: %v", err)
		}
		if entry.Group != nil {
			t.Errorf("expected Group stripped in GetEntry, got %+v", entry.Group)
		}
		if entry.UUID != "mock-entry-uuid-1234" {
			t.Errorf("expected UUID preserved in GetEntry, got %q", entry.UUID)
		}

		results, err := sfp.Search(ctx, SearchQuery{})
		if err != nil || len(results) == 0 {
			t.Fatalf("Search failed: %v", err)
		}
		if results[0].Entry.Group != nil {
			t.Errorf("expected Group stripped in Search, got %+v", results[0].Entry.Group)
		}
		if results[0].Entry.UUID != "mock-entry-uuid-1234" {
			t.Errorf("expected UUID preserved in Search, got %q", results[0].Entry.UUID)
		}
	})

	t.Run("include_fields uuid allows :UUID and :uuid, blocks group", func(t *testing.T) {
		sp := newMockSearchProvider()
		fp := NewFilteringProvider(sp, []string{"uuid"}, nil)

		uVal, err := fp.GetSecret(ctx, "services/api:UUID")
		if err != nil || uVal != "mock-entry-uuid-1234" {
			t.Errorf("expected :UUID allowed, got %q, err: %v", uVal, err)
		}

		_, errGrp := fp.GetSecret(ctx, "services/api:group")
		if errGrp == nil || !strings.Contains(errGrp.Error(), "excluded") {
			t.Errorf("expected group excluded when only uuid included, got: %v", errGrp)
		}

		sfp := fp.(SearchableProvider)
		entry, err := sfp.GetEntry(ctx, "services/api")
		if err != nil {
			t.Fatalf("GetEntry failed: %v", err)
		}
		if entry.UUID != "mock-entry-uuid-1234" {
			t.Errorf("expected UUID preserved, got %q", entry.UUID)
		}
		if entry.Group != nil {
			t.Errorf("expected Group stripped, got %+v", entry.Group)
		}
	})

	t.Run("include_fields group_name allows :group_name", func(t *testing.T) {
		sp := newMockSearchProvider()
		fp := NewFilteringProvider(sp, []string{"group_name"}, nil)

		grp, err := fp.GetSecret(ctx, "services/api:group_name")
		if err != nil || grp != "infra" {
			t.Errorf("expected group_name 'infra', got %q, err: %v", grp, err)
		}

		_, errUUID := fp.GetSecret(ctx, "services/api:uuid")
		if errUUID == nil || !strings.Contains(errUUID.Error(), "excluded") {
			t.Errorf("expected uuid excluded when only group_name included, got: %v", errUUID)
		}
	})
}

func TestFieldFiltering_MetaTags(t *testing.T) {
	entry := Entry{
		Title: "my-entry",
		Tags:  []string{"prod", "web"},
		Attributes: map[string]any{
			"username": "admin",
			"password": "secret",
		},
	}

	t.Run("ExcludeMetaTags_StripsTags", func(t *testing.T) {
		filtered := FilterEntryWithPath(entry, "my-entry", nil, nil, []string{"_meta_.tags"})
		if len(filtered.Tags) != 0 {
			t.Errorf("expected tags to be stripped when _meta_.tags excluded, got %v", filtered.Tags)
		}
		if filtered.Attributes["username"] != "admin" {
			t.Errorf("expected username preserved, got %v", filtered.Attributes["username"])
		}
	})

	t.Run("ExcludeMetaWildcard_StripsTags", func(t *testing.T) {
		filtered := FilterEntryWithPath(entry, "my-entry", nil, nil, []string{"_meta_"})
		if len(filtered.Tags) != 0 {
			t.Errorf("expected tags to be stripped when _meta_ excluded, got %v", filtered.Tags)
		}
	})

	t.Run("IncludeMetaTags_PreservesTagsAndExcludesOthers", func(t *testing.T) {
		filtered := FilterEntryWithPath(entry, "my-entry", nil, []string{"_meta_.tags"}, nil)
		if len(filtered.Tags) != 2 {
			t.Errorf("expected tags preserved when _meta_.tags included, got %v", filtered.Tags)
		}
		if _, ok := filtered.Attributes["username"]; ok {
			t.Errorf("expected username excluded when only _meta_.tags included")
		}
	})

	t.Run("FilterSearchResult_MetaTagsAndProvider", func(t *testing.T) {
		sr := SearchResult{
			Provider: "keepass",
			Vault:    "work",
			Path:     "servers/db",
			Entry:    entry,
		}

		res := FilterSearchResult(sr, nil, []string{"_meta_.tags", "_meta_.vault"}, nil)
		if len(res.Entry.Tags) != 2 {
			t.Errorf("expected tags preserved, got %v", res.Entry.Tags)
		}
		if res.Vault != "work" {
			t.Errorf("expected vault preserved, got %q", res.Vault)
		}
		if res.Provider != "" {
			t.Errorf("expected provider stripped when only _meta_.tags and _meta_.vault included, got %q", res.Provider)
		}
	})

	t.Run("CandidateBuilding_Tags", func(t *testing.T) {
		allowed := IsDefaultFieldAllowed("tags", "app/service", nil, "service", []string{"_meta_.tags"}, nil)
		if !allowed {
			t.Errorf("expected tags to be allowed when _meta_.tags is in include_fields")
		}

		disallowed := IsDefaultFieldAllowed("tags", "app/service", nil, "service", nil, []string{"_meta_.tags"})
		if disallowed {
			t.Errorf("expected tags to be disallowed when _meta_.tags is in exclude_fields")
		}
	})
}

type mockStructuredProvider struct {
	scheme       string
	entries      map[string]Entry
	secrets      map[string]string
	rootPrefixes []string
}

func (m *mockStructuredProvider) Scheme() string {
	if m.scheme != "" {
		return m.scheme
	}
	return "mock"
}

func (m *mockStructuredProvider) Initialize(_ context.Context, _ ProviderConfig) error {
	return nil
}

func (m *mockStructuredProvider) SetSecret(_ context.Context, _, _ string) error {
	return nil
}

func (m *mockStructuredProvider) DeleteSecret(_ context.Context, _ string) error {
	return nil
}

func (m *mockStructuredProvider) Validate(_ map[string]string) error {
	return nil
}

func (m *mockStructuredProvider) RootPrefixes() []string {
	return m.rootPrefixes
}

func (m *mockStructuredProvider) GetSecret(_ context.Context, location string) (string, error) {
	if val, ok := m.secrets[location]; ok {
		return val, nil
	}
	if strings.Contains(location, ":") {
		parts := strings.SplitN(location, ":", 2)
		if e, ok := m.entries[parts[0]]; ok {
			if v, ok := e.Attributes[parts[1]]; ok {
				return fmt.Sprint(v), nil
			}
			return "", fmt.Errorf("attribute %q not found in entry %q", parts[1], parts[0])
		}
		return "", fmt.Errorf("entry %q not found", parts[0])
	}
	if strings.Contains(location, ".") {
		parts := strings.Split(location, ".")
		entryKey := parts[0]
		remaining := parts[1:]
		if len(m.rootPrefixes) > 0 && entryKey == m.rootPrefixes[0] && len(parts) > 1 {
			entryKey = parts[1]
			remaining = parts[2:]
		}
		if e, ok := m.entries[entryKey]; ok {
			if len(remaining) == 0 {
				data, _ := json.Marshal(e.Attributes)
				return string(data), nil
			}
			curr := any(e.Attributes)
			for _, part := range remaining {
				if mCurr, ok := curr.(map[string]any); ok {
					curr = mCurr[part]
				} else {
					return "", fmt.Errorf("path %q not found", location)
				}
			}
			if curr != nil {
				if str, ok := curr.(string); ok {
					return str, nil
				}
				data, _ := json.Marshal(curr)
				return string(data), nil
			}
		}
	}
	return "", fmt.Errorf("secret not found: %s", location)
}

func (m *mockStructuredProvider) GetEntry(_ context.Context, location string) (Entry, error) {
	if e, ok := m.entries[location]; ok {
		return e, nil
	}
	return Entry{}, fmt.Errorf("entry %q not found", location)
}

func (m *mockStructuredProvider) Search(_ context.Context, _ SearchQuery) ([]SearchResult, error) {
	var res []SearchResult
	for path, e := range m.entries {
		res = append(res, SearchResult{
			Provider: m.Scheme(),
			Vault:    m.Scheme(),
			Path:     path,
			Entry:    e,
		})
	}
	return res, nil
}

func TestFilteringProvider_StructuredOutput_RecursiveCanonicalProjection(t *testing.T) {
	ctx := context.Background()

	initProvider := func() SecretProvider {
		return &mockStructuredProvider{
			scheme:       "test_mock",
			rootPrefixes: []string{"entities"},
			entries: map[string]Entry{
				"db": {
					Title: "db",
					Attributes: map[string]any{
						"credentials": map[string]any{
							"token": "secret_token_123",
							"user":  "db_admin",
						},
						"accounts": []any{
							map[string]any{"token": "acct_token_1", "name": "acct1"},
							map[string]any{"token": "acct_token_2", "name": "acct2"},
						},
						"flat.dotted.secret": "flat_secret_val",
						"flat.dotted.safe":   "flat_safe_val",
					},
				},
			},
		}
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

func TestFilteringProvider_Static_AncestorExclusion(t *testing.T) {
	ctx := context.Background()
	p := &mockStructuredProvider{
		scheme:       "ancestor_mock",
		rootPrefixes: []string{"entities"},
		entries: map[string]Entry{
			"db": {
				Title: "db",
				Attributes: map[string]any{
					"username": "db_admin",
					"port":     5432,
				},
			},
			"cache": {
				Title: "cache",
				Attributes: map[string]any{
					"host": "redis.internal",
				},
			},
		},
	}

	fp := NewFilteringProvider(p, nil, []string{"db"})

	_, err := fp.GetSecret(ctx, "db.username")
	if err == nil {
		t.Fatalf("expected error for db.username when ancestor 'db' is excluded, got nil")
	}
	if !strings.Contains(err.Error(), "excluded") {
		t.Errorf("unexpected error message: %v", err)
	}

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
	p := &mockStructuredProvider{
		scheme:       "ancestor_struct",
		rootPrefixes: []string{"entities"},
		entries: map[string]Entry{
			"db": {
				Title: "db",
				Attributes: map[string]any{
					"username": "db_admin",
					"port":     5432,
				},
			},
			"cache": {
				Title: "cache",
				Attributes: map[string]any{
					"host": "redis.internal",
				},
			},
		},
	}

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

	results, err := sfp.Search(ctx, SearchQuery{})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	for _, r := range results {
		if r.Path == "db" && len(r.Entry.Attributes) != 0 {
			t.Errorf("Search result for 'db' exposed attributes: %v", r.Entry.Attributes)
		}
	}
}

func TestFilteringProvider_TitleResolutionError(t *testing.T) {
	ctx := context.Background()
	p := &mockStructuredProvider{
		scheme: "custom_mock",
		entries: map[string]Entry{
			"valid_entity": {
				Title: "Valid Title",
				Attributes: map[string]any{
					"Password": "secret",
				},
			},
		},
	}

	fp := NewFilteringProvider(p, nil, []string{"Valid Title.Password"})

	_, err := fp.GetSecret(ctx, "missing_entity:Password")
	if err == nil {
		t.Errorf("expected error when resolving nonexistent entity, got nil")
	}
}

func TestFilteringProvider_AncestorInclusionIsolation(t *testing.T) {
	ctx := context.Background()
	p := &mockStructuredProvider{
		scheme:       "ancestor_inc",
		rootPrefixes: []string{"entities"},
		entries: map[string]Entry{
			"db": {
				Title: "db",
				Attributes: map[string]any{
					"username": "db_user",
					"password": "db_password",
				},
			},
		},
	}

	fp := NewFilteringProvider(p, []string{"entities"}, nil)

	_, err := fp.GetSecret(ctx, "db.username")
	if err == nil {
		t.Errorf("expected GetSecret(db.username) to be rejected by include_fields: ['entities'], got nil")
	}

	sfp := fp.(SearchableProvider)
	entry, err := sfp.GetEntry(ctx, "db")
	if err != nil {
		t.Fatalf("GetEntry failed: %v", err)
	}
	if len(entry.Attributes) != 0 {
		t.Errorf("expected empty attributes with include_fields: ['entities'], got: %v", entry.Attributes)
	}
}
