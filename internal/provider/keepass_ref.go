package provider

import (
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/tobischo/gokeepasslib/v3"
)

// nowFunc returns current time. Overridden in tests for deterministic date/time placeholder testing.
var nowFunc = time.Now

// keepassPlaceholderRegex matches same-entry placeholders like {TITLE}, {USERNAME}, {PASSWORD}, {URL}, {NOTES}, {S:custom},
// date/time placeholders (e.g. {DT_SIMPLE}, {DT_UTC_YEAR}), and cross-entry field references like {REF:<WantedField>@<SearchIn>:<SearchText>}.
var keepassPlaceholderRegex = regexp.MustCompile(`(?i)\{(?:REF:([^@]+)@([^:]+):([^}]+)|(TITLE|USERNAME|PASSWORD|URL|NOTES|DT_SIMPLE|DT_YEAR|DT_MONTH|DT_DAY|DT_HOUR|DT_MINUTE|DT_SECOND|DT_UTC_SIMPLE|DT_UTC_YEAR|DT_UTC_MONTH|DT_UTC_DAY|DT_UTC_HOUR|DT_UTC_MINUTE|DT_UTC_SECOND|S:[^}]+))\}`)

// resolveValue is the top-level entry point for resolving placeholders and references in a field value.
func (k *KeePassProvider) resolveValue(entry *gokeepasslib.Entry, fieldKey string, val string) (string, error) {
	visited := make(map[string]bool)
	return k.resolveField(entry, fieldKey, val, 0, visited)
}

// resolveField recursively resolves placeholders, T-CONV text transformations, and field references within a field value string.
// It enforces a maximum recursion depth limit of 10 and uses a visited stack to detect circular references.
func (k *KeePassProvider) resolveField(entry *gokeepasslib.Entry, fieldKey string, val string, depth int, visited map[string]bool) (string, error) {
	if depth >= 10 {
		return "", errors.New("keepass reference error: maximum recursion depth (10) exceeded")
	}

	if fieldKey != "" {
		refKey := hex.EncodeToString(entry.UUID[:]) + ":" + strings.ToLower(fieldKey)
		if visited[refKey] {
			return "", fmt.Errorf("keepass reference error: circular reference detected for entry %q field %q", k.getEntryTitle(entry), fieldKey)
		}
		visited[refKey] = true
		defer delete(visited, refKey)
	}

	if !strings.Contains(val, "{") || !strings.Contains(val, "}") {
		return val, nil
	}

	// First, resolve any {T-CONV:/<text>/<method>/} placeholders from innermost to outermost
	for {
		match, ok := findInnermostTConv(val)
		if !ok {
			break
		}
		resolvedText, err := k.resolveField(entry, "", match.text, depth+1, visited)
		if err != nil {
			return "", err
		}
		transformed, err := applyTConvMethod(resolvedText, match.method)
		if err != nil {
			return "", err
		}
		val = val[:match.start] + transformed + val[match.end:]
	}

	if !strings.Contains(val, "{") || !strings.Contains(val, "}") {
		return val, nil
	}

	submatches := keepassPlaceholderRegex.FindAllStringSubmatchIndex(val, -1)
	if len(submatches) == 0 {
		return val, nil
	}

	var sb strings.Builder
	lastIdx := 0
	for _, sm := range submatches {
		matchStart := sm[0]
		matchEnd := sm[1]

		sb.WriteString(val[lastIdx:matchStart])
		lastIdx = matchEnd

		if sm[2] != -1 { // Group 1: Cross-entry reference
			wantedField := val[sm[2]:sm[3]]
			searchIn := val[sm[4]:sm[5]]
			searchText := val[sm[6]:sm[7]]

			res, err := k.resolveCrossReference(wantedField, searchIn, searchText, depth, visited)
			if err != nil {
				return "", err
			}
			sb.WriteString(res)
		} else if sm[8] != -1 { // Group 4: Same-entry placeholder
			placeholder := val[sm[8]:sm[9]]

			res, err := k.resolveSameEntryPlaceholder(entry, placeholder, depth, visited)
			if err != nil {
				return "", err
			}
			sb.WriteString(res)
		} else {
			sb.WriteString(val[matchStart:matchEnd])
		}
	}
	sb.WriteString(val[lastIdx:])
	return sb.String(), nil
}

// resolveSameEntryPlaceholder resolves a same-entry placeholder (e.g. {TITLE}, {PASSWORD}, {S:custom}, {DT_SIMPLE}).
func (k *KeePassProvider) resolveSameEntryPlaceholder(entry *gokeepasslib.Entry, placeholder string, depth int, visited map[string]bool) (string, error) {
	if dtVal, ok := formatDateTimePlaceholder(placeholder); ok {
		return dtVal, nil
	}
	targetField := mapPlaceholderToField(placeholder)
	if targetField == "" {
		return "{" + placeholder + "}", nil
	}
	rawVal := k.getEntryRawValue(entry, targetField)
	return k.resolveField(entry, targetField, rawVal, depth+1, visited)
}

