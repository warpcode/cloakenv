package provider

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/warpcode/cloakenv/internal/utils"
)

// staticProvider implements common logic for file-based static providers like JSON and YAML.
type staticProvider struct {
	scheme       string
	unmarshal    func([]byte, any) error
	serialize    func(any) (string, error)
	filePath     string
	entries      map[string]Entry
	rawContent   map[string]any
	singleEntity bool
}

func (p *staticProvider) Scheme() string {
	return p.scheme
}

func (p *staticProvider) Initialize(_ context.Context, cfg ProviderConfig) error {
	vaultPath := cfg.Settings["vault_path"]
	if vaultPath == "" {
		return fmt.Errorf("%s provider: vault_path is required", p.scheme)
	}
	p.filePath = vaultPath
	p.entries = make(map[string]Entry)
	data, err := os.ReadFile(vaultPath) //nolint:gosec // operator-configured vault path; validated by internal/config
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("%s provider: failed to read file %s: %w", p.scheme, vaultPath, err)
	}

	var raw map[string]any
	if err := p.unmarshal(data, &raw); err != nil {
		return fmt.Errorf("%s provider: failed to parse %s %s: %w", p.scheme, strings.ToUpper(p.scheme), vaultPath, err)
	}

	if raw == nil {
		return nil
	}
	p.rawContent = raw

	entitiesRootKey, isSingleEntity := p.determineEntityConfig(cfg, raw)
	p.singleEntity = isSingleEntity

	if p.singleEntity {
		p.parseSingleEntity(cfg, raw, entitiesRootKey)
		return nil
	}

	return p.parseMultiEntities(raw, entitiesRootKey)
}

