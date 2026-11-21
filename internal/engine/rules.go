package engine

import (
	"bytes"
	"fmt"
	"maps"
	pathpkg "path"
	"regexp"
	"slices"
	"strconv"
	"strings"
	texttemplate "text/template"

	"github.com/warpcode/cloakenv/internal/config"
	"github.com/warpcode/cloakenv/internal/provider"
	"github.com/warpcode/cloakenv/internal/utils"
)

// RuleTrace records the deterministic, secret-free key transformation trace for a single rule.
type RuleTrace struct {
	RuleIndex int
	RuleType  string
	KeysAdded []string
}

// RuleEngine executes ordered entry transformation rules ("pass", "match", "set")
// against raw entry maps without mutating the source entry.
type RuleEngine struct {
	funcs texttemplate.FuncMap
}

// NewRuleEngine creates a RuleEngine initialized with standard safe template helper functions.
func NewRuleEngine() *RuleEngine {
	return &RuleEngine{
		funcs: ruleTemplateFuncs(),
	}
}

// EntryToRawMap adapts a legacy provider.Entry struct into a flat raw field map
// containing top-level attributes and canonical metadata fields (title, path, uuid, tags, group, vault, provider).
func EntryToRawMap(entry provider.Entry, providerName, vaultName, entryPath string) map[string]any {
	raw := make(map[string]any, len(entry)+3)
	for k, v := range entry {
		raw[k] = provider.CloneValue(v)
	}
	if _, exists := raw["title"]; !exists {
		raw["title"] = entry.Title()
	}
	if _, exists := raw["path"]; !exists {
		raw["path"] = entryPath
	}
	if _, exists := raw["uuid"]; !exists {
		raw["uuid"] = entry.UUID()
	}
	if _, exists := raw["tags"]; !exists {
		raw["tags"] = []string{}
	}
	if _, exists := raw["vault"]; !exists && vaultName != "" {
		raw["vault"] = vaultName
	}
	if _, exists := raw["provider"]; !exists && providerName != "" {
		raw["provider"] = providerName
	}
	return raw
}

// Apply transforms a raw entry map using ordered rules and defaultKeyFormat ("env", "snake", "kebab", or "none").
// When rules is empty, a single default "pass" rule runs with defaultKeyFormat.
// When rules is non-empty, only the listed rules execute in order, with strict last-rule-wins precedence.
func (re *RuleEngine) Apply(entry map[string]any, rules []config.RuleConfig, defaultKeyFormat string) (map[string]string, error) {
	out, _, err := re.ApplyWithTrace(entry, rules, defaultKeyFormat)
	return out, err
}

// ApplyWithTrace executes rules identically to Apply and also returns key-level rule traces
// without recording plaintext secret values.
func (re *RuleEngine) ApplyWithTrace(entry map[string]any, rules []config.RuleConfig, defaultKeyFormat string) (map[string]string, []RuleTrace, error) {
	effectiveRules := rules
	if len(effectiveRules) == 0 {
		effectiveRules = []config.RuleConfig{{
			Type:      "pass",
			KeyFormat: defaultKeyFormat,
		}}
	}

	out := make(map[string]string)
	traces := make([]RuleTrace, 0, len(effectiveRules))

	sortedEntryKeys := sortedMapKeys(entry)

	for idx, rule := range effectiveRules {
		var added []string
		switch rule.Type {
		case "pass":
			var err error
			added, err = re.applyPass(entry, sortedEntryKeys, rule, defaultKeyFormat, out)
			if err != nil {
				return nil, nil, fmt.Errorf("rule #%d (pass): %w", idx+1, err)
			}
		case "match":
			var err error
			added, err = re.applyMatch(entry, sortedEntryKeys, rule, out)
			if err != nil {
				return nil, nil, fmt.Errorf("rule #%d (match): %w", idx+1, err)
			}
		case "set":
			var err error
			added, err = re.applySet(entry, rule, out)
			if err != nil {
				return nil, nil, fmt.Errorf("rule #%d (set): %w", idx+1, err)
			}
		default:
			return nil, nil, fmt.Errorf("rule #%d: unknown rule type %q", idx+1, rule.Type)
		}
		traces = append(traces, RuleTrace{
			RuleIndex: idx,
			RuleType:  rule.Type,
			KeysAdded: added,
		})
	}

	return out, traces, nil
}

