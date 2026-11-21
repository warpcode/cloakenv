package engine

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"regexp"
	"slices"
	"strings"
	texttemplate "text/template"

	"github.com/warpcode/cloakenv/internal/config"
	"github.com/warpcode/cloakenv/internal/provider"
	"github.com/warpcode/cloakenv/internal/utils"
)

// SourceEntry is a retrieved raw entry with deterministic sort keys (Path, Title).
type SourceEntry struct {
	Vault string
	Path  string
	Title string
	Tags  []string
	Raw   map[string]any
}

// MatchedEntrySummary captures secret-free metadata and generated keys for inspection/auditing.
type MatchedEntrySummary struct {
	Vault      string
	Path       string
	Title      string
	Tags       []string
	OutputKeys []string
	Traces     []RuleTrace
}

// EnvBuildResult contains the flattened environment map plus secret-free build metadata.
type EnvBuildResult struct {
	Env     map[string]string
	Sources []MatchedEntrySummary
}

// TemplateRenderResult contains rendered content, driver metadata, post-rules entries,
// and secret-free match summaries.
type TemplateRenderResult struct {
	Driver  string
	Content string
	Entries []map[string]string
	Sources []MatchedEntrySummary
}

// VaultEntryFetcher retrieves all raw entries for a vault name.
type VaultEntryFetcher interface {
	FetchEntries(ctx context.Context, vaultName string) ([]SourceEntry, error)
}

type providerManagerEntryFetcher struct {
	pm *ProviderManager
}

func (f *providerManagerEntryFetcher) FetchEntries(ctx context.Context, vaultName string) ([]SourceEntry, error) {
	if f.pm == nil {
		return nil, fmt.Errorf("provider manager is nil")
	}
	p, _, err := f.pm.GetProvider(ctx, vaultName)
	if err != nil {
		return nil, err
	}
	sp, ok := p.(provider.SearchableProvider)
	if !ok {
		return nil, fmt.Errorf("vault %q (provider %q) does not support entry listing", vaultName, p.Scheme())
	}
	results, err := sp.Search(ctx, provider.SearchQuery{})
	if err != nil {
		return nil, fmt.Errorf("failed to query vault %q: %w", vaultName, err)
	}
	out := make([]SourceEntry, 0, len(results))
	for _, r := range results {
		raw := EntryToRawMap(r.Entry, p.Scheme(), vaultName, r.Path)
		title := r.Entry.Title
		if title == "" {
			if t, ok := raw["title"].(string); ok {
				title = t
			}
		}
		out = append(out, SourceEntry{
			Vault: vaultName,
			Path:  r.Path,
			Title: title,
			Tags:  slices.Clone(r.Entry.Tags),
			Raw:   raw,
		})
	}
	return out, nil
}

// EvaluateQueryPredicate evaluates a Go text/template boolean predicate against rawEntry.
// Selection is strictly true iff trimmed template output equals "true".
// Template parse or execution errors (including missingkey=error) are returned directly.
func EvaluateQueryPredicate(query string, rawEntry map[string]any) (bool, error) {
	if strings.TrimSpace(query) == "" {
		return true, nil
	}
	t, err := texttemplate.New("query").Funcs(ruleTemplateFuncs()).Option("missingkey=error").Parse(query)
	if err != nil {
		return false, fmt.Errorf("invalid query template: %w", err)
	}
	buf := bufPool.Get().(*bytes.Buffer)
	buf.Reset()
	defer bufPool.Put(buf)

	if err := t.Execute(buf, rawEntry); err != nil {
		return false, fmt.Errorf("query execution failed: %w", err)
	}
	return strings.TrimSpace(buf.String()) == "true", nil
}

// EnvsBuilder resolves named `envs:` configurations into flat map[string]string maps.
type EnvsBuilder struct {
	cfg     *config.Config
	fetcher VaultEntryFetcher
	rules   *RuleEngine
}

