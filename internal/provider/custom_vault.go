package provider

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/warpcode/cloakenv/internal/config"
	"github.com/warpcode/cloakenv/internal/utils"
	"github.com/warpcode/cloakenv/internal/yaml"
)

// CustomVaultProvider implements SecretProvider and SearchableProvider for multi-entity inline static configs.
type CustomVaultProvider struct {
	entities           map[string]config.CustomEntityConfig
	getEntriesFunc     GetEntriesFunc
	renderTemplateFunc TemplateRenderFunc
}

// NewCustomVaultProvider returns a new CustomVaultProvider instance.
func NewCustomVaultProvider() *CustomVaultProvider {
	return &CustomVaultProvider{
		entities: make(map[string]config.CustomEntityConfig),
	}
}

// Scheme returns "custom_vault".
func (c *CustomVaultProvider) Scheme() string {
	return "custom_vault"
}

// Initialize populates the provider with static configs.
func (c *CustomVaultProvider) Initialize(_ context.Context, cfg ProviderConfig) error {
	c.entities = cfg.Entities
	if c.entities == nil {
		c.entities = make(map[string]config.CustomEntityConfig)
	}
	return nil
}

// GetRawAttribute retrieves the raw attribute value for an entity without serialization.
func (c *CustomVaultProvider) GetRawAttribute(entityName, attr string) (any, error) {
	entity, ok := c.entities[entityName]
	if !ok {
		return nil, fmt.Errorf("custom_vault: entity %q not found", entityName)
	}

	val, ok := entity.Attributes[attr]
	if !ok {
		return nil, fmt.Errorf("custom_vault: attribute %q not found for entity %q", attr, entityName)
	}

	return val, nil
}

// GetSecret retrieves a secret from the static inline configuration.
func (c *CustomVaultProvider) GetSecret(ctx context.Context, location string) (string, error) {
	entityName, attr, err := parseCustomVaultLocation(location)
	if err != nil {
		return "", err
	}

	entity, ok := c.entities[entityName]
	if !ok {
		return "", fmt.Errorf("custom_vault: entity %q not found", entityName)
	}

	if entity.IsComposed {
		resolved, err := c.resolveEntity(ctx, entityName, entity)
		if err != nil {
			return "", err
		}
		if val, ok := resolved.Attributes[attr]; ok {
			return serializeVal(val)
		}
		// Case-insensitive lookup fallback
		for k, v := range resolved.Attributes {
			if strings.EqualFold(k, attr) {
				return serializeVal(v)
			}
		}
		// Dot path lookup fallback within nested maps
		if strings.Contains(attr, ".") {
			if val, err := resolveDotPath(resolved.Attributes, attr); err == nil {
				return serializeVal(val)
			}
		}
		return "", fmt.Errorf("custom_vault: attribute %q not found for entity %q", attr, entityName)
	}

	val, err := c.GetRawAttribute(entityName, attr)
	if err != nil {
		return "", err
	}

	return serializeVal(val)
}

// SetSecret is not supported for custom_vault (read-only).
func (c *CustomVaultProvider) SetSecret(_ context.Context, _ string, _ string) error {
	return errors.New("custom_vault provider is read-only")
}

// DeleteSecret is not supported for custom_vault (read-only).
func (c *CustomVaultProvider) DeleteSecret(_ context.Context, _ string) error {
	return errors.New("custom_vault provider is read-only")
}

// Validate is a no-op for custom_vault.
func (c *CustomVaultProvider) Validate(_ map[string]string) error {
	return nil
}

// SupportsValueResolution implements ValueResolvableProvider, opting this
// provider into gated URI resolution of attribute values via resolve_values.
func (c *CustomVaultProvider) SupportsValueResolution() bool {
	return true
}

// GetEntry retrieves a complete structured entry by location.
func (c *CustomVaultProvider) GetEntry(ctx context.Context, location string) (Entry, error) {
	entity, ok := c.entities[location]
	if !ok {
		return Entry{}, fmt.Errorf("custom_vault: entity %q not found", location)
	}

	return c.resolveEntity(ctx, location, entity)
}

