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
	searchable  bool
	decryptFunc DecryptFunc

	mu        sync.RWMutex
	fileCache map[string]*Entry
}

// NewPassProvider returns a new PassProvider instance.
func NewPassProvider() *PassProvider {
	return &PassProvider{
		searchable:  true,
		decryptFunc: defaultGPGDecrypt,
		fileCache:   make(map[string]*Entry),
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

	p.storeDir = expandHome(dir)
	p.searchable = cfg.Searchable
	p.fileCache = make(map[string]*Entry)

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

// GetSecret resolves a secret value from the password store.
// Location format: "path/to/entry" or "path/to/entry:attribute".
// If attribute is omitted or "password" / "Password", the first line is returned.
func (p *PassProvider) GetSecret(ctx context.Context, location string) (string, error) {
	entryPath, attr := parsePassLocation(location)

	entry, err := p.getParsedEntry(ctx, entryPath)
	if err != nil {
		return "", err
	}

	// Default to first line password
	if attr == "" || strings.EqualFold(attr, "password") {
		if pw, ok := entry.Attributes["Password"].(string); ok {
			return pw, nil
		}
		if pw, ok := entry.Attributes["password"].(string); ok {
			return pw, nil
		}
		return "", fmt.Errorf("pass provider: password not found in entry %q", entryPath)
	}

	// Check attributes (exact match first, then case-insensitive)
	if val, ok := entry.Attributes[attr]; ok {
		return fmt.Sprintf("%v", val), nil
	}
	for k, v := range entry.Attributes {
		if strings.EqualFold(k, attr) {
			return fmt.Sprintf("%v", v), nil
		}
	}

	return "", fmt.Errorf("pass provider: attribute %q not found in entry %q", attr, entryPath)
}

// GetEntry retrieves a complete structured entry by location.
func (p *PassProvider) GetEntry(ctx context.Context, location string) (Entry, error) {
	entry, _, err := p.GetEntryWithPath(ctx, location)
	return entry, err
}

// GetEntryWithPath retrieves a structured entry along with its authoritative path.
func (p *PassProvider) GetEntryWithPath(ctx context.Context, location string) (Entry, string, error) {
	entryPath, _ := parsePassLocation(location)
	entry, err := p.getParsedEntry(ctx, entryPath)
	if err != nil {
		return Entry{}, "", err
	}
	return *entry, entryPath, nil
}

// GetEntryTitle retrieves the entry title (the base name of the entry).
func (p *PassProvider) GetEntryTitle(_ context.Context, location string) (string, error) {
	entryPath, _ := parsePassLocation(location)
	if entryPath == "" {
		return "", errors.New("pass provider: empty location")
	}
	clean := filepath.Clean(filepath.FromSlash(entryPath))
	return filepath.Base(clean), nil
}

// Search retrieves all entries matching the query criteria by scanning the password store.
func (p *PassProvider) Search(ctx context.Context, query SearchQuery) ([]SearchResult, error) {
	storeDir := p.StoreDir()
	if storeDir == "" {
		return nil, errors.New("pass provider: store directory is not configured")
	}

	var results []SearchResult

	queryTitleLower := strings.ToLower(query.Title)
	queryPathLower := strings.ToLower(query.Path)
	var queryTagsLower []string
	for _, t := range query.Tags {
		queryTagsLower = append(queryTagsLower, strings.ToLower(t))
	}

	err := filepath.WalkDir(storeDir, func(path string, d fs.DirEntry, err error) error {
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

		// Normalize to forward slashes for entry path
		entryPath := filepath.ToSlash(strings.TrimSuffix(relPath, ".gpg"))
		title := filepath.Base(entryPath)

		if queryTitleLower != "" && !strings.Contains(strings.ToLower(title), queryTitleLower) {
			return nil
		}
		if queryPathLower != "" && !strings.Contains(strings.ToLower(entryPath), queryPathLower) {
			return nil
		}

		entry, parseErr := p.getParsedEntry(ctx, entryPath)
		if parseErr != nil {
			// Skip entries that cannot be decrypted or parsed during global search
			return nil
		}

		if len(queryTagsLower) > 0 {
			entryTags := make(map[string]bool)
			for _, tag := range entry.Tags {
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

		results = append(results, SearchResult{
			Provider: "pass",
			Path:     entryPath,
			Entry:    *entry,
		})

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("pass provider: failed to search directory %s: %w", storeDir, err)
	}

	return results, nil
}

// SetSecret returns an error because pass provider currently supports reading secrets.
func (p *PassProvider) SetSecret(_ context.Context, _ string, _ string) error {
	return fmt.Errorf("pass provider is read-only")
}

// DeleteSecret returns an error because pass provider currently supports reading secrets.
func (p *PassProvider) DeleteSecret(_ context.Context, _ string) error {
	return fmt.Errorf("pass provider is read-only")
}

func (p *PassProvider) getParsedEntry(ctx context.Context, entryPath string) (*Entry, error) {
	if entryPath == "" {
		return nil, errors.New("pass provider: empty entry path")
	}

	p.mu.RLock()
	if cached, ok := p.fileCache[entryPath]; ok {
		p.mu.RUnlock()
		clone := cached.Clone()
		return &clone, nil
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
		p.fileCache = make(map[string]*Entry)
	}
	cloned := entry.Clone()
	p.fileCache[entryPath] = &cloned
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
func parsePassData(data []byte, entryPath string) (*Entry, error) {
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

	dir := filepath.Dir(cleanPath)
	var group *GroupInfo
	if dir != "" && dir != "." {
		group = &GroupInfo{
			Name: filepath.Base(dir),
			Path: dir,
		}
	}

	entry := &Entry{
		Title:      title,
		Tags:       tags,
		Group:      group,
		Attributes: attrs,
	}
	*entry = entry.EnsureOriginal()

	return entry, nil
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
