package provider

import (
	"fmt"
	"strings"

	"github.com/warpcode/cloakenv/internal/yaml"
)

// YamlProvider implements SecretProvider and SearchableProvider for static YAML registries.
type YamlProvider struct {
	staticProvider
}

// NewYamlProvider returns a new YamlProvider instance.
func NewYamlProvider() *YamlProvider {
	return &YamlProvider{
		staticProvider: staticProvider{
			scheme:    "yaml",
			unmarshal: yaml.Unmarshal,
			serialize: serializeYamlVal,
		},
	}
}

// serializeYamlVal converts structured YAML data to string format.
func serializeYamlVal(val any) (string, error) {
	switch v := val.(type) {
	case string:
		return v, nil
	case []any, map[string]any, map[any]any:
		data, err := yaml.Marshal(v)
		if err != nil {
			return "", fmt.Errorf("yaml serialization failed: %w", err)
		}
		return strings.TrimSuffix(string(data), "\n"), nil
	default:
		return anyToString(v), nil
	}
}
