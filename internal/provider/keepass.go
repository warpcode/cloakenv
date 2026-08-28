package provider

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/warpcode/cloakenv/internal/utils"

	"github.com/tobischo/gokeepasslib/v3"
	"github.com/zalando/go-keyring"
	"golang.org/x/term"
)

// KeePassProvider implements SecretProvider for KeePass .kdbx databases.
// It is a remote-type provider: the URI scheme is the user-defined remote
// name from config (e.g., "work://"), not a fixed string.
type KeePassProvider struct {
	db *gokeepasslib.Database

	cacheMu     sync.RWMutex
	entryCache  map[*gokeepasslib.Entry]map[string]string
	binaryCache map[int]string
}

// NewKeePassProvider returns a new KeePass provider instance.
func NewKeePassProvider() *KeePassProvider {
	return &KeePassProvider{
		entryCache: make(map[*gokeepasslib.Entry]map[string]string),
	}
}

// Scheme returns "keepass" as the provider type identifier.
// Note: the actual URI scheme used at runtime is the user-defined remote
// name, not this string. This is used for type-matching in config.
func (k *KeePassProvider) Scheme() string {
	return "keepass"
}

// Initialize opens, decrypts, and unlocks a KeePass database.
// Settings in ProviderConfig:
//   - "vault_path": filesystem path to the .kdbx file
//   - "remote_name": name of the remote configuration (e.g. "work")
//   - "keyring_prefix": service name prefix for keyring
//   - "force_prompt": "true" to force prompting for password
func (k *KeePassProvider) Initialize(_ context.Context, cfg ProviderConfig) error {
	if cfg.SingleEntity != nil && *cfg.SingleEntity {
		return errors.New("keepass provider: cannot be configured as a single-entity vault")
	}

	vaultPath := cfg.Settings["vault_path"]
	if vaultPath == "" {
		return errors.New("keepass provider: vault_path is required")
	}

	remoteName := cfg.Settings["remote_name"]
	if remoteName == "" {
		return errors.New("keepass provider: remote_name is required")
	}

	keyringPrefix := cfg.Settings["keyring_prefix"]
	if keyringPrefix == "" {
		keyringPrefix = "cloakenv"
	}

	forcePrompt := cfg.Settings["force_prompt"] == "true"
	var password []byte
	var fromKeyring bool

	accountName := "provider/" + remoteName

	// Ensure password byte slice is scrubbed from heap memory upon exit
	defer func() {
		if password != nil {
			utils.ZeroBytes(password)
		}
	}()

	// 1. Try keyring if not forcing prompt
	if !forcePrompt {
		pwStr, err := keyring.Get(keyringPrefix, accountName)
		if err == nil && pwStr != "" {
			password = []byte(pwStr)
			fromKeyring = true
		}
	}

	// 2. If not found and not forcing prompt, return an error instructing to login
	if len(password) == 0 && !forcePrompt {
		return fmt.Errorf("no credentials found for remote %q; please log in first using 'cloakenv auth login %s'", remoteName, remoteName)
	}

	// 3. Prompt user if forcePrompt is true or if we are logging in
	var prompted bool
	if len(password) == 0 || forcePrompt {
		if term.IsTerminal(int(os.Stdin.Fd())) {
			fmt.Printf("Enter master password for remote %q: ", remoteName)
			bytePassword, err := term.ReadPassword(int(os.Stdin.Fd()))
			if err != nil {
				return fmt.Errorf("keepass provider: failed to read password: %w", err)
			}
			fmt.Println()
			password = bytePassword
			prompted = true
		} else {
			reader := bufio.NewReader(os.Stdin)
			lineBytes, err := reader.ReadBytes('\n')
			if err != nil {
				return fmt.Errorf("keepass provider: no credentials found for remote %q and stdin is not a terminal (failed to read piped password: %w)", remoteName, err)
			}
			trimmed := bytes.TrimRight(lineBytes, "\r\n")
			password = make([]byte, len(trimmed))
			copy(password, trimmed)
			utils.ZeroBytes(lineBytes)
			prompted = true
		}
	}

	// 4. Try to open and decrypt the database
	unlockErr := k.unlock(vaultPath, password)
	if unlockErr != nil {
		if fromKeyring {
			// Delete invalid credentials
			_ = keyring.Delete(keyringPrefix, accountName)
			return fmt.Errorf("decryption failed using credentials from keyring. The stored password may be incorrect. Please log in again using 'cloakenv auth login %s'", remoteName)
		}
		return unlockErr
	}

	// 5. Save password to keyring if prompted and verified
	if prompted {
		if err := keyring.Set(keyringPrefix, accountName, string(password)); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to save credentials to keyring: %v\n", err)
		}
	}

	return nil
}

