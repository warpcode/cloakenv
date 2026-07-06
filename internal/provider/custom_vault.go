package provider

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// CustomVaultProvider implements SecretProvider and SearchableProvider for inline static configs.
type CustomVaultProvider struct {
	singleEntity bool
	entityName   string
	tags         []string
	attributes   map[string]any
	entities     map[string]map[string]any
}

// NewCustomVaultProvider returns a new CustomVaultProvider instance.
func NewCustomVaultProvider() *CustomVaultProvider {
	return &CustomVaultProvider{
		attributes: make(map[string]any),
		entities:   make(map[string]map[string]any),
	}
}

// Scheme returns "custom_vault".
func (c *CustomVaultProvider) Scheme() string {
	return "custom_vault"
}

// Initialize populates the provider with static configs.
func (c *CustomVaultProvider) Initialize(_ context.Context, cfg ProviderConfig) error {
	if cfg.SingleEntity != nil {
		c.singleEntity = *cfg.SingleEntity
	} else {
		c.singleEntity = false
	}
	c.entityName = cfg.EntityName
	c.tags = cfg.Tags

	if c.singleEntity {
		c.attributes = cfg.Attributes
		if c.attributes == nil {
			c.attributes = make(map[string]any)
		}
	} else {
		c.entities = cfg.Entities
		if c.entities == nil {
			c.entities = make(map[string]map[string]any)
		}
	}

	return nil
}

// GetSecret retrieves a secret from the static inline configuration.
func (c *CustomVaultProvider) GetSecret(_ context.Context, location string) (string, error) {
	if c.singleEntity {
		val, ok := c.attributes[location]
		if !ok {
			return "", fmt.Errorf("custom_vault: attribute %q not found", location)
		}
		return serializeVal(val)
	}

	// Multiple entity
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

// GetEntry retrieves a complete structured entry by location.
func (c *CustomVaultProvider) GetEntry(_ context.Context, location string) (Entry, error) {
	if c.singleEntity {
		// Single-entity: return the one single entry. The location is ignored or must be empty/ignored.
		title := c.entityName
		if title == "" {
			title = "custom_vault"
		}
		return Entry{
			Title:      title,
			Tags:       c.tags,
			Attributes: c.attributes,
		}, nil
	}

	// Multiple entity: location specifies the entity name
	entity, ok := c.entities[location]
	if !ok {
		return Entry{}, fmt.Errorf("custom_vault: entity %q not found", location)
	}

	return toEntry(location, entity), nil
}

// Search filters the entries using the given SearchQuery criteria.
func (c *CustomVaultProvider) Search(_ context.Context, query SearchQuery) ([]SearchResult, error) {
	var results []SearchResult

	if c.singleEntity {
		title := c.entityName
		if title == "" {
			title = "custom_vault"
		}

		if query.Title != "" {
			if !strings.Contains(strings.ToLower(title), strings.ToLower(query.Title)) {
				return results, nil
			}
		}

		if len(query.Tags) > 0 {
			tagMap := make(map[string]bool)
			for _, t := range c.tags {
				tagMap[strings.ToLower(t)] = true
			}
			for _, qt := range query.Tags {
				if !tagMap[strings.ToLower(qt)] {
					return results, nil
				}
			}
		}

		results = append(results, SearchResult{
			Path: "",
			Entry: Entry{
				Title:      title,
				Tags:       c.tags,
				Attributes: c.attributes,
			},
		})
		return results, nil
	}

	// Multiple entity
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
		if kLower == "tags" {
			if tagSlice, ok := v.([]any); ok {
				for _, t := range tagSlice {
					if str, ok := t.(string); ok {
						entry.Tags = append(entry.Tags, str)
					}
				}
			} else if tagSliceStr, ok := v.([]string); ok {
				entry.Tags = tagSliceStr
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


