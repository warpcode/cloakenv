package provider

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/zalando/go-keyring"

	"github.com/warpcode/cloakenv/internal/utils"
)

const (
	// CacheModeKeyring stores cache entries directly in the OS keyring.
	CacheModeKeyring = "keyring"
	// CacheModeEncryptedDisk stores AES-256-GCM encrypted entries on disk.
	CacheModeEncryptedDisk = "encrypted_disk"

	cacheKeyringAccountPrefix = "cache_v2/"
	cacheDiskKeyringAccount   = "cache_disk_token"
)

// CachingProvider wraps any SecretProvider to provide transparent, opt-in
// caching of retrieved secrets and entries with strict security guarantees.
type CachingProvider struct {
	underlying    SecretProvider
	vaultName     string
	ttl           time.Duration
	mode          string
	keyringPrefix string
	cacheDir      string
	aesKey        []byte
	mu            sync.RWMutex
}

// CachingConfig holds settings for instantiating a CachingProvider.
type CachingConfig struct {
	VaultName     string
	TTL           time.Duration
	Mode          string // "keyring" or "encrypted_disk"
	KeyringPrefix string
}

// NewCachingProvider wraps a SecretProvider with transparent caching.
func NewCachingProvider(p SecretProvider, cfg CachingConfig) (SecretProvider, error) {
	mode := cfg.Mode
	if mode == "" {
		mode = CacheModeKeyring
	}

	prefix := cfg.KeyringPrefix
	if prefix == "" {
		prefix = "cloakenv"
	}

	cp := &CachingProvider{
		underlying:    p,
		vaultName:     cfg.VaultName,
		ttl:           cfg.TTL,
		mode:          mode,
		keyringPrefix: prefix,
	}

	if mode == CacheModeEncryptedDisk {
		userCache, err := userCacheDir()
		if err != nil {
			return nil, fmt.Errorf("caching provider: failed to get user cache dir: %w", err)
		}
		// Structure: ~/.cache/<prefix>/vaults/<vault_hash>
		h := sha256.Sum256([]byte(cfg.VaultName))
		vaultHash := hex.EncodeToString(h[:16])
		cp.cacheDir = filepath.Join(userCache, prefix, "vaults", vaultHash)

		if err := os.MkdirAll(cp.cacheDir, 0700); err != nil {
			return nil, fmt.Errorf("caching provider: failed to create cache directory: %w", err)
		}
		if err := os.Chmod(cp.cacheDir, 0700); err != nil { //nolint:gosec // 0700 required for cache dir
			return nil, fmt.Errorf("caching provider: failed to set permissions on cache directory: %w", err)
		}

		// Ensure AES key in OS keyring
		keyBytes, err := getOrGenerateKey(prefix, cacheDiskKeyringAccount)
		if err != nil {
			return nil, fmt.Errorf("caching provider: %w", err)
		}
		cp.aesKey = keyBytes
	}

	_, isSearchable := p.(SearchableProvider)
	_, isValResolvable := p.(ValueResolvableProvider)

	if isSearchable && isValResolvable {
		return &SearchableValueResolvableCachingProvider{
			SearchableCachingProvider: &SearchableCachingProvider{
				CachingProvider: cp,
			},
		}, nil
	}
	if isSearchable {
		return &SearchableCachingProvider{
			CachingProvider: cp,
		}, nil
	}
	if isValResolvable {
		return &ValueResolvableCachingProvider{
			CachingProvider: cp,
		}, nil
	}

	return cp, nil
}

// getOrGenerateKey fetches or creates an AES-256 key from the OS keyring.
func getOrGenerateKey(service, account string) ([]byte, error) {
	hexKey, err := keyring.Get(service, account)
	if err != nil {
		if !errors.Is(err, keyring.ErrNotFound) {
			return nil, fmt.Errorf("failed to read key from OS keyring: %w", err)
		}

		keyBytes := make([]byte, 32)
		if _, err := io.ReadFull(rand.Reader, keyBytes); err != nil {
			return nil, fmt.Errorf("failed to generate key: %w", err)
		}

		hexKey = hex.EncodeToString(keyBytes)
		if err := keyring.Set(service, account, hexKey); err != nil {
			return nil, fmt.Errorf("failed to save key to OS keyring: %w", err)
		}
		return keyBytes, nil
	}

	keyBytes, err := hex.DecodeString(hexKey)
	if err != nil {
		return nil, fmt.Errorf("malformed key in OS keyring: %w", err)
	}
	if len(keyBytes) != 32 {
		return nil, fmt.Errorf("key in OS keyring has wrong length: %d bytes, expected 32", len(keyBytes))
	}
	return keyBytes, nil
}

