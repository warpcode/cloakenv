package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"text/template"

	"github.com/warpcode/cloakenv/internal/provider"
	"github.com/warpcode/cloakenv/internal/utils"
	"github.com/warpcode/cloakenv/internal/yaml"
)

var userHomeDir = os.UserHomeDir

// TemplateContext holds the evaluation context for templates.
type TemplateContext struct {
	Meta       map[string]any
	Attributes map[string]any
	Entries    []TemplateContext
}

func entryToTemplateContext(e provider.Entry, providerName, vault, path string) TemplateContext {
	meta := map[string]any{
		"Title":    e.Title,
		"Tags":     e.Tags,
		"UUID":     e.UUID,
		"Provider": providerName,
		"Vault":    vault,
		"Path":     path,
	}
	if e.Group != nil {
		meta["Group"] = map[string]any{
			"Name": e.Group.Name,
			"UUID": e.Group.UUID,
			"Path": e.Group.Path,
		}
	}
	return TemplateContext{
		Meta:       meta,
		Attributes: e.Attributes,
	}
}

// TemplateEngine wraps Go's text/template to evaluate strings with context.
type TemplateEngine struct {
	resolver *Resolver
}

// NewTemplateEngine creates a new TemplateEngine.
func NewTemplateEngine(r *Resolver) *TemplateEngine {
	return &TemplateEngine{resolver: r}
}

// Render evaluates a template string.
func (te *TemplateEngine) Render(ctx context.Context, tmplStr string, data any, depth int, configKey string) (string, error) {
	if depth > 5 {
		return "", fmt.Errorf("infinite secret resolution recursion detected: reached max depth 5")
	}

	funcs := template.FuncMap{
		"get": func(uri string) (string, error) {
			return te.resolver.resolveSingleURI(ctx, uri, depth+1, configKey)
		},
		"entry": func(uri string) (TemplateContext, error) {
			e, err := te.resolver.GetEntryRecursive(ctx, uri, depth+1)
			if err != nil {
				return TemplateContext{}, err
			}
			scheme, loc, _ := utils.ParseURI(uri)
			return entryToTemplateContext(e, "", scheme, loc), nil
		},
		"search": func(query string) ([]TemplateContext, error) {
			if te.resolver.searchFunc == nil {
				return nil, fmt.Errorf("search function not configured")
			}
			results, err := te.resolver.searchFunc(ctx, query, depth+1)
			if err != nil {
				return nil, err
			}
			var tcs []TemplateContext
			for _, res := range results {
				tcs = append(tcs, entryToTemplateContext(res.Entry, res.Provider, res.Vault, res.Path))
			}
			return tcs, nil
		},
		"toEnv":  toEnv,
		"toKv":   toEnv,
		"toJson": func(v any) (string, error) {
			b, err := json.Marshal(v)
			return string(b), err
		},
		"toYaml": func(v any) (string, error) {
			b, err := yaml.Marshal(v)
			return strings.TrimSpace(string(b)), err
		},
		"upper": strings.ToUpper,
		"lower": strings.ToLower,
		"env": func(s string) string {
			return utils.FormatKeyWithFormat(s, utils.KeyFormatEnv)
		},
		"snake": func(s string) string {
			return utils.FormatKeyWithFormat(s, utils.KeyFormatSnake)
		},
		"kebab": func(s string) string {
			return utils.FormatKeyWithFormat(s, utils.KeyFormatKebab)
		},
		"keys": func(m map[string]any) []string {
			var ks []string
			for k := range m {
				ks = append(ks, k)
			}
			slices.Sort(ks)
			return ks
		},
		"values": func(m map[string]any) []any {
			var ks []string
			for k := range m {
				ks = append(ks, k)
			}
			slices.Sort(ks)
			var vs []any
			for _, k := range ks {
				vs = append(vs, m[k])
			}
			return vs
		},
		"join": func(sep string, v any) (string, error) {
			switch val := v.(type) {
			case []string:
				return strings.Join(val, sep), nil
			case []any:
				var strs []string
				for _, x := range val {
					strs = append(strs, fmt.Sprintf("%v", x))
				}
				return strings.Join(strs, sep), nil
			default:
				return "", fmt.Errorf("join requires a slice, got %T", v)
			}
		},
		"include": func(name string, tmplCtx any) (string, error) {
			cfg := te.resolver.providers.Config()
			if cfg == nil || cfg.Templates == nil {
				return "", fmt.Errorf("template %q not found", name)
			}
			tCfg, ok := cfg.Templates[name]
			if !ok {
				return "", fmt.Errorf("template %q not found", name)
			}
			return te.Render(ctx, tCfg.Template, tmplCtx, depth+1, configKey)
		},
		"includeFile": func(path string, tmplCtx any) (string, error) {
			cfg := te.resolver.providers.Config()
			var basePath string
			if cfg != nil {
				basePath = cfg.ConfigPath
			}
			expanded := expandPath(path, basePath)
			data, err := os.ReadFile(expanded)
			if err != nil {
				return "", err
			}
			return te.Render(ctx, string(data), tmplCtx, depth+1, configKey)
		},
	}

	tmpl, err := template.New("tmpl").Option("missingkey=error").Funcs(funcs).Parse(tmplStr)
	if err != nil {
		// Try to provide a good error message for template errors
		keyPart := ""
		if configKey != "" {
			keyPart = fmt.Sprintf(" in configuration key %q", configKey)
		}
		return "", fmt.Errorf("template parsing failed%s: %w", keyPart, err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		keyPart := ""
		if configKey != "" {
			keyPart = fmt.Sprintf(" in configuration key %q", configKey)
		}
		return "", fmt.Errorf("template execution failed%s: %w", keyPart, err)
	}
	return buf.String(), nil
}

func toEnv(m map[string]any) (string, error) {
	var ks []string
	for k := range m {
		if strings.EqualFold(k, "title") || strings.EqualFold(k, "tags") || strings.EqualFold(k, "uuid") || strings.EqualFold(k, "group") {
			continue
		}
		ks = append(ks, k)
	}
	slices.Sort(ks)
	var sb strings.Builder
	for _, k := range ks {
		valStr, err := utils.SerializeAttrValue(m[k])
		if err != nil {
			return "", err
		}
		sb.WriteString(fmt.Sprintf("%s=%s\n", k, valStr))
	}
	return strings.TrimSpace(sb.String()), nil
}

func expandPath(path, configPath string) string {
	if strings.HasPrefix(path, "~/") {
		home, err := userHomeDir()
		if err == nil {
			path = filepath.Join(home, path[2:])
		}
	}
	if !filepath.IsAbs(path) && configPath != "" {
		baseDir := filepath.Dir(configPath)
		path = filepath.Join(baseDir, path)
	}
	return path
}
