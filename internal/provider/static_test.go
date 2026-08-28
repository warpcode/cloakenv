package provider

import (
	"context"
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
