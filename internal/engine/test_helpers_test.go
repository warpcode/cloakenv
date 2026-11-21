package engine

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/warpcode/cloakenv/internal/provider"
)

// testMemoryProvider is an in-memory, pure read-only SecretProvider and
// SearchableProvider for unit tests and benchmarks within package engine.
type testMemoryProvider struct {
	scheme  string
	entries map[string]provider.Entry
}

func newTestMemoryProvider(entries map[string]provider.Entry) *testMemoryProvider {
	scheme := "memory"
	cp := make(map[string]provider.Entry, len(entries))
	for path, entry := range entries {
		e := entry.Clone()
		if e == nil {
			e = provider.Entry{}
		}
		if _, ok := e["path"]; !ok {
			e["path"] = path
		}
		if _, ok := e["title"]; !ok {
			e["title"] = path
		}
		cp[path] = e
	}
	return &testMemoryProvider{scheme: scheme, entries: cp}
}

func (p *testMemoryProvider) Scheme() string { return p.scheme }
func (p *testMemoryProvider) Initialize(_ context.Context, _ provider.ProviderConfig) error {
	return nil
}
func (p *testMemoryProvider) Validate(_ map[string]string) error { return nil }

func (p *testMemoryProvider) GetSecret(_ context.Context, location string) (string, error) {
	entryKey := location
	attrKey := "Password"
	if idx := strings.LastIndex(location, ":"); idx >= 0 {
		entryKey = location[:idx]
		attrKey = location[idx+1:]
	}
	entry, ok := p.entries[entryKey]
	if !ok {
		return "", fmt.Errorf("entry %q not found", entryKey)
	}
	val, ok := entry[attrKey]
	if !ok {
		for k, v := range entry {
			if strings.EqualFold(k, attrKey) {
				val = v
				ok = true
				break
			}
		}
	}
	if !ok {
		return "", fmt.Errorf("attribute %q not found in entry %q", attrKey, entryKey)
	}
	return fmt.Sprintf("%v", val), nil
}

func (p *testMemoryProvider) GetEntry(_ context.Context, location string) (provider.Entry, error) {
	entry, ok := p.entries[location]
	if !ok {
		return nil, fmt.Errorf("entry %q not found", location)
	}
	return entry.Clone(), nil
}

func (p *testMemoryProvider) Search(_ context.Context, _ provider.SearchQuery) ([]provider.Entry, error) {
	keys := make([]string, 0, len(p.entries))
	for k := range p.entries {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]provider.Entry, 0, len(keys))
	for _, k := range keys {
		out = append(out, p.entries[k].Clone())
	}
	return out, nil
}

// InjectTestVaultForTest registers an in-memory read-only vault on an Orchestrator
// for black-box tests in package engine_test.
func InjectTestVaultForTest(orch *Orchestrator, name string, entries map[string]provider.Entry) {
	orch.providerManager.vaultCache[name] = newTestMemoryProvider(entries)
}
