package engine

import (
	"bytes"
	"container/list"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/warpcode/cloakenv/internal/provider"
	"github.com/warpcode/cloakenv/internal/utils"
	"github.com/warpcode/cloakenv/internal/yaml"
)

// bufPool pools *bytes.Buffer instances to reduce heap allocations during template rendering.
var bufPool = sync.Pool{
	New: func() any {
		return new(bytes.Buffer)
	},
}

const defaultTemplateCacheCapacity = 512

type templateCacheEntry struct {
	key  string
	tmpl *template.Template
}

// templateLRUCache is a thread-safe LRU cache for precompiled *template.Template prototypes.
type templateLRUCache struct {
	mu       sync.Mutex
	capacity int
	items    map[string]*list.Element
	order    *list.List
}

func newTemplateLRUCache(capacity int) *templateLRUCache {
	if capacity <= 0 {
		capacity = defaultTemplateCacheCapacity
	}
	return &templateLRUCache{
		capacity: capacity,
		items:    make(map[string]*list.Element, capacity),
		order:    list.New(),
	}
}

func (c *templateLRUCache) get(key string) (*template.Template, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	elem, ok := c.items[key]
	if !ok {
		return nil, false
	}
	c.order.MoveToFront(elem)
	entry := elem.Value.(*templateCacheEntry)
	cloned, err := entry.tmpl.Clone()
	if err != nil {
		return nil, false
	}
	return cloned, true
}

func (c *templateLRUCache) put(key string, tmpl *template.Template) {
	c.mu.Lock()
	defer c.mu.Unlock()

	cloned, err := tmpl.Clone()
	if err != nil {
		return
	}

	if elem, ok := c.items[key]; ok {
		c.order.MoveToFront(elem)
		elem.Value.(*templateCacheEntry).tmpl = cloned
		return
	}

	if c.order.Len() >= c.capacity {
		back := c.order.Back()
		if back != nil {
			c.order.Remove(back)
			oldEntry := back.Value.(*templateCacheEntry)
			delete(c.items, oldEntry.key)
		}
	}

	elem := c.order.PushFront(&templateCacheEntry{
		key:  key,
		tmpl: cloned,
	})
	c.items[key] = elem
}

var globalTemplateCache = newTemplateLRUCache(defaultTemplateCacheCapacity)

func templateHashKey(tmplStr string) string {
	sum := sha256.Sum256([]byte(tmplStr))
	return hex.EncodeToString(sum[:])
}

var userHomeDir = os.UserHomeDir

// TemplateContext holds the evaluation context for templates.
type TemplateContext struct {
	Meta       map[string]any
	Attributes map[string]any
	Original   *TemplateContext
	Entries    []TemplateContext
}

// ToMap converts TemplateContext into a map supporting both lowercase and PascalCase keys
// so missingkey=error works with .meta.*, .attributes.*, .original.*, .output.*, and .entries.
func (tc TemplateContext) ToMap() map[string]any {
	metaMap := normalizeMetaMap(tc.Meta)

	attrsMap := make(map[string]any)
	for k, v := range tc.Attributes {
		attrsMap[k] = v
	}

	var origMap map[string]any
	if tc.Original != nil {
		origMap = tc.Original.ToMap()
	} else {
		origMap = map[string]any{
			"Meta":       metaMap,
			"meta":       metaMap,
			"_meta_":     metaMap,
			"Attributes": attrsMap,
			"attributes": attrsMap,
		}
	}

	outputMap := map[string]any{
		"Meta":       metaMap,
		"meta":       metaMap,
		"_meta_":     metaMap,
		"Attributes": attrsMap,
		"attributes": attrsMap,
	}

	var entriesList []any
	for _, entry := range tc.Entries {
		entriesList = append(entriesList, entry.ToMap())
	}

	return map[string]any{
		"Meta":       metaMap,
		"meta":       metaMap,
		"_meta_":     metaMap,
		"Attributes": attrsMap,
		"attributes": attrsMap,
		"Output":     outputMap,
		"output":     outputMap,
		"Original":   origMap,
		"original":   origMap,
		"Entries":    entriesList,
		"entries":    entriesList,
	}
}

