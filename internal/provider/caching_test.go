package provider

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zalando/go-keyring"
)

type mockBackendProvider struct {
	calls atomic.Int64
	data  map[string]string
}

func (m *mockBackendProvider) Scheme() string { return "mock" }
func (m *mockBackendProvider) Initialize(_ context.Context, _ ProviderConfig) error {
	return nil
}
func (m *mockBackendProvider) GetSecret(_ context.Context, location string) (string, error) {
	m.calls.Add(1)
	if val, ok := m.data[location]; ok {
		return val, nil
	}
	return "mock_secret_" + location, nil
}
func (m *mockBackendProvider) Validate(_ map[string]string) error { return nil }

type mockSearchableBackendProvider struct {
	mockBackendProvider
	entries map[string]Entry
}

func (m *mockSearchableBackendProvider) GetEntry(_ context.Context, location string) (Entry, error) {
	m.calls.Add(1)
	if e, ok := m.entries[location]; ok {
		return e, nil
	}
	return Entry{
		"title":    location,
		"path":     location,
		"Password": "mock_entry_pass_" + location,
	}, nil
}

func (m *mockSearchableBackendProvider) Search(_ context.Context, query SearchQuery) ([]Entry, error) {
	m.calls.Add(1)
	var res []Entry
	for k, e := range m.entries {
		if query.Title == "" || strings.Contains(e.Title(), query.Title) {
			cp := e.Clone()
			cp["path"] = k
			res = append(res, cp)
		}
	}
	return res, nil
}

func TestCachingProvider_KeyringMode(t *testing.T) {
	keyring.MockInit()
	tempDir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", tempDir)
	t.Setenv("HOME", tempDir)

	backend := &mockBackendProvider{data: make(map[string]string)}
	cp, err := NewCachingProvider(backend, CachingConfig{
		VaultName:     "test_vault",
		TTL:           100 * time.Millisecond,
		Mode:          CacheModeKeyring,
		KeyringPrefix: "test_prefix",
	})
	if err != nil {
		t.Fatalf("NewCachingProvider failed: %v", err)
	}

	ctx := context.Background()

	// 1. Initial lookup -> cache miss -> queries backend
	val1, err := cp.GetSecret(ctx, "account_1")
	if err != nil {
		t.Fatalf("GetSecret failed: %v", err)
	}
	if val1 != "mock_secret_account_1" {
		t.Errorf("val1 = %q, want 'mock_secret_account_1'", val1)
	}
	if backend.calls.Load() != 1 {
		t.Errorf("expected 1 backend call on miss, got %d", backend.calls.Load())
	}

	// In keyring mode: zero files touch disk under tempDir/test_prefix
	files, _ := os.ReadDir(tempDir)
	for _, f := range files {
		if f.Name() == "test_prefix" {
			t.Errorf("found disk cache directory in keyring mode: %s", f.Name())
		}
	}

	// 2. Immediate second lookup -> cache hit -> does NOT query backend
	val2, err := cp.GetSecret(ctx, "account_1")
	if err != nil {
		t.Fatalf("GetSecret second call failed: %v", err)
	}
	if val2 != val1 {
		t.Errorf("val2 = %q, want %q", val2, val1)
	}
	if backend.calls.Load() != 1 {
		t.Errorf("expected cache hit (still 1 backend call), got %d", backend.calls.Load())
	}

	// 3. Expiration: after TTL, entry is rejected and evicted
	time.Sleep(150 * time.Millisecond)

	val3, err := cp.GetSecret(ctx, "account_1")
	if err != nil {
		t.Fatalf("GetSecret after TTL failed: %v", err)
	}
	if val3 != val1 {
		t.Errorf("val3 = %q, want %q", val3, val1)
	}
	if backend.calls.Load() != 2 {
		t.Errorf("expected cache miss after expiration (2 backend calls), got %d", backend.calls.Load())
	}

	// 4. Clear()
	cachingConcrete, ok := cp.(*CachingProvider)
	if !ok {
		t.Fatalf("expected *CachingProvider")
	}
	if err := cachingConcrete.Clear(); err != nil {
		t.Fatalf("Clear() failed: %v", err)
	}
	_, _ = cp.GetSecret(ctx, "account_1")
	if backend.calls.Load() != 3 {
		t.Errorf("expected backend call after Clear(), got %d", backend.calls.Load())
	}
}