// NewEnvsBuilder constructs an EnvsBuilder backed by ProviderManager and RuleEngine.
func NewEnvsBuilder(cfg *config.Config, pm *ProviderManager, rules *RuleEngine) *EnvsBuilder {
	if rules == nil {
		rules = NewRuleEngine()
	}
	return &EnvsBuilder{
		cfg:     cfg,
		fetcher: &providerManagerEntryFetcher{pm: pm},
		rules:   rules,
	}
}

// NewEnvsBuilderWithFetcher constructs an EnvsBuilder using a custom VaultEntryFetcher (useful in tests).
func NewEnvsBuilderWithFetcher(cfg *config.Config, fetcher VaultEntryFetcher, rules *RuleEngine) *EnvsBuilder {
	if rules == nil {
		rules = NewRuleEngine()
	}
	return &EnvsBuilder{
		cfg:     cfg,
		fetcher: fetcher,
		rules:   rules,
	}
}

// Build resolves a named env from cfg.Envs into a deterministic map[string]string
// with last-entry-wins precedence.
func (b *EnvsBuilder) Build(ctx context.Context, envName string) (map[string]string, error) {
	res, err := b.BuildDetailed(ctx, envName)
	if err != nil {
		return nil, err
	}
	return res.Env, nil
}

// BuildDetailed resolves a named env and also returns secret-free metadata for inspection.
func (b *EnvsBuilder) BuildDetailed(ctx context.Context, envName string) (*EnvBuildResult, error) {
	if b.cfg == nil || b.cfg.Envs == nil {
		return nil, fmt.Errorf("env %q is not defined", envName)
	}
	sources, ok := b.cfg.Envs[envName]
	if !ok {
		return nil, fmt.Errorf("env %q is not defined", envName)
	}

	entries, summaries, err := resolveSources(ctx, sources, utils.KeyFormatEnv, b.fetcher, b.rules)
	if err != nil {
		return nil, fmt.Errorf("env %q: %w", envName, err)
	}

	flat := make(map[string]string)
	for _, entryMap := range entries {
		maps.Copy(flat, entryMap)
	}

	return &EnvBuildResult{
		Env:     flat,
		Sources: summaries,
	}, nil
}

// TemplatesRenderer renders named `templates:` configurations using drivers
// "template", "json", "yaml", and "dotenv".
type TemplatesRenderer struct {
	cfg     *config.Config
	fetcher VaultEntryFetcher
	rules   *RuleEngine
	envs    *EnvsBuilder
}

// NewTemplatesRenderer constructs a TemplatesRenderer backed by ProviderManager, RuleEngine, and EnvsBuilder.
func NewTemplatesRenderer(cfg *config.Config, pm *ProviderManager, rules *RuleEngine, envs *EnvsBuilder) *TemplatesRenderer {
	if rules == nil {
		rules = NewRuleEngine()
	}
	fetcher := &providerManagerEntryFetcher{pm: pm}
	if envs == nil {
		envs = NewEnvsBuilderWithFetcher(cfg, fetcher, rules)
	}
	return &TemplatesRenderer{
		cfg:     cfg,
		fetcher: fetcher,
		rules:   rules,
		envs:    envs,
	}
}

// NewTemplatesRendererWithFetcher constructs a TemplatesRenderer with a custom VaultEntryFetcher (for testing).
func NewTemplatesRendererWithFetcher(cfg *config.Config, fetcher VaultEntryFetcher, rules *RuleEngine, envs *EnvsBuilder) *TemplatesRenderer {
	if rules == nil {
		rules = NewRuleEngine()
	}
	if envs == nil {
		envs = NewEnvsBuilderWithFetcher(cfg, fetcher, rules)
	}
	return &TemplatesRenderer{
		cfg:     cfg,
		fetcher: fetcher,
		rules:   rules,
		envs:    envs,
	}
}

// Render executes a named template generator and returns its rendered output string.
func (r *TemplatesRenderer) Render(ctx context.Context, templateName string) (string, error) {
	res, err := r.RenderDetailed(ctx, templateName)
	if err != nil {
		return "", err
	}
	return res.Content, nil
}

