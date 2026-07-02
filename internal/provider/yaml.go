package provider

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// YamlProvider implements SecretProvider and SearchableProvider for static YAML registries.
type YamlProvider struct {
	filePath   string
	entries    map[string]Entry
	rawContent map[string]any
}

// NewYamlProvider returns a new YamlProvider instance.
func NewYamlProvider() *YamlProvider {
	return &YamlProvider{
		entries:    make(map[string]Entry),
		rawContent: make(map[string]any),
	}
}

// Scheme returns "yaml" as the provider type.
func (y *YamlProvider) Scheme() string {
	return "yaml"
}

func convertToEntriesMap(val any) (map[string]map[string]any, error) {
	switch m := val.(type) {
	case map[string]map[string]any:
		return m, nil
	case map[string]any:
		res := make(map[string]map[string]any)
		for k, v := range m {
			if entryMap, ok := v.(map[string]any); ok {
				res[k] = entryMap
			} else if entryMap2, ok := v.(map[any]any); ok {
				converted := make(map[string]any)
				for ek, ev := range entryMap2 {
					converted[fmt.Sprintf("%v", ek)] = ev
				}
				res[k] = converted
			} else {
				return nil, fmt.Errorf("entry %q is not a valid map", k)
			}
		}
		return res, nil
	case map[any]any:
		res := make(map[string]map[string]any)
		for k, v := range m {
			kStr := fmt.Sprintf("%v", k)
			if entryMap, ok := v.(map[string]any); ok {
				res[kStr] = entryMap
			} else if entryMap2, ok := v.(map[any]any); ok {
				converted := make(map[string]any)
				for ek, ev := range entryMap2 {
					converted[fmt.Sprintf("%v", ek)] = ev
				}
				res[kStr] = converted
			} else {
				return nil, fmt.Errorf("entry %q is not a valid map", kStr)
			}
		}
		return res, nil
	default:
		return nil, fmt.Errorf("invalid entries map type: %T", val)
	}
}

// Initialize opens, parses, and loads the entries YAML database file.
func (y *YamlProvider) Initialize(_ context.Context, cfg ProviderConfig) error {
	dbPath := cfg.Settings["database_path"]
	if dbPath == "" {
		return errors.New("yaml provider: database_path is required")
	}
	y.filePath = dbPath
	y.entries = make(map[string]Entry)

	data, err := os.ReadFile(dbPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("yaml provider: failed to read file %s: %w", dbPath, err)
	}

	var raw map[string]any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("yaml provider: failed to parse YAML %s: %w", dbPath, err)
	}

	if raw == nil {
		return nil
	}
	y.rawContent = raw

	entriesKey := cfg.Settings["entries_key"]
	if entriesKey == "" {
		entriesKey = "entries"
	}

	var rawEntries map[string]map[string]any
	if entriesKey == "." {
		var err error
		rawEntries, err = convertToEntriesMap(raw)
		if err != nil {
			return fmt.Errorf("yaml provider: failed to parse root entries: %w", err)
		}
	} else {
		val, ok := raw[entriesKey]
		if !ok {
			return nil
		}
		var err error
		rawEntries, err = convertToEntriesMap(val)
		if err != nil {
			return fmt.Errorf("yaml provider: failed to parse entries under key %q: %w", entriesKey, err)
		}
	}

	for name, rawEntry := range rawEntries {
		entry := Entry{
			Title:      name,
			Attributes: make(map[string]any),
		}

		for k, v := range rawEntry {
			kLower := strings.ToLower(k)
			if kLower == "tags" {
				if tagSlice, ok := v.([]any); ok {
					for _, t := range tagSlice {
						if str, ok := t.(string); ok {
							entry.Tags = append(entry.Tags, str)
						}
					}
				} else if tagStr, ok := v.(string); ok {
					entry.Tags = parseTags(tagStr)
				}
			} else if kLower == "title" {
				if str, ok := v.(string); ok {
					entry.Title = str
				}
			} else {
				entry.Attributes[k] = v
			}
		}

		y.entries[name] = entry
	}

	return nil
}