func normalizeMetaMap(rawMeta map[string]any) map[string]any {
	if rawMeta == nil {
		return make(map[string]any)
	}
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
	return metaMap
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

		// Normalize meta/Meta/_meta_
		var rawMeta map[string]any
		if m, ok := out["Meta"].(map[string]any); ok {
			rawMeta = m
		} else if m, ok := out["meta"].(map[string]any); ok {
			rawMeta = m
		} else if m, ok := out["_meta_"].(map[string]any); ok {
			rawMeta = m
		}
		if rawMeta != nil {
			metaMap := normalizeMetaMap(rawMeta)
			out["Meta"] = metaMap
			out["meta"] = metaMap
			out["_meta_"] = metaMap
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

		hasMeta := (out["Meta"] != nil || out["meta"] != nil || out["_meta_"] != nil)
		hasAttrs := (out["Attributes"] != nil || out["attributes"] != nil)
		hasOrig := (out["Original"] != nil || out["original"] != nil)
		hasOutput := (out["Output"] != nil || out["output"] != nil)

		if hasMeta || hasAttrs || hasOrig || hasOutput {
			// Normalize original/Original
			var rawOrig map[string]any
			if o, ok := out["Original"].(map[string]any); ok {
				rawOrig = o
			} else if o, ok := out["original"].(map[string]any); ok {
				rawOrig = o
			}
			if rawOrig != nil {
				wrappedOrig := wrapTemplateData(rawOrig).(map[string]any)
				out["Original"] = wrappedOrig
				out["original"] = wrappedOrig
			} else {
				origMap := map[string]any{
					"meta":       out["meta"],
					"Meta":       out["Meta"],
					"_meta_":     out["_meta_"],
					"attributes": out["attributes"],
					"Attributes": out["Attributes"],
				}
				out["Original"] = origMap
				out["original"] = origMap
			}

			// Normalize output/Output
			outMap := map[string]any{
				"meta":       out["meta"],
				"Meta":       out["Meta"],
				"_meta_":     out["_meta_"],
				"attributes": out["attributes"],
				"Attributes": out["Attributes"],
			}
			out["Output"] = outMap
			out["output"] = outMap
		}

		return out
	default:
		return data
	}
}

func entryToTemplateContext(e provider.Entry, providerName, vault, path string) TemplateContext {
	meta := provider.BuildMetaMap(e, providerName, vault, path)

	origEntry := e
	if e.Original != nil {
		origEntry = *e.Original
	}
	origMeta := provider.BuildMetaMap(origEntry, providerName, vault, path)

	return TemplateContext{
		Meta:       meta,
		Attributes: e.Attributes,
		Original: &TemplateContext{
			Meta:       origMeta,
			Attributes: origEntry.Attributes,
		},
	}
}

// execInvocationError wraps an error originating inside the exec template function
// to sanitize the error message and avoid leaking raw secret arguments from template execution.
type execInvocationError struct {
	err error
}

func (e *execInvocationError) Error() string {
	return e.err.Error()
}

