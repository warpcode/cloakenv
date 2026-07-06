package provider

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/tobischo/gokeepasslib/v3"
	"github.com/zalando/go-keyring"
	"golang.org/x/term"
)

// KeePassProvider implements SecretProvider for KeePass .kdbx databases.
// It is a remote-type provider: the URI scheme is the user-defined remote
// name from config (e.g., "work://"), not a fixed string.
type KeePassProvider struct {
	db *gokeepasslib.Database
}

// NewKeePassProvider returns a new KeePass provider instance.
func NewKeePassProvider() *KeePassProvider {
	return &KeePassProvider{}
}

// Scheme returns "keepass" as the provider type identifier.
// Note: the actual URI scheme used at runtime is the user-defined remote
// name, not this string. This is used for type-matching in config.
func (k *KeePassProvider) Scheme() string {
	return "keepass"
}

// Initialize opens, decrypts, and unlocks a KeePass database.
// Settings in ProviderConfig:
//   - "database_path": filesystem path to the .kdbx file
//   - "remote_name": name of the remote configuration (e.g. "work")
//   - "keyring_prefix": service name prefix for keyring
//   - "force_prompt": "true" to force prompting for password
func (k *KeePassProvider) Initialize(_ context.Context, cfg ProviderConfig) error {
	if cfg.SingleEntity != nil && *cfg.SingleEntity {
		return errors.New("keepass provider: cannot be configured as a single-entity vault")
	}

	dbPath := cfg.Settings["database_path"]
	if dbPath == "" {
		return errors.New("keepass provider: database_path is required")
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
	var password string
	var fromKeyring bool

	accountName := "provider/" + remoteName

	// 1. Try keyring if not forcing prompt
	if !forcePrompt {
		var err error
		password, err = keyring.Get(keyringPrefix, accountName)
		if err == nil && password != "" {
			fromKeyring = true
		}
	}

	// 2. If not found and not forcing prompt, return an error instructing to login
	if password == "" && !forcePrompt {
		return fmt.Errorf("no credentials found for remote %q; please log in first using 'cloakenv auth login %s'", remoteName, remoteName)
	}

	// 3. Prompt user if forcePrompt is true or if we are logging in
	var prompted bool
	if password == "" || forcePrompt {
		if term.IsTerminal(int(os.Stdin.Fd())) {
			fmt.Printf("Enter master password for remote %q: ", remoteName)
			bytePassword, err := term.ReadPassword(int(os.Stdin.Fd()))
			if err != nil {
				return fmt.Errorf("keepass provider: failed to read password: %w", err)
			}
			fmt.Println()
			password = string(bytePassword)
			prompted = true
		} else {
			reader := bufio.NewReader(os.Stdin)
			line, err := reader.ReadString('\n')
			if err != nil {
				return fmt.Errorf("keepass provider: no credentials found for remote %q and stdin is not a terminal (failed to read piped password: %w)", remoteName, err)
			}
			password = strings.TrimRight(line, "\r\n")
			prompted = true
		}
	}

	// 4. Try to open and decrypt the database
	unlockErr := k.unlock(dbPath, password)
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
		if err := keyring.Set(keyringPrefix, accountName, password); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to save credentials to keyring: %v\n", err)
		}
	}

	return nil
}

