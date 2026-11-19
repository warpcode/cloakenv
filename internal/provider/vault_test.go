package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestVaultProvider_BareMountListAndErrorPropagation(t *testing.T) {
	t.Run("KVv2 bare mount_path empty query lists /v1/<mount>/metadata?list=true", func(t *testing.T) {
		var requestedURLs []string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestedURLs = append(requestedURLs, r.URL.RequestURI())
			switch {
			case r.URL.Path == "/v1/kv/metadata" && r.URL.Query().Get("list") == "true":
				_ = json.NewEncoder(w).Encode(map[string]any{
					"data": map[string]any{"keys": []string{"app1", "vanished"}},
				})
			case r.URL.Path == "/v1/kv/data/app1":
				_ = json.NewEncoder(w).Encode(map[string]any{
					"data": map[string]any{"data": map[string]any{"API_KEY": "k1"}},
				})
			case r.URL.Path == "/v1/kv/data/vanished":
				w.WriteHeader(http.StatusNotFound)
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		}))
		defer srv.Close()

		vp := NewVaultProvider()
		ctx := context.Background()
		if err := vp.Initialize(ctx, ProviderConfig{
			Settings: map[string]string{
				"address":        srv.URL,
				"token":          "tok",
				"mount_path":     "kv",
				"engine_version": "2",
			},
		}); err != nil {
			t.Fatalf("Initialize failed: %v", err)
		}
		entries, err := vp.Search(ctx, SearchQuery{})
		if err != nil {
			t.Fatalf("Search failed: %v", err)
		}
		if len(entries) != 1 || entries[0]["API_KEY"] != "k1" {
			t.Fatalf("unexpected entries: %+v", entries)
		}
		if len(requestedURLs) == 0 || requestedURLs[0] != "/v1/kv/metadata?list=true" {
			t.Fatalf("expected bare mount list URL /v1/kv/metadata?list=true, got %v", requestedURLs)
		}
	})

	t.Run("listKeys HTTP 403 propagates error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"errors":["permission denied"]}`))
		}))
		defer srv.Close()

		vp := NewVaultProvider()
		ctx := context.Background()
		if err := vp.Initialize(ctx, ProviderConfig{
			Settings: map[string]string{"address": srv.URL, "token": "tok", "mount_path": "secret"},
		}); err != nil {
			t.Fatalf("Initialize failed: %v", err)
		}
		if _, err := vp.Search(ctx, SearchQuery{}); err == nil || !strings.Contains(err.Error(), "403") {
			t.Fatalf("expected HTTP 403 error propagation, got %v", err)
		}
	})

	t.Run("GetEntry HTTP 500 propagates error instead of silently skipping", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.URL.Path == "/v1/secret/metadata" && r.URL.Query().Get("list") == "true":
				_ = json.NewEncoder(w).Encode(map[string]any{
					"data": map[string]any{"keys": []string{"broken"}},
				})
			default:
				w.WriteHeader(http.StatusInternalServerError)
			}
		}))
		defer srv.Close()

		vp := NewVaultProvider()
		ctx := context.Background()
		if err := vp.Initialize(ctx, ProviderConfig{
			Settings: map[string]string{"address": srv.URL, "token": "tok", "mount_path": "secret"},
		}); err != nil {
			t.Fatalf("Initialize failed: %v", err)
		}
		if _, err := vp.Search(ctx, SearchQuery{}); err == nil || !strings.Contains(err.Error(), "500") {
			t.Fatalf("expected HTTP 500 error propagation, got %v", err)
		}
	})
}
