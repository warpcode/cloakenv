package provider

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/warpcode/cloakenv/internal/utils"
)

// serializeVal converts structured data to string format.
func serializeVal(val any) (string, error) {
	s, err := utils.SerializeYAMLValue(val)
	if err != nil {
		return "", fmt.Errorf("serialization failed: %w", err)
	}
	return s, nil
}

func matchTags(entryTags, queryTagsLower []string) bool {
	for _, qt := range queryTagsLower {
		found := false
		for _, t := range entryTags {
			// Optimization: strings.EqualFold performs Unicode case-folding directly without
			// allocating new strings on the heap, unlike strings.ToLower(t).
			if strings.EqualFold(t, qt) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func matchEntry(entry Entry, path string, query SearchQuery, queryTitleLower, queryPathLower string, queryTagsLower []string) bool {
	if query.Title != "" && !strings.Contains(strings.ToLower(entry.Title), queryTitleLower) {
		return false
	}
	if query.Path != "" && !strings.Contains(strings.ToLower(path), queryPathLower) {
		return false
	}
	if len(query.Tags) > 0 {
		tags := entry.Tags
		if len(tags) == 0 {
			if v, ok := entry.Attributes["tags"]; ok {
				tags = utils.ParseTags(v)
			} else if v, ok := entry.Attributes["Tags"]; ok {
				tags = utils.ParseTags(v)
			}
		}
		if !matchTags(tags, queryTagsLower) {
			return false
		}
	}
	return true
}

func anyToString(v any) string {
	switch val := v.(type) {
	case string:
		return val
	case int:
		return strconv.Itoa(val)
	case bool:
		return strconv.FormatBool(val)
	default:
		return fmt.Sprintf("%v", v)
	}
}

func normalizeEntryMap(v any) (map[string]any, bool) {
	switch m := v.(type) {
	case map[string]any:
		return m, true
	case map[any]any:
		converted := make(map[string]any, len(m))
		for ek, ev := range m {
			converted[anyToString(ek)] = ev
		}
		return converted, true
	default:
		return nil, false
	}
}

func resolveDotPath(val any, path string) (any, error) {
	if path == "" {
		return val, nil
	}
	curr := val
	remaining := path

	for len(remaining) > 0 {
		var part string
		part, remaining, _ = strings.Cut(remaining, ".")
		if part == "" {
			continue
		}
		switch m := curr.(type) {
		case map[string]any:
			next, ok := m[part]
			if !ok {
				return nil, fmt.Errorf("key %q not found", part)
			}
			curr = next
		case map[any]any:
			next, ok := m[part]
			if !ok {
				found := false
				for k, v := range m {
					if anyToString(k) == part {
						next = v
						found = true
						break
					}
				}
				if !found {
					return nil, fmt.Errorf("key %q not found", part)
				}
			}
			curr = next
		case []any:
			idx, err := strconv.Atoi(part)
			if err != nil {
				return nil, fmt.Errorf("cannot index array with non-integer %q", part)
			}
			if idx < 0 || idx >= len(m) {
				return nil, fmt.Errorf("index %d out of bounds (length %d)", idx, len(m))
			}
			curr = m[idx]
		default:
			return nil, fmt.Errorf("cannot traverse key %q on value of type %T", part, curr)
		}
	}
	return curr, nil
}
