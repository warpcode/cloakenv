package provider

import (
	"context"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"
)

func TestKeePassProvider(t *testing.T) {
	keyring.MockInit()
	ctx := context.Background()

	t.Run("Scheme", func(t *testing.T) {
		kp := NewKeePassProvider()
		if kp.Scheme() != "keepass" {
			t.Errorf("expected scheme 'keepass', got %q", kp.Scheme())
		}
	})

	t.Run("Validate", func(t *testing.T) {
		kp := NewKeePassProvider()

		tests := []struct {
			name     string
			settings map[string]string
			wantErr  bool
		}{
			{
				name: "valid vault_path",
				settings: map[string]string{
					"vault_path": "/path/to/vault.kdbx",
				},
				wantErr: false,
			},
			{
				name: "empty vault_path",
				settings: map[string]string{
					"vault_path": "",
				},
				wantErr: true,
			},
			{
				name:     "missing vault_path",
				settings: map[string]string{},
				wantErr:  true,
			},
			{
				name:     "nil settings",
				settings: nil,
				wantErr:  true,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				err := kp.Validate(tt.settings)
				if (err != nil) != tt.wantErr {
					t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
				}
			})
		}
	})

	t.Run("Initialize_NoCredentials", func(t *testing.T) {
		kp := NewKeePassProvider()
		cfg := ProviderConfig{
			Settings: map[string]string{
				"vault_path":  "../../testdata/testDB.kdbx",
				"remote_name": "testdb",
			},
		}
		if err := kp.Initialize(ctx, cfg); err == nil {
			t.Errorf("expected Initialize to fail without stored keyring credentials")
		}
	})

	// Setup keyring credentials and initialize the shared provider for subsequent tests
	// prefix: cloakenv, account: provider/testdb, password: password123
	if err := keyring.Set("cloakenv", "provider/testdb", "password123"); err != nil {
		t.Fatalf("failed to set mock credentials: %v", err)
	}

	kp := NewKeePassProvider()
	cfg := ProviderConfig{
		Settings: map[string]string{
			"vault_path":  "../../testdata/testDB.kdbx",
			"remote_name": "testdb",
		},
	}
	if err := kp.Initialize(ctx, cfg); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	t.Run("Initialize_CredentialsCleared", func(t *testing.T) {
		// Test credentials deletion in memory
		if kp.db.Credentials != nil {
			t.Errorf("expected kp.db.Credentials to be nil after unlock")
		}
	})

	t.Run("GetSecret", func(t *testing.T) {
		tests := []struct {
			name     string
			location string
			expected string
		}{
			{"DefaultPassword", "website/Test Website", "testPassword123!"},
			{"UserNameAttribute", "website/Test Website:UserName", "user@email.com"},
			{"FileAttachment", "website/Test Website:hello.txt", "Hello, World! Extraction Test Successful."},
			{"RootPrefix", "Root/website/Test Website", "testPassword123!"},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				val, err := kp.GetSecret(ctx, tt.location)
				if err != nil {
					t.Errorf("GetSecret failed: %v", err)
				}
				if val != tt.expected {
					t.Errorf("expected %q, got %q", tt.expected, val)
				}
			})
		}
	})

	t.Run("GetEntry", func(t *testing.T) {
		entry, err := kp.GetEntry(ctx, "website/Test Website")
		if err != nil {
			t.Fatalf("GetEntry failed: %v", err)
		}
		if entry.Title != "Test Website" {
			t.Errorf("expected title 'Test Website', got %q", entry.Title)
		}
	})

	t.Run("Search", func(t *testing.T) {
		tests := []struct {
			name          string
			query         SearchQuery
			wantCount     int
			wantFirstPath string
		}{
			{
				name:          "ByTitle",
				query:         SearchQuery{Title: "Test Website"},
				wantCount:     1,
				wantFirstPath: "website/Test Website",
			},
			{
				name:      "ByPath",
				query:     SearchQuery{Path: "website/Test"},
				wantCount: 1,
			},
			{
				name:      "NoMatch",
				query:     SearchQuery{Title: "NonExistentTitleItem12345"},
				wantCount: 0,
			},
			{
				name:      "NonExistentTag",
				query:     SearchQuery{Tags: []string{"nonexistenttag"}},
				wantCount: 0,
			},
			{
				name:          "EmptyTagFilter",
				query:         SearchQuery{Title: "Test Website", Tags: nil},
				wantCount:     1,
				wantFirstPath: "website/Test Website",
			},
		}

		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				results, err := kp.Search(ctx, tc.query)
				if err != nil {
					t.Fatalf("Search failed: %v", err)
				}
				if len(results) != tc.wantCount {
					t.Fatalf("expected %d result(s), got %d", tc.wantCount, len(results))
				}
				if tc.wantFirstPath != "" && results[0].Path != tc.wantFirstPath {
					t.Errorf("expected path %q, got %q", tc.wantFirstPath, results[0].Path)
				}
			})
		}
	})

	t.Run("ReadOnly", func(t *testing.T) {
		if err := kp.SetSecret(ctx, "a", "b"); err == nil {
			t.Errorf("expected SetSecret to fail")
		} else if !strings.Contains(err.Error(), "read-only") {
			t.Errorf("expected error to contain 'read-only', got %q", err.Error())
		}
		if err := kp.DeleteSecret(ctx, "a"); err == nil {
			t.Errorf("expected DeleteSecret to fail")
		} else if !strings.Contains(err.Error(), "read-only") {
			t.Errorf("expected error to contain 'read-only', got %q", err.Error())
		}
	})
}

func TestKeePassProvider_SetSecret(t *testing.T) {
	kp := NewKeePassProvider()
	err := kp.SetSecret(context.Background(), "KEY", "VAL")
	if err == nil {
		t.Error("expected error for SetSecret, got nil")
	}
	if err != nil && !strings.Contains(err.Error(), "read-only") {
		t.Errorf("expected error to contain 'read-only', got %q", err.Error())
	}
}

func TestKeePassProvider_DeleteSecret(t *testing.T) {
	kp := NewKeePassProvider()
	err := kp.DeleteSecret(context.Background(), "KEY")
	if err == nil {
		t.Error("expected error for DeleteSecret, got nil")
	}
	if err != nil && !strings.Contains(err.Error(), "read-only") {
		t.Errorf("expected error to contain 'read-only', got %q", err.Error())
	}
}

func TestMatchEntryTags(t *testing.T) {
	tests := []struct {
		name      string
		tagString string
		queryTags []string
		want      bool
	}{
		{
			name:      "empty query tags matches anything",
			tagString: "tag1, tag2",
			queryTags: nil,
			want:      true,
		},
		{
			name:      "matching single tag case-insensitive",
			tagString: "Production, Database",
			queryTags: []string{"production"},
			want:      true,
		},
		{
			name:      "matching all query tags",
			tagString: "Production, Database, Web",
			queryTags: []string{"production", "database"},
			want:      true,
		},
		{
			name:      "missing query tag",
			tagString: "Production, Database",
			queryTags: []string{"production", "redis"},
			want:      false,
		},
		{
			name:      "empty entry tags with non-empty query",
			tagString: "",
			queryTags: []string{"production"},
			want:      false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := matchEntryTags(tc.tagString, tc.queryTags)
			if got != tc.want {
				t.Errorf("matchEntryTags(%q, %v) = %v, want %v", tc.tagString, tc.queryTags, got, tc.want)
			}
		})
	}
}
