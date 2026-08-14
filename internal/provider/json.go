package provider

import (
	"encoding/json"
	"fmt"
)

// JsonProvider implements SecretProvider and SearchableProvider for static JSON registries.
type JsonProvider struct {
	staticProvider
}

// NewJsonProvider returns a new JsonProvider instance.
func NewJsonProvider() *JsonProvider {
	return &JsonProvider{
		staticProvider: staticProvider{
			scheme:    "json",
			unmarshal: json.Unmarshal,
			serialize: serializeJsonVal,
		},
	}
}

func serializeJsonVal(val any) (string, error) {
	switch v := val.(type) {
	case string:
		return v, nil
	case []any, map[string]any:
		data, err := json.Marshal(v)
		if err != nil {
			return "", fmt.Errorf("json serialization failed: %w", err)
		}
		return string(data), nil
	default:
		return fmt.Sprintf("%v", v), nil
	}
}