func (k *KeePassProvider) unlock(vaultPath string, password []byte) error {
	k.cacheMu.Lock()
	k.entryCache = make(map[*gokeepasslib.Entry]map[string]string)
	k.binaryCache = nil
	k.cacheMu.Unlock()

	// Expand ~ to home directory
	if strings.HasPrefix(vaultPath, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("keepass provider: failed to resolve home directory: %w", err)
		}
		vaultPath = home + vaultPath[1:]
	}

	file, err := os.Open(vaultPath) //nolint:gosec // operator-configured vault path; validated by internal/config
	if err != nil {
		return fmt.Errorf("keepass provider: failed to open database %s: %w", vaultPath, err)
	}
	defer func() {
		_ = file.Close() // read-only handle; close errors carry no actionable signal here
	}()

	k.db = gokeepasslib.NewDatabase()
	k.db.Credentials = gokeepasslib.NewPasswordCredentials(string(password))

	if err := gokeepasslib.NewDecoder(file).Decode(k.db); err != nil {
		k.db.Credentials = nil
		return fmt.Errorf("keepass provider: decoding failed (check master key): %w", err)
	}

	if err := k.db.UnlockProtectedEntries(); err != nil {
		k.db.Credentials = nil
		return fmt.Errorf("keepass provider: failed to unlock protected entries: %w", err)
	}

	k.db.Credentials = nil
	return nil
}

// findEntry locates an entry and returns it along with the parsed attribute.
func (k *KeePassProvider) findEntry(location string) (*gokeepasslib.Entry, string, error) {
	path, attr := parseKeePassLocation(location)
	segments := strings.Split(path, "/")

	if len(segments) == 0 {
		return nil, "", errors.New("keepass provider: empty path")
	}

	if len(k.db.Content.Root.Groups) == 0 {
		return nil, "", errors.New("keepass provider: database has no root group")
	}

	// Navigate through groups to reach the parent group of the target entry
	currentGroup := &k.db.Content.Root.Groups[0]
	groupPath := segments[:len(segments)-1]
	entryTitle := segments[len(segments)-1]

	if len(groupPath) > 0 && groupPath[0] == currentGroup.Name {
		groupPath = groupPath[1:]
	}

	for _, groupName := range groupPath {
		found := false
		for i := range currentGroup.Groups {
			if currentGroup.Groups[i].Name == groupName {
				currentGroup = &currentGroup.Groups[i]
				found = true
				break
			}
		}
		if !found {
			return nil, "", fmt.Errorf("keepass provider: group not found: %s", groupName)
		}
	}

	// Find the entry by title within the target group
	for i := range currentGroup.Entries {
		entry := &currentGroup.Entries[i]
		if k.getEntryTitle(entry) == entryTitle {
			return entry, attr, nil
		}
	}

	return nil, "", fmt.Errorf("keepass provider: entry not found: %s", entryTitle)
}