// Scheme returns the scheme of the underlying provider.
func (c *CachingProvider) Scheme() string {
	return c.underlying.Scheme()
}

// Initialize delegates to the underlying provider.
func (c *CachingProvider) Initialize(ctx context.Context, config ProviderConfig) error {
	return c.underlying.Initialize(ctx, config)
}

// SetSecret invalidates cache and delegates to underlying provider.
func (c *CachingProvider) SetSecret(ctx context.Context, location, value string) error {
	_ = c.deleteCacheEntry("secret:" + location)
	_ = c.deleteCacheEntry("entry:" + location)
	return c.underlying.SetSecret(ctx, location, value)
}

// DeleteSecret invalidates cache and delegates to underlying provider.
func (c *CachingProvider) DeleteSecret(ctx context.Context, location string) error {
	_ = c.deleteCacheEntry("secret:" + location)
	_ = c.deleteCacheEntry("entry:" + location)
	return c.underlying.DeleteSecret(ctx, location)
}

// Validate delegates to the underlying provider.
func (c *CachingProvider) Validate(settings map[string]string) error {
	return c.underlying.Validate(settings)
}

// Underlying returns the wrapped SecretProvider.
func (c *CachingProvider) Underlying() SecretProvider {
	return c.underlying
}

// RootPrefixes returns root prefixes if underlying provider supports them.
func (c *CachingProvider) RootPrefixes() []string {
	if rpProvider, ok := c.underlying.(interface{ RootPrefixes() []string }); ok {
		return rpProvider.RootPrefixes()
	}
	return nil
}

// cacheKey creates a unique key for the secret / entry location.
func (c *CachingProvider) cacheKey(location string) string {
	h := sha256.Sum256([]byte(c.vaultName + ":" + location))
	return hex.EncodeToString(h[:])
}

// getCacheFilePath returns the path to the disk cache file for a given cache key.
func (c *CachingProvider) getCacheFilePath(key string) string {
	return filepath.Join(c.cacheDir, key)
}

// readPayload reads and validates a cached payload, checking TTL and shredding if expired.
// Returns (payloadBytes, found, error). Caller must scrub payloadBytes with utils.ZeroBytes().
func (c *CachingProvider) readPayload(location string) ([]byte, bool, error) {
	c.mu.RLock()
	mode := c.mode
	key := c.cacheKey(location)
	c.mu.RUnlock()

	var wireData []byte
	var err error

	switch mode {
	case CacheModeKeyring:
		service := c.keyringPrefix + "_vault_cache"
		account := cacheKeyringAccountPrefix + key
		hexStr, kErr := keyring.Get(service, account)
		if kErr != nil {
			if errors.Is(kErr, keyring.ErrNotFound) {
				return nil, false, nil
			}
			return nil, false, kErr
		}
		data, decErr := hex.DecodeString(hexStr)
		if decErr != nil {
			_ = keyring.Delete(service, account)
			return nil, false, nil
		}
		wireData = data

	case CacheModeEncryptedDisk:
		filePath := c.getCacheFilePath(key)
		data, fErr := os.ReadFile(filePath) //nolint:gosec // deterministic cache path
		if fErr != nil {
			if errors.Is(fErr, os.ErrNotExist) {
				return nil, false, nil
			}
			return nil, false, fErr
		}

		c.mu.RLock()
		aesKey := c.aesKey
		c.mu.RUnlock()

		block, err := aes.NewCipher(aesKey)
		if err != nil {
			return nil, false, err
		}
		aesgcm, err := cipher.NewGCM(block)
		if err != nil {
			return nil, false, err
		}

		nonceSize := aesgcm.NonceSize()
		if len(data) < nonceSize {
			_ = c.deleteCacheEntry(location)
			return nil, false, nil
		}

		nonce, ciphertext := data[:nonceSize], data[nonceSize:]
		plaintext, decErr := aesgcm.Open(nil, nonce, ciphertext, nil)
		if decErr != nil {
			_ = c.deleteCacheEntry(location)
			return nil, false, nil
		}
		wireData = plaintext

	default:
		return nil, false, fmt.Errorf("unsupported cache mode: %q", mode)
	}

	defer func() {
		// Clean up wireData if not returning it
		if err != nil || len(wireData) < 16 {
			utils.ZeroBytes(wireData)
		}
	}()

	if len(wireData) < 16 {
		_ = c.deleteCacheEntry(location)
		return nil, false, nil
	}

	writeTime := int64(binary.BigEndian.Uint64(wireData[0:8])) //nolint:gosec
	ttl := int64(binary.BigEndian.Uint64(wireData[8:16]))      //nolint:gosec

	if ttl > 0 {
		now := time.Now().UnixNano()
		if now > writeTime+ttl {
			// Expired: shred entry immediately
			_ = c.deleteCacheEntry(location)
			utils.ZeroBytes(wireData)
			return nil, false, nil
		}
	}

	payload := make([]byte, len(wireData)-16)
	copy(payload, wireData[16:])
	utils.ZeroBytes(wireData)
	return payload, true, nil
}

