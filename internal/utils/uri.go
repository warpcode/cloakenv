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

	// RFC 3986 section 3.1: scheme = ALPHA *( ALPHA / DIGIT / "+" / "-" / "." )
	if !isAlpha(scheme[0]) {
		return "", "", fmt.Errorf("malformed URI: %q (scheme must begin with a letter)", uri)
	}
	for i := 1; i < len(scheme); i++ {
		c := scheme[i]
		if !isAlpha(c) && !isDigit(c) && c != '+' && c != '-' && c != '.' && c != '_' {
			return "", "", fmt.Errorf("malformed URI: %q (invalid character %q in scheme)", uri, c)
		}
	}

	return scheme, location, nil
}

func isAlpha(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isDigit(c byte) bool {
	return c >= '0' && c <= '9'
}
