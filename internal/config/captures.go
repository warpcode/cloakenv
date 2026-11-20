package config

import (
	"fmt"
	"strconv"
	"strings"
)

// RewriteCapturePlaceholdersForTemplate replaces $0..$N and ${0}..${N} capture
// placeholders in a Go text/template string with safe index lookups against
// .__captures so captured runtime strings are treated strictly as data rather
// than template syntax. When capCount < 0, any non-negative integer capture
// index is rewritten; when capCount == 0, tmpl is returned unchanged.
func RewriteCapturePlaceholdersForTemplate(tmpl string, capCount int) string {
	if capCount == 0 || !strings.ContainsRune(tmpl, '$') {
		return tmpl
	}
	inRange := func(idx int) bool {
		if idx < 0 {
			return false
		}
		return capCount < 0 || idx < capCount
	}
	var sb strings.Builder
	sb.Grow(len(tmpl))
	inDelim := false
	inQuote := byte(0)
	for i := 0; i < len(tmpl); i++ {
		if !inDelim && i+1 < len(tmpl) && tmpl[i] == '{' && tmpl[i+1] == '{' {
			inDelim = true
			sb.WriteString("{{")
			i++
			continue
		}
		if inDelim {
			if inQuote != 0 {
				if tmpl[i] == '\\' && inQuote == '"' && i+1 < len(tmpl) {
					sb.WriteByte(tmpl[i])
					sb.WriteByte(tmpl[i+1])
					i++
					continue
				}
				if tmpl[i] == inQuote {
					inQuote = 0
				}
				sb.WriteByte(tmpl[i])
				continue
			}
			if tmpl[i] == '"' || tmpl[i] == '`' {
				inQuote = tmpl[i]
				sb.WriteByte(tmpl[i])
				continue
			}
			if i+1 < len(tmpl) && tmpl[i] == '}' && tmpl[i+1] == '}' {
				inDelim = false
				sb.WriteString("}}")
				i++
				continue
			}
		}

		if tmpl[i] == '$' && i+1 < len(tmpl) {
			if tmpl[i+1] == '{' {
				end := strings.IndexByte(tmpl[i+2:], '}')
				if end > 0 {
					idxStr := tmpl[i+2 : i+2+end]
					if idx, err := strconv.Atoi(idxStr); err == nil && inRange(idx) {
						if inDelim {
							fmt.Fprintf(&sb, "(index .__captures %d)", idx)
						} else {
							fmt.Fprintf(&sb, "{{ index .__captures %d }}", idx)
						}
						i += 2 + end
						continue
					}
				}
			} else if tmpl[i+1] >= '0' && tmpl[i+1] <= '9' {
				j := i + 1
				for j < len(tmpl) && tmpl[j] >= '0' && tmpl[j] <= '9' {
					j++
				}
				if idx, err := strconv.Atoi(tmpl[i+1 : j]); err == nil && inRange(idx) {
					if inDelim {
						fmt.Fprintf(&sb, "(index .__captures %d)", idx)
					} else {
						fmt.Fprintf(&sb, "{{ index .__captures %d }}", idx)
					}
					i = j - 1
					continue
				}
			}
		}
		sb.WriteByte(tmpl[i])
	}
	return sb.String()
}
