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
			if closeErr := enc.Close(); closeErr != nil {
				return fmt.Errorf("failed to serialize %s to YAML: %w (close error: %w)", errorLabel, err, closeErr)
			}
			return fmt.Errorf("failed to serialize %s to YAML: %w", errorLabel, err)
		}
		if err := enc.Close(); err != nil {
			return fmt.Errorf("failed to serialize %s to YAML: %w", errorLabel, err)
		}
	}
	return nil
}

// Key format identifiers.
const (
	KeyFormatNone  = "none"
	KeyFormatEnv   = "env"
	KeyFormatSnake = "snake"
	KeyFormatKebab = "kebab"
)

// FormatKeyWithFormat transforms key according to the specified format:
// - "none" (default): returns key unmodified
// - "env": uppercase with non-alphanumeric runs replaced by a single underscore
// - "snake": lowercase with non-alphanumeric runs replaced by a single underscore
// - "kebab": lowercase with non-alphanumeric runs replaced by a single hyphen
func FormatKeyWithFormat(key string, format string) string {
	if key == "" || format == "" || format == KeyFormatNone {
		return key
	}

	sep := byte('_')
	toUpper := false
	if format == KeyFormatEnv {
		toUpper = true
	} else if format == KeyFormatKebab {
		sep = '-'
	}

	needsTransform := false
	inNonAlpha := false
	for i := range len(key) {
		c := key[i]
		if 'A' <= c && c <= 'Z' {
			if !toUpper {
				needsTransform = true
				break
			}
			inNonAlpha = false
		} else if 'a' <= c && c <= 'z' {
			if toUpper {
				needsTransform = true
				break
			}
			inNonAlpha = false
		} else if '0' <= c && c <= '9' {
			inNonAlpha = false
		} else {
			if inNonAlpha || c != sep {
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
		if 'a' <= c && c <= 'z' {
			if toUpper {
				c = c - 'a' + 'A'
			}
			b = append(b, c)
			inNonAlpha = false
		} else if 'A' <= c && c <= 'Z' {
			if !toUpper {
				c = c - 'A' + 'a'
			}
			b = append(b, c)
			inNonAlpha = false
		} else if '0' <= c && c <= '9' {
			b = append(b, c)
			inNonAlpha = false
		} else {
			if !inNonAlpha {
				b = append(b, sep)
				inNonAlpha = true
			}
		}
	}
	return string(b)
}

// FormatKey formats a key to THIS_KEY format: uppercase with non-alphanumeric
// runs replaced by a single underscore.
func FormatKey(key string) string {
	return FormatKeyWithFormat(key, KeyFormatEnv)
}

// SerializeAttrValue converts an attribute value into a string representation for environment usage.
// Strings are returned as-is, slices and maps are serialized as YAML, and other types are formatted using fast-path conversions or %v.
func SerializeAttrValue(val any) (string, error) {
	return yaml.SerializeValue(val)
}
