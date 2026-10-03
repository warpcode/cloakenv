package provider

import (
	"context"
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
		_ = rule1.Compile()

		attrs := map[string]any{
			"env:OPENROUTER_API_KEY": "sk-12345",
			"env:test:foo":           "bar",
			"OTHER_VAR":              "baz",
		}

		got := ApplyMappingAndFilteringToAttributes(attrs, []config.MappingRule{rule1}, nil, nil)

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

	t.Run("mapped fields exempt from include_fields and exclude_fields", func(t *testing.T) {
		rule := config.MappingRule{Match: "env:(.*)", Key: "$1"}
		_ = rule.Compile()

		attrs := map[string]any{
			"env:OPENROUTER_API_KEY": "sk-123",
			"env:FOO":                "bar",
			"KEEP_UNMAPPED":          "keep_val",
			"REMOVE_UNMAPPED":        "remove_val",
		}

		// include_fields only lists "KEEP_UNMAPPED", exclude_fields lists "FOO"
		includeFields := []string{"KEEP_UNMAPPED"}
		excludeFields := []string{"FOO"}

		got := ApplyMappingAndFilteringToAttributes(attrs, []config.MappingRule{rule}, includeFields, excludeFields)

		// OPENROUTER_API_KEY (mapped) -> exempt from include_fields -> KEPT!
		if got["OPENROUTER_API_KEY"] != "sk-123" {
			t.Errorf("expected mapped field OPENROUTER_API_KEY to be kept, got %v", got["OPENROUTER_API_KEY"])
		}
		// FOO (mapped) -> exempt from exclude_fields -> KEPT!
		if got["FOO"] != "bar" {
			t.Errorf("expected mapped field FOO to be kept despite exclude_fields, got %v", got["FOO"])
		}
		// KEEP_UNMAPPED (unmapped) -> in include_fields -> KEPT!
		if got["KEEP_UNMAPPED"] != "keep_val" {
			t.Errorf("expected KEEP_UNMAPPED to be kept, got %v", got["KEEP_UNMAPPED"])
		}
		// REMOVE_UNMAPPED (unmapped) -> not in include_fields -> REMOVED!
		if _, ok := got["REMOVE_UNMAPPED"]; ok {
			t.Error("expected REMOVE_UNMAPPED to be removed by include_fields filtering")
		}
	})

	t.Run("multiple mapping rules evaluation order", func(t *testing.T) {
		rule1 := config.MappingRule{Match: "env:secret:(.*)", Key: "secret_$1"}
		_ = rule1.Compile()
		rule2 := config.MappingRule{Match: "env:(.*)", Key: "$1"}
		_ = rule2.Compile()

		attrs := map[string]any{
			"env:secret:TOKEN": "token_val",
			"env:API_KEY":      "key_val",
		}

		got := ApplyMappingAndFilteringToAttributes(attrs, []config.MappingRule{rule1, rule2}, nil, nil)

		if got["secret_TOKEN"] != "token_val" {
			t.Errorf("expected rule1 to match env:secret:TOKEN -> secret_TOKEN, got %v", got["secret_TOKEN"])
		}
		if got["API_KEY"] != "key_val" {
			t.Errorf("expected rule2 to match env:API_KEY -> API_KEY, got %v", got["API_KEY"])
		}
	})

	t.Run("nested maps and slices mapping", func(t *testing.T) {
		rule := config.MappingRule{Match: "raw_(.*)", Key: "$1"}
		_ = rule.Compile()

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

		got := ApplyMappingAndFilteringToAttributes(attrs, []config.MappingRule{rule}, nil, nil)

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
			},
		},
	})

	rule := config.MappingRule{Match: "env:(.*)", Key: "\\1"}
	_ = rule.Compile()

	mp := NewMappingProvider(cp, []config.MappingRule{rule}, []string{"OPENROUTER_API_KEY", "test:foo"}, nil)

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
}

func TestMappingProvider_InterfaceCapabilities(t *testing.T) {
	cp := NewCustomVaultProvider()
	rule := config.MappingRule{Match: "(.*)", Key: "$1"}
	_ = rule.Compile()

	p := NewMappingProvider(cp, []config.MappingRule{rule}, nil, nil)

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