func (re *RuleEngine) applyPass(entry map[string]any, sortedKeys []string, rule config.RuleConfig, defaultKeyFormat string, out map[string]string) ([]string, error) {
	keyFmt := rule.KeyFormat
	if keyFmt == "" {
		keyFmt = defaultKeyFormat
	}
	if keyFmt == "" {
		keyFmt = utils.KeyFormatNone
	}

	includes := rule.Include
	if len(includes) == 0 {
		includes = []string{"*"}
	}

	var added []string
	for _, k := range sortedKeys {
		isReserved := isReservedMetadataKey(k)
		matched := false
		explicitlyMatched := false
		for _, pattern := range includes {
			if isCatchAllWildcard(pattern) {
				matched = true
				continue
			}
			ok, err := pathpkg.Match(pattern, k)
			if err != nil {
				return nil, fmt.Errorf("invalid include glob %q: %w", pattern, err)
			}
			if ok {
				matched = true
				explicitlyMatched = true
				break
			}
		}
		if !matched {
			continue
		}
		if isReserved && !explicitlyMatched {
			continue
		}

		excluded := false
		for _, ex := range rule.Exclude {
			ok, err := pathpkg.Match(ex, k)
			if err != nil {
				return nil, fmt.Errorf("invalid exclude glob %q: %w", ex, err)
			}
			if ok {
				excluded = true
				break
			}
		}
		if excluded {
			continue
		}

		valStr, err := utils.SerializeAttrValue(entry[k])
		if err != nil {
			return nil, fmt.Errorf("failed to serialize field %q: %w", k, err)
		}
		targetKey := utils.FormatKeyWithFormat(k, keyFmt)
		if targetKey == "" {
			continue
		}
		out[targetKey] = valStr
		added = append(added, targetKey)
	}
	return added, nil
}

func (re *RuleEngine) applyMatch(entry map[string]any, sortedKeys []string, rule config.RuleConfig, out map[string]string) ([]string, error) {
	keyRe := rule.CompiledMatch
	if keyRe == nil && rule.Match != "" {
		var err error
		keyRe, err = regexp.Compile(rule.Match)
		if err != nil {
			return nil, fmt.Errorf("invalid match regex %q: %w", rule.Match, err)
		}
	}
	valRe := rule.CompiledValueMatch
	if valRe == nil && rule.ValueMatch != "" {
		var err error
		valRe, err = regexp.Compile(rule.ValueMatch)
		if err != nil {
			return nil, fmt.Errorf("invalid value_match regex %q: %w", rule.ValueMatch, err)
		}
	}
	if keyRe == nil && valRe == nil {
		return nil, fmt.Errorf("match rule requires 'match' or 'value_match'")
	}
	if rule.Key == "" {
		return nil, fmt.Errorf("match rule missing required 'key'")
	}

	outputSnap := maps.Clone(out)
	var added []string

	for _, k := range sortedKeys {
		var keyCaps []string
		if keyRe != nil {
			keyCaps = keyRe.FindStringSubmatch(k)
			if keyCaps == nil {
				continue
			}
		}
		valStr, err := utils.SerializeAttrValue(entry[k])
		if err != nil {
			return nil, fmt.Errorf("failed to serialize field %q: %w", k, err)
		}
		var valCaps []string
		if valRe != nil {
			valCaps = valRe.FindStringSubmatch(valStr)
			if valCaps == nil {
				continue
			}
		}

		ctx := makeRuleTemplateContext(entry, outputSnap)
		ctx["value"] = valStr
		ctx["key"] = k

		keyPrimaryCaps := keyCaps
		if len(keyPrimaryCaps) == 0 {
			keyPrimaryCaps = valCaps
		}
		targetKey, err := re.evalMatchExpr("match.key", rule.Key, keyPrimaryCaps, ctx)
		if err != nil {
			return nil, err
		}
		if targetKey == "" {
			continue
		}

		targetVal := valStr
		if rule.Value != "" {
			valPrimaryCaps := valCaps
			if len(valPrimaryCaps) == 0 {
				valPrimaryCaps = keyCaps
			}
			targetVal, err = re.evalMatchExpr("match.value", rule.Value, valPrimaryCaps, ctx)
			if err != nil {
				return nil, err
			}
		}

		out[targetKey] = targetVal
		added = append(added, targetKey)
	}
	return added, nil
}

