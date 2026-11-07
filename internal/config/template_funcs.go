package config

import (
	"fmt"
	"strings"
	texttemplate "text/template"
)

// StandardTemplateFuncs returns a text/template FuncMap containing standard cloakenv functions
// with stub/validator implementations for template syntax checking.
func StandardTemplateFuncs() texttemplate.FuncMap {
	return texttemplate.FuncMap{
		"get":         func(uri string) (string, error) { return "", nil },
		"tryGet":      func(uris ...string) string { return "" },
		"default":     func(fallback any, given ...any) any { return fallback },
		"coalesce":    func(vals ...any) string { return "" },
		"entry":       func(uri string) (map[string]any, error) { return nil, nil },
		"search":      func(query string) ([]map[string]any, error) { return nil, nil },
		"toEnv":       func(m any) (string, error) { return "", nil },
		"toKv":        func(m any) (string, error) { return "", nil },
		"toJSON":      func(v any) (string, error) { return "", nil },
		"toJson":      func(v any) (string, error) { return "", nil },
		"json":        func(v any) (string, error) { return "", nil },
		"fromJSON":    func(str string) (any, error) { return nil, nil },
		"fromJson":    func(str string) (any, error) { return nil, nil },
		"toYAML":      func(v any) (string, error) { return "", nil },
		"toYaml":      func(v any) (string, error) { return "", nil },
		"yaml":        func(v any) (string, error) { return "", nil },
		"fromYAML":    func(str string) (any, error) { return nil, nil },
		"fromYaml":    func(str string) (any, error) { return nil, nil },
		"upper":       strings.ToUpper,
		"lower":       strings.ToLower,
		"env":         func(s string) string { return strings.ToUpper(s) },
		"snake":       func(s string) string { return strings.ToLower(s) },
		"kebab":       func(s string) string { return strings.ToLower(s) },
		"keys":        func(m map[string]any) []string { return nil },
		"values":      func(m map[string]any) []any { return nil },
		"join":        func(sep string, v any) (string, error) { return "", nil },
		"include":     func(name string, tmplCtx any) (string, error) { return "", nil },
		"includeFile": func(path string, tmplCtx any) (string, error) { return "", nil },
		"shquote":     func(s string) string { return fmt.Sprintf("'%s'", s) },
		"shellQuote":  func(s string) string { return fmt.Sprintf("'%s'", s) },
		"exec":        func(name string, args ...any) (string, error) { return "", nil },
	}
}
