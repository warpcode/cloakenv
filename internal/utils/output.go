package utils

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
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
