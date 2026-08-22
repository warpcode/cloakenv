package engine

import (
	"context"
	"strings"
	"testing"

	"github.com/warpcode/cloakenv/internal/config"
	"github.com/warpcode/cloakenv/internal/provider"
)

func TestResolveValues(t *testing.T) {
	ctx := context.Background()

	// vault_b holds a plain secret value.
	// vault_a holds an attribute whose value is a URI pointing into vault_b.
	boolPtr := func(b bool) *bool { return &b }

	makeConfig := func(resolveValues bool) *config.Config {
		return &config.Config{
			Vaults: map[string]config.VaultConfig{
				"vault_a": {
					Provider:      "custom_vault",
					ResolveValues: resolveValues,
					Entities: map[string]map[string]any{
						"entity1": {
							"Password": "${vault_b://entry1:secret}",
						},
					},
				},
				"vault_b": {
					Provider:   "custom_vault",
					Searchable: boolPtr(true),
					Entities: map[string]map[string]any{
						"entry1": {
							"secret": "resolved_value",
						},
					},
				},
			},
		}
	}

	t.Run("ResolveValues_On", func(t *testing.T) {
		orch, err := NewOrchestrator(makeConfig(true))
		if err != nil {
			t.Fatalf("failed to create orchestrator: %v", err)
		}

		val, err := orch.Resolve(ctx, "${vault_a://entity1:Password}")
		if err != nil {
			t.Fatalf("Resolve failed: %v", err)
		}
		if val != "resolved_value" {
			t.Errorf("expected resolved_value, got %q", val)
		}
	})

	t.Run("ResolveValues_Off", func(t *testing.T) {
		orch, err := NewOrchestrator(makeConfig(false))
		if err != nil {
			t.Fatalf("failed to create orchestrator: %v", err)
		}

		val, err := orch.Resolve(ctx, "${vault_a://entity1:Password}")
		if err != nil {
			t.Fatalf("Resolve failed: %v", err)
		}
		// Should return the raw URI string, not the resolved secret.
		if val != "${vault_b://entry1:secret}" {
			t.Errorf("expected raw URI, got %q", val)
		}
	})

	t.Run("ResolveValues_Off_GetEntry", func(t *testing.T) {
		// GetEntry must also honour resolve_values: false — attribute values
		// that are URIs must be returned raw without being resolved.
		orch, err := NewOrchestrator(makeConfig(false))
		if err != nil {
			t.Fatalf("failed to create orchestrator: %v", err)
		}

		entry, err := orch.GetEntry(ctx, "vault_a://entity1")
		if err != nil {
			t.Fatalf("GetEntry failed: %v", err)
		}
		raw, ok := entry.Attributes["Password"]
		if !ok {
			t.Fatal("expected 'Password' attribute in entry")
		}
		if raw != "${vault_b://entry1:secret}" {
			t.Errorf("expected raw URI in attribute, got %q", raw)
		}
	})

	t.Run("ResolveValues_CircularReference", func(t *testing.T) {
		// vault_c.entry references vault_c itself — forms a cycle.
		cfg := &config.Config{
			Vaults: map[string]config.VaultConfig{
				"vault_c": {
					Provider:      "custom_vault",
					ResolveValues: true,
					Entities: map[string]map[string]any{
						"entry": {
							"Password": "${vault_c://entry:Password}",
						},
					},
				},
			},
		}

		orch, err := NewOrchestrator(cfg)
		if err != nil {
			t.Fatalf("failed to create orchestrator: %v", err)
		}

		_, err = orch.Resolve(ctx, "${vault_c://entry:Password}")
		if err == nil {
			t.Error("expected error due to circular reference, got nil")
		}
		if !strings.Contains(err.Error(), "max depth") {
			t.Errorf("expected max depth error, got: %v", err)
		}
	})

	t.Run("ResolveValues_RejectedOnNonCustomVault", func(t *testing.T) {
		// resolve_values must be rejected at startup for non-custom_vault providers.
		cfg := &config.Config{
			Vaults: map[string]config.VaultConfig{
				"bad_yaml": {
					Provider:      "yaml",
					VaultPath:     "/tmp/irrelevant.yaml",
					ResolveValues: true,
				},
			},
		}

		_, err := NewOrchestrator(cfg)
		if err == nil {
			t.Fatal("expected error for resolve_values on yaml provider, got nil")
		}
		if !strings.Contains(err.Error(), "resolve_values") {
			t.Errorf("expected resolve_values error, got: %v", err)
		}
	})
}

