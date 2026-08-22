package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/warpcode/cloakenv/internal/provider"
	"github.com/warpcode/cloakenv/internal/utils"
)

// SearchFunc defines the signature for searching across repositories.
type SearchFunc func(ctx context.Context, expressionStr string, depth int) ([]provider.SearchResult, error)

// Resolver handles secret resolution logic and URI routing.
type Resolver struct {
	providers      *ProviderManager
	concurrencySem chan struct{}
	searchFunc     SearchFunc
}

// NewResolver creates a new Resolver.
func NewResolver(pm *ProviderManager, concurrencySem chan struct{}) *Resolver {
	return &Resolver{
		providers:      pm,
		concurrencySem: concurrencySem,
	}
}

// SetSearchFunc sets the callback used for evaluating search:// URIs.
func (r *Resolver) SetSearchFunc(sf SearchFunc) {
	r.searchFunc = sf
}

// Resolve takes a full value, expands any ${...} expressions, and
// returns the resolved secret value.
func (r *Resolver) Resolve(ctx context.Context, uri string) (string, error) {
	return r.ResolveWithKey(ctx, uri, "")
}

// ResolveWithKey takes a full value, expands any ${...} expressions, and
// includes the configKey in any failure messages if provided.
func (r *Resolver) ResolveWithKey(ctx context.Context, uri string, configKey string) (string, error) {
	if !strings.Contains(uri, "${") {
		if scheme, _, err := utils.ParseURI(uri); err == nil && r.isKnownScheme(scheme) {
			uri = "${" + uri + "}"
		}
	}
	return r.expandString(ctx, uri, 0, configKey)
}

func (r *Resolver) isKnownScheme(scheme string) bool {
	cfg := r.providers.Config()
	if cfg != nil {
		if _, ok := cfg.Vaults[scheme]; ok {
			return true
		}
	}
	// Check builtins by fetching it (a bit hacky but works without exposing internal map)
	_, isBuiltin, err := r.providers.GetProvider(context.Background(), scheme)
	return err == nil && isBuiltin
}

func (r *Resolver) expandString(ctx context.Context, s string, depth int, configKey string) (string, error) {
	if depth > 5 {
		return "", fmt.Errorf("infinite secret resolution recursion detected: reached max depth 5")
	}

	return utils.ExpandString(s, configKey, func(innerText string) (string, error) {
		return r.resolveSingleURI(ctx, innerText, depth, configKey)
	})
}

func (r *Resolver) resolveSingleURI(ctx context.Context, uri string, depth int, configKey string) (string, error) {
	scheme, location, err := utils.ParseURI(uri)
	if err != nil {
		return "", fmt.Errorf("invalid URI format: %w", err)
	}

	p, isBuiltin, err := r.providers.GetProvider(ctx, scheme)
	if err != nil && scheme != "search" {
		return "", err
	}

	// Handle search scheme dynamically
	if scheme == "search" {
		if r.searchFunc == nil {
			return "", fmt.Errorf("search function not configured")
		}

		exprQuery, attr, err := parseSearchURI(location)
		if err != nil {
			return "", err
		}

		results, err := r.searchFunc(ctx, exprQuery, depth+1)
		if err != nil {
			return "", fmt.Errorf("search URI evaluation failed: %w", err)
		}

		if len(results) == 0 {
			return "", fmt.Errorf("no secrets found matching search query %q", exprQuery)
		}

		matchedEntry := results[0].Entry
		val, ok := matchedEntry.Attributes[attr]
		if !ok {
			return "", fmt.Errorf("attribute %q not found in matched entry %q", attr, matchedEntry.Title)
		}

		var valStr string
		if str, ok := val.(string); ok {
			valStr = str
		} else {
			valStr = fmt.Sprintf("%v", val)
		}
		return r.expandString(ctx, valStr, depth+1, configKey)
	}

	val, err := p.GetSecret(ctx, location)
	if err != nil {
		return "", err
	}

	shouldResolve := true
	if !isBuiltin {
		if _, ok := p.(provider.ValueResolvableProvider); ok {
			shouldResolve = r.providers.Config().Vaults[scheme].ResolveValues
		}
	}

	if shouldResolve {
		return r.expandString(ctx, val, depth+1, configKey)
	}
	return val, nil
}

// GetEntry retrieves a complete structured entry by location.
func (r *Resolver) GetEntry(ctx context.Context, uri string) (provider.Entry, error) {
	return r.GetEntryRecursive(ctx, uri, 0)
}

