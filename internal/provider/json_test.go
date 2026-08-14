package provider

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestJsonProvider(t *testing.T) {
	tempDir := t.TempDir()

	jsonContent := `{
		"entries": {
			"ssh_prod": {
				"tags": ["auth:ssh", "env:prod"],
				"title": "Production SSH Key",
				"username": "admin",
				"bit_strength": 4096,
				"public_keys": ["key1", "key2"]
			},
			"db_staging": {
				"tags": ["env:staging"],
				"title": "Staging Database",
				"password": "cache://db/staging_pass"
			}
		}
	}`
	jsonPath := filepath.Join(tempDir, "entries.json")
	if err := os.WriteFile(jsonPath, []byte(jsonContent), 0644); err != nil {
		t.Fatalf("failed to write temp json file: %v", err)
	}

	jp := NewJsonProvider()
	ctx := context.Background()
	cfg := ProviderConfig{
		Settings: map[string]string{
			"vault_path": jsonPath,
		},
	}

	if err := jp.Initialize(ctx, cfg); err != nil {
		t.Fatalf("failed to initialize json provider: %v", err)
	}

	// 1. Test GetSecret
	val, err := jp.GetSecret(ctx, "entries.ssh_prod.username")
	if err != nil {
		t.Errorf("GetSecret failed: %v", err)
	}
	if val != "admin" {
		t.Errorf("expected 'admin', got %q", val)
	}

	val, err = jp.GetSecret(ctx, "entries.ssh_prod.bit_strength")
	if err != nil {
		t.Errorf("GetSecret failed: %v", err)
	}
	if val != "4096" {
		t.Errorf("expected '4096', got %q", val)
	}

	// Test array index resolution
	val, err = jp.GetSecret(ctx, "entries.ssh_prod.public_keys.1")
	if err != nil {
		t.Errorf("GetSecret array index failed: %v", err)
	}
	if val != "key2" {
		t.Errorf("expected 'key2', got %q", val)
	}

	// 2. Test GetEntry
	entry, err := jp.GetEntry(ctx, "ssh_prod")
	if err != nil {
		t.Fatalf("GetEntry failed: %v", err)
	}
	if entry.Title != "Production SSH Key" {
		t.Errorf("expected Title 'Production SSH Key', got %q", entry.Title)
	}
	expectedTags := []string{"auth:ssh", "env:prod"}
	if !reflect.DeepEqual(entry.Tags, expectedTags) {
		t.Errorf("expected tags %v, got %v", expectedTags, entry.Tags)
	}

	// 3. Test Search (internal filtering)
	results, err := jp.Search(ctx, SearchQuery{Tags: []string{"env:prod"}})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("expected 1 result, got %d", len(results))
	} else if results[0].Path != "ssh_prod" {
		t.Errorf("expected path 'ssh_prod', got %q", results[0].Path)
	}
}

