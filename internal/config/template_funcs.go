package config

import (
	"fmt"
	"strings"
	texttemplate "text/template"
)

// StandardTemplateFuncs returns a text/template FuncMap containing standard safe
// cloakenv helper functions for compile-time template syntax validation.
func StandardTemplateFuncs() texttemplate.FuncMap {
	return texttemplate.FuncMap{
		"hasTag":     func(tag string, tags any) bool { return false },
		"default":    func(fallback any, given ...any) any { return fallback },
		"coalesce":   func(vals ...any) string { return "" },
		"toEnv":      func(m any) (string, error) { return "", nil },
		"toKv":       func(m any) (string, error) { return "", nil },
		"toJSON":     func(v any) (string, error) { return "", nil },
		"toJson":     func(v any) (string, error) { return "", nil },
		"json":       func(v any) (string, error) { return "", nil },
		"fromJSON":   func(str string) (any, error) { return nil, nil },
		"fromJson":   func(str string) (any, error) { return nil, nil },
		"toYAML":     func(v any) (string, error) { return "", nil },
		"toYaml":     func(v any) (string, error) { return "", nil },
		"yaml":       func(v any) (string, error) { return "", nil },
		"fromYAML":   func(str string) (any, error) { return nil, nil },
		"fromYaml":   func(str string) (any, error) { return nil, nil },
		"upper":      strings.ToUpper,
		"lower":      strings.ToLower,
		"env":        func(s string) string { return strings.ToUpper(s) },
		"snake":      func(s string) string { return strings.ToLower(s) },
		"kebab":      func(s string) string { return strings.ToLower(s) },
		"keys":       func(m any) ([]string, error) { return nil, nil },
		"values":     func(m any) ([]any, error) { return nil, nil },
		"join":       func(sep string, v any) (string, error) { return "", nil },
		"shquote":    func(s string) string { return fmt.Sprintf("'%s'", s) },
		"shellQuote": func(s string) string { return fmt.Sprintf("'%s'", s) },
	}
}
