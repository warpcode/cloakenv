package provider

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// SearchVaultProvider implements SecretProvider, SearchableProvider, and ValueResolvableProvider
// for virtual search vaults that represent filtered views over source vaults.
type SearchVaultProvider struct {
	vaultName    string
	sourceVaults []string
	query        string
	searchRunner SearchRunner
}

// NewSearchVaultProvider creates a new SearchVaultProvider instance.
func NewSearchVaultProvider() *SearchVaultProvider {
	return &SearchVaultProvider{}
}

// Scheme returns "search".
func (s *SearchVaultProvider) Scheme() string {
	return "search"
}

// Initialize configures the search vault provider.
func (s *SearchVaultProvider) Initialize(_ context.Context, cfg ProviderConfig) error {
	s.vaultName = cfg.Settings["vault_name"]
	s.sourceVaults = cfg.SourceVaults
	s.query = cfg.Query
	s.searchRunner = cfg.SearchRunner

	if len(s.sourceVaults) == 0 {
		return errors.New("search provider: source_vaults is required")
	}
	if strings.TrimSpace(s.query) == "" {
		return errors.New("search provider: query is required")
	}
	if s.searchRunner == nil {
		return errors.New("search provider: SearchRunner callback is required")
	}

	return nil
}

// GetSecret resolves a secret attribute value from the stored search results.
func (s *SearchVaultProvider) GetSecret(ctx context.Context, location string) (string, error) {
	if location == "" {
		return "", errors.New("search provider: empty location")
	}

	entryPath, attrName, err := parseSearchVaultLocation(location)
	if err != nil {
		return "", err
	}

	results, err := s.searchRunner(ctx, s.query, s.sourceVaults)
	if err != nil {
		return "", fmt.Errorf("search vault %q query failed: %w", s.vaultName, err)
	}

	for _, r := range results {
		if r.Path == entryPath || r.Entry.Title == entryPath {
			if val, ok := r.Entry.Attributes[attrName]; ok {
				return serializeVal(val)
			}
			if strings.EqualFold(attrName, "title") {
				return r.Entry.Title, nil
			}
			if strings.EqualFold(attrName, "tags") {
				return serializeVal(r.Entry.Tags)
			}
			return "", fmt.Errorf("search vault %q: attribute %q not found in entry %q", s.vaultName, attrName, entryPath)
		}
	}

	return "", fmt.Errorf("search vault %q: entry %q not found", s.vaultName, entryPath)
}

// SetSecret is not supported for search vaults (read-only).
func (s *SearchVaultProvider) SetSecret(_ context.Context, _, _ string) error {
	return errors.New("search provider is read-only")
}

// DeleteSecret is not supported for search vaults (read-only).
func (s *SearchVaultProvider) DeleteSecret(_ context.Context, _ string) error {
	return errors.New("search provider is read-only")
}

// Validate checks provider settings.
func (s *SearchVaultProvider) Validate(_ map[string]string) error {
	return nil
}

// SupportsValueResolution implements ValueResolvableProvider.
func (s *SearchVaultProvider) SupportsValueResolution() bool {
	return true
}

// GetEntry retrieves a complete structured entry matching location from the search results.
func (s *SearchVaultProvider) GetEntry(ctx context.Context, location string) (Entry, error) {
	if location == "" {
		return Entry{}, errors.New("search provider: empty location")
	}

	results, err := s.searchRunner(ctx, s.query, s.sourceVaults)
	if err != nil {
		return Entry{}, fmt.Errorf("search vault %q query failed: %w", s.vaultName, err)
	}

	for _, r := range results {
		if r.Path == location || r.Entry.Title == location {
			return r.Entry, nil
		}
	}

	return Entry{}, fmt.Errorf("search vault %q: entry %q not found", s.vaultName, location)
}

// Search executes the stored search query and returns synthetic SearchResults.
func (s *SearchVaultProvider) Search(ctx context.Context, query SearchQuery) ([]SearchResult, error) {
	results, err := s.searchRunner(ctx, s.query, s.sourceVaults)
	if err != nil {
		return nil, fmt.Errorf("search vault %q query failed: %w", s.vaultName, err)
	}

	var syntheticResults []SearchResult
	queryTitleLower := strings.ToLower(query.Title)
	queryPathLower := strings.ToLower(query.Path)

	for _, r := range results {
		synthetic := SearchResult{
			Provider: "search",
			Vault:    s.vaultName,
			Path:     r.Path,
			Entry:    r.Entry,
		}

		if matchEntry(synthetic.Entry, synthetic.Path, query, queryTitleLower, queryPathLower) {
			syntheticResults = append(syntheticResults, synthetic)
		}
	}

	sort.Slice(syntheticResults, func(i, j int) bool {
		return syntheticResults[i].Path < syntheticResults[j].Path
	})

	return syntheticResults, nil
}

func parseSearchVaultLocation(location string) (string, string, error) {
	if strings.Contains(location, ":") {
		parts := strings.SplitN(location, ":", 2)
		if parts[0] == "" {
			return "", "", errors.New("search provider: empty entry path in location")
		}
		if parts[1] == "" {
			return parts[0], "Password", nil
		}
		return parts[0], parts[1], nil
	}
	return location, "Password", nil
}