func (re *RuleEngine) applySet(entry map[string]any, rule config.RuleConfig, out map[string]string) ([]string, error) {
	if len(rule.Values) == 0 {
		return nil, nil
	}
	outputSnap := maps.Clone(out)
	ctx := makeRuleTemplateContext(entry, outputSnap)

	valKeys := make([]string, 0, len(rule.Values))
	for k := range rule.Values {
		valKeys = append(valKeys, k)
	}
	slices.Sort(valKeys)

	added := make([]string, 0, len(valKeys))
	for _, rawKey := range valKeys {
		rawVal := rule.Values[rawKey]
		targetKey := rawKey
		var err error
		if strings.Contains(targetKey, "{{") {
			targetKey, err = re.evalTemplate("set.key", targetKey, ctx)
			if err != nil {
				return nil, err
			}
		}
		if targetKey == "" {
			continue
		}

		targetVal := rawVal
		if strings.Contains(targetVal, "{{") {
			targetVal, err = re.evalTemplate("set.value", targetVal, ctx)
			if err != nil {
				return nil, err
			}
		}

		out[targetKey] = targetVal
		added = append(added, targetKey)
	}
	return added, nil
}

// evalMatchExpr evaluates a match rule's key or value expression without exposing
// captured runtime strings to Go text/template parsing (preventing template injection
// and parse failures when captured values contain literal "{{").
func (re *RuleEngine) evalMatchExpr(name, expr string, caps []string, ctx map[string]any) (string, error) {
	if !strings.Contains(expr, "{{") {
		return expandCaptureGroups(expr, caps), nil
	}
	exprCtx := make(map[string]any, len(ctx)+2)
	maps.Copy(exprCtx, ctx)
	exprCtx["captures"] = caps
	exprCtx["__captures"] = caps
	safeTmpl := config.RewriteCapturePlaceholdersForTemplate(expr, len(caps))
	return re.evalTemplate(name, safeTmpl, exprCtx)
}

func (re *RuleEngine) evalTemplate(name, tmplStr string, ctx map[string]any) (string, error) {
	t, err := texttemplate.New(name).Funcs(re.funcs).Option("missingkey=error").Parse(tmplStr)
	if err != nil {
		return "", fmt.Errorf("failed to parse template in %s: %w", name, err)
	}
	buf := bufPool.Get().(*bytes.Buffer)
	buf.Reset()
	defer bufPool.Put(buf)

	if err := t.Execute(buf, ctx); err != nil {
		return "", fmt.Errorf("failed to execute template in %s: %w", name, err)
	}
	return buf.String(), nil
}

func makeRuleTemplateContext(entry map[string]any, output map[string]string) map[string]any {
	ctx := make(map[string]any, len(entry)+2)
	for k, v := range entry {
		ctx[k] = v
	}
	outCopy := make(map[string]string, len(output))
	maps.Copy(outCopy, output)
	ctx["output"] = outCopy
	return ctx
}

func sortedMapKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

func isReservedMetadataKey(key string) bool {
	switch strings.ToLower(key) {
	case "title", "path", "uuid", "tags", "group", "vault", "provider":
		return true
	default:
		return false
	}
}

func isCatchAllWildcard(pattern string) bool {
	return pattern != "" && strings.Trim(pattern, "*") == ""
}