// GetSecret retrieves a secret from the decrypted KeePass database.
// Location format: "Group/SubGroup/EntryTitle" or "Group/SubGroup/EntryTitle:Attribute".
// If no attribute is specified, defaults to "Password".
func (k *KeePassProvider) GetSecret(_ context.Context, location string) (string, error) {
	if k.db == nil {
		return "", errors.New("keepass provider: not initialized")
	}

	entry, attr, err := k.findEntry(location)
	if err != nil {
		return "", err
	}

	// First, try standard string value attribute (e.g. Password, UserName)
	val := k.getEntryValue(entry, attr)
	if val != "" {
		return val, nil
	}

	// If not found in Values, check if it exists in Binaries (as an attachment name)
	for _, ref := range entry.Binaries {
		if ref.Name == attr {
			// Found the attachment reference! Let's find it in the binary cache
			if content, ok := k.getBinaryContent(ref.Value.ID); ok {
				return content, nil
			}
			return "", fmt.Errorf("keepass provider: binary reference ID %d not found in database metadata", ref.Value.ID)
		}
	}

	return "", fmt.Errorf("keepass provider: attribute %q is empty or not found for entry %q", attr, k.getEntryTitle(entry))
}

// GetEntry retrieves a complete structured entry by location.
func (k *KeePassProvider) GetEntry(_ context.Context, location string) (Entry, error) {
	if k.db == nil {
		return Entry{}, errors.New("keepass provider: not initialized")
	}

	entry, _, err := k.findEntry(location)
	if err != nil {
		return Entry{}, err
	}

	return k.toEntry(entry), nil
}

// Search retrieves all entries matching the query criteria.
func (k *KeePassProvider) Search(ctx context.Context, query SearchQuery) ([]SearchResult, error) {
	if k.db == nil {
		return nil, errors.New("keepass provider: not initialized")
	}

	if len(k.db.Content.Root.Groups) == 0 {
		return nil, errors.New("keepass provider: database has no root group")
	}
	rootGroup := &k.db.Content.Root.Groups[0]

	var results []SearchResult

	queryTitleLower := strings.ToLower(query.Title)
	queryPathLower := strings.ToLower(query.Path)
	var queryTagsLower []string
	if len(query.Tags) > 0 {
		queryTagsLower = make([]string, len(query.Tags))
		for i, t := range query.Tags {
			queryTagsLower[i] = strings.ToLower(t)
		}
	}

	var traverse func(g *gokeepasslib.Group, currentPath string)
	traverse = func(g *gokeepasslib.Group, currentPath string) {
		var groupPath string
		if g == rootGroup {
			groupPath = ""
		} else if currentPath == "" {
			groupPath = g.Name
		} else {
			groupPath = currentPath + "/" + g.Name
		}

		for i := range g.Entries {
			entry := &g.Entries[i]
			title := k.getEntryTitle(entry)
			var entryPath string
			if groupPath == "" {
				entryPath = title
			} else {
				entryPath = groupPath + "/" + title
			}

			if !k.matchSearchEntry(entry, title, entryPath, queryTitleLower, queryPathLower, queryTagsLower) {
				continue
			}

			results = append(results, SearchResult{
				Path:  entryPath,
				Entry: k.toEntry(entry),
			})
		}

		for i := range g.Groups {
			traverse(&g.Groups[i], groupPath)
		}
	}

	traverse(rootGroup, "")
	return results, nil
}

// matchSearchEntry evaluates whether a KeePass entry satisfies title, path, and tag search filters.
func (k *KeePassProvider) matchSearchEntry(entry *gokeepasslib.Entry, title, entryPath string, queryTitleLower, queryPathLower string, queryTagsLower []string) bool {
	if queryTitleLower != "" && !strings.Contains(strings.ToLower(title), queryTitleLower) {
		return false
	}

	if queryPathLower != "" && !strings.Contains(strings.ToLower(entryPath), queryPathLower) {
		return false
	}

	return matchEntryTags(entry.Tags, queryTagsLower)
}

// matchEntryTags checks whether all required query tags exist on the entry's tag string.
func matchEntryTags(tagString string, queryTagsLower []string) bool {
	if len(queryTagsLower) == 0 {
		return true
	}

	entryTags := utils.ParseTagString(tagString)
	tagMap := make(map[string]bool, len(entryTags))
	for _, t := range entryTags {
		tagMap[strings.ToLower(t)] = true
	}

	for _, qt := range queryTagsLower {
		if !tagMap[qt] {
			return false
		}
	}

	return true
}

