package provider

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

type fileCacheEntry struct {
	mtime time.Time
	raw   map[string]any
}

var (
	fileCache   = make(map[string]fileCacheEntry)
	fileCacheMu sync.RWMutex
)

// getParsedFile reads and parses a JSON or YAML file, caching the result based on file modification time.
func getParsedFile(path string, format string) (map[string]any, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		absPath = path
	}

	info, err := os.Stat(absPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil // Return nil map without error for not exist
		}
		return nil, fmt.Errorf("failed to stat file %s: %w", path, err)
	}
	mtime := info.ModTime()

	fileCacheMu.RLock()
	entry, ok := fileCache[absPath]
	fileCacheMu.RUnlock()

	if ok && entry.mtime.Equal(mtime) {
		return entry.raw, nil
	}

	fileCacheMu.Lock()
	defer fileCacheMu.Unlock()

	// Double check
	entry, ok = fileCache[absPath]
	if ok && entry.mtime.Equal(mtime) {
		return entry.raw, nil
	}

	data, err := os.ReadFile(absPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read file %s: %w", path, err)
	}

	var raw map[string]any
	if format == "json" {
		if err := json.Unmarshal(data, &raw); err != nil {
			return nil, fmt.Errorf("failed to parse JSON %s: %w", path, err)
		}
	} else if format == "yaml" {
		if err := yaml.Unmarshal(data, &raw); err != nil {
			return nil, fmt.Errorf("failed to parse YAML %s: %w", path, err)
		}
	} else {
		return nil, fmt.Errorf("unsupported format: %s", format)
	}

	fileCache[absPath] = fileCacheEntry{
		mtime: mtime,
		raw:   raw,
	}

	return raw, nil
}
