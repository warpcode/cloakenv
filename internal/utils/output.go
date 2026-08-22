package utils

import (
	"encoding/json"
	"fmt"
	"os"

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

// FormatKey formats a key to THIS_KEY format: uppercase with non-alphanumeric
// runs replaced by a single underscore.
func FormatKey(key string) string {
	if key == "" {
		return ""
	}

	needsTransform := false
	inNonAlpha := false
	for i := range len(key) {
		c := key[i]
		if ('A' <= c && c <= 'Z') || ('0' <= c && c <= '9') {
			inNonAlpha = false
		} else if 'a' <= c && c <= 'z' {
			needsTransform = true
			break
		} else {
			if inNonAlpha {
				needsTransform = true
				break
			}
			if c != '_' {
				needsTransform = true
				break
			}
			inNonAlpha = true
		}
	}

	if !needsTransform {
		return key
	}

	b := make([]byte, 0, len(key))
	inNonAlpha = false
	for i := range len(key) {
		c := key[i]
		if ('a' <= c && c <= 'z') || ('A' <= c && c <= 'Z') || ('0' <= c && c <= '9') {
			if 'a' <= c && c <= 'z' {
				c = c - 'a' + 'A'
			}
			b = append(b, c)
			inNonAlpha = false
		} else {
			if !inNonAlpha {
				b = append(b, '_')
				inNonAlpha = true
			}
		}
	}
	return string(b)
}

// SerializeAttrValue converts an attribute value into a string representation for environment usage.
// Strings are returned as-is, slices and maps are serialized as YAML, and other types are formatted using fast-path conversions or %v.
func SerializeAttrValue(val any) (string, error) {
	return yaml.SerializeValue(val)
}
