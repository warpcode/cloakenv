package provider

import (
	"fmt"

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
	s, err := yaml.SerializeValue(val)
	if err != nil {
		return "", fmt.Errorf("yaml serialization failed: %w", err)
	}
	return s, nil
}
