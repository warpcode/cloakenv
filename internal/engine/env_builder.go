package engine

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/warpcode/cloakenv/internal/config"
	"github.com/warpcode/cloakenv/internal/provider"
	"github.com/warpcode/cloakenv/internal/utils"
)

// EnvResolver defines the interface for resolving URIs needed by EnvBuilder.
type EnvResolver interface {
	Resolve(ctx context.Context, uri string) (string, error)
	ResolveWithKey(ctx context.Context, uri string, configKey string) (string, error)
	GetEntry(ctx context.Context, uri string) (provider.Entry, error)
}

// EnvBuilder handles the construction of the environment block.
type EnvBuilder struct {
	config   *config.Config
	resolver *Resolver
}

// NewEnvBuilder creates a new EnvBuilder.
func NewEnvBuilder(cfg *config.Config, r *Resolver) *EnvBuilder {
	return &EnvBuilder{
		config:   cfg,
		resolver: r,
	}
}

func getParentEnv() map[string]string {
	parentEnvMap := make(map[string]string)
	for _, envStr := range os.Environ() {
		k, v, ok := strings.Cut(envStr, "=")
		if ok && k != "" {
			parentEnvMap[k] = v
		}
	}
	return parentEnvMap
}

func (eb *EnvBuilder) resolveMergeSources(ctx context.Context, merges []string, whitelist []string) ([]map[string]string, error) {
	whitelistSet := make(map[string]bool)
	for _, k := range whitelist {
		whitelistSet[utils.FormatKey(k)] = true
	}
	hasWhitelist := len(whitelist) > 0

	loaded := make([]map[string]string, len(merges))
	var wg sync.WaitGroup
	var errOnce sync.Once
	var firstErr error

	for i, m := range merges {
		wg.Add(1)
		go func(idx int, uri string) {
			defer wg.Done()
			keys := make(map[string]string)
			entry, err := eb.resolver.GetEntry(ctx, uri)
			if err != nil {
				errOnce.Do(func() {
					firstErr = fmt.Errorf("failed to get entry %s: %w", uri, err)
				})
				return
			}
			for k, v := range entry.Attributes {
				if strings.EqualFold(k, "title") || strings.EqualFold(k, "tags") {
					continue
				}
				formattedKey := utils.FormatKey(k)
				if hasWhitelist && !whitelistSet[formattedKey] {
					continue
				}
				strVal, err := utils.SerializeAttrValue(v)
				if err != nil {
					errOnce.Do(func() {
						firstErr = fmt.Errorf("failed to serialize attribute %q in entry %s: %w", k, uri, err)
					})
					return
				}
				keys[formattedKey] = strVal
			}
			loaded[idx] = keys
		}(i, m)
	}
	wg.Wait()
	if firstErr != nil {
		return nil, firstErr
	}
	return loaded, nil
}

func (eb *EnvBuilder) resolveExplicitMappings(ctx context.Context, explicit map[string]string) (map[string]string, error) {
	if len(explicit) == 0 {
		return nil, nil
	}

	resolved := make(map[string]string)
	var wg sync.WaitGroup
	var errOnce sync.Once
	var explicitErr error
	var mu sync.Mutex

	for k, uri := range explicit {
		wg.Add(1)
		go func(k, uri string) {
			defer wg.Done()
			val, err := eb.resolver.ResolveWithKey(ctx, uri, k)
			if err != nil {
				errOnce.Do(func() {
					explicitErr = fmt.Errorf("failed to resolve explicit env %s=%s: %w", k, uri, err)
				})
				return
			}
			mu.Lock()
			resolved[utils.FormatKey(k)] = val
			mu.Unlock()
		}(k, uri)
	}
	wg.Wait()
	if explicitErr != nil {
		return nil, explicitErr
	}
	return resolved, nil
}

// BuildEnv constructs the full environment block without command autoloading.
func (eb *EnvBuilder) BuildEnv(ctx context.Context, explicit map[string]string, merges []string, whitelist []string, emptyEnv bool) ([]string, error) {
	_, env, err := eb.BuildEnvForCommand(ctx, nil, explicit, merges, whitelist, emptyEnv)
	return env, err
}

// BuildEnvForCommand constructs the full environment block and evaluates config autoload rules.
func (eb *EnvBuilder) BuildEnvForCommand(ctx context.Context, cmdArgs []string, explicit map[string]string, merges []string, whitelist []string, emptyEnv bool) ([]string, []string, error) {
	var autoMerges []string
	autoExplicit := make(map[string]string)
	var autoWhitelist []string
	currentCmdArgs := cmdArgs

	if len(cmdArgs) > 0 && eb.config != nil {
		for _, rule := range eb.config.Autoload {
			matched, newCmdArgs, err := MatchCommandRule(rule, currentCmdArgs)
			if err != nil {
				return nil, nil, err
			}
			if matched {
				currentCmdArgs = newCmdArgs
				for _, v := range rule.Vaults {
					v = strings.TrimSpace(v)
					if v != "" {
						if !strings.Contains(v, "://") {
							v = v + "://"
						}
						autoMerges = append(autoMerges, v)
					}
				}
				for _, m := range rule.Merge {
					m = strings.TrimSpace(m)
					if m != "" {
						if !strings.Contains(m, "://") {
							m = m + "://"
						}
						autoMerges = append(autoMerges, m)
					}
				}
				for k, uri := range rule.Env {
					if k != "" && uri != "" {
						autoExplicit[k] = uri
					}
				}
				for _, w := range rule.Whitelist {
					w = strings.TrimSpace(w)
					if w != "" {
						autoWhitelist = append(autoWhitelist, w)
					}
				}
			}
		}
	}

	combinedMerges := append(autoMerges, merges...)
	combinedWhitelist := append(autoWhitelist, whitelist...)

	combinedExplicit := make(map[string]string)
	for k, v := range autoExplicit {
		combinedExplicit[k] = v
	}
	for k, v := range explicit {
		combinedExplicit[k] = v // CLI explicit flags -e override autoload env
	}

	var finalEnv map[string]string
	if !emptyEnv {
		finalEnv = getParentEnv()
	} else {
		finalEnv = make(map[string]string)
	}

	mergedSources, err := eb.resolveMergeSources(ctx, combinedMerges, combinedWhitelist)
	if err != nil {
		return nil, nil, err
	}

	for _, src := range mergedSources {
		for k, v := range src {
			finalEnv[k] = v
		}
	}

	explicitResolved, err := eb.resolveExplicitMappings(ctx, combinedExplicit)
	if err != nil {
		return nil, nil, err
	}

	for k, v := range explicitResolved {
		finalEnv[k] = v
	}

	// Convert finalEnv map to []string slice in "KEY=VALUE" format
	var result []string
	for k, v := range finalEnv {
		result = append(result, fmt.Sprintf("%s=%s", k, v))
	}

	if len(currentCmdArgs) == 0 {
		currentCmdArgs = cmdArgs
	}

	resolvedCmdArgs := make([]string, len(currentCmdArgs))
	for i, arg := range currentCmdArgs {
		resolvedArg, err := eb.resolver.Resolve(ctx, arg)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to resolve command argument %q: %w", arg, err)
		}
		resolvedCmdArgs[i] = resolvedArg
	}

	return resolvedCmdArgs, result, nil
}