// resolveCrossReference resolves a cross-entry field reference ({REF:<WantedField>@<SearchIn>:<SearchText>}).
func (k *KeePassProvider) resolveCrossReference(wantedFieldStr, searchInStr, searchText string, depth int, visited map[string]bool) (string, error) {
	targetEntry := k.findCrossReferenceEntry(searchInStr, searchText)
	if targetEntry == nil {
		return "", fmt.Errorf("keepass reference error: entry matching %s:%q not found", searchInStr, searchText)
	}

	targetField := mapWantedFieldToField(wantedFieldStr)
	if targetField == "" {
		return "", fmt.Errorf("keepass reference error: invalid WantedField %q in reference", wantedFieldStr)
	}

	rawVal := k.getEntryRawValue(targetEntry, targetField)
	return k.resolveField(targetEntry, targetField, rawVal, depth+1, visited)
}

// formatDateTimePlaceholder evaluates date/time placeholders (local or UTC) and returns the formatted time string.
// If the placeholder is not a date/time placeholder, it returns ("", false).
func formatDateTimePlaceholder(placeholder string) (string, bool) {
	now := nowFunc()
	up := strings.ToUpper(placeholder)
	switch up {
	case "DT_SIMPLE":
		return now.Format("20060102150405"), true
	case "DT_YEAR":
		return now.Format("2006"), true
	case "DT_MONTH":
		return now.Format("01"), true
	case "DT_DAY":
		return now.Format("02"), true
	case "DT_HOUR":
		return now.Format("15"), true
	case "DT_MINUTE":
		return now.Format("04"), true
	case "DT_SECOND":
		return now.Format("05"), true

	case "DT_UTC_SIMPLE":
		return now.UTC().Format("20060102150405"), true
	case "DT_UTC_YEAR":
		return now.UTC().Format("2006"), true
	case "DT_UTC_MONTH":
		return now.UTC().Format("01"), true
	case "DT_UTC_DAY":
		return now.UTC().Format("02"), true
	case "DT_UTC_HOUR":
		return now.UTC().Format("15"), true
	case "DT_UTC_MINUTE":
		return now.UTC().Format("04"), true
	case "DT_UTC_SECOND":
		return now.UTC().Format("05"), true
	}
	return "", false
}

// mapPlaceholderToField converts a placeholder string (e.g. "TITLE", "USERNAME", "S:foo") to a field key.
func mapPlaceholderToField(placeholder string) string {
	up := strings.ToUpper(placeholder)
	switch up {
	case "TITLE":
		return "Title"
	case "USERNAME":
		return "UserName"
	case "PASSWORD":
		return "Password"
	case "URL":
		return "URL"
	case "NOTES":
		return "Notes"
	}
	if strings.HasPrefix(up, "S:") {
		return placeholder[2:]
	}
	return ""
}

// mapWantedFieldToField converts a WantedField code (e.g. "T", "U", "P", "A", "N", "S:foo") to a field key.
func mapWantedFieldToField(wantedFieldStr string) string {
	up := strings.ToUpper(wantedFieldStr)
	switch up {
	case "T":
		return "Title"
	case "U":
		return "UserName"
	case "P":
		return "Password"
	case "A":
		return "URL"
	case "N":
		return "Notes"
	}
	if strings.HasPrefix(up, "S:") {
		return wantedFieldStr[2:]
	}
	return ""
}

// getEntryRawValue retrieves the raw string value of a field from a KeePass entry.
func (k *KeePassProvider) getEntryRawValue(entry *gokeepasslib.Entry, fieldKey string) string {
	// 1. Exact match
	for _, v := range entry.Values {
		if v.Key == fieldKey {
			return v.Value.Content
		}
	}
	// 2. Special case for UserName / Username
	if strings.EqualFold(fieldKey, "UserName") {
		for _, v := range entry.Values {
			if strings.EqualFold(v.Key, "UserName") || strings.EqualFold(v.Key, "Username") {
				return v.Value.Content
			}
		}
	}
	// 3. Case-insensitive match
	for _, v := range entry.Values {
		if strings.EqualFold(v.Key, fieldKey) {
			return v.Value.Content
		}
	}
	return ""
}

// findCrossReferenceEntry locates a target entry matching SearchIn:SearchText.
// Exact match is attempted across all entries first, falling back to case-insensitive substring search.
func (k *KeePassProvider) findCrossReferenceEntry(searchInStr, searchText string) *gokeepasslib.Entry {
	entries := k.getAllEntries()
	if len(entries) == 0 {
		return nil
	}

	// Pass 1: Exact match
	for _, entry := range entries {
		if k.matchSearchIn(entry, searchInStr, searchText, true) {
			return entry
		}
	}

	// Pass 2: Case-insensitive substring match
	for _, entry := range entries {
		if k.matchSearchIn(entry, searchInStr, searchText, false) {
			return entry
		}
	}

	return nil
}

// getAllEntries collects pointers to all entries in the database in traversal order.
func (k *KeePassProvider) getAllEntries() []*gokeepasslib.Entry {
	if k.db == nil || len(k.db.Content.Root.Groups) == 0 {
		return nil
	}
	var entries []*gokeepasslib.Entry
	var traverse func(g *gokeepasslib.Group)
	traverse = func(g *gokeepasslib.Group) {
		for i := range g.Entries {
			entries = append(entries, &g.Entries[i])
		}
		for i := range g.Groups {
			traverse(&g.Groups[i])
		}
	}
	traverse(&k.db.Content.Root.Groups[0])
	return entries
}

