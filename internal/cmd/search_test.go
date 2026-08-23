package cmd

import (
	"bytes"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/warpcode/cloakenv/internal/config"
	"github.com/warpcode/cloakenv/internal/provider"
)

func TestParseSearchArgs(t *testing.T) {
	tests := []struct {
		name             string
		args             []string
		wantQuery        string
		wantRepoScopes   []string
		wantSelectedKeys []string
		wantOutputFormat string
		wantErrMsg       string
	}{
		{
			name:             "default values",
			args:             []string{},
			wantQuery:        "",
			wantRepoScopes:   nil,
			wantSelectedKeys: nil,
			wantOutputFormat: "yaml",
			wantErrMsg:       "",
		},
		{
			name:             "just query",
			args:             []string{"myquery"},
			wantQuery:        "myquery",
			wantRepoScopes:   nil,
			wantSelectedKeys: nil,
			wantOutputFormat: "yaml",
			wantErrMsg:       "",
		},
		{
			name:             "output format json with -o",
			args:             []string{"-o", "json"},
			wantQuery:        "",
			wantRepoScopes:   nil,
			wantSelectedKeys: nil,
			wantOutputFormat: "json",
			wantErrMsg:       "",
		},
		{
			name:             "output format yaml with --output",
			args:             []string{"--output", "yaml"},
			wantQuery:        "",
			wantRepoScopes:   nil,
			wantSelectedKeys: nil,
			wantOutputFormat: "yaml",
			wantErrMsg:       "",
		},
		{
			name:             "single vault",
			args:             []string{"--vault", "vault1"},
			wantQuery:        "",
			wantRepoScopes:   []string{"vault1"},
			wantSelectedKeys: nil,
			wantOutputFormat: "yaml",
			wantErrMsg:       "",
		},
		{
			name:             "multiple vaults",
			args:             []string{"--vault", "vault1", "--vault", "vault2"},
			wantQuery:        "",
			wantRepoScopes:   []string{"vault1", "vault2"},
			wantSelectedKeys: nil,
			wantOutputFormat: "yaml",
			wantErrMsg:       "",
		},
		{
			name:             "single selected key",
			args:             []string{"-i", "key1"},
			wantQuery:        "",
			wantRepoScopes:   nil,
			wantSelectedKeys: []string{"key1"},
			wantOutputFormat: "yaml",
			wantErrMsg:       "",
		},
		{
			name:             "multiple selected keys",
			args:             []string{"-i", "key1", "-i", "key2"},
			wantQuery:        "",
			wantRepoScopes:   nil,
			wantSelectedKeys: []string{"key1", "key2"},
			wantOutputFormat: "yaml",
			wantErrMsg:       "",
		},
		{
			name:             "all flags and query",
			args:             []string{"myquery", "--vault", "vault1", "-i", "key1", "-o", "json"},
			wantQuery:        "myquery",
			wantRepoScopes:   []string{"vault1"},
			wantSelectedKeys: []string{"key1"},
			wantOutputFormat: "json",
			wantErrMsg:       "",
		},
		{
			name:             "missing output format argument",
			args:             []string{"-o"},
			wantQuery:        "",
			wantRepoScopes:   nil,
			wantSelectedKeys: nil,
			wantOutputFormat: "",
			wantErrMsg:       "requires an argument",
		},
		{
			name:             "missing vault argument",
			args:             []string{"--vault"},
			wantQuery:        "",
			wantRepoScopes:   nil,
			wantSelectedKeys: nil,
			wantOutputFormat: "",
			wantErrMsg:       "requires an argument",
		},
		{
			name:             "missing selected key argument",
			args:             []string{"-i"},
			wantQuery:        "",
			wantRepoScopes:   nil,
			wantSelectedKeys: nil,
			wantOutputFormat: "",
			wantErrMsg:       "requires an argument",
		},
		{
			name:             "invalid output format",
			args:             []string{"-o", "xml"},
			wantQuery:        "",
			wantRepoScopes:   nil,
			wantSelectedKeys: nil,
			wantOutputFormat: "",
			wantErrMsg:       "invalid output format",
		},
		{
			name:             "unknown flag",
			args:             []string{"-x"},
			wantQuery:        "",
			wantRepoScopes:   nil,
			wantSelectedKeys: nil,
			wantOutputFormat: "",
			wantErrMsg:       "unknown flag: -x",
		},
		{
			name:             "too many positional arguments",
			args:             []string{"query1", "query2"},
			wantQuery:        "",
			wantRepoScopes:   nil,
			wantSelectedKeys: nil,
			wantOutputFormat: "",
			wantErrMsg:       "usage: cloakenv search",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			query, repoScopes, selectedKeys, outputFormat, err := parseSearchArgs(tc.args)

			if tc.wantErrMsg != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tc.wantErrMsg)
				}
				if !strings.Contains(err.Error(), tc.wantErrMsg) {
					t.Errorf("expected error containing %q, got %v", tc.wantErrMsg, err)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if query != tc.wantQuery {
				t.Errorf("query = %q, want %q", query, tc.wantQuery)
			}

			if !reflect.DeepEqual(repoScopes, tc.wantRepoScopes) {
				t.Errorf("repoScopes = %v, want %v", repoScopes, tc.wantRepoScopes)
			}

			if !reflect.DeepEqual(selectedKeys, tc.wantSelectedKeys) {
				t.Errorf("selectedKeys = %v, want %v", selectedKeys, tc.wantSelectedKeys)
			}

			if outputFormat != tc.wantOutputFormat {
				t.Errorf("outputFormat = %q, want %q", outputFormat, tc.wantOutputFormat)
			}
		})
	}
}

