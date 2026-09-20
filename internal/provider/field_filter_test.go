package provider_test

import (
	"context"
	"testing"

	"github.com/warpcode/cloakenv/internal/provider"
)

func TestFieldFilter_IsAllowed(t *testing.T) {
	tests := []struct {
		name      string
		include   []string
		exclude   []string
		fieldName string
		want      bool
	}{
		{
			name:      "no filters allows everything",
			include:   nil,
			exclude:   nil,
			fieldName: "Password",
			want:      true,
		},
		{
			name:      "include filter matches glob",
			include:   []string{"env:*", "UserName"},
			exclude:   nil,
			fieldName: "env:prod",
			want:      true,
		},
		{
			name:      "include filter matches exact name",
			include:   []string{"env:*", "UserName"},
			exclude:   nil,
			fieldName: "UserName",
			want:      true,
		},
		{
			name:      "include filter rejects non-matching field",
			include:   []string{"env:*", "UserName"},
			exclude:   nil,
			fieldName: "Password",
			want:      false,
		},
		{
			name:      "exclude filter rejects matching glob",
			include:   nil,
			exclude:   []string{"*.notes", "secret:*"},
			fieldName: "db.notes",
			want:      false,
		},
		{
			name:      "exclude filter rejects prefix glob",
			include:   nil,
			exclude:   []string{"*.notes", "secret:*"},
			fieldName: "secret:aws",
			want:      false,
		},
		{
			name:      "exclude filter allows non-matching field",
			include:   nil,
			exclude:   []string{"*.notes", "secret:*"},
			fieldName: "UserName",
			want:      true,
		},
		{
			name:      "exclude overrides include when both match",
			include:   []string{"env:*"},
			exclude:   []string{"env:internal_*"},
			fieldName: "env:internal_token",
			want:      false,
		},
		{
			name:      "include matches and exclude does not match",
			include:   []string{"env:*"},
			exclude:   []string{"env:internal_*"},
			fieldName: "env:public_key",
			want:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filter := provider.NewFieldFilter(tt.include, tt.exclude)
			if err := filter.Validate(); err != nil {
				t.Fatalf("unexpected validation error: %v", err)
			}
			got := filter.IsAllowed(tt.fieldName)
			if got != tt.want {
				t.Errorf("IsAllowed(%q) = %v, want %v", tt.fieldName, got, tt.want)
			}
		})
	}
}

func TestFieldFilter_Validate(t *testing.T) {
	t.Run("valid patterns", func(t *testing.T) {
		filter := provider.NewFieldFilter([]string{"env:*", "UserName"}, []string{"*.notes"})
		if err := filter.Validate(); err != nil {
			t.Errorf("expected no error, got %v", err)
		}
	})

	t.Run("invalid include pattern", func(t *testing.T) {
		filter := provider.NewFieldFilter([]string{"["}, nil)
		if err := filter.Validate(); err == nil {
			t.Errorf("expected error for invalid glob pattern, got nil")
		}
	})

	t.Run("invalid exclude pattern", func(t *testing.T) {
		filter := provider.NewFieldFilter(nil, []string{"["})
		if err := filter.Validate(); err == nil {
			t.Errorf("expected error for invalid glob pattern, got nil")
		}
	})
}

func TestFilteredProvider(t *testing.T) {
	ctx := context.Background()

	cp := provider.NewCustomVaultProvider()
	err := cp.Initialize(ctx, provider.ProviderConfig{
		Entities: map[string]map[string]any{
			"web": {
				"UserName":   "admin",
				"Password":   "secret123",
				"env:prod":   "prod_token",
				"user.notes": "some notes",
				"secret:aws": "key_val",
			},
		},
	})
	if err != nil {
		t.Fatalf("failed to initialize custom_vault: %v", err)
	}

	filter := provider.NewFieldFilter(
		[]string{"env:*", "UserName", "Password"},
		[]string{"*.notes", "secret:*", "Password"},
	)

	fp := provider.NewFilteredProvider(cp, filter, "work", false)

	t.Run("GetSecret allowed field", func(t *testing.T) {
		val, err := fp.GetSecret(ctx, "web:UserName")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if val != "admin" {
			t.Errorf("got %q, want %q", val, "admin")
		}
	})

	t.Run("GetSecret excluded field", func(t *testing.T) {
		_, err := fp.GetSecret(ctx, "web:Password")
		if err == nil {
			t.Errorf("expected error for excluded Password field, got nil")
		}
	})

	t.Run("GetSecret non-included field", func(t *testing.T) {
		_, err := fp.GetSecret(ctx, "web:secret:aws")
		if err == nil {
			t.Errorf("expected error for non-included field, got nil")
		}
	})

	t.Run("GetEntry attributes filtered", func(t *testing.T) {
		entry, err := fp.GetEntry(ctx, "web")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, ok := entry.Attributes["UserName"]; !ok {
			t.Errorf("expected UserName attribute to be present")
		}
		if _, ok := entry.Attributes["env:prod"]; !ok {
			t.Errorf("expected env:prod attribute to be present")
		}
		if _, ok := entry.Attributes["Password"]; ok {
			t.Errorf("expected Password attribute to be excluded")
		}
		if _, ok := entry.Attributes["user.notes"]; ok {
			t.Errorf("expected user.notes attribute to be excluded")
		}
		if _, ok := entry.Attributes["secret:aws"]; ok {
			t.Errorf("expected secret:aws attribute to be excluded")
		}
	})

	t.Run("Search attributes filtered", func(t *testing.T) {
		results, err := fp.Search(ctx, provider.SearchQuery{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(results) != 1 {
			t.Fatalf("got %d results, want 1", len(results))
		}
		attrs := results[0].Entry.Attributes
		if _, ok := attrs["UserName"]; !ok {
			t.Errorf("expected UserName attribute to be present")
		}
		if _, ok := attrs["Password"]; ok {
			t.Errorf("expected Password attribute to be excluded")
		}
	})

	t.Run("SupportsValueResolution passthrough", func(t *testing.T) {
		if !fp.SupportsValueResolution() {
			t.Errorf("expected SupportsValueResolution() to be true for custom_vault wrapper")
		}
	})
}
