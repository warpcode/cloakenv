package utils

import (
	"fmt"
	"strings"
)

// ParseURI splits "scheme://location" into its components.
func ParseURI(uri string) (string, string, error) {
	parts := strings.SplitN(uri, "://", 2)
	if len(parts) != 2 || parts[0] == "" {
		return "", "", fmt.Errorf("malformed URI: %q (expected scheme://location)", uri)
	}
	return parts[0], parts[1], nil
}
