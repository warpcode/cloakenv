package provider

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestPassProvider_Scheme(t *testing.T) {
	p := NewPassProvider()
	if got, want := p.Scheme(), "pass"; got != want {
		t.Errorf("Scheme() = %q, want %q", got, want)
	}
}

func TestPassProvider_ReadOnly(t *testing.T) {
	p := NewPassProvider()
	ctx := context.Background()

	if err := p.SetSecret(ctx, "dev/db", "newpass"); err == nil {
		t.Error("SetSecret() expected read-only error, got nil")
	}

	if err := p.DeleteSecret(ctx, "dev/db"); err == nil {
		t.Error("DeleteSecret() expected read-only error, got nil")
	}
}

func TestPassProvider_Validate(t *testing.T) {
	p := NewPassProvider()

	t.Run("empty settings is valid", func(t *testing.T) {
		if err := p.Validate(map[string]string{}); err != nil {
			t.Errorf("Validate() with empty settings = %v, want nil", err)
		}
	})

	t.Run("valid directory", func(t *testing.T) {
		tmpDir := t.TempDir()
		if err := p.Validate(map[string]string{"store_dir": tmpDir}); err != nil {
			t.Errorf("Validate() with valid dir = %v, want nil", err)
		}
	})

	t.Run("invalid path (file not dir)", func(t *testing.T) {
		tmpDir := t.TempDir()
		filePath := filepath.Join(tmpDir, "testfile")
		if err := os.WriteFile(filePath, []byte("content"), 0o600); err != nil {
			t.Fatalf("failed to create temp file: %v", err)
		}

		if err := p.Validate(map[string]string{"store_dir": filePath}); err == nil {
			t.Errorf("Validate() with regular file = nil, want error")
		}
	})
}

func TestPassProvider_Initialize(t *testing.T) {
	p := NewPassProvider()
	ctx := context.Background()
	tmpDir := t.TempDir()

	err := p.Initialize(ctx, ProviderConfig{
		Settings: map[string]string{
			"store_dir": tmpDir,
		},
	})
	if err != nil {
		t.Fatalf("Initialize() failed: %v", err)
	}

	if got, want := p.StoreDir(), tmpDir; got != want {
		t.Errorf("StoreDir() = %q, want %q", got, want)
	}
}

func TestPassProvider_StoreDir_Precedence(t *testing.T) {
	p := NewPassProvider()
	ctx := context.Background()

	t.Run("explicit setting takes precedence over env", func(t *testing.T) {
		tmp1 := t.TempDir()
		tmp2 := t.TempDir()
		t.Setenv("PASSWORD_STORE_DIR", tmp1)

		err := p.Initialize(ctx, ProviderConfig{
			Settings: map[string]string{
				"store_dir": tmp2,
			},
		})
		if err != nil {
			t.Fatalf("Initialize() failed: %v", err)
		}

		if got, want := p.StoreDir(), tmp2; got != want {
			t.Errorf("StoreDir() = %q, want %q", got, want)
		}
	})

	t.Run("falls back to PASSWORD_STORE_DIR", func(t *testing.T) {
		tmp := t.TempDir()
		t.Setenv("PASSWORD_STORE_DIR", tmp)

		p2 := NewPassProvider()
		err := p2.Initialize(ctx, ProviderConfig{})
		if err != nil {
			t.Fatalf("Initialize() failed: %v", err)
		}

		if got, want := p2.StoreDir(), tmp; got != want {
			t.Errorf("StoreDir() = %q, want %q", got, want)
		}
	})
}

