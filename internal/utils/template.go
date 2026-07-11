package utils

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// ParseTemplateFile reads a template file and returns a map of KEY to URI.
func ParseTemplateFile(filepath string) (map[string]string, error) {
	file, err := os.Open(filepath)
	if err != nil {
		return nil, fmt.Errorf("failed to open template file: %w", err)
	}
	defer file.Close()

	envs := make(map[string]string)
	scanner := bufio.NewScanner(file)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())
		// Skip empty lines and comment lines
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		key, val, ok := strings.Cut(line, "=")
		if !ok {
			return nil, fmt.Errorf("line %d: invalid format (expected KEY=value)", lineNum)
		}

		key = strings.TrimSpace(key)
		if key == "" {
			return nil, fmt.Errorf("line %d: empty key", lineNum)
		}

		val = strings.TrimSpace(val)
		// Trim surrounding quotes if present
		if len(val) >= 2 {
			if (strings.HasPrefix(val, `"`) && strings.HasSuffix(val, `"`)) ||
				(strings.HasPrefix(val, `'`) && strings.HasSuffix(val, `'`)) {
				val = val[1 : len(val)-1]
			}
		}

		if val == "" {
			return nil, fmt.Errorf("line %d: empty value for key %q", lineNum, key)
		}

		envs[key] = val
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading template file: %w", err)
	}

	return envs, nil
}