func TestCachingProvider_EncryptedDiskMode(t *testing.T) {
	keyring.MockInit()
	tempDir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", tempDir)
	t.Setenv("HOME", tempDir)

	backend := &mockBackendProvider{data: make(map[string]string)}
	cp, err := NewCachingProvider(backend, CachingConfig{
		VaultName:     "disk_vault",
		TTL:           100 * time.Millisecond,
		Mode:          CacheModeEncryptedDisk,
		KeyringPrefix: "disk_prefix",
	})
	if err != nil {
		t.Fatalf("NewCachingProvider failed: %v", err)
	}

	ctx := context.Background()

	val1, err := cp.GetSecret(ctx, "sec_key")
	if err != nil {
		t.Fatalf("GetSecret failed: %v", err)
	}
	if val1 != "mock_secret_sec_key" {
		t.Errorf("val1 = %q", val1)
	}

	cachingConcrete, ok := cp.(*CachingProvider)
	if !ok {
		t.Fatalf("expected *CachingProvider")
	}

	// Verify disk file is created and encrypted
	cacheFiles, err := os.ReadDir(cachingConcrete.cacheDir)
	if err != nil {
		t.Fatalf("failed to read cache dir: %v", err)
	}
	if len(cacheFiles) != 1 {
		t.Fatalf("expected 1 cache file on disk, got %d", len(cacheFiles))
	}

	cacheFilePath := filepath.Join(cachingConcrete.cacheDir, cacheFiles[0].Name())
	rawDiskContent, err := os.ReadFile(cacheFilePath)
	if err != nil {
		t.Fatalf("failed to read cache file: %v", err)
	}

	// Plaintext secret must NOT exist on disk!
	if strings.Contains(string(rawDiskContent), "mock_secret_sec_key") {
		t.Fatalf("PLAINTEXT LEAK DETECTED: cache file on disk contains plaintext secret!")
	}

	// Verify file permissions (0600)
	if runtime.GOOS != "windows" {
		fi, err := os.Stat(cacheFilePath)
		if err != nil {
			t.Fatalf("stat failed: %v", err)
		}
		if fi.Mode().Perm() != 0600 {
			t.Errorf("expected 0600 file permissions, got %v", fi.Mode().Perm())
		}
	}

	// 2. Cache hit
	val2, err := cp.GetSecret(ctx, "sec_key")
	if err != nil || val2 != val1 {
		t.Errorf("cache hit failed: val2=%q, err=%v", val2, err)
	}
	if backend.calls.Load() != 1 {
		t.Errorf("expected 1 call on cache hit, got %d", backend.calls.Load())
	}

	// 3. Expiration & Prune
	time.Sleep(150 * time.Millisecond)

	count, bytesReclaimed, err := cachingConcrete.Prune()
	if err != nil {
		t.Fatalf("Prune() failed: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 pruned file, got %d", count)
	}
	if bytesReclaimed <= 0 {
		t.Errorf("expected >0 bytes reclaimed, got %d", bytesReclaimed)
	}
	remainingFiles, _ := os.ReadDir(cachingConcrete.cacheDir)
	if len(remainingFiles) != 0 {
		t.Errorf("expected 0 files after Prune() on expired cache, got %d", len(remainingFiles))
	}

	// 4. Clear()
	_, _ = cp.GetSecret(ctx, "sec_key")
	if err := cachingConcrete.Clear(); err != nil {
		t.Fatalf("Clear() failed: %v", err)
	}
	clearedFiles, _ := os.ReadDir(cachingConcrete.cacheDir)
	if len(clearedFiles) != 0 {
		t.Errorf("expected 0 files after Clear(), got %d", len(clearedFiles))
	}
}