// writePayload encodes and writes a cached payload with a 16-byte header.
func (c *CachingProvider) writePayload(location string, payload []byte) error {
	defer utils.ZeroBytes(payload)

	c.mu.RLock()
	mode := c.mode
	ttl := c.ttl
	key := c.cacheKey(location)
	c.mu.RUnlock()

	writeTime := time.Now().UnixNano()
	metadata := make([]byte, 16)
	binary.BigEndian.PutUint64(metadata[0:8], uint64(writeTime))
	binary.BigEndian.PutUint64(metadata[8:16], uint64(ttl.Nanoseconds())) //nolint:gosec

	wireData := append(metadata, payload...)
	defer utils.ZeroBytes(wireData)

	switch mode {
	case CacheModeKeyring:
		service := c.keyringPrefix + "_vault_cache"
		account := cacheKeyringAccountPrefix + key
		hexStr := hex.EncodeToString(wireData)
		return keyring.Set(service, account, hexStr)

	case CacheModeEncryptedDisk:
		c.mu.RLock()
		aesKey := c.aesKey
		c.mu.RUnlock()

		block, err := aes.NewCipher(aesKey)
		if err != nil {
			return err
		}
		aesgcm, err := cipher.NewGCM(block)
		if err != nil {
			return err
		}

		nonce := make([]byte, aesgcm.NonceSize())
		if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
			return fmt.Errorf("caching provider: failed to generate nonce: %w", err)
		}

		ciphertext := aesgcm.Seal(nil, nonce, wireData, nil)
		fileData := append(nonce, ciphertext...)

		filePath := c.getCacheFilePath(key)
		f, err := os.CreateTemp(filepath.Dir(filePath), filepath.Base(filePath)+".*.tmp")
		if err != nil {
			return fmt.Errorf("caching provider: failed to create temp file: %w", err)
		}
		tmpFile := f.Name()

		if err := f.Chmod(0600); err != nil {
			return fmt.Errorf("caching provider: failed to set permissions: %w", errors.Join(err, f.Close(), os.Remove(tmpFile)))
		}

		if _, err := f.Write(fileData); err != nil {
			return fmt.Errorf("caching provider: failed to write temp file: %w", errors.Join(err, f.Close(), os.Remove(tmpFile)))
		}

		if err := f.Close(); err != nil {
			_ = os.Remove(tmpFile)
			return fmt.Errorf("caching provider: failed to close temp file: %w", err)
		}

		if err := os.Rename(tmpFile, filePath); err != nil {
			_ = os.Remove(tmpFile)
			return fmt.Errorf("caching provider: failed to commit cache file: %w", err)
		}
		return nil

	default:
		return fmt.Errorf("unsupported cache mode: %q", mode)
	}
}

