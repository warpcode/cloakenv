package provider

import (
	"context"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestResolveDotPath(t *testing.T) {
	tests := []struct {
		name       string
		val        any
		path       string
		want       any
		wantErr    bool
		wantErrMsg string
	}{
		{
			name: "empty path",
			val:  map[string]any{"a": 1},
			path: "",
			want: map[string]any{"a": 1},
		},
		{
			name: "path with empty parts ignored",
			val:  map[string]any{"a": map[string]any{"b": 2}},
			path: ".a..b.",
			want: 2,
		},
		{
			name: "valid path map[string]any",
			val:  map[string]any{"a": map[string]any{"b": 3}},
			path: "a.b",
			want: 3,
		},
		{
			name: "valid path map[any]any",
			val:  map[any]any{"a": map[any]any{"b": 4}},
			path: "a.b",
			want: 4,
		},
		{
			name: "valid path map[any]any with non-string keys",
			val:  map[any]any{1: map[any]any{2: 4}},
			path: "1.2",
			want: 4,
		},
		{
			name: "valid path []any",
			val:  []any{10, 20, []any{30, 40}},
			path: "2.1",
			want: 40,
		},
		{
			name:       "invalid key map[string]any",
			val:        map[string]any{"a": 1},
			path:       "b",
			wantErr:    true,
			wantErrMsg: `key "b" not found`,
		},
		{
			name:       "invalid key map[any]any",
			val:        map[any]any{"a": 1},
			path:       "b",
			wantErr:    true,
			wantErrMsg: `key "b" not found`,
		},
		{
			name:       "invalid array index non-integer",
			val:        []any{1, 2},
			path:       "foo",
			wantErr:    true,
			wantErrMsg: `cannot index array with non-integer "foo"`,
		},
		{
			name:       "invalid array index out of bounds",
			val:        []any{1, 2},
			path:       "2",
			wantErr:    true,
			wantErrMsg: "index 2 out of bounds",
		},
		{
			name:       "invalid array index negative",
			val:        []any{1, 2},
			path:       "-1",
			wantErr:    true,
			wantErrMsg: "index -1 out of bounds",
		},
		{
			name:       "traverse unsupported type",
			val:        map[string]any{"a": "string"},
			path:       "a.b",
			wantErr:    true,
			wantErrMsg: `cannot traverse key "b" on value of type string`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := resolveDotPath(tt.val, tt.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("resolveDotPath() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && err != nil && tt.wantErrMsg != "" && !strings.Contains(err.Error(), tt.wantErrMsg) {
				t.Errorf("resolveDotPath() error = %v, want error msg to contain %q", err, tt.wantErrMsg)
			}
			if !tt.wantErr && !reflect.DeepEqual(got, tt.want) {
				t.Errorf("resolveDotPath() got = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestStaticProvider_Scheme(t *testing.T) {
	tests := []struct {
		name   string
		scheme string
	}{
		{name: "json scheme", scheme: "json"},
		{name: "yaml scheme", scheme: "yaml"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := &staticProvider{scheme: tt.scheme}
			if got := p.Scheme(); got != tt.scheme {
				t.Errorf("Scheme() = %q, want %q", got, tt.scheme)
			}
		})
	}
}

func TestStaticProvider_GetSecret(t *testing.T) {
	ctx := context.Background()

	dummySerialize := func(val any) (string, error) {
		if _, ok := val.(chan int); ok {
			return "", errors.New("dummy serialization error")
		}
		return anyToString(val), nil
	}

	t.Run("single entity mode", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name       string
			provider   *staticProvider
			location   string
			wantVal    string
			wantErr    bool
			wantErrMsg string
		}{
			{
				name: "happy path",
				provider: &staticProvider{
					scheme:       "json",
					singleEntity: true,
					serialize:    dummySerialize,
					entries: map[string]Entry{
						"": {
							Attributes: map[string]any{
								"api_key": "secret123",
							},
						},
					},
				},
				location: "api_key",
				wantVal:  "secret123",
				wantErr:  false,
			},
			{
				name: "missing single entity entry",
				provider: &staticProvider{
					scheme:       "json",
					singleEntity: true,
					serialize:    dummySerialize,
					entries:      map[string]Entry{},
				},
				location:   "api_key",
				wantErr:    true,
				wantErrMsg: "json provider: single entity not found",
			},
			{
				name: "dot path resolution error",
				provider: &staticProvider{
					scheme:       "yaml",
					singleEntity: true,
					serialize:    dummySerialize,
					entries: map[string]Entry{
						"": {
							Attributes: map[string]any{
								"api_key": "secret123",
							},
						},
					},
				},
				location:   "nonexistent_key",
				wantErr:    true,
				wantErrMsg: `yaml provider: failed to resolve path "nonexistent_key": key "nonexistent_key" not found`,
			},
			{
				name: "serialization error",
				provider: &staticProvider{
					scheme:       "json",
					singleEntity: true,
					serialize:    dummySerialize,
					entries: map[string]Entry{
						"": {
							Attributes: map[string]any{
								"bad_val": make(chan int),
							},
						},
					},
				},
				location:   "bad_val",
				wantErr:    true,
				wantErrMsg: "dummy serialization error",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				got, err := tt.provider.GetSecret(ctx, tt.location)
				if (err != nil) != tt.wantErr {
					t.Fatalf("GetSecret() error = %v, wantErr %v", err, tt.wantErr)
				}
				if tt.wantErr {
					if tt.wantErrMsg != "" && !strings.Contains(err.Error(), tt.wantErrMsg) {
						t.Errorf("GetSecret() error = %q, wantErrMsg containing %q", err.Error(), tt.wantErrMsg)
					}
				} else {
					if got != tt.wantVal {
						t.Errorf("GetSecret() = %q, want %q", got, tt.wantVal)
					}
				}
			})
		}
	})

	t.Run("multi entity mode", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name       string
			provider   *staticProvider
			location   string
			wantVal    string
			wantErr    bool
			wantErrMsg string
		}{
			{
				name: "happy path",
				provider: &staticProvider{
					scheme:       "json",
					singleEntity: false,
					serialize:    dummySerialize,
					rawContent: map[string]any{
						"entries": map[string]any{
							"app": map[string]any{
								"password": "db_pass_123",
							},
						},
					},
				},
				location: "entries.app.password",
				wantVal:  "db_pass_123",
				wantErr:  false,
			},
			{
				name: "uninitialized rawContent",
				provider: &staticProvider{
					scheme:       "json",
					singleEntity: false,
					serialize:    dummySerialize,
					rawContent:   nil,
				},
				location:   "entries.app.password",
				wantErr:    true,
				wantErrMsg: "json provider: not initialized or empty database",
			},
			{
				name: "dot path resolution error",
				provider: &staticProvider{
					scheme:       "yaml",
					singleEntity: false,
					serialize:    dummySerialize,
					rawContent: map[string]any{
						"entries": map[string]any{},
					},
				},
				location:   "entries.missing",
				wantErr:    true,
				wantErrMsg: `yaml provider: failed to resolve path "entries.missing": key "missing" not found`,
			},
			{
				name: "serialization error",
				provider: &staticProvider{
					scheme:       "json",
					singleEntity: false,
					serialize:    dummySerialize,
					rawContent: map[string]any{
						"bad_field": make(chan int),
					},
				},
				location:   "bad_field",
				wantErr:    true,
				wantErrMsg: "dummy serialization error",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				got, err := tt.provider.GetSecret(ctx, tt.location)
				if (err != nil) != tt.wantErr {
					t.Fatalf("GetSecret() error = %v, wantErr %v", err, tt.wantErr)
				}
				if tt.wantErr {
					if tt.wantErrMsg != "" && !strings.Contains(err.Error(), tt.wantErrMsg) {
						t.Errorf("GetSecret() error = %q, wantErrMsg containing %q", err.Error(), tt.wantErrMsg)
					}
				} else {
					if got != tt.wantVal {
						t.Errorf("GetSecret() = %q, want %q", got, tt.wantVal)
					}
				}
			})
		}
	})
}