func TestPassProvider_GetSecret(t *testing.T) {
	storeDir := t.TempDir()

	// Mock file data:
	// development/db.gpg ->
	// supersecretpassword
	// username: dbuser
	// Host: db.example.com
	// port: 5432
	// tags: dev, database
	mockFiles := map[string][]byte{
		filepath.Join(storeDir, "development", "db.gpg"): []byte("supersecretpassword\nusername: dbuser\nHost: db.example.com\nport: 5432\ntags: dev, database\n"),
		filepath.Join(storeDir, "api_key.gpg"):           []byte("sk_live_123456\n"),
	}

	// Create dummy files on disk so Stat / WalkDir see them
	for filePath, content := range mockFiles {
		if err := os.MkdirAll(filepath.Dir(filePath), 0o700); err != nil {
			t.Fatalf("failed to create dir: %v", err)
		}
		if err := os.WriteFile(filePath, content, 0o600); err != nil {
			t.Fatalf("failed to write dummy gpg file: %v", err)
		}
	}

	p := NewPassProvider()
	p.SetDecryptFunc(func(_ context.Context, filePath string) ([]byte, error) {
		clean := filepath.Clean(filePath)
		if data, ok := mockFiles[clean]; ok {
			cp := make([]byte, len(data))
			copy(cp, data)
			return cp, nil
		}
		return nil, fmt.Errorf("gpg: decryption failed: file not found: %s", filePath)
	})

	ctx := context.Background()
	if err := p.Initialize(ctx, ProviderConfig{Settings: map[string]string{"store_dir": storeDir}}); err != nil {
		t.Fatalf("Initialize() failed: %v", err)
	}

	tests := []struct {
		name     string
		location string
		want     string
		wantErr  bool
	}{
		{
			name:     "bare path defaults to password (first line)",
			location: "development/db",
			want:     "supersecretpassword",
		},
		{
			name:     "explicit :password returns first line",
			location: "development/db:password",
			want:     "supersecretpassword",
		},
		{
			name:     "explicit :Password case-insensitive returns first line",
			location: "development/db:Password",
			want:     "supersecretpassword",
		},
		{
			name:     "explicit attribute :username",
			location: "development/db:username",
			want:     "dbuser",
		},
		{
			name:     "explicit attribute case-insensitive :host",
			location: "development/db:host",
			want:     "db.example.com",
		},
		{
			name:     "explicit attribute :port",
			location: "development/db:port",
			want:     "5432",
		},
		{
			name:     "single-line pass file",
			location: "api_key",
			want:     "sk_live_123456",
		},
		{
			name:     "single-line pass file with :password",
			location: "api_key:password",
			want:     "sk_live_123456",
		},
		{
			name:     "non-existent attribute returns error",
			location: "development/db:nonexistent",
			wantErr:  true,
		},
		{
			name:     "non-existent entry returns error",
			location: "missing/entry",
			wantErr:  true,
		},
		{
			name:     "path traversal attempt rejected",
			location: "../../etc/shadow",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := p.GetSecret(ctx, tt.location)
			if (err != nil) != tt.wantErr {
				t.Fatalf("GetSecret(%q) error = %v, wantErr %v", tt.location, err, tt.wantErr)
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("GetSecret(%q) = %q, want %q", tt.location, got, tt.want)
			}
		})
	}
}

func TestPassProvider_GetEntry(t *testing.T) {
	storeDir := t.TempDir()

	entryContent := []byte("secret123\nusername: alice\nservice: web\ntags: production, secure\n")
	gpgPath := filepath.Join(storeDir, "work", "web_login.gpg")
	if err := os.MkdirAll(filepath.Dir(gpgPath), 0o700); err != nil {
		t.Fatalf("failed to create dir: %v", err)
	}
	if err := os.WriteFile(gpgPath, entryContent, 0o600); err != nil {
		t.Fatalf("failed to write dummy file: %v", err)
	}

	p := NewPassProvider()
	p.SetDecryptFunc(func(_ context.Context, filePath string) ([]byte, error) {
		if filepath.Clean(filePath) == filepath.Clean(gpgPath) {
			cp := make([]byte, len(entryContent))
			copy(cp, entryContent)
			return cp, nil
		}
		return nil, os.ErrNotExist
	})

	ctx := context.Background()
	if err := p.Initialize(ctx, ProviderConfig{Settings: map[string]string{"store_dir": storeDir}}); err != nil {
		t.Fatalf("Initialize() failed: %v", err)
	}

	t.Run("GetEntryWithPath", func(t *testing.T) {
		entry, authPath, err := p.GetEntryWithPath(ctx, "work/web_login")
		if err != nil {
			t.Fatalf("GetEntryWithPath() error = %v", err)
		}
		if got, want := authPath, "work/web_login"; got != want {
			t.Errorf("authoritativePath = %q, want %q", got, want)
		}
		if got, want := entry.Title, "web_login"; got != want {
			t.Errorf("entry.Title = %q, want %q", got, want)
		}
		if entry.Group == nil || entry.Group.Path != "work" {
			t.Errorf("entry.Group.Path = %v, want 'work'", entry.Group)
		}
		if got, want := entry.Attributes["Password"], "secret123"; got != want {
			t.Errorf("entry.Attributes[Password] = %v, want %q", got, want)
		}
		if got, want := entry.Attributes["username"], "alice"; got != want {
			t.Errorf("entry.Attributes[username] = %v, want %q", got, want)
		}
		if len(entry.Tags) != 2 || entry.Tags[0] != "production" || entry.Tags[1] != "secure" {
			t.Errorf("entry.Tags = %v, want ['production', 'secure']", entry.Tags)
		}
	})

	t.Run("GetEntryTitle", func(t *testing.T) {
		title, err := p.GetEntryTitle(ctx, "work/web_login")
		if err != nil {
			t.Fatalf("GetEntryTitle() error = %v", err)
		}
		if got, want := title, "web_login"; got != want {
			t.Errorf("GetEntryTitle() = %q, want %q", got, want)
		}
	})
}

