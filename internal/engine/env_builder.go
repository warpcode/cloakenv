package engine

import (
	"context"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"
	"unicode"

	"github.com/warpcode/cloakenv/internal/config"
)

// EnvBuilder constructs command environments and rendered templates from declarative
// autoload rules, envs definitions, and template definitions.
type EnvBuilder struct {
	config            *config.Config
	envsBuilder       *EnvsBuilder
	templatesRenderer *TemplatesRenderer
	ruleEngine        *RuleEngine
}

// NewEnvBuilder creates a new EnvBuilder wired to declarative pipeline components.
func NewEnvBuilder(cfg *config.Config, eb *EnvsBuilder, tr *TemplatesRenderer, re *RuleEngine) *EnvBuilder {
	return &EnvBuilder{
		config:            cfg,
		envsBuilder:       eb,
		templatesRenderer: tr,
		ruleEngine:        re,
	}
}

func parseParentEnv(environ []string) map[string]string {
	parentEnvMap := make(map[string]string)
	for _, envStr := range environ {
		if strings.IndexByte(envStr, 0) != -1 {
			continue
		}
		k, v, ok := strings.Cut(envStr, "=")
		if !ok || k == "" {
			continue
		}
		if strings.IndexFunc(k, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) != -1 {
			continue
		}
		parentEnvMap[k] = v
	}
	return parentEnvMap
}

func getParentEnv() map[string]string {
	return parseParentEnv(os.Environ())
}

// EnvConfig holds configuration parameters for building a command environment block.
type EnvConfig struct {
	CmdArgs         []string
	EmptyEnv        bool
	RedactedPreview bool
}

// formatEnvMap converts an environment map to a slice of "KEY=VALUE" strings with deterministic sorting.
func formatEnvMap(envMap map[string]string) []string {
	keys := make([]string, 0, len(envMap))
	for k := range envMap {
		keys = append(keys, k)
	}
	slices.Sort(keys)

	for i, k := range keys {
		keys[i] = k + "=" + envMap[k]
	}
	return keys
}

// resolveAutoloadEnvs resolves an ordered slice of AutoloadEnvItem (named envs and inline maps)
// into a flat map[string]string where later items override earlier items and inline template
// values can reference previously resolved variables.
func (eb *EnvBuilder) resolveAutoloadEnvs(ctx context.Context, items []config.AutoloadEnvItem) (map[string]string, error) {
	resolved := make(map[string]string)
	if len(items) == 0 {
		return resolved, nil
	}
	re := eb.ruleEngine
	if re == nil {
		re = NewRuleEngine()
	}
	for idx, item := range items {
		if item.Name != "" {
			if eb.envsBuilder == nil {
				return nil, fmt.Errorf("autoload env #%d (%q): envs builder is not configured", idx+1, item.Name)
			}
			built, err := eb.envsBuilder.Build(ctx, item.Name)
			if err != nil {
				return nil, fmt.Errorf("autoload env #%d (%q): %w", idx+1, item.Name, err)
			}
			maps.Copy(resolved, built)
			continue
		}
		if len(item.Inline) > 0 {
			keys := make([]string, 0, len(item.Inline))
			for k := range item.Inline {
				keys = append(keys, k)
			}
			slices.Sort(keys)
			tmplCtx := make(map[string]any, len(resolved))
			for k, v := range resolved {
				tmplCtx[k] = v
			}
			for _, k := range keys {
				if k == "" {
					continue
				}
				rawVal := item.Inline[k]
				val := rawVal
				if strings.Contains(rawVal, "{{") {
					var err error
					val, err = re.evalTemplate(fmt.Sprintf("autoload.env.%s", k), rawVal, tmplCtx)
					if err != nil {
						return nil, fmt.Errorf("autoload env #%d key %q: %w", idx+1, k, err)
					}
				}
				resolved[k] = val
				tmplCtx[k] = val
			}
		}
	}
	return resolved, nil
}

// RenderedTemplateDelivery holds a rendered autoload template along with its
// evaluated target delivery metadata and extracted secret values for masking.
type RenderedTemplateDelivery struct {
	TemplateName string
	Driver       string
	EnvVar       string
	EnvPathVar   string
	Path         string
	Persist      bool
	Content      string
	Secrets      []string
}

// resolveAutoloadTemplates renders each autoload template and evaluates any
// templated target path against the resolved autoload envs map.
const redactedPathPlaceholder = "<REDACTED>"