func TestStaticProvider_SetAndDeleteSecret(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name       string
		scheme     string
		wantSubstr string
	}{
		{
			name:       "json provider",
			scheme:     "json",
			wantSubstr: "json provider is read-only",
		},
		{
			name:       "yaml provider",
			scheme:     "yaml",
			wantSubstr: "yaml provider is read-only",
		},
		{
			name:       "custom static scheme",
			scheme:     "static-custom",
			wantSubstr: "static-custom provider is read-only",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := &staticProvider{scheme: tt.scheme}

			t.Run("SetSecret", func(t *testing.T) {
				err := p.SetSecret(ctx, "key", "val")
				if err == nil {
					t.Error("SetSecret() expected error, got nil")
				} else if !strings.Contains(err.Error(), tt.wantSubstr) {
					t.Errorf("SetSecret() error = %q, want substring %q", err.Error(), tt.wantSubstr)
				}
			})

			t.Run("DeleteSecret", func(t *testing.T) {
				err := p.DeleteSecret(ctx, "key")
				if err == nil {
					t.Error("DeleteSecret() expected error, got nil")
				} else if !strings.Contains(err.Error(), tt.wantSubstr) {
					t.Errorf("DeleteSecret() error = %q, want substring %q", err.Error(), tt.wantSubstr)
				}
			})
		})
	}
}

