package provider

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/warpcode/cloakenv/internal/utils"
)

var userHomeDir = os.UserHomeDir

func expandHome(path string) string {
	if !strings.HasPrefix(path, "~/") {
		return path
	}
	home, err := userHomeDir()
	if err != nil {
		return path
	}
	return filepath.Join(home, path[2:])
}

// DecryptFunc abstracts decryption of a password store .gpg file.
type DecryptFunc func(ctx context.Context, filePath string) ([]byte, error)

// PassProvider implements SecretProvider and SearchableProvider for the standard
// Unix password store (pass). It decrypts .gpg files from the password store directory,
// extracting the password from the first line and key-value attributes from subsequent lines.
type PassProvider struct {
	storeDir    string
	queryPrefix string
	decryptFunc DecryptFunc

	mu        sync.RWMutex
	fileCache map[string]Entry
}

// NewPassProvider returns a new PassProvider instance.
func NewPassProvider() *PassProvider {
	return &PassProvider{
		decryptFunc: defaultGPGDecrypt,
		fileCache:   make(map[string]Entry),
	}
}

// Scheme returns "pass".
func (p *PassProvider) Scheme() string {
	return "pass"
}

// SetDecryptFunc overrides the default GPG decryption routine, primarily for unit testing.
func (p *PassProvider) SetDecryptFunc(fn DecryptFunc) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.decryptFunc = fn
}

// Initialize prepares the PassProvider using backend settings.
// Settings in ProviderConfig:
//   - "store_dir" or "vault_path": filesystem path to the password store directory
func (p *PassProvider) Initialize(_ context.Context, cfg ProviderConfig) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	dir := cfg.Settings["store_dir"]
	if dir == "" {
		dir = cfg.Settings["vault_path"]
	}
	if dir == "" {
		dir = os.Getenv("PASSWORD_STORE_DIR")
	}
	if dir == "" {
		home, err := userHomeDir()
		if err != nil {
			return fmt.Errorf("pass provider: failed to resolve home directory: %w", err)
		}
		dir = filepath.Join(home, ".password-store")
	}

	queryPrefix := strings.Trim(filepath.ToSlash(strings.TrimSpace(cfg.Query)), "/")
	if queryPrefix != "" {
		for _, seg := range strings.Split(queryPrefix, "/") {
			if seg == ".." || seg == "." || seg == "" {
				return fmt.Errorf("pass provider: invalid query scope %q", cfg.Query)
			}
		}
	}
	p.storeDir = expandHome(dir)
	p.queryPrefix = queryPrefix
	p.fileCache = make(map[string]Entry)

	return nil
}

// StoreDir returns the configured password store directory.
func (p *PassProvider) StoreDir() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.storeDir != "" {
		return p.storeDir
	}
	if envDir := os.Getenv("PASSWORD_STORE_DIR"); envDir != "" {
		return expandHome(envDir)
	}
	home, err := userHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".password-store")
}

// Validate checks if the provider configuration is valid.
func (p *PassProvider) Validate(settings map[string]string) error {
	dir := settings["store_dir"]
	if dir == "" {
		dir = settings["vault_path"]
	}
	if dir != "" {
		expanded := expandHome(dir)
		info, err := os.Stat(expanded)
		if err == nil && !info.IsDir() {
			return fmt.Errorf("pass provider: store_dir %q is not a directory", dir)
		}
	}
	return nil
}

// GetSecret resolves a secret value from the pass store.
// Location format: "path/to/entry" or "path/to/entry:attribute".
func (p *PassProvider) GetSecret(ctx context.Context, location string) (string, error) {
	entryPath, attr := parsePassLocation(location)
	entry, err := p.getParsedEntry(ctx, entryPath)
	if err != nil {
		return "", err
	}

	if attr == "" || strings.EqualFold(attr, "Password") {
		if pw, ok := entry["Password"].(string); ok {
			return pw, nil
		}
		if pw, ok := entry["password"].(string); ok {
			return pw, nil
		}
		return "", fmt.Errorf("pass provider: password not found in entry %q", entryPath)
	}

	if val, ok := entry[attr]; ok {
		return fmt.Sprint(val), nil
	}
	for k, v := range entry {
		if strings.EqualFold(k, attr) {
			return fmt.Sprint(v), nil
		}
	}

	return "", fmt.Errorf("pass provider: attribute %q not found in entry %q", attr, entryPath)
}

// GetEntry retrieves a complete flat raw Entry by location.
func (p *PassProvider) GetEntry(ctx context.Context, location string) (Entry, error) {
	entryPath, _ := parsePassLocation(location)
	return p.getParsedEntry(ctx, entryPath)
}

