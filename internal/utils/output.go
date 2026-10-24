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

// SupportedKeyFormats defines the valid key formatting options.
var SupportedKeyFormats = []string{"none", "env", "snake", "kebab"}

// ValidateKeyFormat checks if the provided format is supported.
func ValidateKeyFormat(format string) error {
	switch format {
	case "none", "env", "snake", "kebab":
		return nil
	default:
		return fmt.Errorf("invalid key format %q (expected none, env, snake, or kebab)", format)
	}
}

// FormatKeyWithFormat formats a key according to the requested format:
// "none": unchanged
// "env": UPPER_CASE with single underscores
// "snake": lower_snake_case with single underscores
// "kebab": lower-kebab-case with single hyphens
func FormatKeyWithFormat(key string, format string) string {
	if key == "" || format == "none" {
		return key
	}
	switch format {
	case "env":
		return FormatKey(key)
	case "snake":
		return formatKeyWithSep(key, '_', false)
	case "kebab":
		return formatKeyWithSep(key, '-', false)
	default:
		return key
	}
}

func formatKeyWithSep(key string, sep byte, uppercase bool) string {
	if key == "" {
		return ""
	}

	b := make([]byte, 0, len(key))
	inNonAlpha := false
	for i := range len(key) {
		c := key[i]
		if ('a' <= c && c <= 'z') || ('A' <= c && c <= 'Z') || ('0' <= c && c <= '9') {
			if uppercase && ('a' <= c && c <= 'z') {
				c = c - 'a' + 'A'
			} else if !uppercase && ('A' <= c && c <= 'Z') {
				c = c - 'A' + 'a'
			}
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

// FormatValueWithKeyFormat recursively formats keys in nested maps/structures.
func FormatValueWithKeyFormat(val any, format string) any {
	if format == "none" || val == nil {
		return val
	}
	switch m := val.(type) {
	case map[string]any:
		res := make(map[string]any, len(m))
		for k, v := range m {
			res[FormatKeyWithFormat(k, format)] = FormatValueWithKeyFormat(v, format)
		}
		return res
	case map[any]any:
		res := make(map[string]any, len(m))
		for k, v := range m {
			strKey := fmt.Sprintf("%v", k)
			res[FormatKeyWithFormat(strKey, format)] = FormatValueWithKeyFormat(v, format)
		}
		return res
	case []any:
		res := make([]any, len(m))
		for i, v := range m {
			res[i] = FormatValueWithKeyFormat(v, format)
		}
		return res
	default:
		return val
	}
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