func (eb *EnvBuilder) resolveAutoloadTemplates(ctx context.Context, templates []config.AutoloadTemplateConfig, resolvedEnvs map[string]string, redactedPreview bool) ([]RenderedTemplateDelivery, error) {
	if len(templates) == 0 {
		return nil, nil
	}
	if eb.templatesRenderer == nil {
		return nil, fmt.Errorf("autoload templates renderer is not configured")
	}
	re := eb.ruleEngine
	if re == nil {
		re = NewRuleEngine()
	}
	envCtx := make(map[string]any, len(resolvedEnvs))
	for k, v := range resolvedEnvs {
		if redactedPreview {
			envCtx[k] = redactedPathPlaceholder
		} else {
			envCtx[k] = v
		}
	}

	out := make([]RenderedTemplateDelivery, 0, len(templates))
	for idx, tpl := range templates {
		res, err := eb.templatesRenderer.RenderDetailed(ctx, tpl.Template)
		if err != nil {
			return nil, fmt.Errorf("autoload template #%d (%q): %w", idx+1, tpl.Template, err)
		}
		resolvedPath := tpl.Path
		if resolvedPath != "" && strings.Contains(resolvedPath, "{{") {
			resolvedPath, err = re.evalTemplate(fmt.Sprintf("autoload.template.%s.path", tpl.Template), resolvedPath, envCtx)
			if err != nil {
				return nil, fmt.Errorf("autoload template #%d (%q) path evaluation failed: %w", idx+1, tpl.Template, err)
			}
		}
		if strings.TrimSpace(tpl.Path) != "" && strings.TrimSpace(resolvedPath) == "" {
			return nil, fmt.Errorf("autoload template #%d (%q): evaluated path is empty", idx+1, tpl.Template)
		}
		var secrets []string
		if res.Content != "" {
			secrets = append(secrets, res.Content)
		}
		for _, entry := range res.Entries {
			for _, v := range entry {
				if v != "" {
					secrets = append(secrets, v)
				}
			}
		}
		out = append(out, RenderedTemplateDelivery{
			TemplateName: tpl.Template,
			Driver:       res.Driver,
			EnvVar:       tpl.Env,
			EnvPathVar:   tpl.EnvPath,
			Path:         resolvedPath,
			Persist:      tpl.Persist,
			Content:      res.Content,
			Secrets:      secrets,
		})
	}
	return out, nil
}

// CommandEnvDetails contains the execution details for a command environment.
type CommandEnvDetails struct {
	Command           []string
	RawCommand        []string
	Env               []string
	InjectedKeys      []string
	ResolvedEnvs      map[string]string
	Templates         []config.AutoloadTemplateConfig
	RenderedTemplates []RenderedTemplateDelivery
	MaskSecrets       []string
}

// BuildEnvForCommandDetails constructs the full environment block, evaluates autoload rules,
// and returns execution details including the sorted list of injected variable keys.
// Child command arguments are passed through verbatim without URI or template evaluation.
func (eb *EnvBuilder) BuildEnvForCommandDetails(ctx context.Context, cfg EnvConfig) (*CommandEnvDetails, error) {
	autoEval, err := EvaluateAutoload(eb.config, cfg.CmdArgs)
	if err != nil {
		return nil, err
	}
	currentCmdArgs := autoEval.Command

	autoEnvs, err := eb.resolveAutoloadEnvs(ctx, autoEval.Envs)
	if err != nil {
		return nil, err
	}

	renderedTemplates, err := eb.resolveAutoloadTemplates(ctx, autoEval.Templates, autoEnvs, cfg.RedactedPreview)
	if err != nil {
		return nil, err
	}

	var finalEnv map[string]string
	if !cfg.EmptyEnv {
		finalEnv = getParentEnv()
	} else {
		finalEnv = make(map[string]string)
	}

	injectedKeysSet := make(map[string]bool)
	var maskSecrets []string

	for k, v := range autoEnvs {
		finalEnv[k] = v
		injectedKeysSet[k] = true
		if v != "" {
			maskSecrets = append(maskSecrets, v)
		}
	}

	for _, rt := range renderedTemplates {
		if rt.EnvVar != "" {
			finalEnv[rt.EnvVar] = rt.Content
			injectedKeysSet[rt.EnvVar] = true
		}
		if rt.EnvPathVar != "" {
			injectedKeysSet[rt.EnvPathVar] = true
		}
		maskSecrets = append(maskSecrets, rt.Secrets...)
	}

	injectedKeys := make([]string, 0, len(injectedKeysSet))
	for k := range injectedKeysSet {
		injectedKeys = append(injectedKeys, k)
	}
	slices.Sort(injectedKeys)

	result := formatEnvMap(finalEnv)

	if len(currentCmdArgs) == 0 {
		currentCmdArgs = cfg.CmdArgs
	}
	cmdCopy := slices.Clone(currentCmdArgs)

	return &CommandEnvDetails{
		Command:           cmdCopy,
		RawCommand:        slices.Clone(cmdCopy),
		Env:               result,
		InjectedKeys:      injectedKeys,
		ResolvedEnvs:      autoEnvs,
		Templates:         autoEval.Templates,
		RenderedTemplates: renderedTemplates,
		MaskSecrets:       maskSecrets,
	}, nil
}

// BuildEnvForCommand constructs the full environment block and evaluates config autoload rules.
func (eb *EnvBuilder) BuildEnvForCommand(ctx context.Context, cfg EnvConfig) ([]string, []string, error) {
	details, err := eb.BuildEnvForCommandDetails(ctx, cfg)
	if err != nil {
		return nil, nil, err
	}
	return details.Command, details.Env, nil
}
