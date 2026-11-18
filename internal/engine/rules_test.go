package engine

import (
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
