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
		got := ConvertBackslashGroups(tt.input)
		if got != tt.want {
			t.Errorf("ConvertBackslashGroups(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestApplyMappingAndFilteringToAttributes(t *testing.T) {

	t.Run("metadata interpolation in mapping rules", func(t *testing.T) {
		rule := config.MappingRule{Match: "env:(.*)", Key: "{{ upper .meta.title }}_$1"}
		if err := rule.Compile(); err != nil {
			t.Fatalf("rule.Compile failed: %v", err)
		}

		attrs := map[string]any{
			"env:API_KEY": "val",
		}

		rawEntry := Entry{Title: "my title"}

		got, err := ApplyMappingAndFilteringToAttributes(attrs, []config.MappingRule{rule}, nil, nil, "", nil, rawEntry, "test", "testvault")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if _, ok := got["MY TITLE_API_KEY"]; !ok {
			t.Errorf("expected mapped key MY TITLE_API_KEY, got keys: %v", keysOf(got))
		}
	})
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

		got, err := ApplyMappingAndFilteringToAttributes(attrs, []config.MappingRule{rule1}, nil, nil, "", nil, Entry{}, "", "")
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

		got, err := ApplyMappingAndFilteringToAttributes(attrs, []config.MappingRule{rule}, nil, nil, "", nil, Entry{}, "", "")
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
		excludeFields := []string{"REMOVE_UNMAPPED", "FOO"}

		got, err := ApplyMappingAndFilteringToAttributes(attrs, []config.MappingRule{rule}, includeFields, excludeFields, "", nil, Entry{}, "", "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if _, ok := got["OPENROUTER_API_KEY"]; !ok {
			t.Errorf("expected mapped field OPENROUTER_API_KEY to be kept (keys: %v)", keysOf(got))
		}
		if _, ok := got["FOO"]; !ok {
			t.Errorf("expected mapped field FOO to always show regardless of include/exclude fields (keys: %v)", keysOf(got))
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

		got, err := ApplyMappingAndFilteringToAttributes(attrs, []config.MappingRule{rule1, rule2}, nil, nil, "", nil, Entry{}, "", "")
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

		got, err := ApplyMappingAndFilteringToAttributes(attrs, []config.MappingRule{rule}, nil, nil, "", nil, Entry{}, "", "")
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

					got, err := ApplyMappingAndFilteringToAttributes(attrs, []config.MappingRule{rule}, include, exclude, "", nil, Entry{}, "", "")
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

				got, err := ApplyMappingAndFilteringToAttributes(attrs, []config.MappingRule{rule1, rule2}, nil, nil, "", nil, Entry{}, "", "")
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
				"a": "V_A",
				"b": "V_B",
			},
		}

		includeFields := []string{"outer.a"}
		got, err := ApplyMappingAndFilteringToAttributes(attrs, []config.MappingRule{rule}, includeFields, nil, "", nil, Entry{}, "", "")
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

	t.Run("mapped keys always show regardless of exclude_fields", func(t *testing.T) {
		rule := config.MappingRule{Match: "env:(.*)", Key: "$1"}
		if err := rule.Compile(); err != nil {
			t.Fatalf("rule.Compile failed: %v", err)
		}

		attrs := map[string]any{
			"env:SECRET_B": "V_B",
			"PLAIN":        "V_P",
		}

		excludeFields := []string{"SECRET_B", "PLAIN"}
		got, err := ApplyMappingAndFilteringToAttributes(attrs, []config.MappingRule{rule}, nil, excludeFields, "", nil, Entry{}, "", "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if _, ok := got["SECRET_B"]; !ok {
			t.Error("expected mapped key SECRET_B to be kept regardless of exclude_fields")
		}
		if _, ok := got["PLAIN"]; ok {
			t.Error("expected PLAIN to be excluded by exclude_fields")
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
		got, err := ApplyMappingAndFilteringToAttributes(attrs, []config.MappingRule{rule}, nil, excludeFields, "", nil, Entry{}, "", "")
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

		_, err := ApplyMappingAndFilteringToAttributes(attrs, []config.MappingRule{invalidRule}, nil, nil, "", nil, Entry{}, "", "")
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

	t.Run("GetSecret bare location with excluded Password returns error", func(t *testing.T) {
		cp2 := NewCustomVaultProvider()
		_ = cp2.Initialize(ctx, ProviderConfig{
			Entities: map[string]map[string]any{
				"app1": {
					"Password": "secret_password",
					"UserName": "test_user",
				},
			},
		})

		excludeFields := []string{"Password"}
		fp2 := NewFilteringProvider(cp2, nil, excludeFields)

		// Unmapped chain check
		_, errUnmapped := fp2.GetSecret(ctx, "app1")
		if errUnmapped == nil {
			t.Fatal("expected GetSecret bare location on FilteringProvider to error when Password is excluded, got nil")
		}

		// Mapped chain check
		mp2, errNew := NewMappingProvider(fp2, []config.MappingRule{rule})
		if errNew != nil {
			t.Fatalf("NewMappingProvider failed: %v", errNew)
		}

		_, errMapped := mp2.GetSecret(ctx, "app1")
		if errMapped == nil {
			t.Fatal("expected GetSecret bare location on MappingProvider to error when Password is excluded, got nil")
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

func TestMappingIsolationForDefaultFields(t *testing.T) {
	ctx := context.Background()

	t.Run("DefaultFieldsPreservedWhenMappingOnly", func(t *testing.T) {
		sp := NewSearchProvider()
		sp.SetSearchExecutor(func(ctx context.Context, query string, sourceVaults []string, depth int) ([]SearchResult, error) {
			return []SearchResult{
				{
					Provider: "keepass",
					Vault:    "main_vault",
					Path:     "services/api",
					Entry: Entry{
						Title: "API Key Service",
						Tags:  []string{"prod", "backend"},
						Attributes: map[string]any{
							"env:API_KEY": "secret_key_123",
						},
					},
				},
			}, nil
		})

		rule := config.MappingRule{Match: "env:(.*)", Key: "$1"}
		if err := rule.Compile(); err != nil {
			t.Fatalf("rule.Compile failed: %v", err)
		}

		mp, err := NewMappingProvider(sp, []config.MappingRule{rule})
		if err != nil {
			t.Fatalf("NewMappingProvider failed: %v", err)
		}

		smp := mp.(SearchableProvider)
		results, err := smp.Search(ctx, SearchQuery{})
		if err != nil {
			t.Fatalf("Search failed: %v", err)
		}
		if len(results) != 1 {
			t.Fatalf("expected 1 result, got %d", len(results))
		}

		res := results[0]
		if res.Provider != "search" || res.Path != "services/api" || res.Entry.Title != "API Key Service" || len(res.Entry.Tags) != 2 {
			t.Errorf("expected all default fields preserved when mapping without field filtering, got Provider=%q Vault=%q Path=%q Title=%q Tags=%v",
				res.Provider, res.Vault, res.Path, res.Entry.Title, res.Entry.Tags)
		}
		if res.Entry.Attributes["API_KEY"] != "secret_key_123" {
			t.Errorf("expected API_KEY to be mapped, got %v", res.Entry.Attributes["API_KEY"])
		}

		// Resolves GetSecret for title/tags
		valTitle, errTitle := mp.GetSecret(ctx, "services/api:title")
		if errTitle != nil {
			t.Errorf("expected title to resolve when mapping only, got error: %v", errTitle)
		}
		if valTitle != "API Key Service" {
			t.Errorf("expected title 'API Key Service', got %q", valTitle)
		}
	})

	t.Run("DefaultFieldsExcludedWhenExcludeFieldsSet", func(t *testing.T) {
		sp := NewSearchProvider()
		sp.SetSearchExecutor(func(ctx context.Context, query string, sourceVaults []string, depth int) ([]SearchResult, error) {
			return []SearchResult{
				{
					Provider: "keepass",
					Vault:    "main_vault",
					Path:     "services/api",
					Entry: Entry{
						Title: "API Key Service",
						Tags:  []string{"prod", "backend"},
						Attributes: map[string]any{
							"env:API_KEY": "secret_key_123",
						},
					},
				},
			}, nil
		})

		rule := config.MappingRule{Match: "env:(.*)", Key: "$1"}
		if err := rule.Compile(); err != nil {
			t.Fatalf("rule.Compile failed: %v", err)
		}

		fp := NewFilteringProvider(sp, nil, []string{"*"})
		mp, err := NewMappingProvider(fp, []config.MappingRule{rule})
		if err != nil {
			t.Fatalf("NewMappingProvider failed: %v", err)
		}

		smp := mp.(SearchableProvider)
		results, err := smp.Search(ctx, SearchQuery{})
		if err != nil {
			t.Fatalf("Search failed: %v", err)
		}
		if len(results) != 1 {
			t.Fatalf("expected 1 result, got %d", len(results))
		}

		res := results[0]
		if res.Provider != "" || res.Vault != "" || res.Path != "" || res.Entry.Title != "" || res.Entry.Tags != nil {
			t.Errorf("expected all default fields excluded by exclude_fields: ['*'], got Provider=%q Vault=%q Path=%q Title=%q Tags=%v",
				res.Provider, res.Vault, res.Path, res.Entry.Title, res.Entry.Tags)
		}
		if res.Entry.Attributes["API_KEY"] != "secret_key_123" {
			t.Errorf("expected API_KEY to be mapped, got %v", res.Entry.Attributes["API_KEY"])
		}

		// Rejects GetSecret for title/tags
		_, errTitle := mp.GetSecret(ctx, "services/api:title")
		if errTitle == nil {
			t.Errorf("expected error for unmapped title under exclude_fields: ['*'], got nil")
		}
	})

	t.Run("DefaultFieldsPreservedWhenMappedOrIncluded", func(t *testing.T) {
		sp := NewSearchProvider()
		sp.SetSearchExecutor(func(ctx context.Context, query string, sourceVaults []string, depth int) ([]SearchResult, error) {
			return []SearchResult{
				{
					Provider: "keepass",
					Vault:    "main_vault",
					Path:     "services/api",
					Entry: Entry{
						Title: "API Key Service",
						Tags:  []string{"prod"},
						Attributes: map[string]any{
							"env:API_KEY": "secret_key_123",
						},
					},
				},
			}, nil
		})

		// Mapping rule explicitly maps title and tags
		ruleTitle := config.MappingRule{Match: "^title$", Key: "title"}
		ruleTag := config.MappingRule{Match: "^tags$", Key: "tags"}
		ruleKey := config.MappingRule{Match: "env:(.*)", Key: "$1"}

		mp, err := NewMappingProvider(sp, []config.MappingRule{ruleTitle, ruleTag, ruleKey})
		if err != nil {
			t.Fatalf("NewMappingProvider failed: %v", err)
		}

		smp := mp.(SearchableProvider)
		results, err := smp.Search(ctx, SearchQuery{})
		if err != nil {
			t.Fatalf("Search failed: %v", err)
		}

		res := results[0]
		if res.Entry.Title != "API Key Service" {
			t.Errorf("expected Title 'API Key Service', got %q", res.Entry.Title)
		}
		if len(res.Entry.Tags) != 1 || res.Entry.Tags[0] != "prod" {
			t.Errorf("expected Tags ['prod'], got %v", res.Entry.Tags)
		}
	})

	t.Run("MappedDefaultFieldsAlwaysShowRegardlessOfIncludeOrExcludeFields", func(t *testing.T) {
		sp := NewSearchProvider()
		sp.SetSearchExecutor(func(ctx context.Context, query string, sourceVaults []string, depth int) ([]SearchResult, error) {
			return []SearchResult{
				{
					Provider: "keepass",
					Vault:    "main_vault",
					Path:     "services/api",
					Entry: Entry{
						Title: "API Key Service",
						Tags:  []string{"prod"},
						Attributes: map[string]any{
							"env:API_KEY": "secret_key_123",
						},
					},
				},
			}, nil
		})

		ruleTitle := config.MappingRule{Match: "^title$", Key: "title"}
		ruleTag := config.MappingRule{Match: "^tags$", Key: "tags"}
		ruleKey := config.MappingRule{Match: "env:(.*)", Key: "$1"}

		// Both include_fields and exclude_fields are configured to exclude/not include title & tags
		fp := NewFilteringProvider(sp, []string{"unrelated_field"}, []string{"*", "title", "tags"})
		mp, err := NewMappingProvider(fp, []config.MappingRule{ruleTitle, ruleTag, ruleKey})
		if err != nil {
			t.Fatalf("NewMappingProvider failed: %v", err)
		}

		smp := mp.(SearchableProvider)
		results, err := smp.Search(ctx, SearchQuery{})
		if err != nil {
			t.Fatalf("Search failed: %v", err)
		}
		if len(results) != 1 {
			t.Fatalf("expected 1 result, got %d", len(results))
		}

		res := results[0]
		if res.Entry.Title != "API Key Service" {
			t.Errorf("expected mapped Title to show regardless of include/exclude fields, got %q", res.Entry.Title)
		}
		if len(res.Entry.Tags) != 1 || res.Entry.Tags[0] != "prod" {
			t.Errorf("expected mapped Tags to show regardless of include/exclude fields, got %v", res.Entry.Tags)
		}

		// GetEntryWithPath also preserves mapped title and tags
		pathProvider := mp.(interface {
			GetEntryWithPath(ctx context.Context, location string) (Entry, string, error)
		})
		entry, _, err := pathProvider.GetEntryWithPath(ctx, "services/api")
		if err != nil {
			t.Fatalf("GetEntryWithPath failed: %v", err)
		}
		if entry.Title != "API Key Service" {
			t.Errorf("expected mapped Title in GetEntryWithPath, got %q", entry.Title)
		}
		if len(entry.Tags) != 1 || entry.Tags[0] != "prod" {
			t.Errorf("expected mapped Tags in GetEntryWithPath, got %v", entry.Tags)
		}

		// GetSecret resolves mapped title and tags
		valTitle, err := mp.GetSecret(ctx, "services/api:title")
		if err != nil {
			t.Errorf("expected GetSecret for mapped title to succeed, got %v", err)
		}
		if valTitle != "API Key Service" {
			t.Errorf("expected title value 'API Key Service', got %q", valTitle)
		}
	})
}

func TestMappingProvider_MappedFieldsAlwaysShowRegardlessOfFilters(t *testing.T) {
	ctx := context.Background()

	cp := NewCustomVaultProvider()
	_ = cp.Initialize(ctx, ProviderConfig{
		Entities: map[string]map[string]any{
			"app1": {
				"env:API_KEY":       "sk-test-secret-123",
				"env:DATABASE_URL":  "postgres://user:pass@localhost:5432/db",
				"UNMAPPED_ALLOWED":  "allowed_val",
				"UNMAPPED_EXCLUDED": "excluded_val",
				"UNMAPPED_NOT_INCL": "not_included_val",
			},
		},
	})

	rule := config.MappingRule{Match: "env:(.*)", Key: "$1"}
	if err := rule.Compile(); err != nil {
		t.Fatalf("rule.Compile failed: %v", err)
	}

	// include_fields includes only UNMAPPED_ALLOWED (neither API_KEY nor DATABASE_URL)
	// exclude_fields excludes API_KEY and UNMAPPED_EXCLUDED
	includeFields := []string{"UNMAPPED_ALLOWED"}
	excludeFields := []string{"API_KEY", "UNMAPPED_EXCLUDED"}

	fp := NewFilteringProvider(cp, includeFields, excludeFields)
	mp, err := NewMappingProvider(fp, []config.MappingRule{rule})
	if err != nil {
		t.Fatalf("NewMappingProvider failed: %v", err)
	}

	searchable, ok := mp.(SearchableProvider)
	if !ok {
		t.Fatal("expected MappingProvider to implement SearchableProvider")
	}

	t.Run("GetEntry preserves mapped fields and applies filters to unmapped", func(t *testing.T) {
		entry, err := searchable.GetEntry(ctx, "app1")
		if err != nil {
			t.Fatalf("GetEntry failed: %v", err)
		}

		// 1. Mapped field excluded by exclude_fields MUST show
		if val, ok := entry.Attributes["API_KEY"]; !ok {
			t.Error("expected mapped field API_KEY to show even when in exclude_fields")
		} else if val != "sk-test-secret-123" {
			t.Errorf("expected API_KEY='sk-test-secret-123', got %v", val)
		}

		// 2. Mapped field omitted from include_fields MUST show
		if val, ok := entry.Attributes["DATABASE_URL"]; !ok {
			t.Error("expected mapped field DATABASE_URL to show even when omitted from include_fields")
		} else if val != "postgres://user:pass@localhost:5432/db" {
			t.Errorf("expected DATABASE_URL='postgres://user:pass@localhost:5432/db', got %v", val)
		}

		// 3. Unmapped field in include_fields MUST show
		if val, ok := entry.Attributes["UNMAPPED_ALLOWED"]; !ok {
			t.Error("expected UNMAPPED_ALLOWED to show")
		} else if val != "allowed_val" {
			t.Errorf("expected UNMAPPED_ALLOWED='allowed_val', got %v", val)
		}

		// 4. Unmapped field matching exclude_fields MUST NOT show
		if _, ok := entry.Attributes["UNMAPPED_EXCLUDED"]; ok {
			t.Error("expected UNMAPPED_EXCLUDED to be filtered out")
		}

		// 5. Unmapped field not matching include_fields MUST NOT show
		if _, ok := entry.Attributes["UNMAPPED_NOT_INCL"]; ok {
			t.Error("expected UNMAPPED_NOT_INCL to be filtered out")
		}

		// 6. Pre-mapping raw key MUST NOT show
		if _, ok := entry.Attributes["env:API_KEY"]; ok {
			t.Error("expected pre-mapping key env:API_KEY to be absent")
		}
	})

	t.Run("Search preserves mapped fields", func(t *testing.T) {
		results, err := searchable.Search(ctx, SearchQuery{})
		if err != nil {
			t.Fatalf("Search failed: %v", err)
		}
		if len(results) != 1 {
			t.Fatalf("expected 1 result, got %d", len(results))
		}
		attrs := results[0].Entry.Attributes
		if attrs["API_KEY"] != "sk-test-secret-123" {
			t.Errorf("expected mapped API_KEY in Search, got %v", attrs["API_KEY"])
		}
		if attrs["DATABASE_URL"] != "postgres://user:pass@localhost:5432/db" {
			t.Errorf("expected mapped DATABASE_URL in Search, got %v", attrs["DATABASE_URL"])
		}
		if attrs["UNMAPPED_ALLOWED"] != "allowed_val" {
			t.Errorf("expected UNMAPPED_ALLOWED in Search, got %v", attrs["UNMAPPED_ALLOWED"])
		}
		if _, ok := attrs["UNMAPPED_EXCLUDED"]; ok {
			t.Error("expected UNMAPPED_EXCLUDED to be filtered out in Search")
		}
	})

	t.Run("GetSecret retrieves mapped fields regardless of filters", func(t *testing.T) {
		// Mapped key in exclude_fields succeeds
		val, err := mp.GetSecret(ctx, "app1:API_KEY")
		if err != nil {
			t.Fatalf("GetSecret app1:API_KEY failed: %v", err)
		}
		if val != "sk-test-secret-123" {
			t.Errorf("expected secret value 'sk-test-secret-123', got %q", val)
		}

		// Mapped key omitted from include_fields succeeds
		valDB, err := mp.GetSecret(ctx, "app1:DATABASE_URL")
		if err != nil {
			t.Fatalf("GetSecret app1:DATABASE_URL failed: %v", err)
		}
		if valDB != "postgres://user:pass@localhost:5432/db" {
			t.Errorf("expected secret value 'postgres://user:pass@localhost:5432/db', got %q", valDB)
		}

		// Unmapped allowed key succeeds
		valAllowed, err := mp.GetSecret(ctx, "app1:UNMAPPED_ALLOWED")
		if err != nil {
			t.Fatalf("GetSecret app1:UNMAPPED_ALLOWED failed: %v", err)
		}
		if valAllowed != "allowed_val" {
			t.Errorf("expected 'allowed_val', got %q", valAllowed)
		}

		// Unmapped excluded key fails
		_, err = mp.GetSecret(ctx, "app1:UNMAPPED_EXCLUDED")
		if err == nil {
			t.Fatal("expected GetSecret for UNMAPPED_EXCLUDED to fail, got nil")
		}

		// Unmapped key not in include_fields fails
		_, err = mp.GetSecret(ctx, "app1:UNMAPPED_NOT_INCL")
		if err == nil {
			t.Fatal("expected GetSecret for UNMAPPED_NOT_INCL to fail, got nil")
		}

		// Raw unmapped original key fails
		_, err = mp.GetSecret(ctx, "app1:env:API_KEY")
		if err == nil {
			t.Fatal("expected GetSecret for pre-mapping key to fail, got nil")
		}
	})
}

func TestFieldPolicy_ApplyToEntry_PreservesUUIDAndGroup(t *testing.T) {
	origEntry := Entry{
		Title: "Test Entry",
		Tags:  []string{"prod", "web"},
		UUID:  "orig-uuid-12345",
		Group: &GroupInfo{
			Name: "mygroup",
			UUID: "group-uuid-67890",
			Path: "mygroup",
		},
		Attributes: map[string]any{
			"api_key": "secret_val",
			"db_pass": "pass_val",
		},
	}

	t.Run("without mapping rules", func(t *testing.T) {
		fp := &FieldPolicy{
			ExcludeFields: []string{"*"},
		}
		res := fp.ApplyToEntry(origEntry, "services/api", nil)
		if res.UUID != "orig-uuid-12345" {
			t.Errorf("expected UUID preserved, got %q", res.UUID)
		}
		if res.Group == nil || res.Group.Name != "mygroup" || res.Group.UUID != "group-uuid-67890" {
			t.Errorf("expected Group preserved, got %+v", res.Group)
		}
		if res.Title != "Test Entry" {
			t.Errorf("expected Title preserved, got %q", res.Title)
		}
		if len(res.Tags) != 2 {
			t.Errorf("expected Tags preserved, got %v", res.Tags)
		}
		if len(res.Attributes) != 0 {
			t.Errorf("expected Attributes filtered by '*', got %v", res.Attributes)
		}
	})

	t.Run("with mapping rules", func(t *testing.T) {
		rule := config.MappingRule{Match: "^api_(.*)", Key: "$1"}
		if err := rule.Compile(); err != nil {
			t.Fatalf("Compile failed: %v", err)
		}
		fp := &FieldPolicy{
			MappingRules:  []config.MappingRule{rule},
			ExcludeFields: []string{"*"},
		}
		res := fp.ApplyToEntry(origEntry, "services/api", nil)
		if res.UUID != "orig-uuid-12345" {
			t.Errorf("expected UUID preserved with mapping rules, got %q", res.UUID)
		}
		if res.Group == nil || res.Group.Name != "mygroup" || res.Group.UUID != "group-uuid-67890" {
			t.Errorf("expected Group preserved with mapping rules, got %+v", res.Group)
		}
		if res.Title != "Test Entry" {
			t.Errorf("expected Title preserved, got %q", res.Title)
		}
		if len(res.Tags) != 2 {
			t.Errorf("expected Tags preserved, got %v", res.Tags)
		}
		// "api_key" matches mapping rule and should be preserved (bypassing exclusion)
		if res.Attributes["api_key"] != "secret_val" {
			t.Errorf("expected preserved 'api_key' in Attributes, got %v", res.Attributes)
		}
		if _, ok := res.Attributes["db_pass"]; ok {
			t.Errorf("expected 'db_pass' excluded, got %v", res.Attributes["db_pass"])
		}
	})
}

func TestMappingProvider_UUIDAndGroup(t *testing.T) {
	ctx := context.Background()

	newMockProvider := func(attrs map[string]any) *SearchProvider {
		sp := NewSearchProvider()
		sp.SetSearchExecutor(func(ctx context.Context, query string, sourceVaults []string, depth int) ([]SearchResult, error) {
			return []SearchResult{
				{
					Provider: "mock",
					Vault:    "main_vault",
					Path:     "services/api",
					Entry: Entry{
						Title: "API Service",
						Tags:  []string{"prod"},
						UUID:  "mock-entry-uuid-1234",
						Group: &GroupInfo{
							Name: "infra",
							UUID: "mock-group-uuid-5678",
							Path: "infra",
						},
						Attributes: attrs,
					},
				},
			}, nil
		})
		return sp
	}

	rule := config.MappingRule{Match: "env:(.*)", Key: "$1"}
	_ = rule.Compile()

	t.Run("preserves UUID and Group when mapping rules active without filters", func(t *testing.T) {
		sp := newMockProvider(map[string]any{"env:API_KEY": "sk-123"})
		mp, err := NewMappingProvider(sp, []config.MappingRule{rule})
		if err != nil {
			t.Fatalf("NewMappingProvider failed: %v", err)
		}

		smp := mp.(SearchableProvider)
		entry, err := smp.GetEntry(ctx, "services/api")
		if err != nil {
			t.Fatalf("GetEntry failed: %v", err)
		}
		if entry.UUID != "mock-entry-uuid-1234" {
			t.Errorf("expected UUID preserved, got %q", entry.UUID)
		}
		if entry.Group == nil || entry.Group.Name != "infra" {
			t.Errorf("expected Group preserved, got %+v", entry.Group)
		}

		results, err := smp.Search(ctx, SearchQuery{})
		if err != nil || len(results) == 0 {
			t.Fatalf("Search failed: %v", err)
		}
		if results[0].Entry.UUID != "mock-entry-uuid-1234" {
			t.Errorf("expected UUID in search result, got %q", results[0].Entry.UUID)
		}
		if results[0].Entry.Group == nil || results[0].Entry.Group.Name != "infra" {
			t.Errorf("expected Group in search result, got %+v", results[0].Entry.Group)
		}
	})

	t.Run("strips UUID and Group when exclude_fields: [*] is set", func(t *testing.T) {
		sp := newMockProvider(map[string]any{"env:API_KEY": "sk-123"})
		fp := NewFilteringProvider(sp, nil, []string{"*"})
		mp, err := NewMappingProvider(fp, []config.MappingRule{rule})
		if err != nil {
			t.Fatalf("NewMappingProvider failed: %v", err)
		}

		smp := mp.(SearchableProvider)
		entry, err := smp.GetEntry(ctx, "services/api")
		if err != nil {
			t.Fatalf("GetEntry failed: %v", err)
		}
		if entry.UUID != "" {
			t.Errorf("expected UUID stripped, got %q", entry.UUID)
		}
		if entry.Group != nil {
			t.Errorf("expected Group stripped, got %+v", entry.Group)
		}

		results, err := smp.Search(ctx, SearchQuery{})
		if err != nil || len(results) == 0 {
			t.Fatalf("Search failed: %v", err)
		}
		if results[0].Entry.UUID != "" {
			t.Errorf("expected UUID stripped in search, got %q", results[0].Entry.UUID)
		}
		if results[0].Entry.Group != nil {
			t.Errorf("expected Group stripped in search, got %+v", results[0].Entry.Group)
		}
	})

	t.Run("strips UUID and Group when exclude_fields: [uuid, group] is set", func(t *testing.T) {
		sp := newMockProvider(map[string]any{"env:API_KEY": "sk-123"})
		fp := NewFilteringProvider(sp, nil, []string{"uuid", "group*"})
		mp, err := NewMappingProvider(fp, []config.MappingRule{rule})
		if err != nil {
			t.Fatalf("NewMappingProvider failed: %v", err)
		}

		smp := mp.(SearchableProvider)
		entry, err := smp.GetEntry(ctx, "services/api")
		if err != nil {
			t.Fatalf("GetEntry failed: %v", err)
		}
		if entry.UUID != "" {
			t.Errorf("expected UUID stripped, got %q", entry.UUID)
		}
		if entry.Group != nil {
			t.Errorf("expected Group stripped, got %+v", entry.Group)
		}

		results, err := smp.Search(ctx, SearchQuery{})
		if err != nil || len(results) == 0 {
			t.Fatalf("Search failed: %v", err)
		}
		if results[0].Entry.UUID != "" {
			t.Errorf("expected UUID stripped in search, got %q", results[0].Entry.UUID)
		}
		if results[0].Entry.Group != nil {
			t.Errorf("expected Group stripped in search, got %+v", results[0].Entry.Group)
		}
	})

	t.Run("preserves UUID and Group when mapped or included", func(t *testing.T) {
		sp := newMockProvider(map[string]any{"env:API_KEY": "sk-123"})
		ruleUUID := config.MappingRule{Match: "^uuid$", Key: "uuid"}
		ruleGroup := config.MappingRule{Match: "^group$", Key: "group"}

		// Mapped default fields show even when excluded by wildcard
		fp := NewFilteringProvider(sp, nil, []string{"*"})
		mp, err := NewMappingProvider(fp, []config.MappingRule{ruleUUID, ruleGroup, rule})
		if err != nil {
			t.Fatalf("NewMappingProvider failed: %v", err)
		}

		smp := mp.(SearchableProvider)
		entry, err := smp.GetEntry(ctx, "services/api")
		if err != nil {
			t.Fatalf("GetEntry failed: %v", err)
		}
		if entry.UUID != "mock-entry-uuid-1234" {
			t.Errorf("expected mapped UUID preserved, got %q", entry.UUID)
		}
		if entry.Group == nil || entry.Group.Name != "infra" {
			t.Errorf("expected mapped Group preserved, got %+v", entry.Group)
		}

		// Included default fields show when include_fields configured
		fp2 := NewFilteringProvider(sp, []string{"uuid", "group"}, []string{"*"})
		mp2, err := NewMappingProvider(fp2, []config.MappingRule{rule})
		if err != nil {
			t.Fatalf("NewMappingProvider failed: %v", err)
		}
		smp2 := mp2.(SearchableProvider)
		entry2, err := smp2.GetEntry(ctx, "services/api")
		if err != nil {
			t.Fatalf("GetEntry failed: %v", err)
		}
		if entry2.UUID != "mock-entry-uuid-1234" {
			t.Errorf("expected included UUID preserved, got %q", entry2.UUID)
		}
		if entry2.Group == nil || entry2.Group.Name != "infra" {
			t.Errorf("expected included Group preserved, got %+v", entry2.Group)
		}
	})

	t.Run("resolves :uuid, :group, :group_name, :group_uuid, :group_path via GetSecret", func(t *testing.T) {
		sp := newMockProvider(map[string]any{"env:API_KEY": "sk-123"})
		mp, err := NewMappingProvider(sp, []config.MappingRule{rule})
		if err != nil {
			t.Fatalf("NewMappingProvider failed: %v", err)
		}

		tests := []struct {
			selector string
			want     string
		}{
			{"services/api:uuid", "mock-entry-uuid-1234"},
			{"services/api:UUID", "mock-entry-uuid-1234"},
			{"services/api:group", "infra"},
			{"services/api:group_name", "infra"},
			{"services/api:group_uuid", "mock-group-uuid-5678"},
			{"services/api:group_path", "infra"},
		}

		for _, tt := range tests {
			got, err := mp.GetSecret(ctx, tt.selector)
			if err != nil {
				t.Errorf("GetSecret(%q) failed: %v", tt.selector, err)
			}
			if got != tt.want {
				t.Errorf("GetSecret(%q) = %q, want %q", tt.selector, got, tt.want)
			}
		}
	})

	t.Run("custom UUID attribute takes precedence over entry.UUID case-insensitively", func(t *testing.T) {
		// Custom attribute with uppercase key "UUID"
		spUpper := newMockProvider(map[string]any{
			"UUID": "custom-override-upper",
		})
		mpUpper, err := NewMappingProvider(spUpper, []config.MappingRule{rule})
		if err != nil {
			t.Fatalf("NewMappingProvider failed: %v", err)
		}

		// Both :UUID and :uuid must return the custom attribute, NOT entry.UUID ("mock-entry-uuid-1234")
		valUpper1, err := mpUpper.GetSecret(ctx, "services/api:UUID")
		if err != nil || valUpper1 != "custom-override-upper" {
			t.Errorf("GetSecret :UUID = %q, want 'custom-override-upper', err: %v", valUpper1, err)
		}
		valLower1, err := mpUpper.GetSecret(ctx, "services/api:uuid")
		if err != nil || valLower1 != "custom-override-upper" {
			t.Errorf("GetSecret :uuid = %q, want 'custom-override-upper', err: %v", valLower1, err)
		}

		// Custom attribute with lowercase key "uuid"
		spLower := newMockProvider(map[string]any{
			"uuid": "custom-override-lower",
		})
		mpLower, err := NewMappingProvider(spLower, []config.MappingRule{rule})
		if err != nil {
			t.Fatalf("NewMappingProvider failed: %v", err)
		}

		valUpper2, err := mpLower.GetSecret(ctx, "services/api:UUID")
		if err != nil || valUpper2 != "custom-override-lower" {
			t.Errorf("GetSecret :UUID = %q, want 'custom-override-lower', err: %v", valUpper2, err)
		}
		valLower2, err := mpLower.GetSecret(ctx, "services/api:uuid")
		if err != nil || valLower2 != "custom-override-lower" {
			t.Errorf("GetSecret :uuid = %q, want 'custom-override-lower', err: %v", valLower2, err)
		}
	})
}

func TestMappingProvider_MetadataTemplate(t *testing.T) {
	ctx := context.Background()

	t.Run("GetEntry maps attributes with template", func(t *testing.T) {
		rule := config.MappingRule{Match: "env:(.*)", Key: "{{ upper .meta.title }}_$1"}
		if err := rule.Compile(); err != nil {
			t.Fatalf("rule.Compile failed: %v", err)
		}

		cp := NewCustomVaultProvider()
		_ = cp.Initialize(ctx, ProviderConfig{
			Entities: map[string]map[string]any{
				"app1": {
					"env:API_KEY": "secret_value",
				},
			},
		})

		provider, err := NewMappingProvider(cp, []config.MappingRule{rule})
		if err != nil {
			t.Fatalf("failed to create mapping provider: %v", err)
		}

		searchable, ok := provider.(SearchableProvider)
		if !ok {
			t.Fatalf("expected SearchableProvider")
		}

		got, err := searchable.GetEntry(ctx, "app1")
		if err != nil {
			t.Fatalf("GetEntry failed: %v", err)
		}

		if _, ok := got.Attributes["APP1_API_KEY"]; !ok {
			t.Errorf("expected mapped key APP1_API_KEY, got keys: %v", keysOf(got.Attributes))
		}
	})

	t.Run("GetSecret retrieves mapped attributes with template", func(t *testing.T) {
		rule := config.MappingRule{Match: "env:(.*)", Key: "{{ upper .meta.title }}_$1"}
		if err := rule.Compile(); err != nil {
			t.Fatalf("rule.Compile failed: %v", err)
		}

		cp := NewCustomVaultProvider()
		_ = cp.Initialize(ctx, ProviderConfig{
			Entities: map[string]map[string]any{
				"app1": {
					"env:API_KEY": "secret_value",
				},
			},
		})

		provider, err := NewMappingProvider(cp, []config.MappingRule{rule})
		if err != nil {
			t.Fatalf("failed to create mapping provider: %v", err)
		}

		got, err := provider.GetSecret(ctx, "app1:APP1_API_KEY")
		if err != nil {
			t.Fatalf("GetSecret failed: %v", err)
		}

		if got != "secret_value" {
			t.Errorf("expected secret_value, got %q", got)
		}
	})

	t.Run("Search maps entry attributes in results with template", func(t *testing.T) {
		rule := config.MappingRule{Match: "env:(.*)", Key: "{{ upper .meta.title }}_$1"}
		if err := rule.Compile(); err != nil {
			t.Fatalf("rule.Compile failed: %v", err)
		}

		sp := NewSearchProvider()
		sp.SetSearchExecutor(func(ctx context.Context, query string, sourceVaults []string, depth int) ([]SearchResult, error) {
			return []SearchResult{
				{
					Provider: "mock",
					Vault:    "main_vault",
					Path:     "services/api",
					Entry: Entry{
						Title: "my title",
						Attributes: map[string]any{
							"env:API_KEY": "secret_value",
						},
					},
				},
			}, nil
		})

		provider, err := NewMappingProvider(sp, []config.MappingRule{rule})
		if err != nil {
			t.Fatalf("failed to create mapping provider: %v", err)
		}

		searchable, ok := provider.(SearchableProvider)
		if !ok {
			t.Fatalf("expected SearchableProvider")
		}

		results, err := searchable.Search(ctx, SearchQuery{})
		if err != nil {
			t.Fatalf("Search failed: %v", err)
		}

		if len(results) != 1 {
			t.Fatalf("expected 1 result, got %d", len(results))
		}

		if _, ok := results[0].Entry.Attributes["MY TITLE_API_KEY"]; !ok {
			t.Errorf("expected mapped key MY TITLE_API_KEY in search results, got keys: %v", keysOf(results[0].Entry.Attributes))
		}
	})
}
