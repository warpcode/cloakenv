package engine

import (
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
