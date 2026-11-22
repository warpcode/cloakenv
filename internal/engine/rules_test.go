package engine

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	"github.com/warpcode/cloakenv/internal/config"
	"github.com/warpcode/cloakenv/internal/provider"
	"github.com/warpcode/cloakenv/internal/utils"
)

func TestRuleEngine_DefaultPassAndReservedKeys(t *testing.T) {
	re := NewRuleEngine()
	entry := map[string]any{
		"title":     "OpenAI",
		"path":      "ai/openai",
		"uuid":      "u-123",
		"tags":      []string{"ai", "litellm"},
		"group":     map[string]any{"name": "AI"},
		"vault":     "work",
		"provider":  "keepass",
		"api-key":   "sk-123",
		"modelName": "gpt-4o",
	}

	got, err := re.Apply(entry, nil, utils.KeyFormatEnv)
	if err != nil {
		t.Fatalf("Apply failed: %v", err)
	}

	want := map[string]string{
		"API_KEY":   "sk-123",
		"MODELNAME": "gpt-4o",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if _, clobbered := got["PATH"]; clobbered {
		t.Fatal("expected reserved 'path' key NOT to be emitted on wildcard pass")
	}
}

func TestRuleEngine_ExplicitIncludeReservedKeyAndExclude(t *testing.T) {
	re := NewRuleEngine()
	entry := map[string]any{
		"title":   "OpenAI",
		"path":    "ai/openai",
		"api_key": "sk-123",
		"notes":   "ignore me",
	}

	rules := []config.RuleConfig{
		{
			Type:      "pass",
			Include:   []string{"path", "title", "api_*", "notes"},
			Exclude:   []string{"notes"},
			KeyFormat: utils.KeyFormatEnv,
		},
	}

	got, err := re.Apply(entry, rules, utils.KeyFormatNone)
	if err != nil {
		t.Fatalf("Apply failed: %v", err)
	}

	want := map[string]string{
		"PATH":    "ai/openai",
		"TITLE":   "OpenAI",
		"API_KEY": "sk-123",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestRuleEngine_ExplicitRulesDoNotAutoPass(t *testing.T) {
	re := NewRuleEngine()
	entry := map[string]any{
		"title":     "Stripe",
		"raw_field": "raw-val",
		"token":     "tok-999",
	}

	rules := []config.RuleConfig{
		{
			Type: "set",
			Values: map[string]string{
				"SERVICE_NAME": "{{ .title | upper }}",
			},
		},
	}

	got, err := re.Apply(entry, rules, utils.KeyFormatEnv)
	if err != nil {
		t.Fatalf("Apply failed: %v", err)
	}

	want := map[string]string{
		"SERVICE_NAME": "STRIPE",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected only explicit set key, got %v", got)
	}
}

func TestRuleEngine_MatchAndPrecedence(t *testing.T) {
	re := NewRuleEngine()
	entry := map[string]any{
		"title":      "litellm",
		"OPENAI_KEY": "sk-openai",
		"OTHER_ENV":  "sk-other",
		"NO_SECRET":  "plain-value",
		"notes":      "metadata",
	}

	rules := []config.RuleConfig{
		{
			Type:      "pass",
			Include:   []string{"OPENAI_*"},
			KeyFormat: utils.KeyFormatEnv,
		},
		{
			Type:       "match",
			Match:      `^(OPENAI|OTHER)_(.*)$`,
			ValueMatch: `^sk-(.*)$`,
			Key:        `LLM_$1_$2`,
			Value:      `prefix:{{ .value }}`,
		},
		{
			Type:       "match",
			ValueMatch: `^sk-(.*)$`,
			Key:        `CAPTURED_$1`,
		},
		{
			Type: "set",
			Values: map[string]string{
				"OPENAI_KEY":  "overridden-by-set",
				"SUMMARY_KEY": "{{ .title }}:{{ index .output \"LLM_OPENAI_KEY\" }}",
			},
		},
	}

	got, traces, err := re.ApplyWithTrace(entry, rules, utils.KeyFormatEnv)
	if err != nil {
		t.Fatalf("ApplyWithTrace failed: %v", err)
	}

	want := map[string]string{
		"OPENAI_KEY":      "overridden-by-set",
		"LLM_OPENAI_KEY":  "prefix:sk-openai",
		"LLM_OTHER_ENV":   "prefix:sk-other",
		"CAPTURED_openai": "sk-openai",
		"CAPTURED_other":  "sk-other",
		"SUMMARY_KEY":     "litellm:prefix:sk-openai",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got:\n%#v\nwant:\n%#v", got, want)
	}
	if len(traces) != 4 {
		t.Fatalf("expected 4 traces, got %d", len(traces))
	}
}

func TestRuleEngine_MatchTemplatesWithoutKeyFormatReapplied(t *testing.T) {
	re := NewRuleEngine()
	entry := map[string]any{
		"title":    "service-a",
		"apiToken": "tok-123",
	}
	rules := []config.RuleConfig{
		{
			Type:  "match",
			Match: `^apiToken$`,
			Key:   `custom.dot-Key/{{ .title }}`,
			Value: `Bearer {{ .value }}`,
		},
	}
	got, err := re.Apply(entry, rules, utils.KeyFormatEnv)
	if err != nil {
		t.Fatalf("Apply failed: %v", err)
	}
	want := map[string]string{
		"custom.dot-Key/service-a": "Bearer tok-123",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestRuleEngine_Errors(t *testing.T) {
	re := NewRuleEngine()
	entry := map[string]any{"title": "test", "password": "secret"}

	tests := []struct {
		name  string
		rules []config.RuleConfig
		want  string
	}{
		{
			name:  "unknown rule type",
			rules: []config.RuleConfig{{Type: "bogus"}},
			want:  "unknown rule type",
		},
		{
			name:  "match missing patterns",
			rules: []config.RuleConfig{{Type: "match", Key: "FOO"}},
			want:  "requires 'match' or 'value_match'",
		},
		{
			name:  "match missing key",
			rules: []config.RuleConfig{{Type: "match", Match: ".*"}},
			want:  "missing required 'key'",
		},
		{
			name:  "invalid include glob",
			rules: []config.RuleConfig{{Type: "pass", Include: []string{"[bad"}}},
			want:  "invalid include glob",
		},
		{
			name:  "missingkey in set template",
			rules: []config.RuleConfig{{Type: "set", Values: map[string]string{"K": "{{ .missingField }}"}}},
			want:  "failed to execute template",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := re.Apply(entry, tt.rules, utils.KeyFormatEnv)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("expected error containing %q, got %v", tt.want, err)
			}
		})
	}
}

func TestEntryToRawMap(t *testing.T) {
	e := provider.Entry{
		Title: "DB",
		UUID:  "1234",
		Tags:  []string{"prod", "db"},
		Group: &provider.GroupInfo{Name: "Infra", UUID: "g-1", Path: "/Infra"},
		Attributes: map[string]any{
			"password": "pw",
			"port":     5432,
		},
	}
	raw := EntryToRawMap(e, "keepass", "work", "Infra/DB")
	if raw["title"] != "DB" || raw["path"] != "Infra/DB" || raw["password"] != "pw" || raw["port"] != 5432 {
		t.Fatalf("unexpected raw map: %#v", raw)
	}
	grp, ok := raw["group"].(map[string]any)
	if !ok || grp["name"] != "Infra" {
		t.Fatalf("unexpected group in raw map: %#v", raw["group"])
	}
}

func TestRuleEngine_ReviewRegressionCoverage(t *testing.T) {
	re := NewRuleEngine()

	t.Run("match prevents template injection and preserves literal {{ and $ in captures", func(t *testing.T) {
		entry := map[string]any{
			"title":   "InjectTest",
			"Secret":  "SUPER_SECRET_DO_NOT_LEAK",
			"RAW_KEY": "{{ .Secret }}-$1-literal-{{broken",
		}

		// 1. Plain capture rule (no {{ in rule): captured {{ .Secret }} MUST NOT be evaluated as template.
		out, err := re.Apply(entry, []config.RuleConfig{
			{
				Type:       "match",
				Match:      `^RAW_(.*)$`,
				ValueMatch: `^(.*)$`,
				Key:        "CAPTURED_$1",
				Value:      "val:$1",
			},
		}, "none")
		if err != nil {
			t.Fatalf("Apply failed on secret containing literal {{: %v", err)
		}
		want := "val:{{ .Secret }}-$1-literal-{{broken"
		if out["CAPTURED_KEY"] != want {
			t.Fatalf("got %q, want %q", out["CAPTURED_KEY"], want)
		}
		if strings.Contains(out["CAPTURED_KEY"], "SUPER_SECRET_DO_NOT_LEAK") {
			t.Fatal("template injection occurred: secret leaked into captured value")
		}

		// 2. Combined template + capture rule (outside and inside {{ }}): captured {{ .Secret }} stays data.
		out2, err := re.Apply(entry, []config.RuleConfig{
			{
				Type:       "match",
				Match:      `^RAW_(.*)$`,
				ValueMatch: `^(.*)$`,
				Key:        `{{ .title | upper }}_${1}`,
				Value:      `prefix={{ upper $1 }}|tail=$1`,
			},
		}, "none")
		if err != nil {
			t.Fatalf("Apply failed on combined template+capture rule: %v", err)
		}
		if got := out2["INJECTTEST_KEY"]; got != "prefix={{ .SECRET }}-$1-LITERAL-{{BROKEN|tail={{ .Secret }}-$1-literal-{{broken" {
			t.Fatalf("unexpected combined output: %q", got)
		}
		if strings.Contains(out2["INJECTTEST_KEY"], "SUPER_SECRET_DO_NOT_LEAK") {
			t.Fatal("template injection occurred in combined template+capture rule")
		}
	})

	t.Run("capture syntax ${1}, $0, and out-of-bounds $9", func(t *testing.T) {
		entry := map[string]any{"ENV_API_KEY": "tok_1"}
		out, err := re.Apply(entry, []config.RuleConfig{
			{
				Type:  "match",
				Match: `^ENV_(API)_(KEY)$`,
				Key:   "${1}_FULL_$0_OOB_$9",
				Value: "${0}_$2_$9",
			},
		}, "none")
		if err != nil {
			t.Fatalf("Apply failed: %v", err)
		}
		if got := out["API_FULL_ENV_API_KEY_OOB_$9"]; got != "ENV_API_KEY_KEY_$9" {
			t.Fatalf("got %q, want %q", got, "ENV_API_KEY_KEY_$9")
		}
	})

	t.Run("keys and values template helpers accept .output map[string]string and reject non-maps", func(t *testing.T) {
		entry := map[string]any{"B": "2", "A": "1"}
		out, err := re.Apply(entry, []config.RuleConfig{
			{Type: "pass", KeyFormat: "none"},
			{
				Type: "set",
				Values: map[string]string{
					"OUT_KEYS": `{{ join "," (keys .output) }}`,
					"OUT_VALS": `{{ join "," (values .output) }}`,
				},
			},
		}, "none")
		if err != nil {
			t.Fatalf("Apply failed: %v", err)
		}
		if out["OUT_KEYS"] != "A,B" {
			t.Errorf("OUT_KEYS = %q, want A,B", out["OUT_KEYS"])
		}
		if out["OUT_VALS"] != "1,2" {
			t.Errorf("OUT_VALS = %q, want 1,2", out["OUT_VALS"])
		}

		// Non-map argument must error explicitly
		if _, err := re.Apply(entry, []config.RuleConfig{
			{Type: "set", Values: map[string]string{"BAD": `{{ keys "not-a-map" }}`}},
		}, "none"); err == nil {
			t.Fatal("expected error calling keys on string")
		}
		if _, err := re.Apply(entry, []config.RuleConfig{
			{Type: "set", Values: map[string]string{"BAD": `{{ values 123 }}`}},
		}, "none"); err == nil {
			t.Fatal("expected error calling values on int")
		}
	})

	t.Run("applyMatch skips serialization for keys not matching key regex", func(t *testing.T) {
		entry := map[string]any{
			"UNMATCHED_BAD": func() {}, // unserializable if touched
			"MATCH_OK":      "ok-val",
		}
		out, err := re.Apply(entry, []config.RuleConfig{
			{Type: "match", Match: `^MATCH_(.*)$`, Key: "OUT_$1"},
		}, "none")
		if err != nil {
			t.Fatalf("expected unmatched unserializable field to be skipped, got err: %v", err)
		}
		if out["OUT_OK"] != "ok-val" {
			t.Fatalf("OUT_OK = %q, want ok-val", out["OUT_OK"])
		}
	})

	t.Run("non-string coercion through RuleEngine.Apply", func(t *testing.T) {
		entry := map[string]any{
			"PORT":    8080,
			"ENABLED": true,
			"LIST":    []string{"a", "b"},
			"OBJ":     map[string]any{"k": "v"},
		}
		out, err := re.Apply(entry, []config.RuleConfig{{Type: "pass", KeyFormat: "none"}}, "none")
		if err != nil {
			t.Fatalf("Apply failed: %v", err)
		}
		if out["PORT"] != "8080" || out["ENABLED"] != "true" || out["LIST"] != "- a\n- b" || out["OBJ"] != "k: v" {
			t.Fatalf("unexpected coerced output: %#v", out)
		}
	})

	t.Run("pass key_format modes snake, kebab, none", func(t *testing.T) {
		entry := map[string]any{"API-Key Name": "v1"}
		for _, tc := range []struct {
			format string
			want   string
		}{
			{"snake", "api_key_name"},
			{"kebab", "api-key-name"},
			{"none", "API-Key Name"},
		} {
			out, err := re.Apply(entry, []config.RuleConfig{{Type: "pass", KeyFormat: tc.format}}, "none")
			if err != nil {
				t.Fatalf("key_format %q failed: %v", tc.format, err)
			}
			if out[tc.want] != "v1" {
				t.Errorf("key_format %q: expected key %q, got %#v", tc.format, tc.want, out)
			}
		}
	})

	t.Run("set deterministic sorted evaluation order", func(t *testing.T) {
		entry := map[string]any{"x": "1"}
		_, traces, err := re.ApplyWithTrace(entry, []config.RuleConfig{
			{Type: "set", Values: map[string]string{"Z_KEY": "z", "A_KEY": "a", "M_KEY": "m"}},
		}, "none")
		if err != nil {
			t.Fatalf("ApplyWithTrace failed: %v", err)
		}
		got := strings.Join(traces[0].KeysAdded, ",")
		if got != "A_KEY,M_KEY,Z_KEY" {
			t.Fatalf("set trace order = %q, want A_KEY,M_KEY,Z_KEY", got)
		}
	})

	t.Run("error branches: invalid value_match regex and invalid exclude glob", func(t *testing.T) {
		entry := map[string]any{"A": "1"}
		if _, err := re.Apply(entry, []config.RuleConfig{
			{Type: "match", ValueMatch: "[invalid", Key: "K"},
		}, "none"); err == nil {
			t.Fatal("expected error for invalid value_match regex")
		}
		if _, err := re.Apply(entry, []config.RuleConfig{
			{Type: "pass", Exclude: []string{"[invalid"}},
		}, "none"); err == nil {
			t.Fatal("expected error for invalid exclude glob")
		}
	})
}

func TestRuleEngine_MissingCanonicalMetadataDefaults(t *testing.T) {
	re := NewRuleEngine()
	// Sparse raw entry with no canonical metadata keys populated.
	entry := map[string]any{"APP_TOKEN": "s3cr3t"}

	rules := []config.RuleConfig{
		{Type: "pass", KeyFormat: "none"},
		{
			Type:  "match",
			Match: `^APP_(.*)$`,
			Key:   `{{ if .group }}GRP_{{ end }}OUT_${1}`,
			Value: `{{ default "n/a" .title }}:{{ if hasTag "prod" .tags }}prod{{ else }}dev{{ end }}:{{ .path }}:{{ .uuid }}:{{ .vault }}:{{ .provider }}`,
		},
		{
			Type: "set",
			Values: map[string]string{
				"HAS_PROD":  `{{ if hasTag "prod" .tags }}yes{{ else }}no{{ end }}`,
				"TITLE_OR":  `{{ default "untitled" .title }}`,
				"HAS_GROUP": `{{ if .group }}yes{{ else }}no{{ end }}`,
			},
		},
	}

	out, err := re.Apply(entry, rules, "none")
	if err != nil {
		t.Fatalf("Apply failed on sparse entry: %v", err)
	}

	if out["APP_TOKEN"] != "s3cr3t" {
		t.Errorf("expected APP_TOKEN=s3cr3t, got %q", out["APP_TOKEN"])
	}
	if out["OUT_TOKEN"] != "n/a:dev::::" {
		t.Errorf("expected OUT_TOKEN=n/a:dev::::, got %q", out["OUT_TOKEN"])
	}
	if out["HAS_PROD"] != "no" || out["TITLE_OR"] != "untitled" || out["HAS_GROUP"] != "no" {
		t.Errorf("unexpected set outputs: %#v", out)
	}
	// Ensure defaulting canonical keys in template context did not pollute entry or pass output.
	for _, metaKey := range []string{"title", "path", "uuid", "tags", "vault", "provider", "group"} {
		if _, exists := entry[metaKey]; exists {
			t.Errorf("entry was mutated with metadata key %q", metaKey)
		}
		if _, exists := out[metaKey]; exists {
			t.Errorf("pass output unexpectedly emitted metadata key %q", metaKey)
		}
	}

	// Also verify an EntryToRawMap output with empty metadata fields works identically.
	rawFromEntry := EntryToRawMap(provider.Entry{
		Attributes: map[string]any{"APP_TOKEN": "s3cr3t"},
	}, "", "", "")
	out2, err := re.Apply(rawFromEntry, rules, "none")
	if err != nil {
		t.Fatalf("Apply failed on EntryToRawMap sparse entry: %v", err)
	}
	if out2["OUT_TOKEN"] != "n/a:dev::::" || out2["HAS_PROD"] != "no" || out2["TITLE_OR"] != "untitled" || out2["HAS_GROUP"] != "no" {
		t.Errorf("unexpected outputs from EntryToRawMap sparse entry: %#v", out2)
	}
}

func TestRuleEngine_EvalTemplateZeroesAndCapsBufPool(t *testing.T) {
	re := NewRuleEngine()

	// Verify oversized rendered output (> 64 KiB) succeeds and does not panic on pool cleanup.
	bigVal := strings.Repeat("S", 70*1024)
	out, err := re.Apply(map[string]any{"SECRET": bigVal}, []config.RuleConfig{
		{Type: "set", Values: map[string]string{"BIG": "{{ .SECRET }}"}},
	}, "none")
	if err != nil {
		t.Fatalf("Apply with oversized template value failed: %v", err)
	}
	if out["BIG"] != bigVal {
		t.Fatalf("unexpected BIG output length: got %d, want %d", len(out["BIG"]), len(bigVal))
	}

	// Verify small buffer backing array is zeroed before reuse in bufPool.
	secret := "SUPER_SENSITIVE_RULE_SECRET"
	got, err := re.evalTemplate("zero_check", "{{ .VAL }}", map[string]any{"VAL": secret})
	if err != nil {
		t.Fatalf("evalTemplate failed: %v", err)
	}
	if got != secret {
		t.Fatalf("got %q, want %q", got, secret)
	}
	buf := bufPool.Get().(*bytes.Buffer)
	defer bufPool.Put(buf)
	rawCap := buf.Bytes()[:cap(buf.Bytes())]
	if strings.Contains(string(rawCap), secret) {
		t.Fatalf("pooled buffer still contains secret bytes after evalTemplate")
	}
}