func TestCachingProvider_SearchableInterface(t *testing.T) {
	keyring.MockInit()
	tempDir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", tempDir)
	t.Setenv("HOME", tempDir)

	searchableBackend := &mockSearchableBackendProvider{
		mockBackendProvider: mockBackendProvider{data: make(map[string]string)},
		entries: map[string]Entry{
			"site/app": {
				"title": "Test App",
				"path":  "site/app",
				"HOST":  "db.example.internal",
			},
		},
	}

	cp, err := NewCachingProvider(searchableBackend, CachingConfig{
		VaultName:     "search_vault",
		TTL:           500 * time.Millisecond,
		Mode:          CacheModeKeyring,
		KeyringPrefix: "search_prefix",
	})
	if err != nil {
		t.Fatalf("NewCachingProvider failed: %v", err)
	}

	searchable, ok := cp.(SearchableProvider)
	if !ok {
		t.Fatalf("expected CachingProvider to implement SearchableProvider")
	}

	ctx := context.Background()

	// 1. GetEntry
	entry1, err := searchable.GetEntry(ctx, "site/app")
	if err != nil {
		t.Fatalf("GetEntry failed: %v", err)
	}
	if entry1["HOST"] != "db.example.internal" {
		t.Errorf("entry1 HOST = %v", entry1["HOST"])
	}
	if searchableBackend.calls.Load() != 1 {
		t.Errorf("expected 1 call on miss, got %d", searchableBackend.calls.Load())
	}

	// 2. Second GetEntry -> cache hit
	entry2, err := searchable.GetEntry(ctx, "site/app")
	if err != nil {
		t.Fatalf("second GetEntry failed: %v", err)
	}
	if entry2["HOST"] != "db.example.internal" {
		t.Errorf("entry2 HOST = %v", entry2["HOST"])
	}
	if searchableBackend.calls.Load() != 1 {
		t.Errorf("expected still 1 call on cache hit, got %d", searchableBackend.calls.Load())
	}

	// 3. Search delegates to backend
	results, err := searchable.Search(ctx, SearchQuery{Title: "App"})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("expected 1 search result, got %d", len(results))
	}
}

type mockSearchableCacheBackend struct {
	mockBackendProvider
	searchCalls atomic.Int64
	entries     []Entry
}

func (m *mockSearchableCacheBackend) GetEntry(_ context.Context, location string) (Entry, error) {
	m.calls.Add(1)
	return Entry{"title": location, "path": location, "tags": []string{"t1"}, "secret": "val"}, nil
}

func (m *mockSearchableCacheBackend) Search(_ context.Context, _ SearchQuery) ([]Entry, error) {
	m.searchCalls.Add(1)
	out := make([]Entry, len(m.entries))
	for i, e := range m.entries {
		out[i] = e.Clone()
	}
	return out, nil
}

func TestSearchableCachingProviderSearchCachesResults(t *testing.T) {
	for _, mode := range []string{CacheModeKeyring, CacheModeEncryptedDisk} {
		t.Run(mode, func(t *testing.T) {
			keyring.MockInit()
			tmpHome := t.TempDir()
			t.Setenv("HOME", tmpHome)
			t.Setenv("XDG_CACHE_HOME", filepath.Join(tmpHome, ".cache"))

			backend := &mockSearchableCacheBackend{
				entries: []Entry{
					{"title": "db", "path": "prod/db", "tags": []string{"prod", "db"}, "password": "pw1"},
				},
			}
			wrapped, err := NewCachingProvider(backend, CachingConfig{
				VaultName:     "work_" + mode,
				TTL:           time.Minute,
				Mode:          mode,
				KeyringPrefix: "cloakenv_test",
			})
			if err != nil {
				t.Fatalf("NewCachingProvider failed: %v", err)
			}
			sp, ok := wrapped.(SearchableProvider)
			if !ok {
				t.Fatalf("expected wrapped provider to implement SearchableProvider")
			}

			ctx := context.Background()
			got1, err := sp.Search(ctx, SearchQuery{Tags: []string{"db", "prod"}})
			if err != nil {
				t.Fatalf("first Search failed: %v", err)
			}
			if len(got1) != 1 || got1[0]["password"] != "pw1" {
				t.Fatalf("unexpected first Search result: %v", got1)
			}
			if backend.searchCalls.Load() != 1 {
				t.Fatalf("expected 1 backend search call, got %d", backend.searchCalls.Load())
			}

			// Canonicalized tag order must hit the same cache entry without invoking backend.
			got2, err := sp.Search(ctx, SearchQuery{Tags: []string{"prod", "db"}})
			if err != nil {
				t.Fatalf("second Search failed: %v", err)
			}
			if len(got2) != 1 || got2[0]["password"] != "pw1" {
				t.Fatalf("unexpected cached Search result: %v", got2)
			}
			if len(got2[0].Tags()) != 2 {
				t.Fatalf("expected cached Entry tags preserved, got %v", got2[0].Tags())
			}
			if backend.searchCalls.Load() != 1 {
				t.Fatalf("expected cached Search hit without calling backend again, got %d calls", backend.searchCalls.Load())
			}

			// Different query must miss cache.
			_, err = sp.Search(ctx, SearchQuery{Path: "other"})
			if err != nil {
				t.Fatalf("third Search failed: %v", err)
			}
			if backend.searchCalls.Load() != 2 {
				t.Fatalf("expected cache miss on distinct SearchQuery, got %d calls", backend.searchCalls.Load())
			}
		})
	}
}
