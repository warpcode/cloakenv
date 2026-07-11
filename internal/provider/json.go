package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// JsonProvider implements SecretProvider and SearchableProvider for static JSON registries.
type JsonProvider struct {
	filePath     string
	entries      map[string]Entry
	rawContent   map[string]any
	singleEntity bool
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
	vaultPath := cfg.Settings["vault_path"]
	if vaultPath == "" {
		return errors.New("json provider: vault_path is required")
	}
	j.filePath = vaultPath
	j.entries = make(map[string]Entry)
	data, err := os.ReadFile(vaultPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("json provider: failed to read file %s: %w", vaultPath, err)
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("json provider: failed to parse JSON %s: %w", vaultPath, err)
	}

	if raw == nil {
		return nil
	}
	j.rawContent = raw

	// Determine singleEntity status: defaults to true if no entities root key is configured in Settings or config and database has no entries/entities root key.
	if cfg.SingleEntity != nil {
		j.singleEntity = *cfg.SingleEntity
	} else {
		_, hasEntities := raw["entities"]
		_, hasEntries := raw["entries"]
		hasRootKey := hasEntities || hasEntries
		j.singleEntity = (cfg.EntitiesRootKey == "" && cfg.Settings["entities_root_key"] == "" && cfg.Settings["entries_key"] == "" && !hasRootKey)
	}

	entitiesRootKey := cfg.EntitiesRootKey
	if entitiesRootKey == "" {
		entitiesRootKey = cfg.Settings["entities_root_key"]
	}
	if entitiesRootKey == "" {
		entitiesRootKey = cfg.Settings["entries_key"]
	}
	if entitiesRootKey == "" {
		if j.singleEntity {
			entitiesRootKey = "."
		} else {
			if _, ok := raw["entities"]; ok {
				entitiesRootKey = "entities"
			} else if _, ok := raw["entries"]; ok {
				entitiesRootKey = "entries"
			} else {
				entitiesRootKey = "entities"
			}
		}
	}

	if j.singleEntity {
		var attributesMap map[string]any
		if entitiesRootKey == "." {
			attributesMap = raw
		} else {
			val, ok := raw[entitiesRootKey]
			if ok {
				if m, ok := val.(map[string]any); ok {
					attributesMap = m
				}
			}
		}
		if attributesMap == nil {
			attributesMap = make(map[string]any)
		}

		title := cfg.EntityName
		if title == "" {
			if vaultName := cfg.Settings["vault_name"]; vaultName != "" {
				title = vaultName
			} else {
				title = filepath.Base(vaultPath)
			}
		}

		tags := cfg.Tags
		entry := Entry{
			Title:      title,
			Attributes: make(map[string]any),
		}

		for k, v := range attributesMap {
			kLower := strings.ToLower(k)
			switch kLower {
			case "tags":
				if len(tags) == 0 {
					if tagSlice, ok := v.([]any); ok {
						for _, t := range tagSlice {
							if str, ok := t.(string); ok {
								tags = append(tags, str)
							}
						}
					} else if tagStr, ok := v.(string); ok {
						tags = parseTags(tagStr)
					}
				}
			case "title":
				if cfg.EntityName == "" {
					if str, ok := v.(string); ok {
						entry.Title = str
					}
				}
			default:
				entry.Attributes[k] = v
			}
		}
		entry.Tags = tags
		j.entries[""] = entry
		return nil
	}

	var rawEntries map[string]map[string]any
	if entitiesRootKey == "." {
		var err error
		rawEntries, err = convertToEntriesMap(raw)
		if err != nil {
			return fmt.Errorf("json provider: failed to parse root entries: %w", err)
		}
	} else {
		val, ok := raw[entitiesRootKey]
		if !ok {
			return nil
		}
		var err error
		rawEntries, err = convertToEntriesMap(val)
		if err != nil {
			return fmt.Errorf("json provider: failed to parse entries under key %q: %w", entitiesRootKey, err)
		}
	}

	for name, rawEntry := range rawEntries {
		entry := Entry{
			Title:      name,
			Attributes: make(map[string]any),
		}

		for k, v := range rawEntry {
			kLower := strings.ToLower(k)
			switch kLower {
			case "tags":
				if tagSlice, ok := v.([]any); ok {
					for _, t := range tagSlice {
						if str, ok := t.(string); ok {
							entry.Tags = append(entry.Tags, str)
						}
					}
				} else if tagStr, ok := v.(string); ok {
					entry.Tags = parseTags(tagStr)
				}
			case "title":
				if str, ok := v.(string); ok {
					entry.Title = str
				}
			default:
				entry.Attributes[k] = v
			}
		}

		j.entries[name] = entry
	}

	return nil
}

