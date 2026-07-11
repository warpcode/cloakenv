package utils

import "strings"

// ParseTagString splits a comma-separated tags string into a slice of strings.
func ParseTagString(tagsStr string) []string {
	if tagsStr == "" {
		return nil
	}
	parts := strings.Split(tagsStr, ",")
	var tags []string
	for _, p := range parts {
		t := strings.TrimSpace(p)
		if t != "" {
			tags = append(tags, t)
		}
	}
	return tags
}

// ParseTags parses a generic value into a slice of tags.
// It supports either a slice of any or a comma-separated string.
func ParseTags(v any) []string {
	var tags []string
	if tagSlice, ok := v.([]any); ok {
		for _, t := range tagSlice {
			if str, ok := t.(string); ok {
				tags = append(tags, str)
			}
		}
	} else if tagStr, ok := v.(string); ok {
		tags = ParseTagString(tagStr)
	}
	return tags
}
