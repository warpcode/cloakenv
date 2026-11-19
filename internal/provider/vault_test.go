package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestVaultProvider_ReadOnlyFlatEntries(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Vault-Token") != "test-token" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/secret/metadata/app" && r.URL.Query().Get("list") == "true":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"keys": []string{"db"},
				},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/secret/data/app/db":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"data": map[string]any{
						"username": "pguser",
						"password": "pgpassword",
						"tags":     "prod,db",
					},
				},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	vp := NewVaultProvider()
	ctx := context.Background()
	err := vp.Initialize(ctx, ProviderConfig{
		Settings: map[string]string{
			"address":        srv.URL,
			"token":          "test-token",
			"mount_path":     "secret",
			"engine_version": "2",
		},
		Query: "app",
	})
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	entries, err := vp.Search(ctx, SearchQuery{})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	e := entries[0]
	if e.Title() != "db" {
		t.Errorf("Title = %q, want 'db'", e.Title())
	}
	if e.Path() != "app/db" {
		t.Errorf("Path = %q, want 'app/db'", e.Path())
	}
	if e["username"] != "pguser" || e["password"] != "pgpassword" {
		t.Errorf("unexpected flat fields: %+v", e)
	}
	if grp := e.Group(); grp == nil || grp["name"] != "app" || grp["path"] != "app" {
		t.Errorf("unexpected group metadata: %+v", grp)
	}
	if tags := e.Tags(); len(tags) != 2 || tags[0] != "prod" || tags[1] != "db" {
		t.Errorf("unexpected tags: %+v", tags)
	}

	pw, err := vp.GetSecret(ctx, "app/db:password")
	if err != nil || pw != "pgpassword" {
		t.Errorf("GetSecret = (%q, %v), want ('pgpassword', nil)", pw, err)
	}
}