func (e *execInvocationError) Unwrap() error {
	return e.err
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
		"tryGet": func(uris ...string) string {
			for _, uri := range uris {
				if uri == "" {
					continue
				}
				val, err := te.resolver.resolveSingleURI(ctx, uri, depth+1, configKey)
				if err == nil && val != "" {
					return val
				}
			}
			return ""
		},
		"default":  templateDefault,
		"coalesce": templateCoalesce,
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
			results, err := te.resolver.searchFunc(ctx, query, nil, depth+1)
			if err != nil {
				return nil, err
			}
			var tcs []map[string]any
			for _, res := range results {
				tcs = append(tcs, entryToTemplateContext(res.Entry, res.Provider, res.Vault, res.Path).ToMap())
			}
			return tcs, nil
		},
		"toEnv":    toEnv,
		"toKv":     toEnv,
		"toJSON":   toJSON,
		"toJson":   toJSON,
		"json":     toJSON,
		"fromJSON": fromJSON,
		"fromJson": fromJSON,
		"toYAML":   toYAML,
		"toYaml":   toYAML,
		"yaml":     toYAML,
		"fromYAML": fromYAML,
		"fromYaml": fromYAML,
		"upper":    strings.ToUpper,
		"lower":    strings.ToLower,
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
				data, err := os.ReadFile(expanded) //nolint:gosec // path comes from user-owned config / template author
				if err != nil {
					return "", fmt.Errorf("failed to read template file %q for %q: %w", tCfg.Path, name, err)
				}
				tmplContent = string(data)
			}
			return te.Render(ctx, tmplContent, tmplCtx, depth+1, configKey)
		},
		// Trust boundary: template authors are the config owner; paths are unrestricted by design.
		"includeFile": func(path string, tmplCtx any) (string, error) {
			cfg := te.resolver.providers.Config()
			var basePath string
			if cfg != nil {
				basePath = cfg.ConfigPath
			}
			expanded := expandPath(path, basePath)
			data, err := os.ReadFile(expanded) //nolint:gosec // path comes from user-owned config / template author
			if err != nil {
				return "", err
			}
			return te.Render(ctx, string(data), tmplCtx, depth+1, configKey)
		},
		"shquote":    shquote,
		"shellQuote": shquote,
		"exec": func(name string, args ...any) (string, error) {
			if strings.TrimSpace(name) == "" {
				return "", fmt.Errorf("exec: executable name cannot be empty")
			}
			if strings.IndexByte(name, 0) != -1 {
				return "", fmt.Errorf("exec: executable name contains null byte")
			}
			binPath, err := exec.LookPath(name)
			if err != nil {
				if errors.Is(err, exec.ErrDot) {
					return "", fmt.Errorf("exec: execution of relative command %q from PATH is blocked due to security risks", name)
				}
				return "", fmt.Errorf("exec: command %q not found: %w", name, err)
			}
			if isWindowsBatchFile(binPath) {
				return "", fmt.Errorf("exec: execution of batch file %q is blocked due to security risks", binPath)
			}
			strArgs := make([]string, len(args))
			for i, arg := range args {
				strArg := fmt.Sprintf("%v", arg)
				if strings.IndexByte(strArg, 0) != -1 {
					return "", fmt.Errorf("exec: argument at index %d contains null byte", i)
				}
				strArgs[i] = strArg
			}
			const defaultExecTimeout = 30 * time.Second
			execCtx := ctx
			var cancel context.CancelFunc
			if _, hasDeadline := ctx.Deadline(); !hasDeadline {
				execCtx, cancel = context.WithTimeout(ctx, defaultExecTimeout)
				defer cancel()
			}
			cmd := exec.CommandContext(execCtx, binPath, strArgs...) //nolint:gosec // operator-configured executable name and discrete argv parameters
			var stdout, stderr bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr
			if err := cmd.Run(); err != nil {
				errMsg := strings.TrimSpace(stderr.String())
				if errMsg != "" {
					return "", &execInvocationError{err: fmt.Errorf("exec: command %q failed: %s", name, errMsg)}
				}
				return "", &execInvocationError{err: fmt.Errorf("exec: command %q failed: %w", name, err)}
			}
			out := strings.TrimRight(stdout.String(), "\r\n")
			return out, nil
		},
	}

	cacheKey := templateHashKey(tmplStr)
	tmpl, hit := globalTemplateCache.get(cacheKey)
	if hit {
		tmpl.Funcs(funcs)
	} else {
		parsed, err := template.New("tmpl").Option("missingkey=error").Funcs(funcs).Parse(tmplStr)
		if err != nil {
			// Try to provide a good error message for template errors
			keyPart := ""
			if configKey != "" {
				keyPart = fmt.Sprintf(" in configuration key %q", configKey)
			}
			return "", fmt.Errorf("template parsing failed%s: %w", keyPart, err)
		}
		globalTemplateCache.put(cacheKey, parsed)
		tmpl = parsed
	}

	execData := wrapTemplateData(data)
	buf := bufPool.Get().(*bytes.Buffer)
	buf.Reset()
	defer func() {
		b := buf.Bytes()
		clear(b[:cap(b)])
		if buf.Cap() <= 64*1024 {
			buf.Reset()
			bufPool.Put(buf)
		}
	}()

	if err := tmpl.Execute(buf, execData); err != nil {
		keyPart := ""
		if configKey != "" {
			keyPart = fmt.Sprintf(" in configuration key %q", configKey)
		}
		var execErr *execInvocationError
		if errors.As(err, &execErr) {
			return "", fmt.Errorf("template execution failed%s: %w", keyPart, execErr.err)
		}
		return "", fmt.Errorf("template execution failed%s: %w", keyPart, err)
	}
	return buf.String(), nil
}