// GetEntryWithPath retrieves a complete structured entry along with its location path.
func (c *CustomVaultProvider) GetEntryWithPath(ctx context.Context, location string) (Entry, string, error) {
	entry, err := c.GetEntry(ctx, location)
	if err != nil {
		return Entry{}, "", err
	}
	return entry, location, nil
}

// GetEntryTitle retrieves only the title of an entity by location.
func (c *CustomVaultProvider) GetEntryTitle(ctx context.Context, location string) (string, error) {
	entity, ok := c.entities[location]
	if !ok {
		return "", fmt.Errorf("custom_vault: entity %q not found", location)
	}
	if entity.Title != "" {
		return entity.Title, nil
	}
	for k, v := range entity.Attributes {
		if strings.EqualFold(k, "title") {
			if str, ok := v.(string); ok {
				return str, nil
			}
		}
	}
	return location, nil
}

// Search filters the entries using the given SearchQuery criteria.
func (c *CustomVaultProvider) Search(_ context.Context, query SearchQuery) ([]SearchResult, error) {
	var results []SearchResult

	var lowerQueryTitle string
	if query.Title != "" {
		lowerQueryTitle = strings.ToLower(query.Title)
	}

	var lowerQueryPath string
	if query.Path != "" {
		lowerQueryPath = strings.ToLower(query.Path)
	}

	for name, entity := range c.entities {
		var entry Entry
		if !entity.IsComposed {
			entry = toEntry(name, entity.Attributes)
		} else {
			e, err := c.resolveEntity(context.Background(), name, entity)
			if err != nil {
				continue
			}
			entry = e
		}

		if lowerQueryTitle != "" {
			if !strings.Contains(strings.ToLower(entry.Title), lowerQueryTitle) {
				continue
			}
		}

		if lowerQueryPath != "" {
			if !strings.Contains(strings.ToLower(name), lowerQueryPath) {
				continue
			}
		}

		if len(query.Tags) > 0 {
			match := true
			for _, qt := range query.Tags {
				found := false
				for _, t := range entry.Tags {
					if strings.EqualFold(t, qt) {
						found = true
						break
					}
				}
				if !found {
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

	// Sort deterministically by path so results are stable across runs
	// despite Go's randomized map iteration order.
	sort.Slice(results, func(i, j int) bool {
		return results[i].Path < results[j].Path
	})

	return results, nil
}

// parseCustomVaultLocation splits "entity_name:attribute" into name and attribute.
// If attribute is missing, it defaults to "Password".
func parseCustomVaultLocation(location string) (string, string, error) {
	if location == "" {
		return "", "", errors.New("custom_vault: empty location")
	}
	if strings.Contains(location, ":") {
		parts := strings.SplitN(location, ":", 2)
		return parts[0], parts[1], nil
	}
	return location, "Password", nil
}

// toEntry maps a raw attributes map to an Entry.
func toEntry(name string, raw map[string]any) Entry {
	entry := Entry{
		Title:      name,
		Attributes: make(map[string]any),
	}

	for k, v := range raw {
		kLower := strings.ToLower(k)
		switch kLower {
		case "tags":
			if tagSliceStr, ok := v.([]string); ok {
				entry.Tags = tagSliceStr
			} else {
				entry.Tags = utils.ParseTags(v)
			}
		case "title":
			if str, ok := v.(string); ok {
				entry.Title = str
			}
		default:
			entry.Attributes[k] = v
		}
	}

	return entry
}

// serializeVal converts structured yaml/json data to string format.
func serializeVal(val any) (string, error) {
	s, err := yaml.SerializeValue(val)
	if err != nil {
		return "", fmt.Errorf("custom_vault serialization failed: %w", err)
	}
	return s, nil
}

func (c *CustomVaultProvider) resolveEntity(ctx context.Context, name string, entity config.CustomEntityConfig) (Entry, error) {
	if !entity.IsComposed {
		return toEntry(name, entity.Attributes), nil
	}

	mergedAttributes := make(map[string]any)
	var mergedTags []string
	var mergedEntries []Entry

	for _, mURI := range entity.Merge {
		if c.getEntriesFunc == nil {
			return Entry{}, fmt.Errorf("custom_vault: merge execution not supported in this context")
		}
		entries, err := c.getEntriesFunc(ctx, mURI)
		if err != nil {
			return Entry{}, fmt.Errorf("custom_vault: failed to merge %q: %w", mURI, err)
		}
		for _, e := range entries {
			mergedEntries = append(mergedEntries, e)
			mergedTags = append(mergedTags, e.Tags...)
			for k, v := range e.Attributes {
				mergedAttributes[k] = v
			}
		}
	}

	mergedTags = uniqueStrings(mergedTags)

	if len(entity.Mapping) > 0 || len(entity.IncludeFields) > 0 || len(entity.ExcludeFields) > 0 {
		mapped, err := ApplyMappingAndFilteringToAttributes(
			mergedAttributes,
			entity.Mapping,
			entity.IncludeFields,
			entity.ExcludeFields,
			name,
			nil,
			Entry{Title: name, Tags: mergedTags, Attributes: mergedAttributes},
			"custom_vault",
			"custom_vault",
		)
		if err != nil {
			return Entry{}, fmt.Errorf("custom_vault: failed to apply mapping/filtering: %w", err)
		}
		mergedAttributes = mapped
	}

	if entity.KeyFormat != "" && entity.KeyFormat != "none" {
		tempEntry := ApplyKeyFormat(Entry{
			Title:      name,
			Tags:       mergedTags,
			Attributes: mergedAttributes,
		}, entity.KeyFormat)
		mergedAttributes = tempEntry.Attributes
	}

	if len(entity.Tags) > 0 {
		mergedTags = append(mergedTags, entity.Tags...)
		mergedTags = uniqueStrings(mergedTags)
	}

	title := entity.Title
	if title == "" {
		title = name
	}

	meta := map[string]any{
		"Title":    title,
		"title":    title,
		"Tags":     mergedTags,
		"tags":     mergedTags,
		"Vault":    "custom_vault",
		"vault":    "custom_vault",
		"Provider": "custom_vault",
		"provider": "custom_vault",
	}

	templateCtx := map[string]any{
		"meta":       meta,
		"Meta":       meta,
		"entries":    mergedEntries,
		"Entries":    mergedEntries,
		"attributes": mergedAttributes,
		"Attributes": mergedAttributes,
	}

	for k, v := range entity.Attributes {
		if str, ok := v.(string); ok && strings.Contains(str, "{{") {
			if c.renderTemplateFunc == nil {
				return Entry{}, fmt.Errorf("custom_vault: template rendering not supported in this context")
			}
			rendered, err := c.renderTemplateFunc(ctx, str, templateCtx)
			if err != nil {
				return Entry{}, fmt.Errorf("custom_vault: failed to render template for attribute %q: %w", k, err)
			}
			mergedAttributes[k] = rendered
		} else {
			mergedAttributes[k] = v
		}
	}

	return Entry{
		Title:      title,
		Tags:       mergedTags,
		Attributes: mergedAttributes,
	}, nil
}

func ApplyKeyFormat(entry Entry, format string) Entry {
	if format == "" || format == utils.KeyFormatNone {
		return entry
	}
	formatted := make(map[string]any, len(entry.Attributes))
	for k, v := range entry.Attributes {
		formatted[utils.FormatKeyWithFormat(k, format)] = formatValue(v, format)
	}
	entry.Attributes = formatted
	return entry
}

func uniqueStrings(s []string) []string {
	seen := make(map[string]bool)
	var res []string
	for _, v := range s {
		if !seen[v] {
			seen[v] = true
			res = append(res, v)
		}
	}
	return res
}

// SetGetEntriesFunc sets the resolver function.
func (c *CustomVaultProvider) SetGetEntriesFunc(fn GetEntriesFunc) {
	c.getEntriesFunc = fn
}

// SetRenderTemplateFunc sets the template renderer function.
func (c *CustomVaultProvider) SetRenderTemplateFunc(fn TemplateRenderFunc) {
	c.renderTemplateFunc = fn
}