// GetSecret retrieves an attribute value from the static JSON entry registry using a dot path from the root.
func (j *JsonProvider) GetSecret(_ context.Context, location string) (string, error) {
	if j.singleEntity {
		entry, ok := j.entries[""]
		if !ok {
			return "", fmt.Errorf("json provider: single entity not found")
		}
		val, err := resolveDotPath(entry.Attributes, location)
		if err != nil {
			return "", fmt.Errorf("json provider: failed to resolve path %q: %w", location, err)
		}
		return serializeJsonVal(val)
	}

	if j.rawContent == nil {
		return "", fmt.Errorf("json provider: not initialized or empty database")
	}

	val, err := resolveDotPath(j.rawContent, location)
	if err != nil {
		return "", fmt.Errorf("json provider: failed to resolve path %q: %w", location, err)
	}

	return serializeJsonVal(val)
}

// SetSecret returns an error because the JSON provider is read-only.
func (j *JsonProvider) SetSecret(_ context.Context, _ string, _ string) error {
	return errors.New("json provider is read-only")
}

// DeleteSecret returns an error because the JSON provider is read-only.
func (j *JsonProvider) DeleteSecret(_ context.Context, _ string) error {
	return errors.New("json provider is read-only")
}

// Validate checks if the vault_path setting is provided.
func (j *JsonProvider) Validate(settings map[string]string) error {
	if settings["vault_path"] == "" {
		return errors.New("json provider: vault_path is required")
	}
	return nil
}

// GetEntry retrieves a complete structured entry by location.
func (j *JsonProvider) GetEntry(_ context.Context, location string) (Entry, error) {
	if j.singleEntity {
		entry, ok := j.entries[""]
		if !ok {
			return Entry{}, fmt.Errorf("json provider: single entity not found")
		}
		return entry, nil
	}

	entry, ok := j.entries[location]
	if !ok {
		return Entry{}, fmt.Errorf("json provider: entry %q not found", location)
	}
	return entry, nil
}

// Search filters the entries using the given SearchQuery criteria.
func (j *JsonProvider) Search(_ context.Context, query SearchQuery) ([]SearchResult, error) {
	var results []SearchResult

	if j.singleEntity {
		entry, ok := j.entries[""]
		if !ok {
			return nil, fmt.Errorf("json provider: single entity not found")
		}

		if query.Title != "" {
			if !strings.Contains(strings.ToLower(entry.Title), strings.ToLower(query.Title)) {
				return results, nil
			}
		}

		if len(query.Tags) > 0 {
			tagMap := make(map[string]bool)
			for _, t := range entry.Tags {
				tagMap[strings.ToLower(t)] = true
			}
			for _, qt := range query.Tags {
				if !tagMap[strings.ToLower(qt)] {
					return results, nil
				}
			}
		}

		results = append(results, SearchResult{
			Path:  "",
			Entry: entry,
		})
		return results, nil
	}

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

func serializeJsonVal(val any) (string, error) {
	switch v := val.(type) {
	case string:
		return v, nil
	case []any, map[string]any:
		data, err := json.Marshal(v)
		if err != nil {
			return "", fmt.Errorf("json serialization failed: %w", err)
		}
		return string(data), nil
	default:
		return fmt.Sprintf("%v", v), nil
	}
}
