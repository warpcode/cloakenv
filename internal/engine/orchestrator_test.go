package engine

import (
	"context"
	"errors"

	"github.com/warpcode/cloakenv/internal/provider"
)

// TestResolveValues verifies that the resolve_values vault config flag
// gates URI resolution of attribute values.

// TestGetEntry_AttributeSelector verifies that GetEntry returns a synthetic
// single-key entry when the URI contains an :attr suffix, instead of the full
// entry from the provider.

// TestBuiltinsNonSearchable verifies that built-in schemes (env, keyring, cache)
// are not searchable by default and cannot be registered as vault names.

type failInitProvider struct{}

func (f *failInitProvider) Scheme() string { return "failinit" }
func (f *failInitProvider) Initialize(_ context.Context, _ provider.ProviderConfig) error {
	return errors.New("init failed")
}
func (f *failInitProvider) GetSecret(_ context.Context, _ string) (string, error) { return "", nil }
func (f *failInitProvider) SetSecret(_ context.Context, _, _ string) error        { return nil }
func (f *failInitProvider) DeleteSecret(_ context.Context, _ string) error        { return nil }
func (f *failInitProvider) Validate(_ map[string]string) error                    { return nil }
func (f *failInitProvider) Search(_ context.Context, _ provider.SearchQuery) ([]provider.SearchResult, error) {
	return nil, nil
}
func (f *failInitProvider) GetEntry(_ context.Context, _ string) (provider.Entry, error) {
	return provider.Entry{}, nil
}

func boolPtr(b bool) *bool {
	return &b
}

type mockCacheProvider struct {
	clearCalled bool
}

func (m *mockCacheProvider) Scheme() string { return "cache" }
func (m *mockCacheProvider) Initialize(_ context.Context, _ provider.ProviderConfig) error {
	return nil
}
func (m *mockCacheProvider) GetSecret(_ context.Context, _ string) (string, error) {
	return "", nil
}
func (m *mockCacheProvider) SetSecret(_ context.Context, _, _ string) error {
	return nil
}
func (m *mockCacheProvider) DeleteSecret(_ context.Context, _ string) error {
	return nil
}
func (m *mockCacheProvider) Validate(_ map[string]string) error {
	return nil
}
func (m *mockCacheProvider) ClearCache() error {
	m.clearCalled = true
	return nil
}