func TestGetEntry_AttributeSelector(t *testing.T) {
	ctx := context.Background()

	cfg := &config.Config{
		Vaults: map[string]config.VaultConfig{
			"vault_x": {
				Provider: "custom_vault",
				Entities: map[string]map[string]any{
					"myentity": {
						"Password": "s3cr3t",
						"UserName": "alice",
						"Notes":    "some notes",
					},
				},
			},
		},
	}

	orch, err := NewOrchestrator(cfg)
	if err != nil {
		t.Fatalf("failed to create orchestrator: %v", err)
	}

	t.Run("single_attr_only", func(t *testing.T) {
		entry, err := orch.GetEntry(ctx, "vault_x://myentity:Password")
		if err != nil {
			t.Fatalf("GetEntry failed: %v", err)
		}
		if len(entry.Attributes) != 1 {
			t.Errorf("expected 1 attribute, got %d: %v", len(entry.Attributes), entry.Attributes)
		}
		val, ok := entry.Attributes["Password"]
		if !ok {
			t.Fatal("expected 'Password' key in synthetic entry")
		}
		if val != "s3cr3t" {
			t.Errorf("expected s3cr3t, got %q", val)
		}
	})

	t.Run("full_entry_without_attr", func(t *testing.T) {
		entry, err := orch.GetEntry(ctx, "vault_x://myentity")
		if err != nil {
			t.Fatalf("GetEntry failed: %v", err)
		}
		if len(entry.Attributes) != 3 {
			t.Errorf("expected 3 attributes, got %d: %v", len(entry.Attributes), entry.Attributes)
		}
	})
}