// matchSearchIn evaluates whether an entry matches SearchText in the SearchIn field/attribute.
func (k *KeePassProvider) matchSearchIn(entry *gokeepasslib.Entry, searchInStr, searchText string, exact bool) bool {
	up := strings.ToUpper(searchInStr)
	switch up {
	case "I": // Entry UUID
		uuidHex := hex.EncodeToString(entry.UUID[:])
		cleanedSearch := strings.ReplaceAll(searchText, "-", "")
		if exact {
			return strings.EqualFold(uuidHex, cleanedSearch)
		}
		return strings.Contains(strings.ToLower(uuidHex), strings.ToLower(cleanedSearch))

	case "T": // Title
		val := k.getEntryRawValue(entry, "Title")
		if exact {
			return val == searchText
		}
		return strings.Contains(strings.ToLower(val), strings.ToLower(searchText))

	case "U": // Username
		val := k.getEntryRawValue(entry, "UserName")
		if exact {
			return val == searchText
		}
		return strings.Contains(strings.ToLower(val), strings.ToLower(searchText))

	case "P": // Password
		val := k.getEntryRawValue(entry, "Password")
		if exact {
			return val == searchText
		}
		return strings.Contains(strings.ToLower(val), strings.ToLower(searchText))

	case "A": // URL
		val := k.getEntryRawValue(entry, "URL")
		if exact {
			return val == searchText
		}
		return strings.Contains(strings.ToLower(val), strings.ToLower(searchText))

	case "N": // Notes
		val := k.getEntryRawValue(entry, "Notes")
		if exact {
			return val == searchText
		}
		return strings.Contains(strings.ToLower(val), strings.ToLower(searchText))

	case "O": // Custom attributes ("Other")
		for _, v := range entry.Values {
			if isStandardFieldKey(v.Key) {
				continue
			}
			if exact {
				if v.Value.Content == searchText {
					return true
				}
			} else {
				if strings.Contains(strings.ToLower(v.Value.Content), strings.ToLower(searchText)) {
					return true
				}
			}
		}
		return false
	}

	return false
}

// isStandardFieldKey checks if a field key is one of the standard KeePass entry fields.
func isStandardFieldKey(key string) bool {
	switch strings.ToLower(key) {
	case "title", "username", "password", "url", "notes":
		return true
	default:
		return false
	}
}

type tConvMatch struct {
	start  int
	end    int
	text   string
	method string
}

// findInnermostTConv locates the innermost {T-CONV:<sep><text><sep><method><sep>} placeholder in val.
func findInnermostTConv(val string) (tConvMatch, bool) {
	valLower := strings.ToLower(val)
	searchOffset := len(val)

	for {
		idx := strings.LastIndex(valLower[:searchOffset], "{t-conv:")
		if idx == -1 {
			return tConvMatch{}, false
		}

		sepIdx := idx + 8 // length of "{t-conv:"
		if sepIdx >= len(val) {
			searchOffset = idx
			continue
		}

		sep := val[sepIdx]
		if sep == '}' {
			searchOffset = idx
			continue
		}

		// Find second separator
		textStart := sepIdx + 1
		nextSep := strings.IndexByte(val[textStart:], sep)
		if nextSep == -1 {
			searchOffset = idx
			continue
		}
		textEnd := textStart + nextSep
		text := val[textStart:textEnd]

		// Find third separator
		methodStart := textEnd + 1
		methodSep := strings.IndexByte(val[methodStart:], sep)
		if methodSep == -1 {
			searchOffset = idx
			continue
		}
		methodEnd := methodStart + methodSep
		method := val[methodStart:methodEnd]

		closingIdx := methodEnd + 1
		if closingIdx >= len(val) || val[closingIdx] != '}' {
			searchOffset = idx
			continue
		}

		return tConvMatch{
			start:  idx,
			end:    closingIdx + 1,
			text:   text,
			method: method,
		}, true
	}
}

// applyTConvMethod applies a text transformation method to text.
func applyTConvMethod(text, method string) (string, error) {
	switch strings.ToUpper(strings.TrimSpace(method)) {
	case "UPPER", "U":
		return strings.ToUpper(text), nil
	case "LOWER", "L":
		return strings.ToLower(text), nil
	case "BASE64":
		return base64.StdEncoding.EncodeToString([]byte(text)), nil
	case "HEX":
		return hex.EncodeToString([]byte(text)), nil
	case "URI":
		return url.QueryEscape(text), nil
	case "URI-DEC", "URIDEC", "URI-DECODE":
		res, err := url.QueryUnescape(text)
		if err != nil {
			return "", fmt.Errorf("keepass reference error: failed to URI decode text %q: %w", text, err)
		}
		return res, nil
	case "RAW":
		return text, nil
	default:
		return "", fmt.Errorf("keepass reference error: unknown T-CONV method %q", method)
	}
}