func TestPassProvider_Search(t *testing.T) {
	storeDir := t.TempDir()

	files := map[string][]byte{
		filepath.Join(storeDir, "development", "db.gpg"): []byte("dbpass123\nusername: postgres\ntags: dev, database\n"),
		filepath.Join(storeDir, "production", "db.gpg"):  []byte("prodpass456\nusername: postgres\ntags: prod, database\n"),
		filepath.Join(storeDir, "tokens", "github.gpg"):  []byte("ghp_secret\ntags: dev, api\n"),
	}

	for p, content := range files {
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatalf("failed to create dir: %v", err)
		}
		if err := os.WriteFile(p, content, 0o600); err != nil {
			t.Fatalf("failed to write dummy file: %v", err)
		}
	}

	p := NewPassProvider()
	p.SetDecryptFunc(func(_ context.Context, filePath string) ([]byte, error) {
		clean := filepath.Clean(filePath)
		if data, ok := files[clean]; ok {
			cp := make([]byte, len(data))
			copy(cp, data)
			return cp, nil
		}
		return nil, os.ErrNotExist
	})

	ctx := context.Background()
	if err := p.Initialize(ctx, ProviderConfig{Settings: map[string]string{"store_dir": storeDir}}); err != nil {
		t.Fatalf("Initialize() failed: %v", err)
	}

	t.Run("search all", func(t *testing.T) {
		results, err := p.Search(ctx, SearchQuery{})
		if err != nil {
			t.Fatalf("Search() error = %v", err)
		}
		if len(results) != 3 {
			t.Fatalf("Search() returned %d results, want 3", len(results))
		}
	})

	t.Run("search by tag", func(t *testing.T) {
		results, err := p.Search(ctx, SearchQuery{Tags: []string{"prod"}})
		if err != nil {
			t.Fatalf("Search() error = %v", err)
		}
		if len(results) != 1 {
			t.Fatalf("Search(Tags: [prod]) returned %d results, want 1", len(results))
		}
		if got, want := results[0].Path, "production/db"; got != want {
			t.Errorf("Path = %q, want %q", got, want)
		}
	})

	t.Run("search by title", func(t *testing.T) {
		results, err := p.Search(ctx, SearchQuery{Title: "github"})
		if err != nil {
			t.Fatalf("Search() error = %v", err)
		}
		if len(results) != 1 {
			t.Fatalf("Search(Title: github) returned %d results, want 1", len(results))
		}
		if got, want := results[0].Path, "tokens/github"; got != want {
			t.Errorf("Path = %q, want %q", got, want)
		}
	})

	t.Run("search by path", func(t *testing.T) {
		results, err := p.Search(ctx, SearchQuery{Path: "development"})
		if err != nil {
			t.Fatalf("Search() error = %v", err)
		}
		if len(results) != 1 {
			t.Fatalf("Search(Path: development) returned %d results, want 1", len(results))
		}
		if got, want := results[0].Path, "development/db"; got != want {
			t.Errorf("Path = %q, want %q", got, want)
		}
	})
}