// resolveDotPath traverses a map or slice structure using a dot-separated path.
func resolveDotPath(val any, path string) (any, error) {
	if path == "" {
		return val, nil
	}
	parts := strings.Split(path, ".")
	curr := val

	for _, part := range parts {
		if part == "" {
			continue
		}
		switch m := curr.(type) {
		case map[string]any:
			next, ok := m[part]
			if !ok {
				return nil, fmt.Errorf("key %q not found", part)
			}
			curr = next
		case map[any]any:
			next, ok := m[part]
			if !ok {
				found := false
				for k, v := range m {
					if fmt.Sprintf("%v", k) == part {
						next = v
						found = true
						break
					}
				}
				if !found {
					return nil, fmt.Errorf("key %q not found", part)
				}
			}
			curr = next
		case []any:
			var idx int
			_, err := fmt.Sscan(part, &idx)
			if err != nil {
				return nil, fmt.Errorf("cannot index array with non-integer %q", part)
			}
			if idx < 0 || idx >= len(m) {
				return nil, fmt.Errorf("index %d out of bounds (length %d)", idx, len(m))
			}
			curr = m[idx]
		default:
			return nil, fmt.Errorf("cannot traverse key %q on value of type %T", part, curr)
		}
	}
	return curr, nil
}

// GetSecret retrieves an attribute value from the static YAML entry registry using a dot path from the root.
func (y *YamlProvider) GetSecret(_ context.Context, location string) (string, error) {
	if y.rawContent == nil {
		return "", fmt.Errorf("yaml provider: not initialized or empty database")
	}

	val, err := resolveDotPath(y.rawContent, location)
	if err != nil {
		return "", fmt.Errorf("yaml provider: failed to resolve path %q: %w", location, err)
	}

	if str, ok := val.(string); ok {
		return str, nil
	}
	return fmt.Sprintf("%v", val), nil
}

// SetSecret returns an error because the YAML provider is read-only.
func (y *YamlProvider) SetSecret(_ context.Context, _ string, _ string) error {
	return errors.New("yaml provider is read-only")
}

// DeleteSecret returns an error because the YAML provider is read-only.
func (y *YamlProvider) DeleteSecret(_ context.Context, _ string) error {
	return errors.New("yaml provider is read-only")
}

// Validate checks if the database_path setting is provided.
func (y *YamlProvider) Validate(settings map[string]string) error {
	if settings["database_path"] == "" {
		return errors.New("yaml provider: database_path is required")
	}
	return nil
}

// GetEntry retrieves a complete structured entry by location.
func (y *YamlProvider) GetEntry(_ context.Context, location string) (Entry, error) {
	entry, ok := y.entries[location]
	if !ok {
		return Entry{}, fmt.Errorf("yaml provider: entry %q not found", location)
	}
	return entry, nil
}

// Search filters the entries using the given SearchQuery criteria.
func (y *YamlProvider) Search(_ context.Context, query SearchQuery) ([]SearchResult, error) {
	var results []SearchResult

	for name, entry := range y.entries {
		if query.Title != "" {
			if !strings.Contains(strings.ToLower(entry.Title), strings.ToLower(query.Title)) {
				continue
			}
		}

		if query.Path != "" {
			if !strings.Contains(strings.ToLower(name), strings.ToLower(query.Path)) {
				continue
			}
		}

		if len(query.Tags) > 0 {
			tagMap := make(map[string]bool)
			for _, t := range entry.Tags {
				tagMap[strings.ToLower(t)] = true
			}
			match := true
			for _, qt := range query.Tags {
				if !tagMap[strings.ToLower(qt)] {
					match = false
					break
				}
			}
			if !match {
				continue
			}
		}

		results = append(results, SearchResult{
			Path:  name,
			Entry: entry,
		})
	}

	return results, nil
}
