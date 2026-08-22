package utils

import (
	"fmt"
	"strings"
)

// ExpandString parses a string for `${...}` placeholders and replaces them
// by invoking resolveFunc on the inner text. It supports escaping `$` as `$$`.
// If configKey is provided, it is included in error messages.
func ExpandString(s string, configKey string, resolveFunc func(uri string) (string, error)) (string, error) {
	var sb strings.Builder
	i := 0
	n := len(s)
	for i < n {
		if i+1 < n && s[i] == '$' && s[i+1] == '$' {
			sb.WriteByte('$')
			i += 2
			continue
		}
		if i+1 < n && s[i] == '$' && s[i+1] == '{' {
			// Find matching '}'
			start := i + 2
			end := -1
			for j := start; j < n; j++ {
				if s[j] == '}' {
					end = j
					break
				}
			}
			if end == -1 {
				keyPart := ""
				if configKey != "" {
					keyPart = fmt.Sprintf(" in configuration key %q", configKey)
				}
				return "", fmt.Errorf("unclosed expansion syntax '${...}'%s", keyPart)
			}

			innerText := s[start:end]
			if strings.Contains(innerText, "${") {
				keyPart := ""
				if configKey != "" {
					keyPart = fmt.Sprintf(" in configuration key %q", configKey)
				}
				return "", fmt.Errorf("nested expansions are not supported%s: %q", keyPart, s)
			}

			resolved, err := resolveFunc(innerText)
			if err != nil {
				keyPart := ""
				if configKey != "" {
					keyPart = fmt.Sprintf(" in configuration key %q", configKey)
				}
				return "", fmt.Errorf("failed to resolve expansion %q%s: %w", innerText, keyPart, err)
			}

			sb.WriteString(resolved)
			i = end + 1
			continue
		}
		sb.WriteByte(s[i])
		i++
	}
	return sb.String(), nil
}