func TestFlattenSearchResults(t *testing.T) {
	entry := provider.SearchResult{
		Provider: "keepass",
		Vault:    "myvault",
		Path:     "path/to/entry",
		Entry: provider.Entry{
			Title: "My Title",
			Tags:  []string{"tag1", "tag2"},
			Attributes: map[string]any{
				"password": "secret_password",
				"USERNAME": "admin",
				"Title":    "conflict_title", // Should be ignored in default
				"TAGS":     "conflict_tags",
				"some-key": "value",
			},
		},
	}

	tests := []struct {
		name         string
		results      []provider.SearchResult
		selectedKeys []string
		want         []map[string]any
	}{
		{
			name:         "empty results",
			results:      []provider.SearchResult{},
			selectedKeys: nil,
			want:         []map[string]any{},
		},
		{
			name:         "default flattening (no selected keys)",
			results:      []provider.SearchResult{entry},
			selectedKeys: nil,
			want: []map[string]any{
				{
					"provider": "keepass",
					"vault":    "myvault",
					"path":     "path/to/entry",
					"title":    "My Title",
					"tags":     []string{"tag1", "tag2"},
					"PASSWORD": "secret_password",
					"USERNAME": "admin",
					"SOME_KEY": "value",
				},
			},
		},
		{
			name:         "specific metadata keys",
			results:      []provider.SearchResult{entry},
			selectedKeys: []string{"provider", "TITLE", "vault", "path", "tags"},
			want: []map[string]any{
				{
					"provider": "keepass",
					"title":    "My Title",
					"vault":    "myvault",
					"path":     "path/to/entry",
					"tags":     []string{"tag1", "tag2"},
				},
			},
		},
		{
			name:         "specific attributes case insensitive",
			results:      []provider.SearchResult{entry},
			selectedKeys: []string{"password", "username", "some-key"},
			want: []map[string]any{
				{
					"PASSWORD": "secret_password",
					"USERNAME": "admin",
					"SOME_KEY": "value",
				},
			},
		},
		{
			name:         "missing attributes",
			results:      []provider.SearchResult{entry},
			selectedKeys: []string{"nonexistent"},
			want: []map[string]any{
				{
					"NONEXISTENT": nil,
				},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := flattenSearchResults(tc.results, tc.selectedKeys)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("flattenSearchResults() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSearch(t *testing.T) {
	t.Run("HelpFlag", func(t *testing.T) {
		oldStdout := os.Stdout
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatalf("failed to create pipe: %v", err)
		}
		defer func() { os.Stdout = oldStdout }()
		defer func() { _ = w.Close() }()
		os.Stdout = w

		args := []string{"--help"}
		cfg := &config.Config{}
		exitCode := Search(args, cfg)

		if exitCode != 0 {
			t.Errorf("expected exit code 0, got %d", exitCode)
		}

		var buf bytes.Buffer
		_ = w.Close()
		if _, err := io.Copy(&buf, r); err != nil {
			t.Fatalf("failed to copy: %v", err)
		}
		output := buf.String()

		if !strings.Contains(output, "Usage:") || !strings.Contains(output, "cloakenv search") {
			t.Errorf("expected help output, got %s", output)
		}
	})

	t.Run("InvalidArgs", func(t *testing.T) {
		oldStderr := os.Stderr
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatalf("failed to create pipe: %v", err)
		}
		defer func() { os.Stderr = oldStderr }()
		defer func() { _ = w.Close() }()
		os.Stderr = w

		args := []string{"-o"}
		cfg := &config.Config{}
		exitCode := Search(args, cfg)

		if exitCode != 1 {
			t.Errorf("expected exit code 1, got %d", exitCode)
		}

		var buf bytes.Buffer
		_ = w.Close()
		if _, err := io.Copy(&buf, r); err != nil {
			t.Fatalf("failed to copy: %v", err)
		}
		output := buf.String()

		if !strings.Contains(output, "requires an argument") {
			t.Errorf("expected output to contain 'requires an argument', got %s", output)
		}
	})

	t.Run("InvalidConfig", func(t *testing.T) {
		oldStderr := os.Stderr
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatalf("failed to create pipe: %v", err)
		}
		defer func() { os.Stderr = oldStderr }()
		defer func() { _ = w.Close() }()
		os.Stderr = w

		args := []string{}
		cfg := &config.Config{
			Vaults: map[string]config.VaultConfig{
				"my_yaml": {
					Provider: "invalid_provider",
				},
			},
		}
		exitCode := Search(args, cfg)

		if exitCode != 1 {
			t.Errorf("expected exit code 1, got %d", exitCode)
		}

		var buf bytes.Buffer
		_ = w.Close()
		if _, err := io.Copy(&buf, r); err != nil {
			t.Fatalf("failed to copy: %v", err)
		}
		output := buf.String()

		if !strings.Contains(output, "Config error") {
			t.Errorf("expected output to contain 'Config error', got %s", output)
		}
	})

	t.Run("QueryError", func(t *testing.T) {
		oldStderr := os.Stderr
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatalf("failed to create pipe: %v", err)
		}
		defer func() { os.Stderr = oldStderr }()
		defer func() { _ = w.Close() }()
		os.Stderr = w

		args := []string{"invalid syntax === "}
		cfg := &config.Config{
			Vaults: map[string]config.VaultConfig{
				"my_yaml": {
					Provider:  "yaml",
					VaultPath: "../../testdata/test_entries.yaml",
				},
			},
		}
		exitCode := Search(args, cfg)

		if exitCode != 1 {
			t.Errorf("expected exit code 1, got %d", exitCode)
		}

		var buf bytes.Buffer
		_ = w.Close()
		if _, err := io.Copy(&buf, r); err != nil {
			t.Fatalf("failed to copy: %v", err)
		}
		output := buf.String()

		if !strings.Contains(output, "Search failed") {
			t.Errorf("expected output to contain 'Search failed', got %s", output)
		}
	})

	t.Run("SuccessfulQueryJSON", func(t *testing.T) {
		oldStdout := os.Stdout
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatalf("failed to create pipe: %v", err)
		}
		defer func() { os.Stdout = oldStdout }()
		defer func() { _ = w.Close() }()
		os.Stdout = w

		args := []string{"title == 'Production SSH Key'", "-o", "json"}
		cfg := &config.Config{
			Vaults: map[string]config.VaultConfig{
				"my_yaml": {
					Provider:  "yaml",
					VaultPath: "../../testdata/test_entries.yaml",
				},
			},
		}
		exitCode := Search(args, cfg)

		if exitCode != 0 {
			t.Errorf("expected exit code 0, got %d", exitCode)
		}

		var buf bytes.Buffer
		_ = w.Close()
		if _, err := io.Copy(&buf, r); err != nil {
			t.Fatalf("failed to copy: %v", err)
		}
		output := buf.String()

		if !strings.Contains(output, "Production SSH Key") {
			t.Errorf("expected output to contain 'Production SSH Key', got %s", output)
		}
	})

	t.Run("SelectedKeys", func(t *testing.T) {
		oldStdout := os.Stdout
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatalf("failed to create pipe: %v", err)
		}
		defer func() { os.Stdout = oldStdout }()
		defer func() { _ = w.Close() }()
		os.Stdout = w

		args := []string{"-i", "title", "-i", "USERNAME", "-o", "json"}
		cfg := &config.Config{
			Vaults: map[string]config.VaultConfig{
				"my_yaml": {
					Provider:  "yaml",
					VaultPath: "../../testdata/test_entries.yaml",
				},
			},
		}
		exitCode := Search(args, cfg)

		if exitCode != 0 {
			t.Errorf("expected exit code 0, got %d", exitCode)
		}

		var buf bytes.Buffer
		_ = w.Close()
		if _, err := io.Copy(&buf, r); err != nil {
			t.Fatalf("failed to copy: %v", err)
		}
		output := buf.String()

		if !strings.Contains(output, "title") || !strings.Contains(output, "USERNAME") {
			t.Errorf("expected output to contain selected keys 'title' and 'USERNAME', got %s", output)
		}
		if strings.Contains(output, "BIT_STRENGTH") {
			t.Errorf("did not expect output to contain 'BIT_STRENGTH', got %s", output)
		}
	})

	t.Run("YAMLOutput", func(t *testing.T) {
		oldStdout := os.Stdout
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatalf("failed to create pipe: %v", err)
		}
		defer func() { os.Stdout = oldStdout }()
		defer func() { _ = w.Close() }()
		os.Stdout = w

		args := []string{"title == 'Production SSH Key'", "-o", "yaml"}
		cfg := &config.Config{
			Vaults: map[string]config.VaultConfig{
				"my_yaml": {
					Provider:  "yaml",
					VaultPath: "../../testdata/test_entries.yaml",
				},
			},
		}
		exitCode := Search(args, cfg)

		if exitCode != 0 {
			t.Errorf("expected exit code 0, got %d", exitCode)
		}

		var buf bytes.Buffer
		_ = w.Close()
		if _, err := io.Copy(&buf, r); err != nil {
			t.Fatalf("failed to copy: %v", err)
		}
		output := buf.String()

		if !strings.Contains(output, "title: Production SSH Key") {
			t.Errorf("expected yaml output to contain 'title: Production SSH Key', got %s", output)
		}
	})
}