func TestStaticProvider_Validate(t *testing.T) {
	p := &staticProvider{scheme: "json"}

	tests := []struct {
		name       string
		settings   map[string]string
		wantErr    bool
		wantErrMsg string
	}{
		{
			name: "valid vault_path",
			settings: map[string]string{
				"vault_path": "/path/to/vault.json",
			},
			wantErr: false,
		},
		{
			name: "empty vault_path",
			settings: map[string]string{
				"vault_path": "",
			},
			wantErr:    true,
			wantErrMsg: "json provider: vault_path is required",
		},
		{
			name:       "missing vault_path",
			settings:   map[string]string{},
			wantErr:    true,
			wantErrMsg: "json provider: vault_path is required",
		},
		{
			name:       "nil settings",
			settings:   nil,
			wantErr:    true,
			wantErrMsg: "json provider: vault_path is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := p.Validate(tt.settings)
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && err != nil && tt.wantErrMsg != "" && err.Error() != tt.wantErrMsg {
				t.Errorf("Validate() error = %q, wantErrMsg %q", err.Error(), tt.wantErrMsg)
			}
		})
	}
}

func TestStaticProvider_Search(t *testing.T) {
	ctx := context.Background()

	t.Run("single entity search", func(t *testing.T) {
		t.Parallel()
		sp := &staticProvider{
			scheme:       "json",
			singleEntity: true,
			entries: map[string]Entry{
				"": {
					Title: "Production Vault",
					Tags:  []string{"env:prod", "role:db"},
					Attributes: map[string]any{
						"host": "localhost",
					},
				},
			},
		}

		tests := []struct {
			name      string
			query     SearchQuery
			wantCount int
			wantErr   bool
		}{
			{
				name:      "empty query matches single entity",
				query:     SearchQuery{},
				wantCount: 1,
			},
			{
				name:      "matching title substring",
				query:     SearchQuery{Title: "prod"},
				wantCount: 1,
			},
			{
				name:      "non-matching title substring",
				query:     SearchQuery{Title: "staging"},
				wantCount: 0,
			},
			{
				name:      "matching tags case-insensitive",
				query:     SearchQuery{Tags: []string{"ENV:PROD", "ROLE:DB"}},
				wantCount: 1,
			},
			{
				name:      "partially non-matching tags",
				query:     SearchQuery{Tags: []string{"env:prod", "role:web"}},
				wantCount: 0,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				results, err := sp.Search(ctx, tt.query)
				if (err != nil) != tt.wantErr {
					t.Fatalf("Search() error = %v, wantErr %v", err, tt.wantErr)
				}
				if len(results) != tt.wantCount {
					t.Errorf("Search() got %d results, want %d", len(results), tt.wantCount)
				}
			})
		}
	})

	t.Run("single entity missing entry error", func(t *testing.T) {
		t.Parallel()
		sp := &staticProvider{
			scheme:       "json",
			singleEntity: true,
			entries:      map[string]Entry{},
		}
		_, err := sp.Search(ctx, SearchQuery{})
		if err == nil {
			t.Error("expected error when single entity entry is missing, got nil")
		}
	})

	t.Run("multi entity search", func(t *testing.T) {
		t.Parallel()
		sp := &staticProvider{
			scheme:       "json",
			singleEntity: false,
			entries: map[string]Entry{
				"app/prod": {
					Title: "App Prod",
					Tags:  []string{"env:prod", "team:backend"},
				},
				"app/staging": {
					Title: "App Staging",
					Tags:  []string{"env:staging", "team:backend"},
				},
				"db/prod": {
					Title: "Database Prod",
					Tags:  []string{"env:prod", "team:dba"},
				},
			},
		}

		tests := []struct {
			name      string
			query     SearchQuery
			wantCount int
		}{
			{
				name:      "empty query returns all",
				query:     SearchQuery{},
				wantCount: 3,
			},
			{
				name:      "title filter",
				query:     SearchQuery{Title: "database"},
				wantCount: 1,
			},
			{
				name:      "path filter",
				query:     SearchQuery{Path: "app/"},
				wantCount: 2,
			},
			{
				name:      "tags filter",
				query:     SearchQuery{Tags: []string{"env:prod"}},
				wantCount: 2,
			},
			{
				name:      "combined filter",
				query:     SearchQuery{Path: "app/", Tags: []string{"env:prod", "team:backend"}},
				wantCount: 1,
			},
			{
				name:      "no match",
				query:     SearchQuery{Title: "nonexistent"},
				wantCount: 0,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				results, err := sp.Search(ctx, tt.query)
				if err != nil {
					t.Fatalf("Search() unexpected error = %v", err)
				}
				if len(results) != tt.wantCount {
					t.Errorf("Search() got %d results, want %d", len(results), tt.wantCount)
				}
			})
		}
	})
}