func shquote(s string) string {
	// Safely quote a string in POSIX single-quotes.
	// Single quotes inside the string are closed, escaped as \', and reopened: '\''
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func toJSON(v any) (string, error) {
	b, err := json.Marshal(v)
	return string(b), err
}

func fromJSON(str string) (any, error) {
	var out any
	if err := json.Unmarshal([]byte(str), &out); err != nil {
		return nil, fmt.Errorf("fromJSON failed: %w", err)
	}
	return out, nil
}

func toYAML(v any) (string, error) {
	b, err := yaml.Marshal(v)
	return strings.TrimSpace(string(b)), err
}

func fromYAML(str string) (any, error) {
	var out any
	if err := yaml.Unmarshal([]byte(str), &out); err != nil {
		return nil, fmt.Errorf("fromYAML failed: %w", err)
	}
	return out, nil
}

// isEmpty determines if a value is consider empty for template helpers like default.
func isEmpty(v any) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.String:
		return rv.Len() == 0
	case reflect.Bool:
		return !rv.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return rv.Int() == 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return rv.Uint() == 0
	case reflect.Float32, reflect.Float64:
		return rv.Float() == 0
	case reflect.Slice, reflect.Map, reflect.Array:
		return rv.Len() == 0
	case reflect.Chan, reflect.Pointer, reflect.UnsafePointer, reflect.Func, reflect.Interface:
		return rv.IsNil()
	default:
		return false
	}
}

// templateDefault returns the first non-empty value in given, or fallback if all are empty.
// In pipelines like {{ given | default fallback }}, Go's text/template passes pipeline as the last argument,
// so given will contain [pipelineVal].
func templateDefault(fallback any, given ...any) any {
	if len(given) == 0 {
		return fallback
	}
	for _, val := range given {
		if !isEmpty(val) {
			return val
		}
	}
	return fallback
}

// templateCoalesce returns the first non-empty string argument.
func templateCoalesce(vals ...any) string {
	for _, val := range vals {
		if val == nil {
			continue
		}
		if s, ok := val.(string); ok {
			if s != "" {
				return s
			}
			continue
		}
		strVal := fmt.Sprintf("%v", val)
		if strVal != "" {
			return strVal
		}
	}
	return ""
}

func toEnv(m map[string]any) (string, error) {
	var ks []string
	for k := range m {
		if strings.EqualFold(k, "title") || strings.EqualFold(k, "tags") || strings.EqualFold(k, "uuid") || strings.EqualFold(k, "group") ||
			strings.EqualFold(k, "meta") || strings.EqualFold(k, "entries") || strings.EqualFold(k, "attributes") ||
			strings.EqualFold(k, "original") || strings.EqualFold(k, "output") {
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
		fmt.Fprintf(&sb, "%s=%s\n", k, valStr)
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

func isWindowsBatchFile(binPath string) bool {
	if runtime.GOOS != "windows" {
		return false
	}
	cleanBinary := strings.TrimRight(binPath, ". ")
	ext := strings.ToLower(filepath.Ext(cleanBinary))
	return ext == ".bat" || ext == ".cmd"
}