// Search retrieves all raw entries matching the optional query criteria by scanning the password store.
func (p *PassProvider) Search(ctx context.Context, query SearchQuery) ([]Entry, error) {
	storeDir := p.StoreDir()
	if storeDir == "" {
		return nil, errors.New("pass provider: store directory is not configured")
	}

	var results []Entry
	queryTitleLower := strings.ToLower(query.Title)
	queryPathLower := strings.ToLower(query.Path)
	var queryTagsLower []string
	for _, t := range query.Tags {
		queryTagsLower = append(queryTagsLower, strings.ToLower(t))
	}

	p.mu.RLock()
	queryPrefix := p.queryPrefix
	p.mu.RUnlock()

	walkRoot := storeDir
	if queryPrefix != "" {
		candidate := filepath.Join(storeDir, filepath.FromSlash(queryPrefix))
		if info, statErr := os.Stat(candidate); statErr == nil && info.IsDir() {
			walkRoot = candidate
		} else if _, fileErr := os.Stat(candidate + ".gpg"); fileErr == nil {
			walkRoot = candidate + ".gpg"
		} else {
			return nil, nil
		}
	}

	err := filepath.WalkDir(walkRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if d.IsDir() {
			if strings.HasPrefix(d.Name(), ".") && path != storeDir {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".gpg") {
			return nil
		}

		relPath, relErr := filepath.Rel(storeDir, path)
		if relErr != nil {
			return relErr
		}
		entryPath := filepath.ToSlash(strings.TrimSuffix(relPath, ".gpg"))
		if queryPrefix != "" && entryPath != queryPrefix && !strings.HasPrefix(entryPath, queryPrefix+"/") {
			return nil
		}
		title := filepath.Base(entryPath)

		if queryTitleLower != "" && !strings.Contains(strings.ToLower(title), queryTitleLower) {
			return nil
		}
		if queryPathLower != "" && !strings.Contains(strings.ToLower(entryPath), queryPathLower) {
			return nil
		}

		entry, parseErr := p.getParsedEntry(ctx, entryPath)
		if parseErr != nil {
			return nil
		}

		if len(queryTagsLower) > 0 {
			entryTags := make(map[string]bool)
			for _, tag := range entry.Tags() {
				entryTags[strings.ToLower(tag)] = true
			}
			matchedAll := true
			for _, qt := range queryTagsLower {
				if !entryTags[qt] {
					matchedAll = false
					break
				}
			}
			if !matchedAll {
				return nil
			}
		}

		results = append(results, entry)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("pass provider: failed to search directory %s: %w", storeDir, err)
	}
	return results, nil
}

func (p *PassProvider) getParsedEntry(ctx context.Context, entryPath string) (Entry, error) {
	if entryPath == "" {
		return nil, errors.New("pass provider: empty entry path")
	}

	p.mu.RLock()
	if cached, ok := p.fileCache[entryPath]; ok {
		p.mu.RUnlock()
		return cached.Clone(), nil
	}
	p.mu.RUnlock()

	storeDir := p.StoreDir()
	if storeDir == "" {
		return nil, errors.New("pass provider: store directory is not configured")
	}

	// Resolve target file path
	cleanPath := filepath.Clean(filepath.FromSlash(entryPath))
	targetFile := filepath.Join(storeDir, cleanPath+".gpg")

	// Ensure the path does not escape storeDir
	rel, err := filepath.Rel(storeDir, targetFile)
	if err != nil || strings.HasPrefix(rel, "..") {
		return nil, fmt.Errorf("pass provider: access denied to path outside password store: %s", entryPath)
	}

	p.mu.RLock()
	decrypt := p.decryptFunc
	p.mu.RUnlock()

	if decrypt == nil {
		decrypt = defaultGPGDecrypt
	}

	decryptedBytes, err := decrypt(ctx, targetFile)
	if err != nil {
		return nil, fmt.Errorf("pass provider: failed to decrypt %s: %w", entryPath, err)
	}
	defer utils.ZeroBytes(decryptedBytes)

	entry, err := parsePassData(decryptedBytes, entryPath)
	if err != nil {
		return nil, err
	}

	p.mu.Lock()
	if p.fileCache == nil {
		p.fileCache = make(map[string]Entry)
	}
	p.fileCache[entryPath] = entry.Clone()
	p.mu.Unlock()

	return entry, nil
}

// parsePassLocation parses "path/to/entry:attribute" into entry path and attribute name.
func parsePassLocation(location string) (string, string) {
	loc := strings.TrimPrefix(location, "pass://")
	loc = strings.TrimPrefix(loc, "pass:")
	loc = strings.TrimPrefix(loc, ":")

	parts := strings.SplitN(loc, ":", 2)
	entryPath := parts[0]
	var attr string
	if len(parts) == 2 {
		attr = parts[1]
	}
	return entryPath, attr
}

// parsePassData parses decrypted pass text into an Entry.
// First line is Password. Subsequent lines are Key: Value attributes.
func parsePassData(data []byte, entryPath string) (Entry, error) {
	cleanPath := filepath.ToSlash(entryPath)
	title := filepath.Base(cleanPath)

	scanner := bufio.NewScanner(bytes.NewReader(data))
	var firstLine string
	if scanner.Scan() {
		firstLine = scanner.Text()
	}

	attrs := make(map[string]any)
	// Provide Password under both "Password" and "password" for compatibility
	attrs["Password"] = firstLine
	attrs["password"] = firstLine

	var tags []string
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if key, val, found := strings.Cut(line, ":"); found {
			k := strings.TrimSpace(key)
			v := strings.TrimSpace(val)
			if k != "" {
				attrs[k] = v
				if strings.EqualFold(k, "tags") {
					tags = utils.ParseTagString(v)
				}
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("pass provider: error parsing entry data: %w", err)
	}

	flat := make(Entry, len(attrs)+4)
	for k, v := range attrs {
		flat[k] = v
	}
	flat["title"] = title
	flat["path"] = cleanPath
	flat["tags"] = tags

	dir := filepath.Dir(cleanPath)
	if dir != "" && dir != "." {
		flat["group"] = map[string]any{
			"name": filepath.Base(dir),
			"path": dir,
		}
	}
	return flat, nil
}

// defaultGPGDecrypt decrypts a file using the host's gpg binary.
func defaultGPGDecrypt(ctx context.Context, filePath string) ([]byte, error) {
	if _, err := os.Stat(filePath); err != nil {
		return nil, err
	}

	cmd := exec.CommandContext(ctx, "gpg", "--batch", "--use-agent", "--quiet", "--decrypt", filePath) //nolint:gosec // filePath is validated inside password store directory
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		errMsg := strings.TrimSpace(stderr.String())
		if errMsg != "" {
			return nil, fmt.Errorf("gpg error: %s: %w", errMsg, err)
		}
		return nil, err
	}

	return stdout.Bytes(), nil
}