func TestJsonProviderCustomEntriesKey(t *testing.T) {
	tempDir := t.TempDir()

	// 1. Custom key: "hosts"
	hostsContent := `{
		"hosts": {
			"ssh_host": {
				"tags": ["auth:ssh"],
				"title": "Host SSH",
				"hostname": "custom.host"
			}
		}
	}`
	hostsPath := filepath.Join(tempDir, "hosts.json")
	if err := os.WriteFile(hostsPath, []byte(hostsContent), 0644); err != nil {
		t.Fatalf("failed to write hosts file: %v", err)
	}

	jp1 := NewJsonProvider()
	ctx := context.Background()
	cfg1 := ProviderConfig{
		Settings: map[string]string{
			"vault_path":  hostsPath,
			"entries_key": "hosts",
		},
	}
	if err := jp1.Initialize(ctx, cfg1); err != nil {
		t.Fatalf("failed to initialize hosts json: %v", err)
	}

	entry1, err := jp1.GetEntry(ctx, "ssh_host")
	if err != nil {
		t.Fatalf("failed to get entry from custom hosts key: %v", err)
	}
	if entry1.Title != "Host SSH" || entry1.Attributes["hostname"] != "custom.host" {
		t.Errorf("unexpected custom hosts entry: %+v", entry1)
	}

	// 2. Root key: "."
	rootContent := `{
		"ssh_root": {
			"tags": ["auth:root"],
			"title": "Root SSH",
			"hostname": "root.host"
		}
	}`
	rootPath := filepath.Join(tempDir, "root.json")
	if err := os.WriteFile(rootPath, []byte(rootContent), 0644); err != nil {
		t.Fatalf("failed to write root file: %v", err)
	}

	jp2 := NewJsonProvider()
	cfg2 := ProviderConfig{
		Settings: map[string]string{
			"vault_path":  rootPath,
			"entries_key": ".",
		},
	}
	if err := jp2.Initialize(ctx, cfg2); err != nil {
		t.Fatalf("failed to initialize root json: %v", err)
	}

	entry2, err := jp2.GetEntry(ctx, "ssh_root")
	if err != nil {
		t.Fatalf("failed to get entry from root mapping: %v", err)
	}
	if entry2.Title != "Root SSH" || entry2.Attributes["hostname"] != "root.host" {
		t.Errorf("unexpected root entry: %+v", entry2)
	}

	// 3. Missing key: should gracefully initialize with 0 entries
	jp3 := NewJsonProvider()
	cfg3 := ProviderConfig{
		Settings: map[string]string{
			"vault_path":  rootPath,
			"entries_key": "hosts",
		},
	}
	if err := jp3.Initialize(ctx, cfg3); err != nil {
		t.Fatalf("failed to initialize json with missing key: %v", err)
	}

	results, err := jp3.Search(ctx, SearchQuery{})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 entries from missing key database, got %d", len(results))
	}
}

func TestJsonProviderSingleEntity(t *testing.T) {
	tempDir := t.TempDir()

	jsonContent := `{
		"title": "My Single JSON Vault",
		"tags": ["env:local", "dev"],
		"secret1": "value1",
		"secret2": {
			"nested_key": "nested_value"
		},
		"list": ["item1", "item2"]
	}`
	jsonPath := filepath.Join(tempDir, "single.json")
	if err := os.WriteFile(jsonPath, []byte(jsonContent), 0644); err != nil {
		t.Fatalf("failed to write temp json file: %v", err)
	}

	jp := NewJsonProvider()
	ctx := context.Background()
	isTrue := true
	cfg := ProviderConfig{
		Settings: map[string]string{
			"vault_path": jsonPath,
		},
		SingleEntity:    &isTrue,
		EntitiesRootKey: ".",
	}

	if err := jp.Initialize(ctx, cfg); err != nil {
		t.Fatalf("failed to initialize json: %v", err)
	}

	// 1. GetSecret
	val, err := jp.GetSecret(ctx, "secret1")
	if err != nil {
		t.Fatalf("GetSecret failed: %v", err)
	}
	if val != "value1" {
		t.Errorf("expected 'value1', got %q", val)
	}

	// Serialization of nested map
	val, err = jp.GetSecret(ctx, "secret2")
	if err != nil {
		t.Fatalf("GetSecret failed: %v", err)
	}
	expectedMapJson := `{"nested_key":"nested_value"}`
	if val != expectedMapJson {
		t.Errorf("expected serialized map %q, got %q", expectedMapJson, val)
	}

	// Serialization of list
	val, err = jp.GetSecret(ctx, "list")
	if err != nil {
		t.Fatalf("GetSecret failed: %v", err)
	}
	expectedListJson := `["item1","item2"]`
	if val != expectedListJson {
		t.Errorf("expected serialized list %q, got %q", expectedListJson, val)
	}

	// 2. GetEntry
	entry, err := jp.GetEntry(ctx, "")
	if err != nil {
		t.Fatalf("GetEntry failed: %v", err)
	}
	if entry.Title != "My Single JSON Vault" {
		t.Errorf("expected title 'My Single JSON Vault', got %q", entry.Title)
	}
	if len(entry.Tags) != 2 || entry.Tags[0] != "env:local" || entry.Tags[1] != "dev" {
		t.Errorf("unexpected tags: %v", entry.Tags)
	}

	// 3. Search
	results, err := jp.Search(ctx, SearchQuery{})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Path != "" {
		t.Errorf("expected empty path, got %q", results[0].Path)
	}
}

