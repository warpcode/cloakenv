package utils

import "strings"

// ParseTagString splits a comma-separated tags string into a slice of strings.
// It scans the string incrementally using strings.IndexByte to avoid allocating
// an intermediate slice from strings.Split, and pre-counts commas via strings.Count
// to size the returned slice buffer in a single allocation.
func ParseTagString(tagsStr string) []string {
	if tagsStr == "" {
		return nil
	}

	n := strings.Count(tagsStr, ",") + 1

	var tags []string
	s := tagsStr
	for len(s) > 0 {
		var part string
		if idx := strings.IndexByte(s, ','); idx >= 0 {
			part = s[:idx]
			s = s[idx+1:]
		} else {
			part = s
			s = ""
		}

		t := strings.TrimSpace(part)
		if t != "" {
			if tags == nil {
				tags = make([]string, 0, n)
			}
			tags = append(tags, t)
		}
	}
	return tags
}

// ParseTags parses a generic value into a slice of tags.
// It supports either a slice of strings, a slice of any, or a comma-separated string.
// If a []string is provided, it returns a clone to preserve slice immutability.
func ParseTags(v any) []string {
	switch val := v.(type) {
	case []string:
		return append([]string(nil), val...)
	case []any:
		var tags []string
		for _, t := range val {
			if str, ok := t.(string); ok {
				if tags == nil {
					tags = make([]string, 0, len(val))
				}
				tags = append(tags, str)
			}
		}
		return tags
	case string:
		return ParseTagString(val)
	default:
		return nil
	}
}