// RenderDetailed executes a named template generator and returns rendered output plus secret-free metadata.
func (r *TemplatesRenderer) RenderDetailed(ctx context.Context, templateName string) (*TemplateRenderResult, error) {
	if r.cfg == nil || r.cfg.Templates == nil {
		return nil, fmt.Errorf("template %q is not defined", templateName)
	}
	tCfg, ok := r.cfg.Templates[templateName]
	if !ok {
		return nil, fmt.Errorf("template %q is not defined", templateName)
	}

	driver := tCfg.EffectiveDriver()
	defaultKF := utils.KeyFormatNone
	if driver == "dotenv" {
		defaultKF = utils.KeyFormatEnv
	}

	entries, summaries, err := resolveSources(ctx, tCfg.Vaults, defaultKF, r.fetcher, r.rules)
	if err != nil {
		return nil, fmt.Errorf("template %q: %w", templateName, err)
	}

	var content string
	switch driver {
	case "template":
		tmplContent := tCfg.Template
		if tmplContent == "" && tCfg.Path != "" {
			expanded := expandPath(tCfg.Path, r.cfg.ConfigPath)
			data, readErr := os.ReadFile(expanded) //nolint:gosec // operator-configured template file path
			if readErr != nil {
				return nil, fmt.Errorf("template %q: failed to read template path %q: %w", templateName, tCfg.Path, readErr)
			}
			tmplContent = string(data)
		}
		if tmplContent == "" {
			return nil, fmt.Errorf("template %q: template content is empty", templateName)
		}
		t, parseErr := texttemplate.New(templateName).Funcs(ruleTemplateFuncs()).Option("missingkey=error").Parse(tmplContent)
		if parseErr != nil {
			return nil, fmt.Errorf("template %q: parse error: %w", templateName, parseErr)
		}
		buf := bufPool.Get().(*bytes.Buffer)
		buf.Reset()
		defer bufPool.Put(buf)

		tmplData := map[string]any{
			"entries": entries,
		}
		if execErr := t.Execute(buf, tmplData); execErr != nil {
			return nil, fmt.Errorf("template %q: execution error: %w", templateName, execErr)
		}
		content = buf.String()

	case "json":
		b, marshalErr := json.MarshalIndent(entries, "", "  ")
		if marshalErr != nil {
			return nil, fmt.Errorf("template %q: json marshal error: %w", templateName, marshalErr)
		}
		content = string(b)

	case "yaml":
		yStr, marshalErr := utils.MarshalYAMLString(entries)
		if marshalErr != nil {
			return nil, fmt.Errorf("template %q: yaml marshal error: %w", templateName, marshalErr)
		}
		content = yStr

	case "dotenv":
		flat := make(map[string]string)
		if tCfg.FromEnv != "" {
			envRes, envErr := r.envs.BuildDetailed(ctx, tCfg.FromEnv)
			if envErr != nil {
				return nil, fmt.Errorf("template %q: from_env %q: %w", templateName, tCfg.FromEnv, envErr)
			}
			maps.Copy(flat, envRes.Env)
			if len(envRes.Env) > 0 {
				entries = append([]map[string]string{maps.Clone(envRes.Env)}, entries...)
			}
			summaries = append(slices.Clone(envRes.Sources), summaries...)
		}
		for _, entryMap := range entries {
			maps.Copy(flat, entryMap)
		}
		formatted, fmtErr := formatDotenv(flat)
		if fmtErr != nil {
			return nil, fmt.Errorf("template %q: %w", templateName, fmtErr)
		}
		content = formatted

	default:
		return nil, fmt.Errorf("template %q: unsupported driver %q", templateName, driver)
	}

	return &TemplateRenderResult{
		Driver:  driver,
		Content: content,
		Entries: entries,
		Sources: summaries,
	}, nil
}