func TestResolveLiteralValues(t *testing.T) {
	ctx := context.Background()
	cfg := &config.Config{}
	orch, err := NewOrchestrator(cfg)
	if err != nil {
		t.Fatalf("failed to create orchestrator: %v", err)
	}

	tests := []struct {
		input string
		want  string
	}{
		{"just_a_string", "just_a_string"},
		{"12345", "12345"},
		{"https://example.com/api", "https://example.com/api"},
		{"some-other-scheme://foo", "some-other-scheme://foo"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := orch.Resolve(ctx, tt.input)
			if err != nil {
				t.Fatalf("Resolve failed: %v", err)
			}
			if got != tt.want {
				t.Errorf("Resolve(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestOrchestratorExpansion(t *testing.T) {
	ctx := context.Background()
	t.Setenv("EXP_USER", "jane")
	t.Setenv("EXP_HOST", "local")

	cfg := &config.Config{
		Vaults: map[string]config.VaultConfig{
			"vault_exp": {
				Provider: "custom_vault",
				Entities: map[string]map[string]any{
					"database": {
						"password": "supersecretpassword",
					},
				},
			},
		},
	}

	orch, err := NewOrchestrator(cfg)
	if err != nil {
		t.Fatalf("failed to create orchestrator: %v", err)
	}

	t.Run("Single expansion in a value", func(t *testing.T) {
		val, err := orch.Resolve(ctx, "${env://EXP_USER}")
		if err != nil {
			t.Fatalf("failed to resolve: %v", err)
		}
		if val != "jane" {
			t.Errorf("expected 'jane', got %q", val)
		}
	})

	t.Run("Multiple expansions in a single value", func(t *testing.T) {
		val, err := orch.Resolve(ctx, "${env://EXP_USER}-${env://EXP_HOST}")
		if err != nil {
			t.Fatalf("failed to resolve: %v", err)
		}
		if val != "jane-local" {
			t.Errorf("expected 'jane-local', got %q", val)
		}
	})

	t.Run("Expansion embedded in longer strings (URIs, file paths)", func(t *testing.T) {
		val, err := orch.Resolve(ctx, "mysql://${env://EXP_USER}:${vault_exp://database:password}@${env://EXP_HOST}:3306/db")
		if err != nil {
			t.Fatalf("failed to resolve: %v", err)
		}
		expected := "mysql://jane:supersecretpassword@local:3306/db"
		if val != expected {
			t.Errorf("expected %q, got %q", expected, val)
		}
	})

	t.Run("Escaped ${} remains literal", func(t *testing.T) {
		// $$ becomes literal $
		// $${env://EXP_USER} becomes literal ${env://EXP_USER}
		val, err := orch.Resolve(ctx, "$${env://EXP_USER}")
		if err != nil {
			t.Fatalf("failed to resolve: %v", err)
		}
		if val != "${env://EXP_USER}" {
			t.Errorf("expected '${env://EXP_USER}', got %q", val)
		}

		// my$$password becomes literal my$password
		val, err = orch.Resolve(ctx, "my$$password")
		if err != nil {
			t.Fatalf("failed to resolve: %v", err)
		}
		if val != "my$password" {
			t.Errorf("expected 'my$password', got %q", val)
		}
	})

	t.Run("Provider resolution errors surface useful messages", func(t *testing.T) {
		_, err := orch.ResolveWithKey(ctx, "mysql://${env://NON_EXISTENT_VAR_FOR_EXP}", "DATABASE_URI")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		expectedPart := "in configuration key \"DATABASE_URI\""
		if !strings.Contains(err.Error(), expectedPart) {
			t.Errorf("expected error message to contain %q, got %q", expectedPart, err.Error())
		}
		expectedExprPart := "failed to resolve expansion \"env://NON_EXISTENT_VAR_FOR_EXP\""
		if !strings.Contains(err.Error(), expectedExprPart) {
			t.Errorf("expected error message to contain %q, got %q", expectedExprPart, err.Error())
		}
	})

	t.Run("Nested expansions are disallowed", func(t *testing.T) {
		_, err := orch.Resolve(ctx, "${env://${EXP_USER}}")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		expectedPart := "nested expansions are not supported"
		if !strings.Contains(err.Error(), expectedPart) {
			t.Errorf("expected error message to contain %q, got %q", expectedPart, err.Error())
		}
	})
}

func TestGetEntry(t *testing.T) {
	ctx := context.Background()

	cfg := &config.Config{
		Vaults: map[string]config.VaultConfig{
			"vault_x": {
				Provider: "custom_vault",
				Entities: map[string]map[string]any{
					"myentity": {
						"Password":  "s3cr3t",
						"recursive": "vault_x://myentity",
					},
					"error_attr": {
						"Password": "${vault_missing://nonexistent}",
					},
				},
				ResolveValues: true,
			},
			"vault_y": {
				Provider: "custom_vault",
				Entities: map[string]map[string]any{
					"target": {
						"Password": "pass",
					},
					"no_resolve": {
						"Password": "vault_y://target",
					},
				},
				ResolveValues: false,
			},
		},
	}

	orch, err := NewOrchestrator(cfg)
	if err != nil {
		t.Fatalf("failed to create orchestrator: %v", err)
	}

	orch.providerManager.builtins["failinit"] = &failInitProvider{}

	tests := []struct {
		name      string
		uri       string
		depth     int
		wantErr   string
		wantAttrs map[string]string
	}{
		{
			name:    "depth_exceeded",
			uri:     "vault_x://myentity",
			depth:   6,
			wantErr: "infinite secret resolution recursion detected",
		},
		{
			name:    "invalid_uri",
			uri:     "://invalid",
			wantErr: "malformed URI",
		},
		{
			name:    "attribute_selector_resolve_error",
			uri:     "vault_missing://myentity:Password",
			wantErr: "unknown scheme",
		},
		{
			name:    "get_vault_provider_error",
			uri:     "nonexistent_vault://myentity",
			wantErr: "unknown scheme or vault",
		},
		{
			name:    "searchable_get_entry_error",
			uri:     "vault_x://nonexistent_entity",
			wantErr: "not found",
		},
		{
			name:    "resolve_attr_recursive_error",
			uri:     "vault_x://error_attr",
			wantErr: "failed to resolve attribute",
		},
		{
			name:    "provider_not_searchable",
			uri:     "keyring://foo",
			wantErr: "does not support structured entries",
		},
		{
			name:    "ensure_initialized_error",
			uri:     "failinit://foo",
			wantErr: "init failed",
		},
		{
			name:      "attribute_selector_success",
			uri:       "vault_y://target:Password",
			wantAttrs: map[string]string{"Password": "pass"},
		},
		{
			name:      "no_resolve_values",
			uri:       "vault_y://no_resolve",
			wantAttrs: map[string]string{"Password": "vault_y://target"},
		},
		{
			name:      "full_entry_success",
			uri:       "vault_x://myentity",
			wantAttrs: map[string]string{"Password": "s3cr3t", "recursive": "vault_x://myentity"}, // recursive doesn't loop infinitely unless actually resolved deeper?
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var entry provider.Entry
			var err error

			if tt.depth > 0 {
				entry, err = orch.resolver.GetEntryRecursive(ctx, tt.uri, tt.depth)
			} else {
				entry, err = orch.GetEntry(ctx, tt.uri)
			}

			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("expected error containing %q, got %v", tt.wantErr, err)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if tt.wantAttrs != nil {
				if len(entry.Attributes) != len(tt.wantAttrs) {
					t.Errorf("expected %d attributes, got %d: %v", len(tt.wantAttrs), len(entry.Attributes), entry.Attributes)
				}
				for k, v := range tt.wantAttrs {
					if entry.Attributes[k] != v {
						t.Errorf("expected attribute %q to be %q, got %q", k, v, entry.Attributes[k])
					}
				}
			}
		})
	}
}