func (r *Resolver) GetEntryRecursive(ctx context.Context, uri string, depth int) (provider.Entry, error) {
	if depth > 5 {
		return provider.Entry{}, fmt.Errorf("infinite secret resolution recursion detected: reached max depth 5 resolving entry %q", uri)
	}

	scheme, location, err := utils.ParseURI(uri)
	if err != nil {
		return provider.Entry{}, err
	}

	// If the location contains an attribute selector, resolve the single value
	// and return a synthetic entry with that one key.
	if attrIdx := strings.LastIndex(location, ":"); attrIdx >= 0 {
		attrName := location[attrIdx+1:]
		if attrName != "" {
			val, err := r.resolveSingleURI(ctx, uri, 0, "")
			if err != nil {
				return provider.Entry{}, err
			}
			return provider.Entry{
				Attributes: map[string]any{attrName: val},
			}, nil
		}
	}

	p, isBuiltin, err := r.providers.GetProvider(ctx, scheme)
	if err != nil {
		return provider.Entry{}, err
	}

	searchable, ok := p.(provider.SearchableProvider)
	if !ok {
		return provider.Entry{}, fmt.Errorf("provider %q does not support structured entries", scheme)
	}

	entry, err := searchable.GetEntry(ctx, location)
	if err != nil {
		return provider.Entry{}, err
	}

	shouldResolveAttrs := true
	if !isBuiltin {
		if _, ok := p.(provider.ValueResolvableProvider); ok {
			shouldResolveAttrs = r.providers.Config().Vaults[scheme].ResolveValues
		}
	}

	resolvedAttrs := make(map[string]any)
	for k, v := range entry.Attributes {
		if !shouldResolveAttrs {
			resolvedAttrs[k] = v
			continue
		}
		resolvedVal, err := r.ResolveAttrRecursive(ctx, v, depth+1, k)
		if err != nil {
			return provider.Entry{}, fmt.Errorf("failed to resolve attribute %q: %w", k, err)
		}
		resolvedAttrs[k] = resolvedVal
	}
	entry.Attributes = resolvedAttrs

	return entry, nil
}

func resolveSliceRecursive[T any](ctx context.Context, r *Resolver, typedVal []T, depth int, configKey string, formatFn func(any) T) ([]T, error) {
	resolvedSlice := make([]T, len(typedVal))
	var wg sync.WaitGroup
	var firstErr error
	var mu sync.Mutex

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	numWorkers := len(typedVal)
	const maxConcurrency = 16
	if numWorkers > maxConcurrency {
		numWorkers = maxConcurrency
	}
	var idx atomic.Int64

	workersToSpawn := numWorkers - 1
	if workersToSpawn < 0 {
		workersToSpawn = 0
	}

SpawnLoop:
	for i := 0; i < workersToSpawn; i++ {
		select {
		case r.concurrencySem <- struct{}{}:
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer func() { <-r.concurrencySem }()

				for {
					i := int(idx.Add(1) - 1)
					if i >= len(typedVal) {
						break
					}

					select {
					case <-ctx.Done():
						return
					default:
					}

					v := typedVal[i]
					res, err := r.ResolveAttrRecursive(ctx, v, depth, configKey)
					if err != nil {
						mu.Lock()
						if firstErr == nil {
							firstErr = err
							cancel()
						}
						mu.Unlock()
						return
					}
					resolvedSlice[i] = formatFn(res)
				}
			}()
		default:
			break SpawnLoop
		}
	}

	for {
		i := int(idx.Add(1) - 1)
		if i >= len(typedVal) {
			break
		}
		if ctx.Err() != nil {
			break
		}
		v := typedVal[i]
		res, err := r.ResolveAttrRecursive(ctx, v, depth, configKey)
		if err != nil {
			mu.Lock()
			if firstErr == nil {
				firstErr = err
				cancel()
			}
			mu.Unlock()
			break
		}
		resolvedSlice[i] = formatFn(res)
	}

	wg.Wait()
	if firstErr != nil {
		return nil, firstErr
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return resolvedSlice, nil
}

// ResolveAttrRecursive resolves an attribute that could be a string, slice of strings, or slice of any.
func (r *Resolver) ResolveAttrRecursive(ctx context.Context, val any, depth int, configKey string) (any, error) {
	if depth > 5 {
		return nil, errors.New("max recursion depth reached resolving attribute")
	}

	switch typedVal := val.(type) {
	case string:
		return r.expandString(ctx, typedVal, depth+1, configKey)
	case []string:
		return resolveSliceRecursive(ctx, r, typedVal, depth, configKey, func(res any) string {
			if str, ok := res.(string); ok {
				return str
			}
			return fmt.Sprintf("%v", res)
		})
	case []any:
		return resolveSliceRecursive(ctx, r, typedVal, depth, configKey, func(res any) any {
			return res
		})
	default:
		return val, nil
	}
}
