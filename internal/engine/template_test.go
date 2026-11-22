package engine

import (
	"slices"
	"strings"
	"testing"

	"github.com/warpcode/cloakenv/internal/config"
)

func TestPureTemplateHelpers(t *testing.T) {
	re := NewRuleEngine()

	tests := []struct {
		name string
		tmpl string
		data map[string]any
		want string
	}{
		{
			name: "upper and lower",
			tmpl: `{{ upper .a }}/{{ lower .b }}`,
			data: map[string]any{"a": "hello", "b": "WORLD"},
			want: "HELLO/world",
		},
		{
			name: "env snake kebab",
			tmpl: `{{ env .k }}|{{ snake .k }}|{{ kebab .k }}`,
			data: map[string]any{"k": "api-key name"},
			want: "API_KEY_NAME|api_key_name|api-key-name",
		},
		{
			name: "default and coalesce",
			tmpl: `{{ default "fb" .empty }}|{{ coalesce .empty .val "other" }}`,
			data: map[string]any{"empty": "", "val": "ok"},
			want: "fb|ok",
		},
		{
			name: "fromJSON and toJSON",
			tmpl: `{{ (fromJSON .j).token }}|{{ toJSON .m }}`,
			data: map[string]any{"j": `{"token":"tok_123"}`, "m": map[string]any{"a": 1}},
			want: `tok_123|{"a":1}`,
		},
		{
			name: "fromYAML and toYAML",
			tmpl: `{{ (fromYAML .y).env }}|{{ toYAML .m }}`,
			data: map[string]any{"y": "env: prod\n", "m": map[string]any{"k": "v"}},
			want: "prod|k: v",
		},
		{
			name: "toEnv skips reserved metadata keys",
			tmpl: `{{ toEnv . }}`,
			data: map[string]any{"title": "T", "tags": []string{"a"}, "API_KEY": "k1", "HOST": "h1"},
			want: "API_KEY=k1\nHOST=h1",
		},
		{
			name: "shquote escapes single quotes",
			tmpl: `{{ shquote .s }}`,
			data: map[string]any{"s": "it's a secret"},
			want: `'it'\''s a secret'`,
		},
		{
			name: "hasTag is case-insensitive",
			tmpl: `{{ hasTag "PROD" .tags }}`,
			data: map[string]any{"tags": []string{"prod", "api"}},
			want: "true",
		},
		{
			name: "join slice",
			tmpl: `{{ join "," .items }}`,
			data: map[string]any{"items": []string{"a", "b", "c"}},
			want: "a,b,c",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := re.evalTemplate(tc.name, tc.tmpl, tc.data)
			if err != nil {
				t.Fatalf("evalTemplate failed: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestLegacyTemplateLookupFunctionsRemoved(t *testing.T) {
	removed := []string{"get", "tryGet", "entry", "search", "include", "includeFile", "exec"}
	ruleFuncs := ruleTemplateFuncs()
	stdFuncs := config.StandardTemplateFuncs()

	for _, fn := range removed {
		if _, exists := ruleFuncs[fn]; exists {
			t.Errorf("legacy template function %q must not exist in ruleTemplateFuncs()", fn)
		}
		if _, exists := stdFuncs[fn]; exists {
			t.Errorf("legacy template function %q must not exist in config.StandardTemplateFuncs()", fn)
		}
	}

	re := NewRuleEngine()
	if _, err := re.evalTemplate("rejected_get", `{{ get "env://HOME" }}`, map[string]any{}); err == nil || !strings.Contains(err.Error(), `function "get" not defined`) {
		t.Errorf("expected undefined function error for get, got: %v", err)
	}
}

func TestRuleTemplateFuncsParityWithConfigStandardTemplateFuncs(t *testing.T) {
	ruleFuncs := ruleTemplateFuncs()
	stdFuncs := config.StandardTemplateFuncs()

	var ruleNames []string
	for k := range ruleFuncs {
		ruleNames = append(ruleNames, k)
		if _, ok := stdFuncs[k]; !ok {
			t.Errorf("function %q exists in ruleTemplateFuncs() but is missing from config.StandardTemplateFuncs()", k)
		}
	}

	var stdNames []string
	for k := range stdFuncs {
		stdNames = append(stdNames, k)
		if _, ok := ruleFuncs[k]; !ok {
			t.Errorf("function %q exists in config.StandardTemplateFuncs() but is missing from ruleTemplateFuncs()", k)
		}
	}

	slices.Sort(ruleNames)
	slices.Sort(stdNames)
	if !slices.Equal(ruleNames, stdNames) {
		t.Fatalf("template func parity mismatch: rule=%v, std=%v", ruleNames, stdNames)
	}
}

func TestTemplateAliasesAndToEnvMapStringString(t *testing.T) {
	re := NewRuleEngine()

	out, err := re.evalTemplate("json_yaml_aliases", `{{ json .m }}|{{ yaml .m }}`, map[string]any{
		"m": map[string]any{"k": "v"},
	})
	if err != nil {
		t.Fatalf("json/yaml alias evaluation failed: %v", err)
	}
	if out != `{"k":"v"}|k: v` {
		t.Fatalf("got %q, want %q", out, `{"k":"v"}|k: v`)
	}

	out, err = re.evalTemplate("toEnv_map_str", `{{ toEnv .output }}|{{ toKv .entry }}`, map[string]any{
		"output": map[string]string{"DB_HOST": "localhost", "API_KEY": "secret-val", "title": "ignored"},
		"entry":  map[string]string{"FOO": "bar"},
	})
	if err != nil {
		t.Fatalf("toEnv/toKv on map[string]string failed: %v", err)
	}
	if out != "API_KEY=secret-val\nDB_HOST=localhost|FOO=bar" {
		t.Fatalf("got %q", out)
	}

	if _, err := re.evalTemplate("toEnv_invalid", `{{ toEnv .bad }}`, map[string]any{"bad": 42}); err == nil {
		t.Fatalf("expected error for unsupported toEnv input type")
	}
}
