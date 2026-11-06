package provider

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func TestEnvProvider_Scheme(t *testing.T) {
	p := NewEnvProvider()
	if p.Scheme() != "env" {
		t.Errorf("expected scheme 'env', got %q", p.Scheme())
	}
}

func TestEnvProvider_Initialize(t *testing.T) {
	p := NewEnvProvider()
	err := p.Initialize(context.Background(), ProviderConfig{})
	if err != nil {
		t.Errorf("Initialize failed: %v", err)
	}
}

func TestEnvProvider_Validate(t *testing.T) {
	p := NewEnvProvider()
	err := p.Validate(nil)
	if err != nil {
		t.Errorf("Validate failed: %v", err)
	}
}

func TestEnvProvider_GetSecret(t *testing.T) {
	p := NewEnvProvider()
	ctx := context.Background()

	t.Run("Success", func(t *testing.T) {
		key := "CLOAKENV_TEST_KEY"
		val := "test-secret-value"
		t.Setenv(key, val)

		got, err := p.GetSecret(ctx, key)
		if err != nil {
			t.Errorf("GetSecret failed: %v", err)
		}
		if got != val {
			t.Errorf("expected %q, got %q", val, got)
		}
	})

	t.Run("NotSet", func(t *testing.T) {
		key := fmt.Sprintf("CLOAKENV_TEST_NOT_SET_%d", time.Now().UnixNano())
		_, err := p.GetSecret(ctx, key)
		if err == nil {
			t.Error("expected error for non-existent key, got nil")
		}
		if err != nil && !strings.Contains(err.Error(), "not set") {
			t.Errorf("expected error to contain 'not set', got %q", err.Error())
		}
	})

	t.Run("Empty", func(t *testing.T) {
		emptyKey := "CLOAKENV_EMPTY_KEY"
		t.Setenv(emptyKey, "")

		_, err := p.GetSecret(ctx, emptyKey)
		if err == nil {
			t.Error("expected error for empty key, got nil")
		}
		if err != nil && !strings.Contains(err.Error(), "set but empty") {
			t.Errorf("expected error to contain 'set but empty', got %q", err.Error())
		}
	})

	t.Run("LocationEmpty", func(t *testing.T) {
		_, err := p.GetSecret(ctx, "")
		if err == nil {
			t.Error("expected error for empty location, got nil")
		}
	})
}

func TestEnvProvider_SetSecret(t *testing.T) {
	p := NewEnvProvider()
	ctx := context.Background()

	tests := []struct {
		name      string
		key       string
		val       string
		preset    bool
		presetVal string
	}{
		{
			name: "StandardKeyVal",
			key:  "CLOAKENV_TEST_SET_KEY",
			val:  "test-value",
		},
		{
			name: "EmptyKeyAndVal",
			key:  "",
			val:  "",
		},
		{
			name:      "ExistingEnvVarUnchanged",
			key:       "CLOAKENV_TEST_EXISTING_KEY",
			val:       "new-value",
			preset:    true,
			presetVal: "original-value",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.preset {
				t.Setenv(tt.key, tt.presetVal)
			}

			err := p.SetSecret(ctx, tt.key, tt.val)
			if err == nil {
				t.Error("expected error for SetSecret, got nil")
			} else if !strings.Contains(err.Error(), "read-only") {
				t.Errorf("expected error to contain 'read-only', got %q", err.Error())
			}

			if tt.preset {
				if got := os.Getenv(tt.key); got != tt.presetVal {
					t.Errorf("expected env var %q to remain %q, got %q", tt.key, tt.presetVal, got)
				}
			} else if tt.key != "" {
				if _, ok := os.LookupEnv(tt.key); ok {
					t.Errorf("expected env var %q to remain unset", tt.key)
				}
			}
		})
	}
}

