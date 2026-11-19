package engine

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"

	"github.com/warpcode/cloakenv/internal/utils"
)

func shquote(s string) string {
	// Safely quote a string in POSIX single-quotes.
	// Single quotes inside the string are closed, escaped as \', and reopened: '\''
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func toJSON(v any) (string, error) {
	b, err := json.Marshal(v)
	return string(b), err
}

func fromJSON(str string) (any, error) {
	var out any
	if err := json.Unmarshal([]byte(str), &out); err != nil {
		return nil, fmt.Errorf("fromJSON failed: %w", err)
	}
	return out, nil
}

func toYAML(v any) (string, error) {
	b, err := utils.MarshalYAML(v)
	return strings.TrimSpace(string(b)), err
}

func fromYAML(str string) (any, error) {
	var out any
	if err := utils.UnmarshalYAML([]byte(str), &out); err != nil {
		return nil, fmt.Errorf("fromYAML failed: %w", err)
	}
	return out, nil
}

// isEmpty determines if a value is considered empty for template helpers like default.
func isEmpty(v any) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.String:
		return rv.Len() == 0
	case reflect.Bool:
		return !rv.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return rv.Int() == 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return rv.Uint() == 0
	case reflect.Float32, reflect.Float64:
		return rv.Float() == 0
	case reflect.Slice, reflect.Map, reflect.Array:
		return rv.Len() == 0
	case reflect.Chan, reflect.Pointer, reflect.UnsafePointer, reflect.Func, reflect.Interface:
		return rv.IsNil()
	default:
		return false
	}
}

// templateDefault returns the first non-empty value in given, or fallback if all are empty.
func templateDefault(fallback any, given ...any) any {
	if len(given) == 0 {
		return fallback
	}
	for _, val := range given {
		if !isEmpty(val) {
			return val
		}
	}
	return fallback
}

// templateCoalesce returns the first non-empty string argument.
func templateCoalesce(vals ...any) string {
	for _, val := range vals {
		if val == nil {
			continue
		}
		if s, ok := val.(string); ok {
			if s != "" {
				return s
			}
			continue
		}
		strVal := fmt.Sprintf("%v", val)
		if strVal != "" {
			return strVal
		}
	}
	return ""
}

func toEnv(m map[string]any) (string, error) {
	var ks []string
	for k := range m {
		if isReservedMetadataKey(k) || strings.EqualFold(k, "meta") ||
			strings.EqualFold(k, "entries") || strings.EqualFold(k, "attributes") ||
			strings.EqualFold(k, "original") || strings.EqualFold(k, "output") {
			continue
		}
		ks = append(ks, k)
	}
	slices.Sort(ks)
	var sb strings.Builder
	for _, k := range ks {
		valStr, err := utils.SerializeAttrValue(m[k])
		if err != nil {
			return "", err
		}
		fmt.Fprintf(&sb, "%s=%s\n", k, valStr)
	}
	return strings.TrimSpace(sb.String()), nil
}

// bufPool pools *bytes.Buffer instances to reduce heap allocations during template rendering.
var bufPool = sync.Pool{
	New: func() any {
		return new(bytes.Buffer)
	},
}

var userHomeDir = os.UserHomeDir

func expandPath(path, configPath string) string {
	if strings.HasPrefix(path, "~/") {
		home, err := userHomeDir()
		if err == nil {
			path = filepath.Join(home, path[2:])
		}
	}
	if !filepath.IsAbs(path) && configPath != "" {
		baseDir := filepath.Dir(configPath)
		path = filepath.Join(baseDir, path)
	}
	return path
}
