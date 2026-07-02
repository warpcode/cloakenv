package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
)

// JsonProvider implements SecretProvider and SearchableProvider for static JSON registries.
type JsonProvider struct {
	filePath   string
	entries    map[string]Entry
	rawContent map[string]any
}

// NewJsonProvider returns a new JsonProvider instance.
func NewJsonProvider() *JsonProvider {
	return &JsonProvider{
		entries:    make(map[string]Entry),
		rawContent: make(map[string]any),
	}
}

// Scheme returns "json" as the provider type.
func (j *JsonProvider) Scheme() string {
	return "json"
}

// Initialize opens, parses, and loads the entries JSON database file.
func (j *JsonProvider) Initialize(_ context.Context, cfg ProviderConfig) error {
	dbPath := cfg.Settings["database_path"]
	if dbPath == "" {
		return errors.New("json provider: database_path is required")
	}
	j.filePath = dbPath
	j.entries = make(map[string]Entry)

	data, err := os.ReadFile(dbPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("json provider: failed to read file %s: %w", dbPath, err)
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("json provider: failed to parse JSON %s: %w", dbPath, err)
	}

	if raw == nil {
		return nil
	}
	j.rawContent = raw

	entriesKey := cfg.Settings["entries_key"]
	if entriesKey == "" {
		entriesKey = "entries"
	}

	var rawEntries map[string]map[string]any
	if entriesKey == "." {
		var err error
		rawEntries, err = convertToEntriesMap(raw)
		if err != nil {
			return fmt.Errorf("json provider: failed to parse root entries: %w", err)
		}
	} else {
		val, ok := raw[entriesKey]
		if !ok {
			return nil
		}
		var err error
		rawEntries, err = convertToEntriesMap(val)
		if err != nil {
			return fmt.Errorf("json provider: failed to parse entries under key %q: %w", entriesKey, err)
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

		j.entries[name] = entry
	}

	return nil
}

// GetSecret retrieves an attribute value from the static JSON entry registry using a dot path from the root.
func (j *JsonProvider) GetSecret(_ context.Context, location string) (string, error) {
	if j.rawContent == nil {
		return "", fmt.Errorf("json provider: not initialized or empty database")
	}

	val, err := resolveDotPath(j.rawContent, location)
	if err != nil {
		return "", fmt.Errorf("json provider: failed to resolve path %q: %w", location, err)
	}

	if str, ok := val.(string); ok {
		return str, nil
	}
	return fmt.Sprintf("%v", val), nil
}

// SetSecret returns an error because the JSON provider is read-only.
func (j *JsonProvider) SetSecret(_ context.Context, _ string, _ string) error {
	return errors.New("json provider is read-only")
}

// DeleteSecret returns an error because the JSON provider is read-only.
func (j *JsonProvider) DeleteSecret(_ context.Context, _ string) error {
	return errors.New("json provider is read-only")
}

// Validate checks if the database_path setting is provided.
func (j *JsonProvider) Validate(settings map[string]string) error {
	if settings["database_path"] == "" {
		return errors.New("json provider: database_path is required")
	}
	return nil
}

// GetEntry retrieves a complete structured entry by location.
func (j *JsonProvider) GetEntry(_ context.Context, location string) (Entry, error) {
	entry, ok := j.entries[location]
	if !ok {
		return Entry{}, fmt.Errorf("json provider: entry %q not found", location)
	}
	return entry, nil
}

// Search filters the entries using the given SearchQuery criteria.
func (j *JsonProvider) Search(_ context.Context, query SearchQuery) ([]SearchResult, error) {
	var results []SearchResult

	for name, entry := range j.entries {
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
