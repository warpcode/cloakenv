package provider

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestJsonProvider(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "cloakenv-json-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

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
			"database_path": jsonPath,
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
	tempDir, err := os.MkdirTemp("", "cloakenv-json-test-custom")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

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
			"database_path": hostsPath,
			"entries_key":   "hosts",
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
			"database_path": rootPath,
			"entries_key":   ".",
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
			"database_path": rootPath,
			"entries_key":   "hosts",
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