// deleteCacheEntry removes a cache entry from keyring or disk.
func (c *CachingProvider) deleteCacheEntry(location string) error {
	c.mu.RLock()
	mode := c.mode
	key := c.cacheKey(location)
	c.mu.RUnlock()

	switch mode {
	case CacheModeKeyring:
		service := c.keyringPrefix + "_vault_cache"
		account := cacheKeyringAccountPrefix + key
		err := keyring.Delete(service, account)
		if err != nil && !errors.Is(err, keyring.ErrNotFound) {
			return err
		}
		return nil

	case CacheModeEncryptedDisk:
		filePath := c.getCacheFilePath(key)
		// Shred: overwrite with zeros before deletion if it exists
		if info, err := os.Stat(filePath); err == nil && info.Size() > 0 {
			zeros := make([]byte, info.Size())
			_ = os.WriteFile(filePath, zeros, 0600)
		}
		err := os.Remove(filePath)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil

	default:
		return nil
	}
}

// Clear removes all cache entries associated with this provider.
func (c *CachingProvider) Clear() error {
	c.mu.RLock()
	mode := c.mode
	c.mu.RUnlock()

	switch mode {
	case CacheModeKeyring:
		service := c.keyringPrefix + "_vault_cache"
		// Clear all keyring entries under this service
		return keyring.DeleteAll(service)

	case CacheModeEncryptedDisk:
		if c.cacheDir == "" {
			return nil
		}
		files, err := os.ReadDir(c.cacheDir)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return err
		}
		for _, f := range files {
			if !f.IsDir() {
				path := filepath.Join(c.cacheDir, f.Name())
				if info, sErr := f.Info(); sErr == nil && info.Size() > 0 {
					zeros := make([]byte, info.Size())
					_ = os.WriteFile(path, zeros, 0600)
				}
				_ = os.Remove(path)
			}
		}
		return nil

	default:
		return nil
	}
}

// Prune removes only expired cache entries associated with this provider.
func (c *CachingProvider) Prune() error {
	c.mu.RLock()
	mode := c.mode
	c.mu.RUnlock()

	switch mode {
	case CacheModeKeyring:
		// OS Keyring does not support enumeration; expired entries are shredded on read.
		return nil

	case CacheModeEncryptedDisk:
		if c.cacheDir == "" {
			return nil
		}
		files, err := os.ReadDir(c.cacheDir)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return err
		}
		for _, f := range files {
			if !f.IsDir() {
				filePath := filepath.Join(c.cacheDir, f.Name())
				data, err := os.ReadFile(filePath) //nolint:gosec
				if err != nil {
					continue
				}

				c.mu.RLock()
				aesKey := c.aesKey
				c.mu.RUnlock()

				block, err := aes.NewCipher(aesKey)
				if err != nil {
					continue
				}
				aesgcm, err := cipher.NewGCM(block)
				if err != nil {
					continue
				}

				nonceSize := aesgcm.NonceSize()
				if len(data) < nonceSize {
					_ = os.Remove(filePath)
					continue
				}

				nonce, ciphertext := data[:nonceSize], data[nonceSize:]
				plaintext, err := aesgcm.Open(nil, nonce, ciphertext, nil)
				if err != nil {
					_ = os.Remove(filePath)
					continue
				}

				if len(plaintext) >= 16 {
					writeTime := int64(binary.BigEndian.Uint64(plaintext[0:8])) //nolint:gosec
					ttl := int64(binary.BigEndian.Uint64(plaintext[8:16]))      //nolint:gosec
					if ttl > 0 && time.Now().UnixNano() > writeTime+ttl {
						// Overwrite with zeros and remove
						zeros := make([]byte, len(data))
						_ = os.WriteFile(filePath, zeros, 0600)
						_ = os.Remove(filePath)
					}
				}
				utils.ZeroBytes(plaintext)
			}
		}
		return nil

	default:
		return nil
	}
}

