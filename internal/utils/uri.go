package utils

import (
	"fmt"
	"strings"
)

// ParseURI splits "scheme://location" into its components.
//
// Security: URIs containing null bytes (\x00) are explicitly rejected to prevent
// null byte injection and string truncation attacks across downstream providers.
func ParseURI(uri string) (string, string, error) {
	if strings.IndexByte(uri, 0) != -1 {
		return "", "", fmt.Errorf("malformed URI: contains null byte")
	}

	// Optimization: strings.Cut avoids allocating a slice of strings on the heap
	// for every parsed secret URI across resolvers, providers, and templates.
	scheme, location, found := strings.Cut(uri, "://")
	if !found || scheme == "" {
		return "", "", fmt.Errorf("malformed URI: %q (expected scheme://location)", uri)
	}
	return scheme, location, nil
}
