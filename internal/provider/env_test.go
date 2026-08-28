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

	t.Run("LocationEmpty", func(t *testing.T) {
		_, err := p.GetEntry(ctx, "")
		if err == nil {
			t.Error("expected error for empty location, got nil")
		}
		if err != nil && !strings.Contains(err.Error(), "location cannot be empty") {
			t.Errorf("expected error to contain 'location cannot be empty', got %q", err.Error())
		}
	})

	t.Run("SuccessSingle", func(t *testing.T) {
		t.Setenv("CLOAKENV_TEST_VAR_SINGLE", "single-value")
		entry, err := p.GetEntry(ctx, "CLOAKENV_TEST_VAR_SINGLE")
		if err != nil {
			t.Fatalf("GetEntry failed: %v", err)
		}
		if entry.Title != "Environment Variables" {
			t.Errorf("expected title 'Environment Variables', got %q", entry.Title)
		}
		if val, exists := entry.Attributes["CLOAKENV_TEST_VAR_SINGLE"]; !exists || val != "single-value" {
			t.Errorf("expected CLOAKENV_TEST_VAR_SINGLE to be 'single-value', got %v", val)
		}
		if len(entry.Attributes) != 1 {
			t.Errorf("expected attributes length 1, got %d", len(entry.Attributes))
		}
	})

	t.Run("SingleNotSet", func(t *testing.T) {
		_, err := p.GetEntry(ctx, "CLOAKENV_NON_EXISTENT_VAR_123")
		if err == nil {
			t.Error("expected error for non-existent env variable, got nil")
		}
	})
}

func TestEnvProvider_Search(t *testing.T) {
	p := NewEnvProvider()
	ctx := context.Background()
	_, err := p.Search(ctx, SearchQuery{})
	if err == nil {
		t.Error("expected error for search, got nil")
	}
	if err != nil && !strings.Contains(err.Error(), "does not support searching") {
		t.Errorf("expected error to contain 'does not support searching', got %q", err.Error())
	}
}