func (p *staticProvider) determineEntityConfig(cfg ProviderConfig, raw map[string]any) (string, bool) {
	var singleEntity bool
	if cfg.SingleEntity != nil {
		singleEntity = *cfg.SingleEntity
	} else {
		_, hasEntities := raw["entities"]
		_, hasEntries := raw["entries"]
		hasRootKey := hasEntities || hasEntries
		singleEntity = (cfg.EntitiesRootKey == "" && cfg.Settings["entities_root_key"] == "" && cfg.Settings["entries_key"] == "" && !hasRootKey)
	}

	entitiesRootKey := cfg.EntitiesRootKey
	if entitiesRootKey == "" {
		entitiesRootKey = cfg.Settings["entities_root_key"]
	}
	if entitiesRootKey == "" {
		entitiesRootKey = cfg.Settings["entries_key"]
	}
	if entitiesRootKey == "" {
		if singleEntity {
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

	return entitiesRootKey, singleEntity
}

func (p *staticProvider) parseSingleEntity(cfg ProviderConfig, raw map[string]any, entitiesRootKey string) {
	var attributesMap map[string]any
	if entitiesRootKey == "." {
		attributesMap = raw
	} else {
		val, ok := raw[entitiesRootKey]
		if ok {
			if m, ok := val.(map[string]any); ok {
				attributesMap = m
			} else if m2, ok := val.(map[any]any); ok {
				attributesMap = make(map[string]any)
				for k, v := range m2 {
					attributesMap[fmt.Sprintf("%v", k)] = v
				}
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
			title = filepath.Base(p.filePath)
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
				tags = utils.ParseTags(v)
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
	p.entries[""] = entry
}

func (p *staticProvider) parseMultiEntities(raw map[string]any, entitiesRootKey string) error {
	var rawEntries map[string]map[string]any
	if entitiesRootKey == "." {
		var err error
		rawEntries, err = convertToEntriesMap(raw)
		if err != nil {
			return fmt.Errorf("%s provider: failed to parse root entries: %w", p.scheme, err)
		}
	} else {
		val, ok := raw[entitiesRootKey]
		if !ok {
			return nil
		}
		var err error
		rawEntries, err = convertToEntriesMap(val)
		if err != nil {
			return fmt.Errorf("%s provider: failed to parse entries under key %q: %w", p.scheme, entitiesRootKey, err)
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
				entry.Tags = utils.ParseTags(v)
			case "title":
				if str, ok := v.(string); ok {
					entry.Title = str
				}
			default:
				entry.Attributes[k] = v
			}
		}

		p.entries[name] = entry
	}

	return nil
}

func (p *staticProvider) GetSecret(_ context.Context, location string) (string, error) {
	if p.singleEntity {
		entry, ok := p.entries[""]
		if !ok {
			return "", fmt.Errorf("%s provider: single entity not found", p.scheme)
		}
		val, err := resolveDotPath(entry.Attributes, location)
		if err != nil {
			return "", fmt.Errorf("%s provider: failed to resolve path %q: %w", p.scheme, location, err)
		}
		return p.serialize(val)
	}

	if p.rawContent == nil {
		return "", fmt.Errorf("%s provider: not initialized or empty database", p.scheme)
	}

	val, err := resolveDotPath(p.rawContent, location)
	if err != nil {
		return "", fmt.Errorf("%s provider: failed to resolve path %q: %w", p.scheme, location, err)
	}

	return p.serialize(val)
}

func (p *staticProvider) SetSecret(_ context.Context, _ string, _ string) error {
	return fmt.Errorf("%s provider is read-only", p.scheme)
}

func (p *staticProvider) DeleteSecret(_ context.Context, _ string) error {
	return fmt.Errorf("%s provider is read-only", p.scheme)
}

func (p *staticProvider) Validate(settings map[string]string) error {
	if settings["vault_path"] == "" {
		return fmt.Errorf("%s provider: vault_path is required", p.scheme)
	}
	return nil
}

func (p *staticProvider) GetEntry(_ context.Context, location string) (Entry, error) {
	if p.singleEntity {
		entry, ok := p.entries[""]
		if !ok {
			return Entry{}, fmt.Errorf("%s provider: single entity not found", p.scheme)
		}
		return entry, nil
	}

	entry, ok := p.entries[location]
	if !ok {
		return Entry{}, fmt.Errorf("%s provider: entry %q not found", p.scheme, location)
	}
	return entry, nil
}

func (p *staticProvider) Search(_ context.Context, query SearchQuery) ([]SearchResult, error) {
	var results []SearchResult

	queryTitleLower := strings.ToLower(query.Title)
	queryPathLower := strings.ToLower(query.Path)

	if p.singleEntity {
		entry, ok := p.entries[""]
		if !ok {
			return nil, fmt.Errorf("%s provider: single entity not found", p.scheme)
		}

		if matchEntry(entry, "", query, queryTitleLower, queryPathLower) {
			results = append(results, SearchResult{
				Path:  "",
				Entry: entry,
			})
		}
		return results, nil
	}

	for name, entry := range p.entries {
		if matchEntry(entry, name, query, queryTitleLower, queryPathLower) {
			results = append(results, SearchResult{
				Path:  name,
				Entry: entry,
			})
		}
	}

	return results, nil
}

func matchTags(entryTags, queryTags []string) bool {
	for _, qt := range queryTags {
		found := false
		for _, t := range entryTags {
			if strings.EqualFold(t, qt) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func matchEntry(entry Entry, path string, query SearchQuery, queryTitleLower, queryPathLower string) bool {
	if query.Title != "" && !strings.Contains(strings.ToLower(entry.Title), queryTitleLower) {
		return false
	}
	if query.Path != "" && !strings.Contains(strings.ToLower(path), queryPathLower) {
		return false
	}
	if len(query.Tags) > 0 && !matchTags(entry.Tags, query.Tags) {
		return false
	}
	return true
}

func anyToString(v any) string {
	switch val := v.(type) {
	case string:
		return val
	case int:
		return strconv.Itoa(val)
	case bool:
		return strconv.FormatBool(val)
	default:
		return fmt.Sprintf("%v", v)
	}
}

func normalizeEntryMap(v any) (map[string]any, bool) {
	switch m := v.(type) {
	case map[string]any:
		return m, true
	case map[any]any:
		converted := make(map[string]any, len(m))
		for ek, ev := range m {
			converted[anyToString(ek)] = ev
		}
		return converted, true
	default:
		return nil, false
	}
}

func convertToEntriesMap(val any) (map[string]map[string]any, error) {
	switch m := val.(type) {
	case map[string]map[string]any:
		return m, nil
	case map[string]any:
		res := make(map[string]map[string]any)
		for k, v := range m {
			if entryMap, ok := normalizeEntryMap(v); ok {
				res[k] = entryMap
			} else {
				return nil, fmt.Errorf("entry %q is not a valid map", k)
			}
		}
		return res, nil
	case map[any]any:
		res := make(map[string]map[string]any)
		for k, v := range m {
			kStr := anyToString(k)
			if entryMap, ok := normalizeEntryMap(v); ok {
				res[kStr] = entryMap
			} else {
				return nil, fmt.Errorf("entry %q is not a valid map", kStr)
			}
		}
		return res, nil
	default:
		return nil, fmt.Errorf("invalid entries map type: %T", val)
	}
}

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
					if anyToString(k) == part {
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