// expandCaptureGroups expands $1..$N and ${1}..${N} capture placeholders in tmpl
// without stripping unrelated $ characters or Go template syntax.
func expandCaptureGroups(tmpl string, matches []string) string {
	if len(matches) == 0 || !strings.ContainsRune(tmpl, '$') {
		return tmpl
	}
	var sb strings.Builder
	sb.Grow(len(tmpl))
	for i := 0; i < len(tmpl); i++ {
		if tmpl[i] != '$' || i+1 >= len(tmpl) {
			sb.WriteByte(tmpl[i])
			continue
		}
		if tmpl[i+1] == '{' {
			end := strings.IndexByte(tmpl[i+2:], '}')
			if end > 0 {
				idxStr := tmpl[i+2 : i+2+end]
				if idx, err := strconv.Atoi(idxStr); err == nil && idx >= 0 && idx < len(matches) {
					sb.WriteString(matches[idx])
					i += 2 + end
					continue
				}
			}
			sb.WriteByte(tmpl[i])
			continue
		}
		if tmpl[i+1] >= '0' && tmpl[i+1] <= '9' {
			j := i + 1
			for j < len(tmpl) && tmpl[j] >= '0' && tmpl[j] <= '9' {
				j++
			}
			if idx, err := strconv.Atoi(tmpl[i+1 : j]); err == nil && idx >= 0 && idx < len(matches) {
				sb.WriteString(matches[idx])
				i = j - 1
				continue
			}
		}
		sb.WriteByte(tmpl[i])
	}
	return sb.String()
}

func hasTagFunc(tag string, tags any) bool {
	if tag == "" || tags == nil {
		return false
	}
	parsed := utils.ParseTags(tags)
	for _, candidate := range parsed {
		if strings.EqualFold(candidate, tag) {
			return true
		}
	}
	return false
}

func ruleTemplateFuncs() texttemplate.FuncMap {
	return texttemplate.FuncMap{
		"hasTag":     hasTagFunc,
		"default":    templateDefault,
		"coalesce":   templateCoalesce,
		"toJSON":     toJSON,
		"toJson":     toJSON,
		"fromJSON":   fromJSON,
		"fromJson":   fromJSON,
		"toYAML":     toYAML,
		"toYaml":     toYAML,
		"fromYAML":   fromYAML,
		"fromYaml":   fromYAML,
		"toEnv":      toEnv,
		"toKv":       toEnv,
		"upper":      strings.ToUpper,
		"lower":      strings.ToLower,
		"env":        func(s string) string { return utils.FormatKeyWithFormat(s, utils.KeyFormatEnv) },
		"snake":      func(s string) string { return utils.FormatKeyWithFormat(s, utils.KeyFormatSnake) },
		"kebab":      func(s string) string { return utils.FormatKeyWithFormat(s, utils.KeyFormatKebab) },
		"shquote":    shquote,
		"shellQuote": shquote,
		"keys": func(m any) ([]string, error) {
			if m == nil {
				return []string{}, nil
			}
			switch mp := m.(type) {
			case map[string]any:
				ks := make([]string, 0, len(mp))
				for k := range mp {
					ks = append(ks, k)
				}
				slices.Sort(ks)
				return ks, nil
			case map[string]string:
				ks := make([]string, 0, len(mp))
				for k := range mp {
					ks = append(ks, k)
				}
				slices.Sort(ks)
				return ks, nil
			default:
				return nil, fmt.Errorf("keys requires a map, got %T", m)
			}
		},
		"values": func(m any) ([]any, error) {
			if m == nil {
				return []any{}, nil
			}
			switch mp := m.(type) {
			case map[string]any:
				ks := make([]string, 0, len(mp))
				for k := range mp {
					ks = append(ks, k)
				}
				slices.Sort(ks)
				vs := make([]any, 0, len(ks))
				for _, k := range ks {
					vs = append(vs, mp[k])
				}
				return vs, nil
			case map[string]string:
				ks := make([]string, 0, len(mp))
				for k := range mp {
					ks = append(ks, k)
				}
				slices.Sort(ks)
				vs := make([]any, 0, len(ks))
				for _, k := range ks {
					vs = append(vs, mp[k])
				}
				return vs, nil
			default:
				return nil, fmt.Errorf("values requires a map, got %T", m)
			}
		},
		"join": func(sep string, v any) (string, error) {
			if v == nil {
				return "", nil
			}
			switch slice := v.(type) {
			case []string:
				return strings.Join(slice, sep), nil
			case []any:
				strs := make([]string, len(slice))
				for i, item := range slice {
					strs[i] = fmt.Sprintf("%v", item)
				}
				return strings.Join(strs, sep), nil
			default:
				return "", fmt.Errorf("join requires a slice, got %T", v)
			}
		},
	}
}
