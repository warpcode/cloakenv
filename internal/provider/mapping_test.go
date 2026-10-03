package provider

import (
	"context"
	"strings"
	"testing"

	"github.com/warpcode/cloakenv/internal/config"
)

func TestConvertBackslashGroups(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"\\1", "$1"},
		{"$1", "$1"},
		{"${1}", "${1}"},
		{"env:\\1", "env:$1"},
		{"\\1_\\2", "$1_$2"},
		{"prefix_\\1_suffix", "prefix_$1_suffix"},
		{"\\0", "$0"},
		{"no_groups", "no_groups"},
	}

	for _, tt := range tests {
		got := convertBackslashGroups(tt.input)
		if got != tt.want {
			t.Errorf("convertBackslashGroups(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestApplyMappingAndFilteringToAttributes(t *testing.T) {
	t.Run("basic regex key mapping", func(t *testing.T) {
		rule1 := config.MappingRule{Match: "env:(.*)", Key: "\\1"}
		if err := rule1.Compile(); err != nil {
			t.Fatalf("rule1.Compile failed: %v", err)
		}

		attrs := map[string]any{
			"env:OPENROUTER_API_KEY": "sk-12345",
			"env:test:foo":           "bar",
			"OTHER_VAR":              "baz",
		}

		got, err := ApplyMappingAndFilteringToAttributes(attrs, []config.MappingRule{rule1}, nil, nil, "", nil, "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(got) != 3 {
			t.Fatalf("expected 3 attributes, got %d: %+v", len(got), got)
		}
		if got["OPENROUTER_API_KEY"] != "sk-12345" {
			t.Errorf("expected OPENROUTER_API_KEY='sk-12345', got %v", got["OPENROUTER_API_KEY"])
		}
		if got["test:foo"] != "bar" {
			t.Errorf("expected test:foo='bar', got %v", got["test:foo"])
		}
		if got["OTHER_VAR"] != "baz" {
			t.Errorf("expected OTHER_VAR='baz', got %v", got["OTHER_VAR"])
		}
		if _, ok := got["env:OPENROUTER_API_KEY"]; ok {
			t.Error("expected original key 'env:OPENROUTER_API_KEY' to be removed after mapping")
		}
	})

	t.Run("unanchored regex matches span only", func(t *testing.T) {
		rule := config.MappingRule{Match: "env:(.*)", Key: "$1"}
		if err := rule.Compile(); err != nil {
			t.Fatalf("rule.Compile failed: %v", err)
		}

		attrs := map[string]any{
			"xenv:A": "val_A",
		}

		got, err := ApplyMappingAndFilteringToAttributes(attrs, []config.MappingRule{rule}, nil, nil, "", nil, "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if got["A"] != "val_A" {
			t.Errorf("expected mapped key 'A'='val_A', got %v (full map: %+v)", got["A"], got)
		}
		if _, ok := got["xA"]; ok {
			t.Error("expected unanchored match not to preserve unmapped text prefix 'x'")
		}
	})

	t.Run("mapped fields exempt from include_fields and exclude_fields", func(t *testing.T) {
		rule := config.MappingRule{Match: "env:(.*)", Key: "$1"}
		if err := rule.Compile(); err != nil {
			t.Fatalf("rule.Compile failed: %v", err)
		}

		attrs := map[string]any{
			"env:OPENROUTER_API_KEY": "sk-123",
			"env:FOO":                "bar",
			"KEEP_UNMAPPED":          "keep_val",
			"REMOVE_UNMAPPED":        "remove_val",
		}

		includeFields := []string{"KEEP_UNMAPPED"}
		excludeFields := []string{"FOO"}

		got, err := ApplyMappingAndFilteringToAttributes(attrs, []config.MappingRule{rule}, includeFields, excludeFields, "", nil, "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if got["OPENROUTER_API_KEY"] != "sk-123" {
			t.Errorf("expected mapped field OPENROUTER_API_KEY to be kept, got %v", got["OPENROUTER_API_KEY"])
		}
		if got["FOO"] != "bar" {
			t.Errorf("expected mapped field FOO to be kept despite exclude_fields, got %v", got["FOO"])
		}
		if got["KEEP_UNMAPPED"] != "keep_val" {
			t.Errorf("expected KEEP_UNMAPPED to be kept, got %v", got["KEEP_UNMAPPED"])
		}
		if _, ok := got["REMOVE_UNMAPPED"]; ok {
			t.Error("expected REMOVE_UNMAPPED to be removed by include_fields filtering")
		}
	})

	t.Run("multiple mapping rules evaluation order", func(t *testing.T) {
		rule1 := config.MappingRule{Match: "env:secret:(.*)", Key: "secret_$1"}
		if err := rule1.Compile(); err != nil {
			t.Fatalf("rule1.Compile failed: %v", err)
		}
		rule2 := config.MappingRule{Match: "env:(.*)", Key: "$1"}
		if err := rule2.Compile(); err != nil {
			t.Fatalf("rule2.Compile failed: %v", err)
		}

		attrs := map[string]any{
			"env:secret:TOKEN": "token_val",
			"env:API_KEY":      "key_val",
		}

		got, err := ApplyMappingAndFilteringToAttributes(attrs, []config.MappingRule{rule1, rule2}, nil, nil, "", nil, "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if got["secret_TOKEN"] != "token_val" {
			t.Errorf("expected rule1 to match env:secret:TOKEN -> secret_TOKEN, got %v", got["secret_TOKEN"])
		}
		if got["API_KEY"] != "key_val" {
			t.Errorf("expected rule2 to match env:API_KEY -> API_KEY, got %v", got["API_KEY"])
		}
	})

	t.Run("nested maps and slices mapping", func(t *testing.T) {
		rule := config.MappingRule{Match: "raw_(.*)", Key: "$1"}
		if err := rule.Compile(); err != nil {
			t.Fatalf("rule.Compile failed: %v", err)
		}

		attrs := map[string]any{
			"raw_top": "top_val",
			"nested": map[string]any{
				"raw_inner": "inner_val",
			},
			"list": []any{
				map[string]any{
					"raw_elem": "elem_val",
				},
			},
		}

		got, err := ApplyMappingAndFilteringToAttributes(attrs, []config.MappingRule{rule}, nil, nil, "", nil, "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if got["top"] != "top_val" {
			t.Errorf("expected top='top_val', got %v", got["top"])
		}
		if nestedMap, ok := got["nested"].(map[string]any); !ok || nestedMap["inner"] != "inner_val" {
			t.Errorf("expected nested.inner='inner_val', got %v", got["nested"])
		}
		if listSlice, ok := got["list"].([]any); !ok || len(listSlice) != 1 {
			t.Fatalf("expected list slice of len 1, got %v", got["list"])
		} else if elemMap, ok := listSlice[0].(map[string]any); !ok || elemMap["elem"] != "elem_val" {
			t.Errorf("expected list[0].elem='elem_val', got %v", listSlice[0])
		}
	})

	t.Run("mapped and unmapped key collision determinism", func(t *testing.T) {
		rule := config.MappingRule{Match: "env:(.*)", Key: "$1"}
		if err := rule.Compile(); err != nil {
			t.Fatalf("rule.Compile failed: %v", err)
		}

		// "FOO" exists unmapped alongside "env:FOO" which maps to "FOO"
		attrs := map[string]any{
			"FOO":     "unmapped_value",
			"env:FOO": "mapped_value",
		}

		excludeFields := []string{"FOO"}

		got, err := ApplyMappingAndFilteringToAttributes(attrs, []config.MappingRule{rule}, nil, excludeFields, "", nil, "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// The mapped value MUST win deterministically and remain exempt from exclude_fields
		if got["FOO"] != "mapped_value" {
			t.Errorf("expected FOO='mapped_value', got %v", got["FOO"])
		}
	})

	t.Run("compile error propagation", func(t *testing.T) {
		invalidRule := config.MappingRule{Match: "[invalid_regex"}

		attrs := map[string]any{
			"key": "val",
		}

		_, err := ApplyMappingAndFilteringToAttributes(attrs, []config.MappingRule{invalidRule}, nil, nil, "", nil, "")
		if err == nil {
			t.Fatal("expected error for invalid mapping rule regex, got nil")
		}
		if !strings.Contains(err.Error(), "invalid mapping regex") && !strings.Contains(err.Error(), "mapping rule match") {
			t.Errorf("expected error message to explain invalid regex, got %v", err)
		}
	})
}

func TestMappingProvider_CustomVault(t *testing.T) {
	ctx := context.Background()

	cp := NewCustomVaultProvider()
	_ = cp.Initialize(ctx, ProviderConfig{
		Entities: map[string]map[string]any{
			"app1": {
				"env:OPENROUTER_API_KEY": "sk-or-12345",
				"env:test:foo":           "bar",
				"UNMAPPED_FIELD":         "unmapped_val",
				"EXCLUDED_FIELD":         "excluded_val",
			},
		},
	})

	rule := config.MappingRule{Match: "env:(.*)", Key: "\\1"}
	if err := rule.Compile(); err != nil {
		t.Fatalf("rule.Compile failed: %v", err)
	}

	mp, err := NewMappingProvider(cp, []config.MappingRule{rule}, []string{"OPENROUTER_API_KEY", "test:foo"}, []string{"EXCLUDED_FIELD"})
	if err != nil {
		t.Fatalf("NewMappingProvider failed: %v", err)
	}

	t.Run("GetEntry maps attributes", func(t *testing.T) {
		searchable, ok := mp.(SearchableProvider)
		if !ok {
			t.Fatal("expected MappingProvider to implement SearchableProvider")
		}

		entry, err := searchable.GetEntry(ctx, "app1")
		if err != nil {
			t.Fatalf("GetEntry failed: %v", err)
		}

		if entry.Attributes["OPENROUTER_API_KEY"] != "sk-or-12345" {
			t.Errorf("expected OPENROUTER_API_KEY='sk-or-12345', got %v", entry.Attributes["OPENROUTER_API_KEY"])
		}
		if entry.Attributes["test:foo"] != "bar" {
			t.Errorf("expected test:foo='bar', got %v", entry.Attributes["test:foo"])
		}
		if _, ok := entry.Attributes["UNMAPPED_FIELD"]; ok {
			t.Error("expected UNMAPPED_FIELD to be filtered out by include_fields")
		}
		if _, ok := entry.Attributes["EXCLUDED_FIELD"]; ok {
			t.Error("expected EXCLUDED_FIELD to be filtered out by exclude_fields")
		}
	})

	t.Run("Search maps entry attributes in results", func(t *testing.T) {
		searchable := mp.(SearchableProvider)
		results, err := searchable.Search(ctx, SearchQuery{})
		if err != nil {
			t.Fatalf("Search failed: %v", err)
		}
		if len(results) != 1 {
			t.Fatalf("expected 1 search result, got %d", len(results))
		}
		resEntry := results[0].Entry
		if resEntry.Attributes["OPENROUTER_API_KEY"] != "sk-or-12345" {
			t.Errorf("expected OPENROUTER_API_KEY='sk-or-12345', got %v", resEntry.Attributes["OPENROUTER_API_KEY"])
		}
	})

	t.Run("GetSecret retrieves mapped attributes", func(t *testing.T) {
		val, err := mp.GetSecret(ctx, "app1:OPENROUTER_API_KEY")
		if err != nil {
			t.Fatalf("GetSecret failed: %v", err)
		}
		if val != "sk-or-12345" {
			t.Errorf("expected GetSecret app1:OPENROUTER_API_KEY='sk-or-12345', got %q", val)
		}

		valFoo, err := mp.GetSecret(ctx, "app1:test:foo")
		if err != nil {
			t.Fatalf("GetSecret failed: %v", err)
		}
		if valFoo != "bar" {
			t.Errorf("expected GetSecret app1:test:foo='bar', got %q", valFoo)
		}
	})

	t.Run("GetSecret rejects excluded key", func(t *testing.T) {
		_, err := mp.GetSecret(ctx, "app1:EXCLUDED_FIELD")
		if err == nil {
			t.Fatal("expected GetSecret on excluded key to return error, got nil")
		}
	})

	t.Run("GetSecret rejects renamed original key", func(t *testing.T) {
		_, err := mp.GetSecret(ctx, "app1:env:OPENROUTER_API_KEY")
		if err == nil {
			t.Fatal("expected GetSecret on renamed original key 'env:OPENROUTER_API_KEY' to return error, got nil")
		}
	})
}

func TestMappingProvider_FilteringParityForNestedAndArrayPaths(t *testing.T) {
	ctx := context.Background()

	cp := NewCustomVaultProvider()
	_ = cp.Initialize(ctx, ProviderConfig{
		Entities: map[string]map[string]any{
			"app1": {
				"env:API_KEY": "sk-123",
				"db": map[string]any{
					"host": "localhost",
					"pass": "secret_pass",
				},
				"tokens": []any{"tok1", "tok2_secret"},
			},
		},
	})

	rule := config.MappingRule{Match: "env:(.*)", Key: "$1"}
	if err := rule.Compile(); err != nil {
		t.Fatalf("rule.Compile failed: %v", err)
	}

	excludeFields := []string{"db.pass", "tokens.1"}
	mp, err := NewMappingProvider(cp, []config.MappingRule{rule}, nil, excludeFields)
	if err != nil {
		t.Fatalf("NewMappingProvider failed: %v", err)
	}

	searchable := mp.(SearchableProvider)
	entry, err := searchable.GetEntry(ctx, "app1")
	if err != nil {
		t.Fatalf("GetEntry failed: %v", err)
	}

	if entry.Attributes["API_KEY"] != "sk-123" {
		t.Errorf("expected mapped API_KEY='sk-123', got %v", entry.Attributes["API_KEY"])
	}

	dbMap, ok := entry.Attributes["db"].(map[string]any)
	if !ok {
		t.Fatalf("expected db map, got %v", entry.Attributes["db"])
	}
	if dbMap["host"] != "localhost" {
		t.Errorf("expected db.host='localhost', got %v", dbMap["host"])
	}
	if _, ok := dbMap["pass"]; ok {
		t.Error("expected db.pass to be excluded by path-aware filtering 'db.pass'")
	}

	tokenSlice, ok := entry.Attributes["tokens"].([]any)
	if !ok {
		t.Fatalf("expected tokens slice, got %v", entry.Attributes["tokens"])
	}
	if len(tokenSlice) != 1 || tokenSlice[0] != "tok1" {
		t.Errorf("expected tokens slice = ['tok1'], got %v", tokenSlice)
	}
}

func TestMappingProvider_InterfaceCapabilities(t *testing.T) {
	cp := NewCustomVaultProvider()
	rule := config.MappingRule{Match: "(.*)", Key: "$1"}
	if err := rule.Compile(); err != nil {
		t.Fatalf("rule.Compile failed: %v", err)
	}

	p, err := NewMappingProvider(cp, []config.MappingRule{rule}, nil, nil)
	if err != nil {
		t.Fatalf("NewMappingProvider failed: %v", err)
	}

	if _, ok := p.(SearchableProvider); !ok {
		t.Error("expected MappingProvider around CustomVaultProvider to implement SearchableProvider")
	}
	if vr, ok := p.(ValueResolvableProvider); !ok || !vr.SupportsValueResolution() {
		t.Error("expected MappingProvider around CustomVaultProvider to implement ValueResolvableProvider")
	}

	if unwrap, ok := p.(interface{ Underlying() SecretProvider }); !ok || unwrap.Underlying() != cp {
		t.Error("expected Underlying() to return wrapped CustomVaultProvider")
	}
}

func TestMappingProvider_NewMappingProvider_CompileError(t *testing.T) {
	cp := NewCustomVaultProvider()
	invalidRule := config.MappingRule{Match: "[invalid_regex"}

	_, err := NewMappingProvider(cp, []config.MappingRule{invalidRule}, nil, nil)
	if err == nil {
		t.Fatal("expected NewMappingProvider to return error for invalid regex, got nil")
	}
}
