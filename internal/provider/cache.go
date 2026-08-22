package provider

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/zalando/go-keyring"

	"github.com/warpcode/cloakenv/internal/utils"
)

const (
	keyringAccount = "cache_token"
)

// CacheProvider implements SecretProvider for the cache:// scheme.
// It encrypts cached secrets in local files under ~/.cache/cloakenv/
// using AES-GCM with a dynamically generated key stored in the OS keyring.
type CacheProvider struct {
	cacheDir string
	aesKey   []byte
}

// NewCacheProvider returns a new cache provider instance.
func NewCacheProvider() *CacheProvider {
	return &CacheProvider{}
}

// Scheme returns "cache".
func (c *CacheProvider) Scheme() string {
	return "cache"
}

// Initialize prepares the cache directory and resolves or generates
// the AES key from the OS keyring.
func (c *CacheProvider) Initialize(_ context.Context, cfg ProviderConfig) error {
	prefix := cfg.Settings["keyring_prefix"]
	if prefix == "" {
		prefix = "cloakenv"
	}

	// Set default cache directory: ~/.cache/<prefix>
	userCache, err := os.UserCacheDir()
	if err != nil {
		return fmt.Errorf("cache provider: failed to get user cache dir: %w", err)
	}
	c.cacheDir = filepath.Join(userCache, prefix)

	// Ensure the cache directory exists and is restricted to the user
	if err := os.MkdirAll(c.cacheDir, 0700); err != nil {
		return fmt.Errorf("cache provider: failed to create cache directory: %w", err)
	}

	// Fetch or generate the AES-256 key from OS keyring
	hexKey, err := keyring.Get(prefix, keyringAccount)
	if err != nil {
		// Only generate a new key when the keyring confirms the key is absent.
		// Any other failure (locked keyring, D-Bus down, permissions) must abort
		// initialization: regenerating here would silently orphan every existing
		// cache file encrypted under the previous key.
		if !errors.Is(err, keyring.ErrNotFound) {
			return fmt.Errorf("cache provider: failed to read key from OS keyring: %w", err)
		}

		keyBytes := make([]byte, 32) // AES-256 key size
		if _, err := io.ReadFull(rand.Reader, keyBytes); err != nil {
			return fmt.Errorf("cache provider: failed to generate key: %w", err)
		}

		hexKey = hex.EncodeToString(keyBytes)
		if err := keyring.Set(prefix, keyringAccount, hexKey); err != nil {
			return fmt.Errorf("cache provider: failed to save key to OS keyring: %w", err)
		}

		c.aesKey = keyBytes
	} else {
		// Key exists, decode it
		keyBytes, err := hex.DecodeString(hexKey)
		if err != nil {
			return fmt.Errorf("cache provider: malformed key in OS keyring: %w", err)
		}
		if len(keyBytes) != 32 {
			return fmt.Errorf("cache provider: key in OS keyring has wrong length: %d bytes, expected 32", len(keyBytes))
		}
		c.aesKey = keyBytes
	}

	return nil
}

// GetSecret reads, decrypts, and returns a cached secret.
// Location format: the bare identifier/cache key name (e.g. "openai_key").
//
// Note: the returned value is an immutable Go string; only intermediate
// decryption buffers are zeroized. The returned copy lives on the heap until
// garbage collection and cannot be actively scrubbed.
func (c *CacheProvider) GetSecret(ctx context.Context, location string) (string, error) {
	if len(c.aesKey) == 0 {
		return "", errors.New("cache provider: not initialized")
	}

	filePath := c.getCacheFilePath(location)
	data, err := os.ReadFile(filePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("cache provider: secret %q not found in cache", location)
		}
		return "", fmt.Errorf("cache provider: failed to read cache file: %w", err)
	}

	// AES-GCM decryption
	block, err := aes.NewCipher(c.aesKey)
	if err != nil {
		return "", err
	}

	aesgcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonceSize := aesgcm.NonceSize()
	if len(data) < nonceSize {
		return "", errors.New("cache provider: invalid ciphertext size")
	}

	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	plaintext, err := aesgcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("cache provider: decryption failed: %w", err)
	}
	defer utils.ZeroBytes(plaintext)

	// Verify plaintext metadata layout (at least 16 bytes for writeTime + TTL)
	if len(plaintext) < 16 {
		return "", errors.New("cache provider: malformed plaintext metadata")
	}

	writeTime := int64(binary.BigEndian.Uint64(plaintext[0:8]))
	ttl := int64(binary.BigEndian.Uint64(plaintext[8:16]))

	// Check TTL expiration
	if ttl > 0 {
		now := time.Now().UnixNano()
		if now > writeTime+ttl {
			// Cache expired! Clean it up immediately, surfacing any cleanup
			// failure alongside the primary expiration error.
			expiredErr := fmt.Errorf("cache provider: secret %q has expired", location)
			if delErr := c.DeleteSecret(ctx, location); delErr != nil {
				return "", errors.Join(expiredErr, delErr)
			}
			return "", expiredErr
		}
	}

	return string(plaintext[16:]), nil
}

