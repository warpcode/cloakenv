package utils

import (
	"fmt"
	"strings"
)

// ParseURI splits "scheme://location" into its components.
// Uses strings.Cut to avoid slice allocations.
func ParseURI(uri string) (string, string, error) {
	scheme, location, ok := strings.Cut(uri, "://")
	if !ok || scheme == "" {
		return "", "", fmt.Errorf("malformed URI: %q (expected scheme://location)", uri)
	}
	return scheme, location, nil
}
