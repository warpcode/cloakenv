package engine

import (
	"context"
	"fmt"
	"os"
	"sort"
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

// maxResolveConcurrency bounds concurrent source resolutions spawned by the
// EnvBuilder. It is deliberately independent from the Resolver's shared
// semaphore: merge-source goroutines can nest into Resolver slice resolution,
// which acquires additional slots — sharing one pool across nesting levels
// could otherwise deadlock when all slots are held by parents waiting on
// children.
const maxResolveConcurrency = 16

// EnvBuilder handles the construction of the environment block.
type EnvBuilder struct {
	config   *config.Config
	resolver EnvResolver
	sem      chan struct{}
}

// NewEnvBuilder creates a new EnvBuilder.
func NewEnvBuilder(cfg *config.Config, r EnvResolver) *EnvBuilder {
	return &EnvBuilder{
		config:   cfg,
		resolver: r,
		sem:      make(chan struct{}, maxResolveConcurrency),
	}
}

// acquireSem acquires a concurrency slot and returns the release function.
func (eb *EnvBuilder) acquireSem() func() {
	if eb.sem == nil {
		return func() {}
	}
	eb.sem <- struct{}{}
	return func() { <-eb.sem }
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
		release := eb.acquireSem()
		wg.Add(1)
		go func(idx int, uri string) {
			defer wg.Done()
			defer release()
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
		release := eb.acquireSem()
		wg.Add(1)
		go func(k, uri string) {
			defer wg.Done()
			defer release()
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

// formatEnvMap converts an environment map to a slice of "KEY=VALUE" strings with deterministic sorting.
func formatEnvMap(envMap map[string]string) []string {
	keys := make([]string, 0, len(envMap))
	for k := range envMap {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	result := make([]string, 0, len(keys))
	for _, k := range keys {
		result = append(result, k+"="+envMap[k])
	}
	return result
}

// evalAutoloadRules evaluates configured autoload rules against command arguments.
func (eb *EnvBuilder) evalAutoloadRules(cmdArgs []string) ([]string, map[string]string, []string, []string, error) {
	var autoMerges []string
	autoExplicit := make(map[string]string)
	var autoWhitelist []string
	currentCmdArgs := cmdArgs

	if len(cmdArgs) == 0 || eb.config == nil {
		return autoMerges, autoExplicit, autoWhitelist, currentCmdArgs, nil
	}

	parsed := parseCommandArgs(currentCmdArgs)
	for _, rule := range eb.config.Autoload {
		matched, newCmdArgs, err := matchPreparedCommandRule(rule, currentCmdArgs, parsed)
		if err != nil {
			return nil, nil, nil, nil, fmt.Errorf("autoload rule %q: %w", rule.Match, err)
		}
		if matched {
			currentCmdArgs = newCmdArgs
			parsed = parseCommandArgs(currentCmdArgs)
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

	return autoMerges, autoExplicit, autoWhitelist, currentCmdArgs, nil
}

// resolveCmdArgs resolves secret URIs in command arguments.
func (eb *EnvBuilder) resolveCmdArgs(ctx context.Context, cmdArgs []string) ([]string, error) {
	resolvedCmdArgs := make([]string, len(cmdArgs))
	for i, arg := range cmdArgs {
		resolvedArg, err := eb.resolver.Resolve(ctx, arg)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve command argument %q: %w", arg, err)
		}
		resolvedCmdArgs[i] = resolvedArg
	}
	return resolvedCmdArgs, nil
}

// BuildEnvForCommand constructs the full environment block and evaluates config autoload rules.
func (eb *EnvBuilder) BuildEnvForCommand(ctx context.Context, cmdArgs []string, explicit map[string]string, merges []string, whitelist []string, emptyEnv bool) ([]string, []string, error) {
	autoMerges, autoExplicit, autoWhitelist, currentCmdArgs, err := eb.evalAutoloadRules(cmdArgs)
	if err != nil {
		return nil, nil, err
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

	result := formatEnvMap(finalEnv)

	if len(currentCmdArgs) == 0 {
		currentCmdArgs = cmdArgs
	}

	resolvedCmdArgs, err := eb.resolveCmdArgs(ctx, currentCmdArgs)
	if err != nil {
		return nil, nil, err
	}

	return resolvedCmdArgs, result, nil
}