// GetSecret reads, decrypts, and returns a cached secret, or retrieves it from the underlying provider.
func (c *CachingProvider) GetSecret(ctx context.Context, location string) (string, error) {
	cacheKeyLoc := "secret:" + location
	cachedPayload, found, err := c.readPayload(cacheKeyLoc)
	if err == nil && found {
		val := string(cachedPayload)
		utils.ZeroBytes(cachedPayload)
		return val, nil
	}

	// Cache miss or error: query underlying provider
	val, err := c.underlying.GetSecret(ctx, location)
	if err != nil {
		return "", err
	}

	valBytes := []byte(val)
	_ = c.writePayload(cacheKeyLoc, valBytes)
	utils.ZeroBytes(valBytes)

	return val, nil
}

// SearchableCachingProvider wraps CachingProvider for SearchableProvider backends.
type SearchableCachingProvider struct {
	*CachingProvider
}

// cachedEntryEnvelope wraps Entry and AuthoritativePath for structured cache entries.
type cachedEntryEnvelope struct {
	Entry             Entry  `json:"entry"`
	AuthoritativePath string `json:"authoritative_path"`
}

// GetEntryWithPath retrieves a structured entry from cache or the underlying provider.
func (s *SearchableCachingProvider) GetEntryWithPath(ctx context.Context, location string) (Entry, string, error) {
	cacheKeyLoc := "entry:" + location
	cachedPayload, found, err := s.readPayload(cacheKeyLoc)
	if err == nil && found {
		var env cachedEntryEnvelope
		if decErr := json.Unmarshal(cachedPayload, &env); decErr == nil {
			utils.ZeroBytes(cachedPayload)
			return env.Entry, env.AuthoritativePath, nil
		}
		utils.ZeroBytes(cachedPayload)
	}

	// Cache miss: delegate to underlying
	if pathProvider, ok := s.underlying.(interface {
		GetEntryWithPath(ctx context.Context, location string) (Entry, string, error)
	}); ok {
		entry, authPath, err := pathProvider.GetEntryWithPath(ctx, location)
		if err != nil {
			return Entry{}, "", err
		}
		env := cachedEntryEnvelope{Entry: entry, AuthoritativePath: authPath}
		if data, encErr := json.Marshal(env); encErr == nil {
			_ = s.writePayload(cacheKeyLoc, data)
		}
		return entry, authPath, nil
	}

	if searchable, ok := s.underlying.(SearchableProvider); ok {
		entry, err := searchable.GetEntry(ctx, location)
		if err != nil {
			return Entry{}, "", err
		}
		env := cachedEntryEnvelope{Entry: entry, AuthoritativePath: location}
		if data, encErr := json.Marshal(env); encErr == nil {
			_ = s.writePayload(cacheKeyLoc, data)
		}
		return entry, location, nil
	}

	return Entry{}, "", fmt.Errorf("provider %q does not support structured entries", s.Scheme())
}

// GetEntry retrieves a structured entry by delegating to GetEntryWithPath.
func (s *SearchableCachingProvider) GetEntry(ctx context.Context, location string) (Entry, error) {
	entry, _, err := s.GetEntryWithPath(ctx, location)
	return entry, err
}

// Search delegates search queries to the underlying SearchableProvider.
func (s *SearchableCachingProvider) Search(ctx context.Context, query SearchQuery) ([]SearchResult, error) {
	if searchable, ok := s.underlying.(SearchableProvider); ok {
		return searchable.Search(ctx, query)
	}
	return nil, fmt.Errorf("provider %q does not support searching", s.Scheme())
}

// ValueResolvableCachingProvider wraps CachingProvider for ValueResolvableProvider backends.
type ValueResolvableCachingProvider struct {
	*CachingProvider
}

// SupportsValueResolution delegates to underlying provider.
func (v *ValueResolvableCachingProvider) SupportsValueResolution() bool {
	if vr, ok := v.underlying.(ValueResolvableProvider); ok {
		return vr.SupportsValueResolution()
	}
	return false
}

// SearchableValueResolvableCachingProvider implements both SearchableProvider and ValueResolvableProvider.
type SearchableValueResolvableCachingProvider struct {
	*SearchableCachingProvider
}

// SupportsValueResolution delegates to underlying provider.
func (s *SearchableValueResolvableCachingProvider) SupportsValueResolution() bool {
	if vr, ok := s.underlying.(ValueResolvableProvider); ok {
		return vr.SupportsValueResolution()
	}
	return false
}