func TestJsonProvider_SetSecret(t *testing.T) {
	jp := NewJsonProvider()
	err := jp.SetSecret(context.Background(), "KEY", "VAL")
	if err == nil {
		t.Error("expected error for SetSecret, got nil")
	}
	if err != nil && !strings.Contains(err.Error(), "read-only") {
		t.Errorf("expected error to contain 'read-only', got %q", err.Error())
	}
}

func TestJsonProvider_DeleteSecret(t *testing.T) {
	jp := NewJsonProvider()
	err := jp.DeleteSecret(context.Background(), "KEY")
	if err == nil {
		t.Error("expected error for DeleteSecret, got nil")
	}
	if err != nil && !strings.Contains(err.Error(), "read-only") {
		t.Errorf("expected error to contain 'read-only', got %q", err.Error())
	}
}

func TestJsonProviderInitialize_Errors(t *testing.T) {
	tempDir := t.TempDir()

	invalidJsonPath := filepath.Join(tempDir, "invalid.json")
	if err := os.WriteFile(invalidJsonPath, []byte("{invalid json"), 0644); err != nil {
		t.Fatalf("failed to write invalid json: %v", err)
	}

	nullJsonPath := filepath.Join(tempDir, "null.json")
	if err := os.WriteFile(nullJsonPath, []byte("null"), 0644); err != nil {
		t.Fatalf("failed to write null json: %v", err)
	}

	tests := []struct {
		name    string
		cfg     ProviderConfig
		wantErr bool
	}{
		{
			name: "missing vault_path",
			cfg: ProviderConfig{
				Settings: map[string]string{},
			},
			wantErr: true,
		},
		{
			name: "non-existent file",
			cfg: ProviderConfig{
				Settings: map[string]string{
					"vault_path": filepath.Join(tempDir, "does-not-exist.json"),
				},
			},
			wantErr: false,
		},
		{
			name: "directory as file path",
			cfg: ProviderConfig{
				Settings: map[string]string{
					"vault_path": tempDir,
				},
			},
			wantErr: true,
		},
		{
			name: "invalid json content",
			cfg: ProviderConfig{
				Settings: map[string]string{
					"vault_path": invalidJsonPath,
				},
			},
			wantErr: true,
		},
		{
			name: "null json content",
			cfg: ProviderConfig{
				Settings: map[string]string{
					"vault_path": nullJsonPath,
				},
			},
			wantErr: false,
		},
	}

	ctx := context.Background()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jp := NewJsonProvider()
			err := jp.Initialize(ctx, tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("Initialize() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestSerializeJsonVal(t *testing.T) {
	tests := []struct {
		name    string
		val     any
		want    string
		wantErr bool
	}{
		{
			name:    "string",
			val:     "hello world",
			want:    "hello world",
			wantErr: false,
		},
		{
			name:    "slice of any",
			val:     []any{"item1", 2, true},
			want:    `["item1",2,true]`,
			wantErr: false,
		},
		{
			name:    "map of string to any",
			val:     map[string]any{"key1": "val1", "key2": 42},
			want:    `{"key1":"val1","key2":42}`,
			wantErr: false,
		},
		{
			name:    "integer (default)",
			val:     42,
			want:    "42",
			wantErr: false,
		},
		{
			name:    "float (default)",
			val:     3.14,
			want:    "3.14",
			wantErr: false,
		},
		{
			name:    "boolean (default)",
			val:     true,
			want:    "true",
			wantErr: false,
		},
		{
			name:    "nil (default)",
			val:     nil,
			want:    "<nil>",
			wantErr: false,
		},
		{
			name:    "unserializable slice",
			val:     []any{make(chan int)},
			want:    "",
			wantErr: true,
		},
		{
			name:    "unserializable map",
			val:     map[string]any{"key": make(chan int)},
			want:    "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := serializeJsonVal(tt.val)
			if (err != nil) != tt.wantErr {
				t.Errorf("serializeJsonVal() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("serializeJsonVal() = %v, want %v", got, tt.want)
			}
		})
	}
}