func TestStaticProvider_GetEntry(t *testing.T) {
	ctx := context.Background()

	t.Run("single entity mode success", func(t *testing.T) {
		t.Parallel()
		sp := &staticProvider{
			scheme:       "json",
			singleEntity: true,
			entries: map[string]Entry{
				"": {
					Title: "Single Vault",
					Tags:  []string{"env:prod"},
					Attributes: map[string]any{
						"key": "val",
					},
				},
			},
		}

		entry, err := sp.GetEntry(ctx, "")
		if err != nil {
			t.Fatalf("GetEntry() unexpected error = %v", err)
		}
		if entry.Title != "Single Vault" {
			t.Errorf("GetEntry() Title = %q, want %q", entry.Title, "Single Vault")
		}

		entryLoc, err := sp.GetEntry(ctx, "ignored_location")
		if err != nil {
			t.Fatalf("GetEntry() unexpected error = %v", err)
		}
		if entryLoc.Title != "Single Vault" {
			t.Errorf("GetEntry() Title = %q, want %q", entryLoc.Title, "Single Vault")
		}
	})

	t.Run("single entity mode missing entry error", func(t *testing.T) {
		t.Parallel()
		sp := &staticProvider{
			scheme:       "json",
			singleEntity: true,
			entries:      map[string]Entry{},
		}

		_, err := sp.GetEntry(ctx, "")
		if err == nil {
			t.Fatal("GetEntry() expected error when single entity is missing, got nil")
		}
		wantMsg := "json provider: single entity not found"
		if err.Error() != wantMsg {
			t.Errorf("GetEntry() error = %q, want %q", err.Error(), wantMsg)
		}
	})

	t.Run("multi entity mode success", func(t *testing.T) {
		t.Parallel()
		sp := &staticProvider{
			scheme:       "yaml",
			singleEntity: false,
			entries: map[string]Entry{
				"db/prod": {
					Title: "Prod DB",
					Tags:  []string{"role:db"},
					Attributes: map[string]any{
						"host": "db.prod",
					},
				},
			},
		}

		entry, err := sp.GetEntry(ctx, "db/prod")
		if err != nil {
			t.Fatalf("GetEntry() unexpected error = %v", err)
		}
		if entry.Title != "Prod DB" {
			t.Errorf("GetEntry() Title = %q, want %q", entry.Title, "Prod DB")
		}
		if !reflect.DeepEqual(entry.Tags, []string{"role:db"}) {
			t.Errorf("GetEntry() Tags = %v, want %v", entry.Tags, []string{"role:db"})
		}
	})

	t.Run("multi entity mode missing entry error", func(t *testing.T) {
		t.Parallel()
		sp := &staticProvider{
			scheme:       "yaml",
			singleEntity: false,
			entries: map[string]Entry{
				"db/prod": {Title: "Prod DB"},
			},
		}

		_, err := sp.GetEntry(ctx, "db/staging")
		if err == nil {
			t.Fatal("GetEntry() expected error for non-existent entry, got nil")
		}
		wantMsg := `yaml provider: entry "db/staging" not found`
		if err.Error() != wantMsg {
			t.Errorf("GetEntry() error = %q, want %q", err.Error(), wantMsg)
		}
	})
}