// toEntry converts a gokeepasslib.Entry into provider.Entry.
func (k *KeePassProvider) toEntry(entry *gokeepasslib.Entry) Entry {
	title := k.getEntryTitle(entry)
	tags := utils.ParseTagString(entry.Tags)

	attrs := make(map[string]any)
	for _, v := range entry.Values {
		attrs[v.Key] = v.Value.Content
	}

	// Add attachments as attributes
	for _, ref := range entry.Binaries {
		if content, ok := k.getBinaryContent(ref.Value.ID); ok {
			attrs[ref.Name] = content
		}
	}

	return Entry{
		Title:      title,
		Tags:       tags,
		Attributes: attrs,
	}
}

// parseKeePassLocation splits "Path/To/Entry:Attribute" into path and attribute.
// Defaults attribute to "Password" if not specified.
func parseKeePassLocation(location string) (string, string) {
	parts := strings.SplitN(location, ":", 2)
	path := parts[0]
	attr := "Password"
	if len(parts) == 2 && parts[1] != "" {
		attr = parts[1]
	}
	return path, attr
}

// getEntryTitle extracts the Title value from a KeePass entry.
func (k *KeePassProvider) getEntryTitle(entry *gokeepasslib.Entry) string {
	return k.getEntryValue(entry, "Title")
}

// getEntryValue extracts a named value from a KeePass entry.
// It caches the parsed values in a map for faster subsequent lookups.
func (k *KeePassProvider) getEntryValue(entry *gokeepasslib.Entry, key string) string {
	k.cacheMu.RLock()
	if m, ok := k.entryCache[entry]; ok {
		k.cacheMu.RUnlock()
		return m[key]
	}
	k.cacheMu.RUnlock()

	k.cacheMu.Lock()
	defer k.cacheMu.Unlock()

	// Lazily initialize entryCache to prevent panic if provider wasn't created via NewKeePassProvider
	if k.entryCache == nil {
		k.entryCache = make(map[*gokeepasslib.Entry]map[string]string)
	} else if m, ok := k.entryCache[entry]; ok {
		// Double check in case another goroutine populated it while waiting for the lock
		return m[key]
	}

	m := make(map[string]string, len(entry.Values))
	for _, v := range entry.Values {
		m[v.Key] = v.Value.Content
	}
	k.entryCache[entry] = m

	return m[key]
}

// SetSecret returns an error because the KeePass provider is currently read-only.
func (k *KeePassProvider) SetSecret(_ context.Context, _ string, _ string) error {
	return fmt.Errorf("keepass provider is read-only")
}

// DeleteSecret returns an error because the KeePass provider is currently read-only.
func (k *KeePassProvider) DeleteSecret(_ context.Context, _ string) error {
	return fmt.Errorf("keepass provider is read-only")
}

// Validate checks if the KeePass configuration is valid (vault_path must be set).
func (k *KeePassProvider) Validate(settings map[string]string) error {
	if settings["vault_path"] == "" {
		return errors.New("keepass provider: vault_path is required")
	}
	return nil
}

// getBinaryContent extracts the content of a binary by its ID from the global metadata.
// It caches the parsed values in a map for faster subsequent lookups.
func (k *KeePassProvider) getBinaryContent(id int) (string, bool) {
	k.cacheMu.RLock()
	if k.binaryCache != nil {
		content, ok := k.binaryCache[id]
		k.cacheMu.RUnlock()
		return content, ok
	}
	k.cacheMu.RUnlock()

	k.cacheMu.Lock()
	defer k.cacheMu.Unlock()

	// Lazily initialize binaryCache
	if k.binaryCache == nil {
		k.binaryCache = make(map[int]string, len(k.db.Content.Meta.Binaries))
		for _, bin := range k.db.Content.Meta.Binaries {
			k.binaryCache[bin.ID] = string(bin.Content)
		}
	}

	content, ok := k.binaryCache[id]
	return content, ok
}
