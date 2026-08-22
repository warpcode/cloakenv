package utils

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/warpcode/cloakenv/internal/yaml"
)

// RenderOutput serializes the data to YAML or JSON and writes it to stdout.
func RenderOutput(data any, asJSON bool, errorLabel string) error {
	if asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(data); err != nil {
			return fmt.Errorf("failed to serialize %s to JSON: %w", errorLabel, err)
		}
	} else {
		enc := yaml.NewEncoder(os.Stdout)
		enc.SetIndent(2)
		if err := enc.Encode(data); err != nil {
			return fmt.Errorf("failed to serialize %s to YAML: %w", errorLabel, err)
		}
	}
	return nil
}

var nonAlphanumericRun = regexp.MustCompile(`[^a-zA-Z0-9]+`)

// FormatKey formats a key to THIS_KEY format: uppercase with non-alphanumeric
// runs replaced by a single underscore.
func FormatKey(key string) string {
	return strings.ToUpper(nonAlphanumericRun.ReplaceAllString(key, "_"))
}

// SerializeAttrValue converts an attribute value into a string representation for environment usage.
// Strings are returned as-is, slices and maps are serialized as YAML, and other types are formatted using %v.
func SerializeAttrValue(val any) (string, error) {
	switch v := val.(type) {
	case string:
		return v, nil
	case []any, map[string]any, map[any]any, []string:
		data, err := yaml.Marshal(v)
		if err != nil {
			return "", err
		}
		return strings.TrimSuffix(string(data), "\n"), nil
	default:
		return fmt.Sprintf("%v", v), nil
	}
}
