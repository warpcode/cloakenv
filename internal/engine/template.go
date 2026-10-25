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

// ToMap converts TemplateContext into a map supporting both lowercase and PascalCase keys
// so missingkey=error works with .meta.*, .attributes.*, and .entries.
func (tc TemplateContext) ToMap() map[string]any {
	metaMap := make(map[string]any)
	for k, v := range tc.Meta {
		metaMap[k] = v
		metaMap[strings.ToLower(k)] = v
		if k == "UUID" {
			metaMap["uuid"] = v
		}
	}
	if grp, ok := tc.Meta["Group"].(map[string]any); ok && grp != nil {
		grpMap := make(map[string]any)
		for k, v := range grp {
			grpMap[k] = v
			grpMap[strings.ToLower(k)] = v
			if k == "UUID" {
				grpMap["uuid"] = v
			}
		}
		metaMap["Group"] = grpMap
		metaMap["group"] = grpMap
	} else if grp, ok := tc.Meta["group"].(map[string]any); ok && grp != nil {
		grpMap := make(map[string]any)
		for k, v := range grp {
			grpMap[k] = v
			grpMap[strings.ToLower(k)] = v
			if k == "UUID" {
				grpMap["uuid"] = v
			}
		}
		metaMap["Group"] = grpMap
		metaMap["group"] = grpMap
	}

	attrsMap := make(map[string]any)
	for k, v := range tc.Attributes {
		attrsMap[k] = v
	}

	var entriesList []any
	for _, entry := range tc.Entries {
		entriesList = append(entriesList, entry.ToMap())
	}

	return map[string]any{
		"Meta":       metaMap,
		"meta":       metaMap,
		"Attributes": attrsMap,
		"attributes": attrsMap,
		"Entries":    entriesList,
		"entries":    entriesList,
	}
}

func wrapTemplateData(data any) any {
	if data == nil {
		return nil
	}
	switch v := data.(type) {
	case TemplateContext:
		return v.ToMap()
	case *TemplateContext:
		if v == nil {
			return nil
		}
		return v.ToMap()
	case map[string]any:
		out := make(map[string]any, len(v))
		for k, val := range v {
			out[k] = val
		}

		// Normalize meta/Meta
		var rawMeta map[string]any
		if m, ok := out["Meta"].(map[string]any); ok {
			rawMeta = m
		} else if m, ok := out["meta"].(map[string]any); ok {
			rawMeta = m
		}
		if rawMeta != nil {
			metaMap := make(map[string]any, len(rawMeta)*2)
			for mk, mv := range rawMeta {
				metaMap[mk] = mv
				metaMap[strings.ToLower(mk)] = mv
				if mk == "UUID" {
					metaMap["uuid"] = mv
				}
			}
			if grp, ok := rawMeta["Group"].(map[string]any); ok && grp != nil {
				grpMap := make(map[string]any, len(grp)*2)
				for gk, gv := range grp {
					grpMap[gk] = gv
					grpMap[strings.ToLower(gk)] = gv
					if gk == "UUID" {
						grpMap["uuid"] = gv
					}
				}
				metaMap["Group"] = grpMap
				metaMap["group"] = grpMap
			} else if grp, ok := rawMeta["group"].(map[string]any); ok && grp != nil {
				grpMap := make(map[string]any, len(grp)*2)
				for gk, gv := range grp {
					grpMap[gk] = gv
					grpMap[strings.ToLower(gk)] = gv
					if gk == "UUID" {
						grpMap["uuid"] = gv
					}
				}
				metaMap["Group"] = grpMap
				metaMap["group"] = grpMap
			}
			out["Meta"] = metaMap
			out["meta"] = metaMap
		}

		// Normalize attributes/Attributes
		if attrs, ok := out["Attributes"].(map[string]any); ok {
			out["attributes"] = attrs
		} else if attrs, ok := out["attributes"].(map[string]any); ok {
			out["Attributes"] = attrs
		}

		// Normalize entries/Entries
		if entries, ok := out["Entries"]; ok {
			out["entries"] = entries
		} else if entries, ok := out["entries"]; ok {
			out["Entries"] = entries
		}

		return out
	default:
		return data
	}
}

func entryToTemplateContext(e provider.Entry, providerName, vault, path string) TemplateContext {
	meta := map[string]any{
		"Title":    e.Title,
		"title":    e.Title,
		"Tags":     e.Tags,
		"tags":     e.Tags,
		"UUID":     e.UUID,
		"uuid":     e.UUID,
		"Provider": providerName,
		"provider": providerName,
		"Vault":    vault,
		"vault":    vault,
		"Path":     path,
		"path":     path,
	}
	if e.Group != nil {
		grp := map[string]any{
			"Name": e.Group.Name,
			"name": e.Group.Name,
			"UUID": e.Group.UUID,
			"uuid": e.Group.UUID,
			"Path": e.Group.Path,
			"path": e.Group.Path,
		}
		meta["Group"] = grp
		meta["group"] = grp
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
		"entry": func(uri string) (map[string]any, error) {
			e, err := te.resolver.GetEntryRecursive(ctx, uri, depth+1)
			if err != nil {
				return nil, err
			}
			scheme, loc, _ := utils.ParseURI(uri)
			return entryToTemplateContext(e, "", scheme, loc).ToMap(), nil
		},
		"search": func(query string) ([]map[string]any, error) {
			if te.resolver.searchFunc == nil {
				return nil, fmt.Errorf("search function not configured")
			}
			results, err := te.resolver.searchFunc(ctx, query, depth+1)
			if err != nil {
				return nil, err
			}
			var tcs []map[string]any
			for _, res := range results {
				tcs = append(tcs, entryToTemplateContext(res.Entry, res.Provider, res.Vault, res.Path).ToMap())
			}
			return tcs, nil
		},
		"toEnv": toEnv,
		"toKv":  toEnv,
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
			tmplContent := tCfg.Template
			if tmplContent == "" && tCfg.Path != "" {
				var basePath string
				if cfg != nil {
					basePath = cfg.ConfigPath
				}
				expanded := expandPath(tCfg.Path, basePath)
				data, err := os.ReadFile(expanded)
				if err != nil {
					return "", fmt.Errorf("failed to read template file %q for %q: %w", tCfg.Path, name, err)
				}
				tmplContent = string(data)
			}
			return te.Render(ctx, tmplContent, tmplCtx, depth+1, configKey)
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

	execData := wrapTemplateData(data)
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, execData); err != nil {
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
		if strings.EqualFold(k, "title") || strings.EqualFold(k, "tags") || strings.EqualFold(k, "uuid") || strings.EqualFold(k, "group") ||
			strings.EqualFold(k, "meta") || strings.EqualFold(k, "entries") || strings.EqualFold(k, "attributes") {
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
