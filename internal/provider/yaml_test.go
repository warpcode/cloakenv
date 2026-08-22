package provider

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestYamlProvider(t *testing.T) {
	// Create a temporary entries.yaml file
	tempDir, err := os.MkdirTemp("", "cloakenv-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	yamlContent := `
entries:
  ssh_prod:
    tags:
      - auth:ssh
      - env:prod
    title: "Production SSH Key"
    username: "admin"
    bit_strength: 4096
    public_keys:
      - "key1"
      - "key2"
  db_staging:
    tags:
      - env:staging
    title: "Staging Database"
    password: "cache://db/staging_pass"
`
	yamlPath := filepath.Join(tempDir, "entries.yaml")
	if err := os.WriteFile(yamlPath, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("failed to write temp yaml file: %v", err)
	}

	// Initialize YamlProvider
	yp := NewYamlProvider()
	ctx := context.Background()
	cfg := ProviderConfig{
		Settings: map[string]string{
			"vault_path": yamlPath,
		},
	}

	if err := yp.Initialize(ctx, cfg); err != nil {
		t.Fatalf("failed to initialize yaml provider: %v", err)
	}

	// 1. Test GetSecret
	val, err := yp.GetSecret(ctx, "entries.ssh_prod.username")
	if err != nil {
		t.Errorf("GetSecret failed: %v", err)
	}
	if val != "admin" {
		t.Errorf("expected 'admin', got %q", val)
	}

	val, err = yp.GetSecret(ctx, "entries.ssh_prod.bit_strength")
	if err != nil {
		t.Errorf("GetSecret failed: %v", err)
	}
	if val != "4096" {
		t.Errorf("expected '4096', got %q", val)
	}

	// Test array index resolution
	val, err = yp.GetSecret(ctx, "entries.ssh_prod.public_keys.1")
	if err != nil {
		t.Errorf("GetSecret array index failed: %v", err)
	}
	if val != "key2" {
		t.Errorf("expected 'key2', got %q", val)
	}

	// 2. Test GetEntry
	entry, err := yp.GetEntry(ctx, "ssh_prod")
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
	expectedPubKeys := []any{"key1", "key2"}
	if !reflect.DeepEqual(entry.Attributes["public_keys"], expectedPubKeys) {
		t.Errorf("expected public_keys %v, got %v", expectedPubKeys, entry.Attributes["public_keys"])
	}

	// 3. Test Search (internal filtering)
	results, err := yp.Search(ctx, SearchQuery{Tags: []string{"env:prod"}})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("expected 1 result, got %d", len(results))
	} else if results[0].Path != "ssh_prod" {
		t.Errorf("expected path 'ssh_prod', got %q", results[0].Path)
	}
}

func TestYamlProviderCustomEntriesKey(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "cloakenv-test-custom")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	// 1. Custom key: "hosts"
	hostsContent := `
hosts:
  ssh_host:
    tags: [auth:ssh]
    title: "Host SSH"
    hostname: "custom.host"
`
	hostsPath := filepath.Join(tempDir, "hosts.yaml")
	if err := os.WriteFile(hostsPath, []byte(hostsContent), 0644); err != nil {
		t.Fatalf("failed to write hosts file: %v", err)
	}

	yp1 := NewYamlProvider()
	ctx := context.Background()
	cfg1 := ProviderConfig{
		Settings: map[string]string{
			"vault_path":  hostsPath,
			"entries_key": "hosts",
		},
	}
	if err := yp1.Initialize(ctx, cfg1); err != nil {
		t.Fatalf("failed to initialize hosts yaml: %v", err)
	}

	entry1, err := yp1.GetEntry(ctx, "ssh_host")
	if err != nil {
		t.Fatalf("failed to get entry from custom hosts key: %v", err)
	}
	if entry1.Title != "Host SSH" || entry1.Attributes["hostname"] != "custom.host" {
		t.Errorf("unexpected custom hosts entry: %+v", entry1)
	}

	// 2. Root key: "."
	rootContent := `
ssh_root:
  tags: [auth:root]
  title: "Root SSH"
  hostname: "root.host"
`
	rootPath := filepath.Join(tempDir, "root.yaml")
	if err := os.WriteFile(rootPath, []byte(rootContent), 0644); err != nil {
		t.Fatalf("failed to write root file: %v", err)
	}

	yp2 := NewYamlProvider()
	cfg2 := ProviderConfig{
		Settings: map[string]string{
			"vault_path":  rootPath,
			"entries_key": ".",
		},
	}
	if err := yp2.Initialize(ctx, cfg2); err != nil {
		t.Fatalf("failed to initialize root yaml: %v", err)
	}

	entry2, err := yp2.GetEntry(ctx, "ssh_root")
	if err != nil {
		t.Fatalf("failed to get entry from root mapping: %v", err)
	}
	if entry2.Title != "Root SSH" || entry2.Attributes["hostname"] != "root.host" {
		t.Errorf("unexpected root entry: %+v", entry2)
	}

	// 3. Missing key: should gracefully initialize with 0 entries (excluding it from search results)
	yp3 := NewYamlProvider()
	cfg3 := ProviderConfig{
		Settings: map[string]string{
			"vault_path":  rootPath,
			"entries_key": "hosts",
		},
	}
	if err := yp3.Initialize(ctx, cfg3); err != nil {
		t.Fatalf("failed to initialize yaml with missing key: %v", err)
	}

	results, err := yp3.Search(ctx, SearchQuery{})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 entries from missing key database, got %d", len(results))
	}
}

func TestYamlProviderSingleEntity(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "cloakenv-yaml-single")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	yamlContent := `
title: "My Single YAML Vault"
tags: [env:local, dev]
secret1: "value1"
secret2:
  nested_key: "nested_value"
list:
  - item1
  - item2
`
	yamlPath := filepath.Join(tempDir, "single.yaml")
	if err := os.WriteFile(yamlPath, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("failed to write temp yaml file: %v", err)
	}

	yp := NewYamlProvider()
	ctx := context.Background()
	isTrue := true
	cfg := ProviderConfig{
		Settings: map[string]string{
			"vault_path": yamlPath,
		},
		SingleEntity:    &isTrue,
		EntitiesRootKey: ".",
	}

	if err := yp.Initialize(ctx, cfg); err != nil {
		t.Fatalf("failed to initialize yaml: %v", err)
	}

	// 1. GetSecret
	val, err := yp.GetSecret(ctx, "secret1")
	if err != nil {
		t.Fatalf("GetSecret failed: %v", err)
	}
	if val != "value1" {
		t.Errorf("expected 'value1', got %q", val)
	}

	// Serialization of nested map
	val, err = yp.GetSecret(ctx, "secret2")
	if err != nil {
		t.Fatalf("GetSecret failed: %v", err)
	}
	expectedMapYaml := "nested_key: nested_value"
	if val != expectedMapYaml {
		t.Errorf("expected serialized map %q, got %q", expectedMapYaml, val)
	}

	// Serialization of list
	val, err = yp.GetSecret(ctx, "list")
	if err != nil {
		t.Fatalf("GetSecret failed: %v", err)
	}
	expectedListYaml := "- item1\n- item2"
	if val != expectedListYaml {
		t.Errorf("expected serialized list %q, got %q", expectedListYaml, val)
	}

	// 2. GetEntry
	entry, err := yp.GetEntry(ctx, "")
	if err != nil {
		t.Fatalf("GetEntry failed: %v", err)
	}
	if entry.Title != "My Single YAML Vault" {
		t.Errorf("expected title 'My Single YAML Vault', got %q", entry.Title)
	}
	if len(entry.Tags) != 2 || entry.Tags[0] != "env:local" || entry.Tags[1] != "dev" {
		t.Errorf("unexpected tags: %v", entry.Tags)
	}

	// 3. Search
	results, err := yp.Search(ctx, SearchQuery{})
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

func TestYamlProvider_InitializeErrors(t *testing.T) {
	tempDir := t.TempDir()

	invalidYamlPath := filepath.Join(tempDir, "invalid.yaml")
	if err := os.WriteFile(invalidYamlPath, []byte("invalid:\n  yaml: [content"), 0644); err != nil {
		t.Fatalf("failed to write invalid yaml file: %v", err)
	}

	tests := []struct {
		name    string
		cfg     ProviderConfig
		wantErr bool
	}{
		{
			name: "Missing vault_path",
			cfg: ProviderConfig{
				Settings: map[string]string{},
			},
			wantErr: true,
		},
		{
			name: "Invalid YAML syntax",
			cfg: ProviderConfig{
				Settings: map[string]string{
					"vault_path": invalidYamlPath,
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			yp := NewYamlProvider()
			err := yp.Initialize(context.Background(), tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("Initialize() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestConvertToEntriesMap(t *testing.T) {
	tests := []struct {
		name    string
		input   any
		want    map[string]map[string]any
		wantErr bool
	}{
		{
			name: "Valid map[string]map[string]any",
			input: map[string]map[string]any{
				"entry1": {
					"key1": "value1",
				},
			},
			want: map[string]map[string]any{
				"entry1": {
					"key1": "value1",
				},
			},
			wantErr: false,
		},
		{
			name: "Valid map[string]any with map[string]any values",
			input: map[string]any{
				"entry1": map[string]any{
					"key1": "value1",
				},
			},
			want: map[string]map[string]any{
				"entry1": {
					"key1": "value1",
				},
			},
			wantErr: false,
		},
		{
			name: "Valid map[string]any with map[any]any values",
			input: map[string]any{
				"entry1": map[any]any{
					"key1": "value1",
					123:    "value2",
				},
			},
			want: map[string]map[string]any{
				"entry1": {
					"key1": "value1",
					"123":  "value2",
				},
			},
			wantErr: false,
		},
		{
			name: "Invalid map[string]any with string value",
			input: map[string]any{
				"entry1": "string_value_instead_of_map",
			},
			wantErr: true,
		},
		{
			name: "Valid map[any]any with map[string]any values",
			input: map[any]any{
				"entry1": map[string]any{
					"key1": "value1",
				},
				456: map[string]any{
					"key2": "value2",
				},
			},
			want: map[string]map[string]any{
				"entry1": {
					"key1": "value1",
				},
				"456": {
					"key2": "value2",
				},
			},
			wantErr: false,
		},
		{
			name: "Valid map[any]any with map[any]any values",
			input: map[any]any{
				"entry1": map[any]any{
					"key1": "value1",
				},
			},
			want: map[string]map[string]any{
				"entry1": {
					"key1": "value1",
				},
			},
			wantErr: false,
		},
		{
			name: "Invalid map[any]any with string value",
			input: map[any]any{
				"entry1": "string_value_instead_of_map",
			},
			wantErr: true,
		},
		{
			name:    "Invalid input type string",
			input:   "some_string",
			wantErr: true,
		},
		{
			name:    "Invalid input type slice",
			input:   []any{"some", "slice"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := convertToEntriesMap(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("convertToEntriesMap() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && !reflect.DeepEqual(got, tt.want) {
				t.Errorf("convertToEntriesMap() = %v, want %v", got, tt.want)
			}
		})
	}
}
