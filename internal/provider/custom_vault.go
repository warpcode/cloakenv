package provider

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/warpcode/cloakenv/internal/utils"

	"gopkg.in/yaml.v3"
)

// CustomVaultProvider implements SecretProvider and SearchableProvider for multi-entity inline static configs.
type CustomVaultProvider struct {
	entities map[string]map[string]any
}

// NewCustomVaultProvider returns a new CustomVaultProvider instance.
func NewCustomVaultProvider() *CustomVaultProvider {
	return &CustomVaultProvider{
		entities: make(map[string]map[string]any),
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
		c.entities = make(map[string]map[string]any)
	}
	return nil
}

// GetSecret retrieves a secret from the static inline configuration.
func (c *CustomVaultProvider) GetSecret(_ context.Context, location string) (string, error) {
	entityName, attr, err := parseCustomVaultLocation(location)
	if err != nil {
		return "", err
	}

	entity, ok := c.entities[entityName]
	if !ok {
		return "", fmt.Errorf("custom_vault: entity %q not found", entityName)
	}

	val, ok := entity[attr]
	if !ok {
		return "", fmt.Errorf("custom_vault: attribute %q not found for entity %q", attr, entityName)
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
func (c *CustomVaultProvider) GetEntry(_ context.Context, location string) (Entry, error) {
	entity, ok := c.entities[location]
	if !ok {
		return Entry{}, fmt.Errorf("custom_vault: entity %q not found", location)
	}

	return toEntry(location, entity), nil
}

// Search filters the entries using the given SearchQuery criteria.
func (c *CustomVaultProvider) Search(_ context.Context, query SearchQuery) ([]SearchResult, error) {
	var results []SearchResult

	for name, entity := range c.entities {
		entry := toEntry(name, entity)

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
	switch v := val.(type) {
	case string:
		return v, nil
	case []any, map[string]any, map[any]any:
		data, err := yaml.Marshal(v)
		if err != nil {
			return "", fmt.Errorf("custom_vault serialization failed: %w", err)
		}
		return strings.TrimSuffix(string(data), "\n"), nil
	default:
		return fmt.Sprintf("%v", v), nil
	}
}
