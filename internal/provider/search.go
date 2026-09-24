package provider

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// SearchExecutor defines a function that executes a search query across source vaults at a given depth.
type SearchExecutor func(ctx context.Context, query string, sourceVaults []string, depth int) ([]SearchResult, error)

// SearchProvider implements SecretProvider, SearchableProvider, and ValueResolvableProvider
// for virtual stored-search vaults.
type SearchProvider struct {
	vaultName      string
	query          string
	sourceVaults   []string
	searchExecutor SearchExecutor
}

// NewSearchProvider returns a new SearchProvider instance.
func NewSearchProvider() *SearchProvider {
	return &SearchProvider{}
}

// Scheme returns "search".
func (s *SearchProvider) Scheme() string {
	return "search"
}

// SetSearchExecutor sets the callback used to execute searches against source vaults.
func (s *SearchProvider) SetSearchExecutor(exec SearchExecutor) {
	s.searchExecutor = exec
}

// Initialize prepares the search provider with configuration parameters.
func (s *SearchProvider) Initialize(_ context.Context, cfg ProviderConfig) error {
	s.vaultName = cfg.Settings["vault_name"]
	s.query = cfg.Query
	if s.query == "" {
		s.query = cfg.Settings["query"]
	}
	s.sourceVaults = cfg.SourceVaults
	return nil
}

const maxSearchDepth = 5

func (s *SearchProvider) executeSearch(ctx context.Context) ([]SearchResult, error) {
	if s.searchExecutor == nil {
		return nil, errors.New("search provider: search executor not configured")
	}

	depth := depthFromContext(ctx)
	if depth >= maxSearchDepth {
		return nil, fmt.Errorf("search provider %q: maximum recursion depth exceeded", s.vaultName)
	}

	nextCtx := context.WithValue(ctx, ContextKeyDepth, depth+1)
	results, err := s.searchExecutor(nextCtx, s.query, s.sourceVaults, depth+1)
	if err != nil {
		return nil, fmt.Errorf("search provider %q: stored query execution failed: %w", s.vaultName, err)
	}
	return results, nil
}

func getEntryAttributeKey(entry Entry, attrName string) (string, bool) {
	if _, ok := entry.Attributes[attrName]; ok {
		return attrName, true
	}
	keys := make([]string, 0, len(entry.Attributes))
	for k := range entry.Attributes {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if strings.EqualFold(k, attrName) {
			return k, true
		}
	}
	if strings.EqualFold(attrName, "title") {
		return "Title", true
	}
	if strings.EqualFold(attrName, "tags") {
		return "Tags", true
	}
	return "", false
}

func getEntryAttribute(entry Entry, attrName string) (string, bool, error) {
	key, found := getEntryAttributeKey(entry, attrName)
	if !found {
		return "", false, nil
	}
	if key == "Title" {
		return entry.Title, true, nil
	}
	if key == "Tags" {
		sVal, err := serializeVal(entry.Tags)
		return sVal, true, err
	}
	sVal, err := serializeVal(entry.Attributes[key])
	return sVal, true, err
}

// GetSecret resolves a secret attribute from the synthetic entry view.
func (s *SearchProvider) GetSecret(ctx context.Context, location string) (string, error) {
	results, err := s.executeSearch(ctx)
	if err != nil {
		return "", err
	}

	if len(results) == 0 {
		return "", fmt.Errorf("search provider %q: no entries matched stored query", s.vaultName)
	}

	if strings.Contains(location, ":") {
		parts := strings.SplitN(location, ":", 2)
		entryLoc := parts[0]
		attrName := parts[1]

		matching := s.findMatchingResults(results, entryLoc)
		if len(matching) == 0 {
			return "", fmt.Errorf("search provider %q: entry %q not found in search results", s.vaultName, entryLoc)
		}

		for _, targetResult := range matching {
			if val, found, err := getEntryAttribute(targetResult.Entry, attrName); found {
				if err != nil {
					return "", err
				}
				return val, nil
			}
		}
		return "", fmt.Errorf("search provider %q: attribute %q not found in entry %q", s.vaultName, attrName, entryLoc)
	}

	// Single result case
	if len(results) == 1 {
		entry := results[0].Entry
		if val, found, err := getEntryAttribute(entry, location); found {
			if err != nil {
				return "", err
			}
			return val, nil
		}

		if location == "" || location == "default" || location == results[0].Path || location == entry.Title {
			if val, found, err := getEntryAttribute(entry, "Password"); found {
				if err != nil {
					return "", err
				}
				return val, nil
			}
		}

		return "", fmt.Errorf("search provider %q: attribute or entry %q not found", s.vaultName, location)
	}

	// Multiple results case
	matching := s.findMatchingResults(results, location)
	if len(matching) == 0 {
		return "", fmt.Errorf("search provider %q: entry %q not found in search results", s.vaultName, location)
	}

	for _, targetResult := range matching {
		if val, found, err := getEntryAttribute(targetResult.Entry, "Password"); found {
			if err != nil {
				return "", err
			}
			return val, nil
		}
	}
	return "", fmt.Errorf("search provider %q: default attribute \"Password\" not found in entry %q", s.vaultName, location)
}