func resolveSources(
	ctx context.Context,
	sources []config.VaultSourceConfig,
	defaultKeyFormat string,
	fetcher VaultEntryFetcher,
	rules *RuleEngine,
) ([]map[string]string, []MatchedEntrySummary, error) {
	if fetcher == nil {
		return nil, nil, fmt.Errorf("entry fetcher is nil")
	}
	if rules == nil {
		rules = NewRuleEngine()
	}

	entriesOut := make([]map[string]string, 0)
	summaries := make([]MatchedEntrySummary, 0)

	for idx, src := range sources {
		if src.Vault == "" {
			return nil, nil, fmt.Errorf("source #%d: missing vault name", idx+1)
		}
		rawEntries, err := fetcher.FetchEntries(ctx, src.Vault)
		if err != nil {
			return nil, nil, fmt.Errorf("source #%d (vault %q): %w", idx+1, src.Vault, err)
		}

		matched := make([]SourceEntry, 0, len(rawEntries))
		for _, candidate := range rawEntries {
			ok, qErr := EvaluateQueryPredicate(src.Query, candidate.Raw)
			if qErr != nil {
				return nil, nil, fmt.Errorf("source #%d (vault %q): %w", idx+1, src.Vault, qErr)
			}
			if ok {
				matched = append(matched, candidate)
			}
		}

		// Deterministic ordering within a source: Path, Title, then UUID.
		slices.SortStableFunc(matched, func(a, b SourceEntry) int {
			return cmp.Or(
				cmp.Compare(a.Path, b.Path),
				cmp.Compare(a.Title, b.Title),
				cmp.Compare(sourceEntryUUID(a), sourceEntryUUID(b)),
			)
		})

		for _, entry := range matched {
			transformed, traces, applyErr := rules.ApplyWithTrace(entry.Raw, src.Rules, defaultKeyFormat)
			if applyErr != nil {
				return nil, nil, fmt.Errorf("source #%d (vault %q, entry %q): %w", idx+1, src.Vault, entry.Path, applyErr)
			}
			entriesOut = append(entriesOut, transformed)

			outKeys := make([]string, 0, len(transformed))
			for k := range transformed {
				outKeys = append(outKeys, k)
			}
			slices.Sort(outKeys)
			summaries = append(summaries, MatchedEntrySummary{
				Vault:      src.Vault,
				Path:       entry.Path,
				Title:      entry.Title,
				Tags:       slices.Clone(entry.Tags),
				OutputKeys: outKeys,
				Traces:     traces,
			})
		}
	}

	return entriesOut, summaries, nil
}

var dotenvKeyPattern = regexp.MustCompile("^[A-Za-z_][A-Za-z0-9_]*$")

func formatDotenv(flat map[string]string) (string, error) {
	if len(flat) == 0 {
		return "", nil
	}
	keys := make([]string, 0, len(flat))
	for k := range flat {
		if !dotenvKeyPattern.MatchString(k) {
			return "", fmt.Errorf("invalid dotenv key %q: must match ^[A-Za-z_][A-Za-z0-9_]*$", k)
		}
		keys = append(keys, k)
	}
	slices.Sort(keys)

	var sb strings.Builder
	for i, k := range keys {
		if i > 0 {
			sb.WriteByte('\n')
		}
		sb.WriteString(k)
		sb.WriteByte('=')
		sb.WriteString(formatDotenvValue(flat[k]))
	}
	return sb.String(), nil
}

func formatDotenvValue(v string) string {
	v = strings.ReplaceAll(v, `\`, `\\`)
	v = strings.ReplaceAll(v, `"`, `\"`)
	v = strings.ReplaceAll(v, "$", `\$`)
	v = strings.ReplaceAll(v, "`", "\\`")
	v = strings.ReplaceAll(v, "\n", `\n`)
	v = strings.ReplaceAll(v, "\r", `\r`)
	v = strings.ReplaceAll(v, "\t", `\t`)
	return `"` + v + `"`
}

func sourceEntryUUID(e SourceEntry) string {
	if e.Raw == nil {
		return ""
	}
	v, ok := e.Raw["uuid"]
	if !ok || v == nil {
		return ""
	}
	if str, ok := v.(string); ok {
		return str
	}
	return fmt.Sprint(v)
}