// SetSecret encrypts and writes a secret to the local cache file.
// Supports passing a "ttl" (time.Duration) value via context.Context.
func (c *CacheProvider) SetSecret(ctx context.Context, location string, value string) error {
	if len(c.aesKey) == 0 {
		return errors.New("cache provider: not initialized")
	}

	var ttl time.Duration
	if v := ctx.Value(ContextKeyTTL); v != nil {
		if d, ok := v.(time.Duration); ok {
			ttl = d
		}
	}

	// Construct header metadata (8 bytes WriteTime, 8 bytes TTL duration in nanoseconds)
	writeTime := time.Now().UnixNano()
	metadata := make([]byte, 16)
	binary.BigEndian.PutUint64(metadata[0:8], uint64(writeTime))
	binary.BigEndian.PutUint64(metadata[8:16], uint64(ttl.Nanoseconds()))

	valBytes := []byte(value)
	plaintext := append(metadata, valBytes...)
	defer func() {
		utils.ZeroBytes(plaintext)
		utils.ZeroBytes(valBytes)
	}()

	// AES-GCM encryption
	block, err := aes.NewCipher(c.aesKey)
	if err != nil {
		return err
	}

	aesgcm, err := cipher.NewGCM(block)
	if err != nil {
		return err
	}

	nonce := make([]byte, aesgcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return fmt.Errorf("cache provider: failed to generate nonce: %w", err)
	}

	ciphertext := aesgcm.Seal(nil, nonce, plaintext, nil)

	// Combine nonce + ciphertext
	fileData := append(nonce, ciphertext...)

	filePath := c.getCacheFilePath(location)

	f, err := os.CreateTemp(filepath.Dir(filePath), filepath.Base(filePath)+".*.tmp")
	if err != nil {
		return fmt.Errorf("cache provider: failed to create temp cache file: %w", err)
	}
	tmpFile := f.Name()

	if _, err := f.Write(fileData); err != nil {
		// Best-effort cleanup; join cleanup failures with the primary error
		// so nothing is silently discarded.
		if err := errors.Join(err, f.Close(), os.Remove(tmpFile)); err != nil {
			return fmt.Errorf("cache provider: failed to write temp cache file: %w", err)
		}
		return fmt.Errorf("cache provider: failed to write temp cache file")
	}

	if err := f.Close(); err != nil {
		if rmErr := os.Remove(tmpFile); rmErr != nil {
			return fmt.Errorf("cache provider: failed to close temp cache file: %w", errors.Join(err, rmErr))
		}
		return fmt.Errorf("cache provider: failed to close temp cache file: %w", err)
	}

	if err := os.Rename(tmpFile, filePath); err != nil {
		if rmErr := os.Remove(tmpFile); rmErr != nil {
			return fmt.Errorf("cache provider: failed to commit cache file: %w", errors.Join(err, rmErr))
		}
		return fmt.Errorf("cache provider: failed to commit cache file: %w", err)
	}

	return nil
}

// DeleteSecret removes a single cache entry.
func (c *CacheProvider) DeleteSecret(_ context.Context, location string) error {
	filePath := c.getCacheFilePath(location)
	if err := os.Remove(filePath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("cache provider: secret %q not found in cache", location)
		}
		return fmt.Errorf("cache provider: failed to delete cache file: %w", err)
	}
	return nil
}

// ClearCache removes all cached secret files from the cache directory.
func (c *CacheProvider) ClearCache() error {
	if c.cacheDir == "" {
		return errors.New("cache provider: not initialized")
	}

	files, err := os.ReadDir(c.cacheDir)
	if err != nil {
		return fmt.Errorf("cache provider: failed to read cache directory: %w", err)
	}

	for _, f := range files {
		if !f.IsDir() {
			filePath := filepath.Join(c.cacheDir, f.Name())
			if err := os.Remove(filePath); err != nil {
				return fmt.Errorf("cache provider: failed to delete file %s: %w", f.Name(), err)
			}
		}
	}
	return nil
}

// getCacheFilePath maps a location query to a SHA-256 hashed filename under c.cacheDir.
func (c *CacheProvider) getCacheFilePath(location string) string {
	hasher := sha256.New()
	hasher.Write([]byte(location))
	fileName := hex.EncodeToString(hasher.Sum(nil))
	return filepath.Join(c.cacheDir, fileName)
}

// Validate is a no-op for the cache provider.
func (c *CacheProvider) Validate(settings map[string]string) error {
	return nil
}