// resolveCanonicalKey resolves the matching entry and canonical attribute key following the exact
// same precedence and scanning order as GetSecret.
func (s *SearchProvider) resolveCanonicalKey(ctx context.Context, location string) (string, error) {
	results, err := s.executeSearch(ctx)
	if err != nil {
		return "", err
	}

	if len(results) == 0 {
		return "", fmt.Errorf("search provider %q: no entries matched stored query", s.vaultName)
	}

	if strings.Contains(location, ":") {
		parts := strings.SplitN(location, ":", 2)
		entryLoc := parts[0]
		attrName := parts[1]

		matching := s.findMatchingResults(results, entryLoc)
		if len(matching) == 0 {
			return "", fmt.Errorf("search provider %q: entry %q not found in search results", s.vaultName, entryLoc)
		}

		for _, targetResult := range matching {
			if key, found := getEntryAttributeKey(targetResult.Entry, attrName); found {
				return key, nil
			}
		}
		return "", fmt.Errorf("search provider %q: attribute %q not found in entry %q", s.vaultName, attrName, entryLoc)
	}

	// Single result case
	if len(results) == 1 {
		entry := results[0].Entry
		if key, found := getEntryAttributeKey(entry, location); found {
			return key, nil
		}

		if location == "" || location == "default" || location == results[0].Path || location == entry.Title {
			if key, found := getEntryAttributeKey(entry, "Password"); found {
				return key, nil
			}
		}

		return "", fmt.Errorf("search provider %q: attribute or entry %q not found", s.vaultName, location)
	}

	// Multiple results case
	matching := s.findMatchingResults(results, location)
	if len(matching) == 0 {
		return "", fmt.Errorf("search provider %q: entry %q not found in search results", s.vaultName, location)
	}

	for _, targetResult := range matching {
		if key, found := getEntryAttributeKey(targetResult.Entry, "Password"); found {
			return key, nil
		}
	}
	return "", fmt.Errorf("search provider %q: default attribute \"Password\" not found in entry %q", s.vaultName, location)
}

// SetSecret is not supported for search (read-only).
func (s *SearchProvider) SetSecret(_ context.Context, _ string, _ string) error {
	return errors.New("search provider is read-only")
}

// DeleteSecret is not supported for search (read-only).
func (s *SearchProvider) DeleteSecret(_ context.Context, _ string) error {
	return errors.New("search provider is read-only")
}

// Validate checks settings and rejects the searchable flag.
func (s *SearchProvider) Validate(settings map[string]string) error {
	if _, ok := settings["searchable"]; ok {
		return errors.New("search provider does not support the searchable flag")
	}
	return nil
}

// SupportsValueResolution implements ValueResolvableProvider.
func (s *SearchProvider) SupportsValueResolution() bool {
	return true
}

// GetEntry retrieves a complete structured entry by location.
func (s *SearchProvider) GetEntry(ctx context.Context, location string) (Entry, error) {
	results, err := s.executeSearch(ctx)
	if err != nil {
		return Entry{}, err
	}

	if len(results) == 0 {
		return Entry{}, fmt.Errorf("search provider %q: no entries matched stored query", s.vaultName)
	}

	if location == "" || location == "default" {
		if len(results) == 1 {
			return results[0].Entry, nil
		}
		matching := s.findMatchingResults(results, location)
		if len(matching) > 0 {
			return matching[0].Entry, nil
		}
		return Entry{}, fmt.Errorf("search provider %q: location is required when search returns multiple entries", s.vaultName)
	}

	matching := s.findMatchingResults(results, location)
	if len(matching) > 0 {
		return matching[0].Entry, nil
	}

	if len(results) == 1 {
		return results[0].Entry, nil
	}

	return Entry{}, fmt.Errorf("search provider %q: entry %q not found in search results", s.vaultName, location)
}

// Search retrieves entries matching the query criteria.
func (s *SearchProvider) Search(ctx context.Context, query SearchQuery) ([]SearchResult, error) {
	rawResults, err := s.executeSearch(ctx)
	if err != nil {
		return nil, err
	}

	var results []SearchResult
	queryTitleLower := strings.ToLower(query.Title)
	queryPathLower := strings.ToLower(query.Path)

	var queryTagsLower []string
	if len(query.Tags) > 0 {
		queryTagsLower = make([]string, len(query.Tags))
		for i, t := range query.Tags {
			queryTagsLower[i] = strings.ToLower(t)
		}
	}

	for _, r := range rawResults {
		if matchEntry(r.Entry, r.Path, query, queryTitleLower, queryPathLower, queryTagsLower) {
			res := SearchResult{
				Provider: "search",
				Vault:    s.vaultName,
				Path:     r.Path,
				Entry:    r.Entry,
			}
			results = append(results, res)
		}
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].Path != results[j].Path {
			return results[i].Path < results[j].Path
		}
		if results[i].Entry.Title != results[j].Entry.Title {
			return results[i].Entry.Title < results[j].Entry.Title
		}
		return results[i].Vault < results[j].Vault
	})

	return results, nil
}

func (s *SearchProvider) findMatchingResults(results []SearchResult, location string) []SearchResult {
	var matching []SearchResult
	for _, r := range results {
		if r.Path == location || r.Entry.Title == location || fmt.Sprintf("%s/%s", r.Vault, r.Path) == location {
			matching = append(matching, r)
		}
	}
	return matching
}

func depthFromContext(ctx context.Context) int {
	if ctx == nil {
		return 0
	}
	if depth, ok := ctx.Value(ContextKeyDepth).(int); ok {
		return depth
	}
	return 0
}