func TestEnvProvider_DeleteSecret(t *testing.T) {
	p := NewEnvProvider()
	ctx := context.Background()

	t.Run("StandardKey", func(t *testing.T) {
		err := p.DeleteSecret(ctx, "KEY")
		if err == nil {
			t.Error("expected error for DeleteSecret, got nil")
		}
		if err != nil && !strings.Contains(err.Error(), "read-only") {
			t.Errorf("expected error to contain 'read-only', got %q", err.Error())
		}
	})

	t.Run("EmptyKey", func(t *testing.T) {
		err := p.DeleteSecret(ctx, "")
		if err == nil {
			t.Error("expected error for DeleteSecret, got nil")
		}
		if err != nil && !strings.Contains(err.Error(), "read-only") {
			t.Errorf("expected error to contain 'read-only', got %q", err.Error())
		}
	})
}

func TestEnvProvider_GetEntry(t *testing.T) {
	p := NewEnvProvider()
	ctx := context.Background()

	tests := []struct {
		name        string
		setup       func(t *testing.T)
		location    string
		wantErr     bool
		errContains string
		validate    func(t *testing.T, entry Entry)
	}{
		{
			name: "LocationEmptyReturnsAll",
			setup: func(t *testing.T) {
				t.Setenv("CLOAKENV_TEST_GETENTRY_EMPTY", "val_empty")
			},
			location: "",
			wantErr:  false,
			validate: func(t *testing.T, entry Entry) {
				if entry.Title != "Environment Variables" {
					t.Errorf("expected title 'Environment Variables', got %q", entry.Title)
				}
				if val, exists := entry.Attributes["CLOAKENV_TEST_GETENTRY_EMPTY"]; !exists || val != "val_empty" {
					t.Errorf("expected CLOAKENV_TEST_GETENTRY_EMPTY to be 'val_empty', got %v", val)
				}
				if len(entry.Attributes) == 0 {
					t.Error("expected non-empty attributes map for full environment")
				}
			},
		},
		{
			name: "LocationEnvReturnsAll",
			setup: func(t *testing.T) {
				t.Setenv("CLOAKENV_TEST_GETENTRY_ENV", "val_env")
			},
			location: "env",
			wantErr:  false,
			validate: func(t *testing.T, entry Entry) {
				if entry.Title != "Environment Variables" {
					t.Errorf("expected title 'Environment Variables', got %q", entry.Title)
				}
				if val, exists := entry.Attributes["CLOAKENV_TEST_GETENTRY_ENV"]; !exists || val != "val_env" {
					t.Errorf("expected CLOAKENV_TEST_GETENTRY_ENV to be 'val_env', got %v", val)
				}
			},
		},
		{
			name: "SuccessSingle",
			setup: func(t *testing.T) {
				t.Setenv("CLOAKENV_TEST_VAR_SINGLE", "single-value")
			},
			location: "CLOAKENV_TEST_VAR_SINGLE",
			wantErr:  false,
			validate: func(t *testing.T, entry Entry) {
				if entry.Title != "Environment Variables" {
					t.Errorf("expected title 'Environment Variables', got %q", entry.Title)
				}
				if val, exists := entry.Attributes["CLOAKENV_TEST_VAR_SINGLE"]; !exists || val != "single-value" {
					t.Errorf("expected CLOAKENV_TEST_VAR_SINGLE to be 'single-value', got %v", val)
				}
				if len(entry.Attributes) != 1 {
					t.Errorf("expected attributes length 1, got %d", len(entry.Attributes))
				}
			},
		},
		{
			name: "SuccessSingleWithColonPrefix",
			setup: func(t *testing.T) {
				t.Setenv("CLOAKENV_TEST_COLON_VAR", "colon-val")
			},
			location: ":CLOAKENV_TEST_COLON_VAR",
			wantErr:  false,
			validate: func(t *testing.T, entry Entry) {
				if val, exists := entry.Attributes["CLOAKENV_TEST_COLON_VAR"]; !exists || val != "colon-val" {
					t.Errorf("expected CLOAKENV_TEST_COLON_VAR to be 'colon-val', got %v", val)
				}
			},
		},
		{
			name:        "SingleNotSet",
			setup:       func(t *testing.T) {},
			location:    "CLOAKENV_NON_EXISTENT_VAR_123",
			wantErr:     true,
			errContains: "is not set",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.setup(t)
			entry, err := p.GetEntry(ctx, tc.location)

			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if tc.errContains != "" && !strings.Contains(err.Error(), tc.errContains) {
					t.Errorf("expected error to contain %q, got %q", tc.errContains, err.Error())
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if tc.validate != nil {
				tc.validate(t, entry)
			}
		})
	}
}