func (k *KeePassProvider) unlock(dbPath string, password string) error {
	// Expand ~ to home directory
	if strings.HasPrefix(dbPath, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("keepass provider: failed to resolve home directory: %w", err)
		}
		dbPath = home + dbPath[1:]
	}

	file, err := os.Open(dbPath)
	if err != nil {
		return fmt.Errorf("keepass provider: failed to open database %s: %w", dbPath, err)
	}
	defer file.Close()

	k.db = gokeepasslib.NewDatabase()
	k.db.Credentials = gokeepasslib.NewPasswordCredentials(password)

	if err := gokeepasslib.NewDecoder(file).Decode(k.db); err != nil {
		return fmt.Errorf("keepass provider: decoding failed (check master key): %w", err)
	}

	if err := k.db.UnlockProtectedEntries(); err != nil {
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
		if getEntryTitle(entry) == entryTitle {
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
	val := getEntryValue(entry, attr)
	if val != "" {
		return val, nil
	}

	// If not found in Values, check if it exists in Binaries (as an attachment name)
	for _, ref := range entry.Binaries {
		if ref.Name == attr {
			// Found the attachment reference! Let's find it in the global Meta binaries list
			for _, bin := range k.db.Content.Meta.Binaries {
				if bin.ID == ref.Value.ID {
					return string(bin.Content), nil
				}
			}
			return "", fmt.Errorf("keepass provider: binary reference ID %d not found in database metadata", ref.Value.ID)
		}
	}

	return "", fmt.Errorf("keepass provider: attribute %q is empty or not found for entry %q", attr, getEntryTitle(entry))
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
			title := getEntryTitle(entry)
			var entryPath string
			if groupPath == "" {
				entryPath = title
			} else {
				entryPath = groupPath + "/" + title
			}

			// Filter by title substring if specified
			if query.Title != "" {
				if !strings.Contains(strings.ToLower(title), strings.ToLower(query.Title)) {
					continue
				}
			}

			// Filter by path substring if specified
			if query.Path != "" {
				if !strings.Contains(strings.ToLower(entryPath), strings.ToLower(query.Path)) {
					continue
				}
			}

			entryTags := parseTags(entry.Tags)

			// Filter by tags if specified
			if len(query.Tags) > 0 {
				tagMap := make(map[string]bool)
				for _, t := range entryTags {
					tagMap[strings.ToLower(t)] = true
				}
				match := true
				for _, qt := range query.Tags {
					if !tagMap[strings.ToLower(qt)] {
						match = false
						break
					}
				}
				if !match {
					continue
				}
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

// toEntry converts a gokeepasslib.Entry into provider.Entry.
func (k *KeePassProvider) toEntry(entry *gokeepasslib.Entry) Entry {
	title := getEntryTitle(entry)
	tags := parseTags(entry.Tags)

	attrs := make(map[string]any)
	for _, v := range entry.Values {
		attrs[v.Key] = v.Value.Content
	}

	// Add attachments as attributes
	for _, ref := range entry.Binaries {
		for _, bin := range k.db.Content.Meta.Binaries {
			if bin.ID == ref.Value.ID {
				attrs[ref.Name] = string(bin.Content)
				break
			}
		}
	}

	return Entry{
		Title:      title,
		Tags:       tags,
		Attributes: attrs,
	}
}

// parseTags splits a comma-separated tags string into a slice of strings.
func parseTags(tagsStr string) []string {
	if tagsStr == "" {
		return nil
	}
	parts := strings.Split(tagsStr, ",")
	var tags []string
	for _, p := range parts {
		t := strings.TrimSpace(p)
		if t != "" {
			tags = append(tags, t)
		}
	}
	return tags
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
func getEntryTitle(entry *gokeepasslib.Entry) string {
	for _, v := range entry.Values {
		if v.Key == "Title" {
			return v.Value.Content
		}
	}
	return ""
}

// getEntryValue extracts a named value from a KeePass entry.
func getEntryValue(entry *gokeepasslib.Entry, key string) string {
	for _, v := range entry.Values {
		if v.Key == key {
			return v.Value.Content
		}
	}
	return ""
}

// SetSecret returns an error because the KeePass provider is currently read-only.
func (k *KeePassProvider) SetSecret(_ context.Context, _ string, _ string) error {
	return fmt.Errorf("keepass provider is read-only")
}

// DeleteSecret returns an error because the KeePass provider is currently read-only.
func (k *KeePassProvider) DeleteSecret(_ context.Context, _ string) error {
	return fmt.Errorf("keepass provider is read-only")
}

// Validate checks if the KeePass configuration is valid (database_path must be set).
func (k *KeePassProvider) Validate(settings map[string]string) error {
	if settings["database_path"] == "" {
		return errors.New("keepass provider: database_path is required")
	}
	return nil
}