func TestStaticProvider_Initialize(t *testing.T) {
	t.Run("missing vault_path", func(t *testing.T) {
		p := NewJsonProvider()
		err := p.Initialize(context.Background(), ProviderConfig{})
		if err == nil {
			t.Fatal("expected error for missing vault_path")
		}
		if !strings.Contains(err.Error(), "vault_path is required") {
			t.Errorf("expected error message to contain 'vault_path is required', got: %v", err)
		}
	})

	t.Run("file does not exist", func(t *testing.T) {
		p := NewJsonProvider()
		cfg := ProviderConfig{
			Settings: map[string]string{
				"vault_path": "non-existent-file.json",
			},
		}
		err := p.Initialize(context.Background(), cfg)
		if err != nil {
			t.Errorf("expected no error for non-existent file, got: %v", err)
		}
	})

	t.Run("invalid json", func(t *testing.T) {
		p := NewJsonProvider()
		f, err := os.CreateTemp(t.TempDir(), "invalid*.json")
		if err != nil {
			t.Fatalf("failed to create temp file: %v", err)
		}

		if _, err := f.Write([]byte("invalid json")); err != nil {
			t.Fatalf("failed to write temp file: %v", err)
		}
		_ = f.Close()

		cfg := ProviderConfig{
			Settings: map[string]string{
				"vault_path": f.Name(),
			},
		}
		err = p.Initialize(context.Background(), cfg)
		if err == nil {
			t.Fatal("expected error for invalid json")
		}
		if !strings.Contains(err.Error(), "failed to parse") {
			t.Errorf("expected error message to contain 'failed to parse', got: %v", err)
		}
	})

	t.Run("valid json single entity (implicit)", func(t *testing.T) {
		p := NewJsonProvider()
		f, err := os.CreateTemp(t.TempDir(), "single*.json")
		if err != nil {
			t.Fatalf("failed to create temp file: %v", err)
		}

		if _, err := f.Write([]byte(`{"key": "value"}`)); err != nil {
			t.Fatalf("failed to write temp file: %v", err)
		}
		_ = f.Close()

		cfg := ProviderConfig{
			Settings: map[string]string{
				"vault_path": f.Name(),
			},
		}
		err = p.Initialize(context.Background(), cfg)
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		if !p.singleEntity {
			t.Error("expected singleEntity to be true")
		}
		if p.rawContent["key"] != "value" {
			t.Errorf("expected rawContent['key'] to be 'value', got: %v", p.rawContent["key"])
		}
		val, err := p.GetSecret(context.Background(), "key")
		if err != nil {
			t.Fatalf("GetSecret failed: %v", err)
		}
		if val != "value" {
			t.Errorf("expected 'value', got %q", val)
		}
	})

	t.Run("valid json single entity (explicit config)", func(t *testing.T) {
		p := NewJsonProvider()
		f, err := os.CreateTemp(t.TempDir(), "single_explicit*.json")
		if err != nil {
			t.Fatalf("failed to create temp file: %v", err)
		}

		if _, err := f.Write([]byte(`{"entities": {"e1": {"key": "value"}}}`)); err != nil {
			t.Fatalf("failed to write temp file: %v", err)
		}
		_ = f.Close()

		trueVal := true
		cfg := ProviderConfig{
			SingleEntity: &trueVal,
			Settings: map[string]string{
				"vault_path": f.Name(),
			},
		}
		err = p.Initialize(context.Background(), cfg)
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		if !p.singleEntity {
			t.Error("expected singleEntity to be true")
		}
		if p.rawContent == nil {
			t.Error("expected rawContent to be populated")
		}
		entry, ok := p.entries[""]
		if !ok {
			t.Fatal("expected single entry stored under key ''")
		}
		if len(entry.Attributes) == 0 {
			t.Error("expected entry.Attributes to be populated after parseSingleEntity")
		}
	})

	t.Run("valid json multi entities (implicit)", func(t *testing.T) {
		p := NewJsonProvider()
		f, err := os.CreateTemp(t.TempDir(), "multi*.json")
		if err != nil {
			t.Fatalf("failed to create temp file: %v", err)
		}

		if _, err := f.Write([]byte(`{"entities": {"e1": {"key": "value"}}}`)); err != nil {
			t.Fatalf("failed to write temp file: %v", err)
		}
		_ = f.Close()

		cfg := ProviderConfig{
			Settings: map[string]string{
				"vault_path": f.Name(),
			},
		}
		err = p.Initialize(context.Background(), cfg)
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		if p.singleEntity {
			t.Error("expected singleEntity to be false")
		}
		if len(p.entries) != 1 {
			t.Errorf("expected 1 entry, got: %d", len(p.entries))
		}
		if _, ok := p.entries["e1"]; !ok {
			t.Error("expected entry 'e1' to be present")
		}
	})

	t.Run("valid json multi entities (explicit root key)", func(t *testing.T) {
		p := NewJsonProvider()
		f, err := os.CreateTemp(t.TempDir(), "multi_root*.json")
		if err != nil {
			t.Fatalf("failed to create temp file: %v", err)
		}

		if _, err := f.Write([]byte(`{"my_root": {"e1": {"key": "value"}}}`)); err != nil {
			t.Fatalf("failed to write temp file: %v", err)
		}
		_ = f.Close()

		cfg := ProviderConfig{
			EntitiesRootKey: "my_root",
			Settings: map[string]string{
				"vault_path": f.Name(),
			},
		}
		err = p.Initialize(context.Background(), cfg)
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		if p.singleEntity {
			t.Error("expected singleEntity to be false")
		}
		if len(p.entries) != 1 {
			t.Errorf("expected 1 entry, got: %d", len(p.entries))
		}
		if _, ok := p.entries["e1"]; !ok {
			t.Error("expected entry 'e1' to be present")
		}
	})

	t.Run("valid json null content", func(t *testing.T) {
		p := NewJsonProvider()
		f, err := os.CreateTemp(t.TempDir(), "null*.json")
		if err != nil {
			t.Fatalf("failed to create temp file: %v", err)
		}

		if _, err := f.Write([]byte(`null`)); err != nil {
			t.Fatalf("failed to write temp file: %v", err)
		}
		_ = f.Close()

		cfg := ProviderConfig{
			Settings: map[string]string{
				"vault_path": f.Name(),
			},
		}
		err = p.Initialize(context.Background(), cfg)
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		if p.rawContent != nil {
			t.Errorf("expected rawContent to be nil for null content, got: %v", p.rawContent)
		}
		if len(p.entries) != 0 {
			t.Errorf("expected no entries for null content, got: %d", len(p.entries))
		}
	})

	t.Run("invalid yaml", func(t *testing.T) {
		p := NewYamlProvider()
		f, err := os.CreateTemp(t.TempDir(), "invalid*.yaml")
		if err != nil {
			t.Fatalf("failed to create temp file: %v", err)
		}

		if _, err := f.Write([]byte("	tabs are not allowed")); err != nil {
			t.Fatalf("failed to write temp file: %v", err)
		}
		_ = f.Close()

		cfg := ProviderConfig{
			Settings: map[string]string{
				"vault_path": f.Name(),
			},
		}
		err = p.Initialize(context.Background(), cfg)
		if err == nil {
			t.Fatal("expected error for invalid yaml")
		}
		if !strings.Contains(err.Error(), "failed to parse") {
			t.Errorf("expected error message to contain 'failed to parse', got: %v", err)
		}
	})

	t.Run("valid yaml single entity (implicit)", func(t *testing.T) {
		p := NewYamlProvider()
		f, err := os.CreateTemp(t.TempDir(), "single*.yaml")
		if err != nil {
			t.Fatalf("failed to create temp file: %v", err)
		}

		if _, err := f.Write([]byte("key: value")); err != nil {
			t.Fatalf("failed to write temp file: %v", err)
		}
		_ = f.Close()

		cfg := ProviderConfig{
			Settings: map[string]string{
				"vault_path": f.Name(),
			},
		}
		err = p.Initialize(context.Background(), cfg)
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		if !p.singleEntity {
			t.Error("expected singleEntity to be true")
		}

		val, err := p.GetSecret(context.Background(), "key")
		if err != nil {
			t.Fatalf("GetSecret failed: %v", err)
		}
		if val != "value" {
			t.Errorf("expected 'value', got %q", val)
		}
	})

	t.Run("valid yaml multi entities (implicit)", func(t *testing.T) {
		p := NewYamlProvider()
		f, err := os.CreateTemp(t.TempDir(), "multi*.yaml")
		if err != nil {
			t.Fatalf("failed to create temp file: %v", err)
		}

		yamlData := `
entities:
  e1:
    key: value
`
		if _, err := f.Write([]byte(yamlData)); err != nil {
			t.Fatalf("failed to write temp file: %v", err)
		}
		_ = f.Close()

		cfg := ProviderConfig{
			Settings: map[string]string{
				"vault_path": f.Name(),
			},
		}
		err = p.Initialize(context.Background(), cfg)
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		if p.singleEntity {
			t.Error("expected singleEntity to be false")
		}
		if len(p.entries) != 1 {
			t.Errorf("expected 1 entry, got: %d", len(p.entries))
		}
		if _, ok := p.entries["e1"]; !ok {
			t.Error("expected entry 'e1' to be present")
		}

		val, err := p.GetSecret(context.Background(), "entities.e1.key")
		if err != nil {
			t.Fatalf("GetSecret failed: %v", err)
		}
		if val != "value" {
			t.Errorf("expected 'value', got %q", val)
		}
	})
}
