package provider

import (
	"context"
	"strings"
	"testing"
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
		_, err := p.GetSecret(ctx, "NON_EXISTENT_KEY_PROBABLY_NOT_SET")
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
}

func TestEnvProvider_SetSecret(t *testing.T) {
	p := NewEnvProvider()
	err := p.SetSecret(context.Background(), "KEY", "VAL")
	if err == nil {
		t.Error("expected error for SetSecret, got nil")
	}
	if err != nil && !strings.Contains(err.Error(), "read-only") {
		t.Errorf("expected error to contain 'read-only', got %q", err.Error())
	}
}

func TestEnvProvider_DeleteSecret(t *testing.T) {
	p := NewEnvProvider()
	err := p.DeleteSecret(context.Background(), "KEY")
	if err == nil {
		t.Error("expected error for DeleteSecret, got nil")
	}
	if err != nil && !strings.Contains(err.Error(), "read-only") {
		t.Errorf("expected error to contain 'read-only', got %q", err.Error())
	}
}