func TestEnvProvider_GetEntryWithPath(t *testing.T) {
	p := NewEnvProvider()
	ctx := context.Background()

	t.Setenv("CLOAKENV_PATH_TEST_VAR", "path-test-val")

	t.Run("EmptyLocation", func(t *testing.T) {
		entry, path, err := p.GetEntryWithPath(ctx, "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if path != "env" {
			t.Errorf("expected path 'env', got %q", path)
		}
		if entry.Attributes["CLOAKENV_PATH_TEST_VAR"] != "path-test-val" {
			t.Errorf("expected attribute to be present in entry")
		}
	})

	t.Run("SpecificLocation", func(t *testing.T) {
		entry, path, err := p.GetEntryWithPath(ctx, "CLOAKENV_PATH_TEST_VAR")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if path != "CLOAKENV_PATH_TEST_VAR" {
			t.Errorf("expected path 'CLOAKENV_PATH_TEST_VAR', got %q", path)
		}
		if entry.Attributes["CLOAKENV_PATH_TEST_VAR"] != "path-test-val" {
			t.Errorf("expected attribute 'path-test-val', got %v", entry.Attributes["CLOAKENV_PATH_TEST_VAR"])
		}
	})
}

func TestEnvProvider_Search(t *testing.T) {
	p := NewEnvProvider()
	ctx := context.Background()

	t.Setenv("CLOAKENV_TEST_SEARCH_VAR", "search-val")

	t.Run("EmptyQuery", func(t *testing.T) {
		results, err := p.Search(ctx, SearchQuery{})
		if err != nil {
			t.Fatalf("Search failed: %v", err)
		}
		if len(results) != 1 {
			t.Fatalf("expected 1 result, got %d", len(results))
		}
		res := results[0]
		if res.Path != "env" {
			t.Errorf("expected result Path 'env', got %q", res.Path)
		}
		if res.Entry.Title != "Environment Variables" {
			t.Errorf("expected Title 'Environment Variables', got %q", res.Entry.Title)
		}
		if res.Entry.Attributes["CLOAKENV_TEST_SEARCH_VAR"] != "search-val" {
			t.Errorf("expected CLOAKENV_TEST_SEARCH_VAR in attributes, got %v", res.Entry.Attributes["CLOAKENV_TEST_SEARCH_VAR"])
		}
	})

	t.Run("MatchingTitle", func(t *testing.T) {
		results, err := p.Search(ctx, SearchQuery{Title: "Environment"})
		if err != nil {
			t.Fatalf("Search failed: %v", err)
		}
		if len(results) != 1 {
			t.Errorf("expected 1 match, got %d", len(results))
		}
	})

	t.Run("NonMatchingTitle", func(t *testing.T) {
		results, err := p.Search(ctx, SearchQuery{Title: "NonExistentTitle"})
		if err != nil {
			t.Fatalf("Search failed: %v", err)
		}
		if len(results) != 0 {
			t.Errorf("expected 0 matches, got %d", len(results))
		}
	})

	t.Run("MatchingPath", func(t *testing.T) {
		results, err := p.Search(ctx, SearchQuery{Path: "env"})
		if err != nil {
			t.Fatalf("Search failed: %v", err)
		}
		if len(results) != 1 {
			t.Errorf("expected 1 match, got %d", len(results))
		}
	})

	t.Run("NonMatchingPath", func(t *testing.T) {
		results, err := p.Search(ctx, SearchQuery{Path: "other"})
		if err != nil {
			t.Fatalf("Search failed: %v", err)
		}
		if len(results) != 0 {
			t.Errorf("expected 0 matches, got %d", len(results))
		}
	})

	t.Run("TagsNeverMatchEmptyTags", func(t *testing.T) {
		results, err := p.Search(ctx, SearchQuery{Tags: []string{"any:tag"}})
		if err != nil {
			t.Fatalf("Search failed: %v", err)
		}
		if len(results) != 0 {
			t.Errorf("expected 0 matches for tags, got %d", len(results))
		}
	})
}
