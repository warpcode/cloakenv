package provider

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/warpcode/cloakenv/internal/config"
)

type badMarshaler struct{}

func (badMarshaler) MarshalYAML() (any, error) {
	return nil, errors.New("forced serialization error")
}

func keysOf(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

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
			t.Fatalf("expected 3 attributes, got %d (keys: %v)", len(got), keysOf(got))
		}
		if _, ok := got["OPENROUTER_API_KEY"]; !ok {
			t.Errorf("expected OPENROUTER_API_KEY key to be present (keys: %v)", keysOf(got))
		}
		if _, ok := got["test:foo"]; !ok {
			t.Errorf("expected test:foo key to be present (keys: %v)", keysOf(got))
		}
		if _, ok := got["OTHER_VAR"]; !ok {
			t.Errorf("expected OTHER_VAR key to be present (keys: %v)", keysOf(got))
		}
		if _, ok := got["env:OPENROUTER_API_KEY"]; ok {
			t.Error("expected original key 'env:OPENROUTER_API_KEY' to be removed after mapping")
		}
	})

	t.Run("unanchored regex preserves unmatched text span", func(t *testing.T) {
		rule := config.MappingRule{Match: "env:(.*)", Key: "$1"}
		if err := rule.Compile(); err != nil {
			t.Fatalf("rule.Compile failed: %v", err)
		}

		attrs := map[string]any{
			"xenv:A": "val_A",
			"yenv:A": "val_B",
		}

		got, err := ApplyMappingAndFilteringToAttributes(attrs, []config.MappingRule{rule}, nil, nil, "", nil, "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if _, ok := got["xA"]; !ok {
			t.Errorf("expected mapped key 'xA' to be present (keys: %v)", keysOf(got))
		}
		if _, ok := got["yA"]; !ok {
			t.Errorf("expected mapped key 'yA' to be present (keys: %v)", keysOf(got))
		}
		if len(got) != 2 {
			t.Errorf("expected 2 distinct keys without key collapse, got %d (keys: %v)", len(got), keysOf(got))
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

		includeFields := []string{"KEEP_UNMAPPED", "OPENROUTER_API_KEY"}
		excludeFields := []string{"REMOVE_UNMAPPED"}

		got, err := ApplyMappingAndFilteringToAttributes(attrs, []config.MappingRule{rule}, includeFields, excludeFields, "", nil, "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if _, ok := got["OPENROUTER_API_KEY"]; !ok {
			t.Errorf("expected mapped field OPENROUTER_API_KEY to be kept (keys: %v)", keysOf(got))
		}
		if _, ok := got["FOO"]; ok {
			t.Errorf("expected mapped field FOO to be filtered by include_fields (keys: %v)", keysOf(got))
		}
		if _, ok := got["KEEP_UNMAPPED"]; !ok {
			t.Errorf("expected KEEP_UNMAPPED to be kept (keys: %v)", keysOf(got))
		}
		if _, ok := got["REMOVE_UNMAPPED"]; ok {
			t.Error("expected REMOVE_UNMAPPED to be removed by exclude_fields filtering")
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

		if _, ok := got["secret_TOKEN"]; !ok {
			t.Errorf("expected rule1 to match env:secret:TOKEN -> secret_TOKEN (keys: %v)", keysOf(got))
		}
		if _, ok := got["API_KEY"]; !ok {
			t.Errorf("expected rule2 to match env:API_KEY -> API_KEY (keys: %v)", keysOf(got))
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

		if _, ok := got["top"]; !ok {
			t.Errorf("expected top key to be present (keys: %v)", keysOf(got))
		}
		if nestedMap, ok := got["nested"].(map[string]any); !ok {
			t.Fatalf("expected nested map (keys: %v)", keysOf(got))
		} else if _, ok := nestedMap["inner"]; !ok {
			t.Errorf("expected nested.inner key to be present (keys: %v)", keysOf(nestedMap))
		}
		if listSlice, ok := got["list"].([]any); !ok || len(listSlice) != 1 {
			t.Fatalf("expected list slice of len 1")
		} else if elemMap, ok := listSlice[0].(map[string]any); !ok {
			t.Fatalf("expected list[0] map")
		} else if _, ok := elemMap["elem"]; !ok {
			t.Errorf("expected list[0].elem key to be present (keys: %v)", keysOf(elemMap))
		}
	})

	t.Run("mapped and unmapped key collision determinism", func(t *testing.T) {
		rule := config.MappingRule{Match: "env:(.*)", Key: "$1"}
		if err := rule.Compile(); err != nil {
			t.Fatalf("rule.Compile failed: %v", err)
		}

		runCollisionTest := func(name string, include, exclude []string) {
			t.Run(name, func(t *testing.T) {
				for i := range 20 {
					attrs := map[string]any{
						"FOO":     "unmapped_value",
						"env:FOO": "mapped_value",
					}

					got, err := ApplyMappingAndFilteringToAttributes(attrs, []config.MappingRule{rule}, include, exclude, "", nil, "")
					if err != nil {
						t.Fatalf("unexpected error: %v", err)
					}

					if _, ok := got["FOO"]; ok {
						t.Fatalf("iter %d: expected colliding keys 'FOO' to be dropped (keys: %v)", i, keysOf(got))
					}
				}
			})
		}

		runCollisionTest("no filters", nil, nil)
		runCollisionTest("exclude_fields", nil, []string{"FOO"})
		runCollisionTest("include_fields", []string{"FOO"}, nil)

		t.Run("two mapped keys to single target - alphabetically first wins", func(t *testing.T) {
			rule1 := config.MappingRule{Match: "a_env:(.*)", Key: "$1"}
			_ = rule1.Compile()
			rule2 := config.MappingRule{Match: "b_env:(.*)", Key: "$1"}
			_ = rule2.Compile()

			for i := range 20 {
				attrs := map[string]any{
					"b_env:FOO": "second_val",
					"a_env:FOO": "first_val",
				}

				got, err := ApplyMappingAndFilteringToAttributes(attrs, []config.MappingRule{rule1, rule2}, nil, nil, "", nil, "")
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}

				if got["FOO"] != "first_val" {
					t.Fatalf("iter %d: expected FOO='first_val', got %v (keys: %v)", i, got["FOO"], keysOf(got))
				}
			}
		})
	})

	t.Run("container mapping does not exempt subtree from include_fields", func(t *testing.T) {
		rule := config.MappingRule{Match: "raw_(.*)", Key: "$1"}
		if err := rule.Compile(); err != nil {
			t.Fatalf("rule.Compile failed: %v", err)
		}

		attrs := map[string]any{
			"raw_outer": map[string]any{
				"raw_a": "V_A",
				"raw_b": "V_B",
			},
		}

		includeFields := []string{"outer.a"}
		got, err := ApplyMappingAndFilteringToAttributes(attrs, []config.MappingRule{rule}, includeFields, nil, "", nil, "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		outerMap, ok := got["outer"].(map[string]any)
		if !ok {
			t.Fatalf("expected outer map, got %v (keys: %v)", got["outer"], keysOf(got))
		}
		if _, ok := outerMap["a"]; !ok {
			t.Errorf("expected outer.a to be present (keys: %v)", keysOf(outerMap))
		}
		if _, ok := outerMap["b"]; ok {
			t.Errorf("expected outer.b to be filtered out by include_fields=['outer.a'] (keys: %v)", keysOf(outerMap))
		}
	})

	t.Run("exclude_fields applies to mapped keys", func(t *testing.T) {
		rule := config.MappingRule{Match: "env:(.*)", Key: "$1"}
		if err := rule.Compile(); err != nil {
			t.Fatalf("rule.Compile failed: %v", err)
		}

		attrs := map[string]any{
			"env:SECRET_B": "V_B",
			"PLAIN":        "V_P",
		}

		excludeFields := []string{"SECRET_B"}
		got, err := ApplyMappingAndFilteringToAttributes(attrs, []config.MappingRule{rule}, nil, excludeFields, "", nil, "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if _, ok := got["SECRET_B"]; ok {
			t.Error("expected mapped key SECRET_B to be excluded by exclude_fields")
		}
		if _, ok := got["PLAIN"]; !ok {
			t.Error("expected PLAIN to be kept")
		}
	})

	t.Run("post-mapping path exclusions work", func(t *testing.T) {
		rule := config.MappingRule{Match: "raw_(.*)", Key: "$1"}
		if err := rule.Compile(); err != nil {
			t.Fatalf("rule.Compile failed: %v", err)
		}

		attrs := map[string]any{
			"raw_outer": map[string]any{
				"a": "V_A",
				"b": "V_B",
			},
		}

		excludeFields := []string{"outer.b"}
		got, err := ApplyMappingAndFilteringToAttributes(attrs, []config.MappingRule{rule}, nil, excludeFields, "", nil, "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		outerMap, ok := got["outer"].(map[string]any)
		if !ok {
			t.Fatalf("expected outer map, got %v (keys: %v)", got["outer"], keysOf(got))
		}
		if _, ok := outerMap["a"]; !ok {
			t.Errorf("expected outer.a to be present (keys: %v)", keysOf(outerMap))
		}
		if _, ok := outerMap["b"]; ok {
			t.Errorf("expected outer.b to be excluded by exclude_fields=['outer.b'] (keys: %v)", keysOf(outerMap))
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
				"Password":               "default_app1_pass",
			},
		},
	})

	rule := config.MappingRule{Match: "env:(.*)", Key: "\\1"}
	if err := rule.Compile(); err != nil {
		t.Fatalf("rule.Compile failed: %v", err)
	}

	fp := NewFilteringProvider(cp, []string{"OPENROUTER_API_KEY", "test:foo", "Password"}, []string{"EXCLUDED_FIELD"})
	mp, err := NewMappingProvider(fp, []config.MappingRule{rule})
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

		if _, ok := entry.Attributes["OPENROUTER_API_KEY"]; !ok {
			t.Errorf("expected OPENROUTER_API_KEY key to be present (keys: %v)", keysOf(entry.Attributes))
		}
		if _, ok := entry.Attributes["test:foo"]; !ok {
			t.Errorf("expected test:foo key to be present (keys: %v)", keysOf(entry.Attributes))
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
		if _, ok := resEntry.Attributes["OPENROUTER_API_KEY"]; !ok {
			t.Errorf("expected OPENROUTER_API_KEY key to be present in search result (keys: %v)", keysOf(resEntry.Attributes))
		}
	})

	t.Run("GetSecret retrieves mapped attributes", func(t *testing.T) {
		val, err := mp.GetSecret(ctx, "app1:OPENROUTER_API_KEY")
		if err != nil {
			t.Fatalf("GetSecret failed: %v", err)
		}
		if val != "sk-or-12345" {
			t.Errorf("expected GetSecret app1:OPENROUTER_API_KEY to succeed")
		}

		valFoo, err := mp.GetSecret(ctx, "app1:test:foo")
		if err != nil {
			t.Fatalf("GetSecret failed: %v", err)
		}
		if valFoo != "bar" {
			t.Errorf("expected GetSecret app1:test:foo to succeed")
		}
	})

	t.Run("GetSecret bare location defaults to Password", func(t *testing.T) {
		val, err := mp.GetSecret(ctx, "app1")
		if err != nil {
			t.Fatalf("GetSecret bare location failed: %v", err)
		}
		if val != "default_app1_pass" {
			t.Errorf("expected GetSecret app1 to return default Password attribute")
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

func TestMappingProvider_ColonQualifiedExcludeFields(t *testing.T) {
	ctx := context.Background()

	cp := NewCustomVaultProvider()
	_ = cp.Initialize(ctx, ProviderConfig{
		Entities: map[string]map[string]any{
			"website/Test Website": {
				"Password": "secret_pass_123",
				"UserName": "test_user",
			},
		},
	})

	rule := config.MappingRule{Match: "env:(.*)", Key: "$1"}
	_ = rule.Compile()

	excludeFields := []string{"website/Test Website:*"}
	fp := NewFilteringProvider(cp, nil, excludeFields)
	mp, err := NewMappingProvider(fp, []config.MappingRule{rule})
	if err != nil {
		t.Fatalf("NewMappingProvider failed: %v", err)
	}

	_, err = mp.GetSecret(ctx, "website/Test Website:Password")
	if err == nil {
		t.Fatal("expected colon-qualified exclude_fields pattern 'website/Test Website:*' to block GetSecret, got nil error")
	}
}

func TestMappingProvider_MultiEntityYamlJsonDotPathResolution(t *testing.T) {
	ctx := context.Background()

	yp := NewYamlProvider()
	_ = yp.Initialize(ctx, ProviderConfig{
		Settings: map[string]string{
			"vault_path":  "../../testdata/test_hosts.yaml",
			"entries_key": "hosts",
		},
		EntitiesRootKey: "hosts",
	})

	rule := config.MappingRule{Match: "env:(.*)", Key: "$1"}
	_ = rule.Compile()

	mp, err := NewMappingProvider(yp, []config.MappingRule{rule})
	if err != nil {
		t.Fatalf("NewMappingProvider failed: %v", err)
	}

	val, err := mp.GetSecret(ctx, "hosts.ssh_host.hostname")
	if err != nil {
		t.Fatalf("multi-entity YAML/JSON dot-path GetSecret failed: %v", err)
	}
	if val == "" {
		t.Error("expected non-empty hostname")
	}

	// Whole-entity container resolution
	containerVal, err := mp.GetSecret(ctx, "hosts.ssh_host")
	if err != nil {
		t.Fatalf("whole-entity GetSecret failed: %v", err)
	}
	if containerVal == "" {
		t.Error("expected non-empty container value")
	}
}

func TestMappingProvider_SearchVaultMappedKeyLookupAndRawKeyRejection(t *testing.T) {
	ctx := context.Background()

	sp := NewSearchProvider()
	sp.SetSearchExecutor(func(ctx context.Context, query string, sourceVaults []string, depth int) ([]SearchResult, error) {
		return []SearchResult{
			{
				Path: "entry1",
				Entry: Entry{
					Title: "entry1",
					Attributes: map[string]any{
						"env:API_KEY": "pass1",
					},
				},
			},
		}, nil
	})
	_ = sp.Initialize(ctx, ProviderConfig{
		Settings: map[string]string{
			"query": "tag:test",
		},
		SourceVaults: []string{"source1"},
		Query:        "tag:test",
	})

	rule := config.MappingRule{Match: "env:(.*)", Key: "$1"}
	_ = rule.Compile()

	mp, err := NewMappingProvider(sp, []config.MappingRule{rule})
	if err != nil {
		t.Fatalf("NewMappingProvider failed: %v", err)
	}

	// 1. Mapped key lookup succeeds
	val, err := mp.GetSecret(ctx, "entry1:API_KEY")
	if err != nil {
		t.Fatalf("expected GetSecret for mapped key API_KEY to succeed, got %v", err)
	}
	if val == "" {
		t.Error("expected non-empty secret value")
	}

	// 2. Original raw key lookup fails
	_, err = mp.GetSecret(ctx, "entry1:env:API_KEY")
	if err == nil {
		t.Fatal("expected GetSecret for raw pre-mapping key 'env:API_KEY' to fail, got nil error")
	}

	// 3. Absent entry lookup fails
	_, err = mp.GetSecret(ctx, "absent_entry:API_KEY")
	if err == nil {
		t.Fatal("expected search URI with absent entry to return error, got nil")
	}
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
	fp := NewFilteringProvider(cp, nil, excludeFields)
	mp, err := NewMappingProvider(fp, []config.MappingRule{rule})
	if err != nil {
		t.Fatalf("NewMappingProvider failed: %v", err)
	}

	searchable := mp.(SearchableProvider)
	entry, err := searchable.GetEntry(ctx, "app1")
	if err != nil {
		t.Fatalf("GetEntry failed: %v", err)
	}

	if _, ok := entry.Attributes["API_KEY"]; !ok {
		t.Errorf("expected mapped API_KEY key to be present (keys: %v)", keysOf(entry.Attributes))
	}

	dbMap, ok := entry.Attributes["db"].(map[string]any)
	if !ok {
		t.Fatalf("expected db map (keys: %v)", keysOf(entry.Attributes))
	}
	if _, ok := dbMap["host"]; !ok {
		t.Errorf("expected db.host key to be present (keys: %v)", keysOf(dbMap))
	}
	if _, ok := dbMap["pass"]; ok {
		t.Error("expected db.pass to be excluded by path-aware filtering 'db.pass'")
	}

	tokenSlice, ok := entry.Attributes["tokens"].([]any)
	if !ok {
		t.Fatalf("expected tokens slice (keys: %v)", keysOf(entry.Attributes))
	}
	if len(tokenSlice) != 1 {
		t.Errorf("expected tokens slice of len 1, got len %d", len(tokenSlice))
	}
}

func TestMappingProvider_SerializationErrorPropagated(t *testing.T) {
	ctx := context.Background()

	cp := NewCustomVaultProvider()
	_ = cp.Initialize(ctx, ProviderConfig{
		Entities: map[string]map[string]any{
			"app1": {
				"bad": map[string]any{
					"invalid": badMarshaler{},
				},
			},
		},
	})

	rule := config.MappingRule{Match: "(.*)", Key: "$1"}
	_ = rule.Compile()

	mp, err := NewMappingProvider(cp, []config.MappingRule{rule})
	if err != nil {
		t.Fatalf("NewMappingProvider failed: %v", err)
	}

	_, err = mp.GetSecret(ctx, "app1:bad")
	if err == nil {
		t.Fatal("expected serialization error for un-serializable attribute, got nil")
	}
}

func TestMappingProvider_InterfaceCapabilities(t *testing.T) {
	cp := NewCustomVaultProvider()
	rule := config.MappingRule{Match: "(.*)", Key: "$1"}
	if err := rule.Compile(); err != nil {
		t.Fatalf("rule.Compile failed: %v", err)
	}

	p, err := NewMappingProvider(cp, []config.MappingRule{rule})
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

	_, err := NewMappingProvider(cp, []config.MappingRule{invalidRule})
	if err == nil {
		t.Fatal("expected NewMappingProvider to return error for invalid regex, got nil")
	}
}
