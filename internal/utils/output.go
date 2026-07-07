package utils

import (
	"encoding/json"
	"fmt"
	"os"

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
