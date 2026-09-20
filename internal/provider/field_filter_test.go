package provider_test

import (
	"context"
	"testing"

	"github.com/warpcode/cloakenv/internal/provider"
)

func TestMatchGlob(t *testing.T) {
	tests := []struct {
		pattern string
		name    string
		want    bool
	}{
		{pattern: "env:*", name: "env:prod", want: true},
		{pattern: "env:*", name: "env:test/dev", want: true},
		{pattern: "UserName", name: "UserName", want: true},
		{pattern: "UserName", name: "Password", want: false},
		{pattern: "*.notes", name: "db.notes", want: true},
		{pattern: "*.notes", name: "app/db.notes", want: true},
		{pattern: "secret:*", name: "secret:api_token", want: true},
		{pattern: "secret:*", name: "public:key", want: false},
		{pattern: "*", name: "anything", want: true},
		{pattern: "*", name: "foo/bar", want: true},
		{pattern: "[", name: "invalid_pattern", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.pattern+"_vs_"+tt.name, func(t *testing.T) {
			got := provider.MatchGlob(tt.pattern, tt.name)
			if got != tt.want {
				t.Errorf("MatchGlob(%q, %q) = %v, want %v", tt.pattern, tt.name, got, tt.want)
			}
		})
	}
}

func TestIsFieldAllowed(t *testing.T) {
	include := []string{"env:*", "UserName"}
	exclude := []string{"*.notes", "secret:*"}

	tests := []struct {
		name string
		want bool
	}{
		{name: "UserName", want: true},
		{name: "env:prod", want: true},
		{name: "Password", want: false},
		{name: "app.notes", want: false},
		{name: "secret:token", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := provider.IsFieldAllowed(tt.name, include, exclude)
			if got != tt.want {
				t.Errorf("IsFieldAllowed(%q) = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}

func TestFieldFilteringProvider(t *testing.T) {
	ctx := context.Background()

	rawEntities := map[string]map[string]any{
		"service_a": {
			"title":        "Service A",
			"tags":         []string{"prod"},
			"UserName":     "alice",
			"Password":     "secret123",
			"env:PORT":     "8080",
			"env:HOST":     "localhost",
			"app.notes":    "internal notes",
			"secret:token": "tok123",
		},
	}

	cp := provider.NewCustomVaultProvider()
	err := cp.Initialize(ctx, provider.ProviderConfig{
		Entities: rawEntities,
	})
	if err != nil {
		t.Fatalf("Failed to initialize custom vault: %v", err)
	}

	filteredP := provider.NewFieldFilteringProvider(
		cp,
		[]string{"env:*", "UserName"},
		[]string{"*.notes", "secret:*"},
	)

	t.Run("Scheme and Inner", func(t *testing.T) {
		if filteredP.Scheme() != "custom_vault" {
			t.Errorf("Scheme() = %q, want %q", filteredP.Scheme(), "custom_vault")
		}
		if filteredP.Inner() != cp {
			t.Errorf("Inner() did not return original provider")
		}
		if !filteredP.SupportsValueResolution() {
			t.Errorf("SupportsValueResolution() = false, want true")
		}
	})

	t.Run("GetEntry", func(t *testing.T) {
		entry, err := filteredP.GetEntry(ctx, "service_a")
		if err != nil {
			t.Fatalf("GetEntry failed: %v", err)
		}

		if got, want := entry.Attributes["UserName"], "alice"; got != want {
			t.Errorf("UserName = %v, want %q", got, want)
		}
		if got, want := entry.Attributes["env:PORT"], "8080"; got != want {
			t.Errorf("env:PORT = %v, want %q", got, want)
		}
		if got, want := entry.Attributes["env:HOST"], "localhost"; got != want {
			t.Errorf("env:HOST = %v, want %q", got, want)
		}

		for _, excludedKey := range []string{"Password", "app.notes", "secret:token"} {
			if _, ok := entry.Attributes[excludedKey]; ok {
				t.Errorf("attribute %q should have been filtered out", excludedKey)
			}
		}
	})

	t.Run("Search", func(t *testing.T) {
		results, err := filteredP.Search(ctx, provider.SearchQuery{})
		if err != nil {
			t.Fatalf("Search failed: %v", err)
		}
		if len(results) != 1 {
			t.Fatalf("Search returned %d results, want 1", len(results))
		}

		attrs := results[0].Entry.Attributes
		if got, want := attrs["UserName"], "alice"; got != want {
			t.Errorf("UserName = %v, want %q", got, want)
		}
		if _, ok := attrs["Password"]; ok {
			t.Errorf("Password should have been filtered out")
		}
	})

	t.Run("GetSecret - Allowed Field", func(t *testing.T) {
		val, err := filteredP.GetSecret(ctx, "service_a:UserName")
		if err != nil {
			t.Fatalf("GetSecret(UserName) failed: %v", err)
		}
		if val != "alice" {
			t.Errorf("GetSecret(UserName) = %q, want %q", val, "alice")
		}
	})

	t.Run("GetSecret - Filtered Field", func(t *testing.T) {
		_, err := filteredP.GetSecret(ctx, "service_a:Password")
		if err == nil {
			t.Fatalf("GetSecret(Password) succeeded, want error for filtered field")
		}
	})

	t.Run("SetSecret and DeleteSecret - Filtered Field", func(t *testing.T) {
		err := filteredP.SetSecret(ctx, "service_a:Password", "newpass")
		if err == nil {
			t.Fatalf("SetSecret(Password) succeeded, want error for filtered field")
		}

		err = filteredP.DeleteSecret(ctx, "service_a:Password")
		if err == nil {
			t.Fatalf("DeleteSecret(Password) succeeded, want error for filtered field")
		}
	})
}
