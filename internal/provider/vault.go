package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/warpcode/cloakenv/internal/utils"
)

// VaultProvider implements SecretProvider, ValueResolvableProvider, SearchableProvider,
// and WritableProvider for HashiCorp Vault KV secret engines (v1 and v2).
type VaultProvider struct {
	client        *http.Client
	address       string
	token         string
	roleID        string
	secretID      string
	engineVersion int // 1 or 2 (default 2)
	namespace     string
	mountPath     string // optional default mount path, e.g. "secret"

	mu sync.RWMutex
}

// NewVaultProvider returns a new uninitialized VaultProvider.
func NewVaultProvider() *VaultProvider {
	return &VaultProvider{
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
		engineVersion: 2,
	}
}

// Scheme returns "vault".
func (v *VaultProvider) Scheme() string {
	return "vault"
}

// SupportsValueResolution confirms VaultProvider supports the resolve_values config flag.
func (v *VaultProvider) SupportsValueResolution() bool {
	return true
}

// Validate validates HashiCorp Vault configuration settings before initialization.
func (v *VaultProvider) Validate(settings map[string]string) error {
	addr := settings["address"]
	if addr == "" {
		addr = os.Getenv("VAULT_ADDR")
	}
	if addr == "" {
		return errors.New("vault provider: address is required (or VAULT_ADDR environment variable)")
	}

	u, err := url.Parse(addr)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("vault provider: invalid address URL %q", addr)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("vault provider: invalid address scheme %q (must be http or https)", u.Scheme)
	}

	token := settings["token"]
	if token == "" {
		token = os.Getenv("VAULT_TOKEN")
	}
	roleID := settings["role_id"]
	if roleID == "" {
		roleID = os.Getenv("VAULT_ROLE_ID")
	}
	secretID := settings["secret_id"]
	if secretID == "" {
		secretID = os.Getenv("VAULT_SECRET_ID")
	}

	if token == "" && (roleID == "" || secretID == "") {
		return errors.New("vault provider: authentication required: specify token (or VAULT_TOKEN) or approle role_id and secret_id")
	}

	if vStr := settings["engine_version"]; vStr != "" {
		ver, err := strconv.Atoi(vStr)
		if err != nil || (ver != 1 && ver != 2) {
			return fmt.Errorf("vault provider: invalid engine_version %q (must be 1 or 2)", vStr)
		}
	}

	return nil
}

// Initialize configures the VaultProvider using settings from config.Settings.
func (v *VaultProvider) Initialize(ctx context.Context, config ProviderConfig) error {
	v.mu.Lock()
	defer v.mu.Unlock()

	settings := config.Settings
	if settings == nil {
		settings = make(map[string]string)
	}

	addr := settings["address"]
	if addr == "" {
		addr = os.Getenv("VAULT_ADDR")
	}
	if addr == "" {
		return errors.New("vault provider: address is required (or VAULT_ADDR environment variable)")
	}
	v.address = strings.TrimRight(addr, "/")

	v.token = settings["token"]
	if v.token == "" {
		v.token = os.Getenv("VAULT_TOKEN")
	}

	v.roleID = settings["role_id"]
	if v.roleID == "" {
		v.roleID = os.Getenv("VAULT_ROLE_ID")
	}

	v.secretID = settings["secret_id"]
	if v.secretID == "" {
		v.secretID = os.Getenv("VAULT_SECRET_ID")
	}

	v.namespace = settings["namespace"]
	if v.namespace == "" {
		v.namespace = os.Getenv("VAULT_NAMESPACE")
	}

	v.mountPath = strings.Trim(settings["mount_path"], "/")

	v.engineVersion = 2
	if vStr := settings["engine_version"]; vStr != "" {
		if ver, err := strconv.Atoi(vStr); err == nil && (ver == 1 || ver == 2) {
			v.engineVersion = ver
		}
	}

	// If no token provided but role_id and secret_id exist, perform AppRole login
	if v.token == "" && v.roleID != "" && v.secretID != "" {
		tok, err := v.loginAppRole(ctx, v.roleID, v.secretID)
		if err != nil {
			return fmt.Errorf("vault provider: approle authentication failed: %w", err)
		}
		v.token = tok
	}

	if v.token == "" {
		return errors.New("vault provider: missing authentication credentials")
	}

	return nil
}

// loginAppRole performs authentication against /v1/auth/approle/login and returns client token.
func (v *VaultProvider) loginAppRole(ctx context.Context, roleID, secretID string) (string, error) {
	loginPayload := map[string]string{
		"role_id":   roleID,
		"secret_id": secretID,
	}
	bodyBytes, err := json.Marshal(loginPayload)
	if err != nil {
		return "", err
	}
	defer utils.ZeroBytes(bodyBytes)

	reqURL := v.address + "/v1/auth/approle/login"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if v.namespace != "" {
		req.Header.Set("X-Vault-Namespace", v.namespace)
	}

	resp, err := v.client.Do(req)
	if err != nil {
		return "", err
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	defer utils.ZeroBytes(respBytes)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, sanitizeVaultError(respBytes))
	}

	var authResp struct {
		Auth struct {
			ClientToken string `json:"client_token"`
		} `json:"auth"`
		Errors []string `json:"errors"`
	}
	if err := json.Unmarshal(respBytes, &authResp); err != nil {
		return "", fmt.Errorf("failed to parse auth response: %w", err)
	}

	if len(authResp.Errors) > 0 {
		return "", fmt.Errorf("vault error: %s", strings.Join(authResp.Errors, ", "))
	}

	if authResp.Auth.ClientToken == "" {
		return "", errors.New("no client token returned from approle login")
	}

	return authResp.Auth.ClientToken, nil
}

// sanitizeVaultError parses errors array from Vault JSON responses or returns status text.
func sanitizeVaultError(body []byte) string {
	var vaultErr struct {
		Errors []string `json:"errors"`
	}
	if err := json.Unmarshal(body, &vaultErr); err == nil && len(vaultErr.Errors) > 0 {
		return strings.Join(vaultErr.Errors, "; ")
	}
	return "request failed"
}

// splitLocation parses a Vault location string into path and optional attribute field.
// Format: "secret/data/path:field" or "secret/data/path"
func (v *VaultProvider) splitLocation(location string) (path string, field string) {
	loc := strings.TrimPrefix(location, "vault:")
	loc = strings.TrimPrefix(loc, ":")
	loc = strings.TrimPrefix(loc, "/")

	path, field, found := strings.Cut(loc, ":")
	if !found {
		return path, ""
	}
	return path, field
}

// buildReadURL constructs the appropriate HTTP endpoint URL for reading a secret.
// For KV v2: /v1/<mount>/data/<subpath>
// For KV v1: /v1/<mount>/<subpath>
func (v *VaultProvider) buildReadURL(path string) string {
	cleanPath := strings.Trim(path, "/")
	if v.engineVersion == 1 {
		if v.mountPath != "" && !strings.HasPrefix(cleanPath, v.mountPath+"/") && cleanPath != v.mountPath {
			cleanPath = v.mountPath + "/" + cleanPath
		}
		return v.address + "/v1/" + cleanPath
	}

	// KV v2
	mount := v.mountPath
	subpath := cleanPath
	if mount == "" {
		// If mount_path is not explicitly configured, use first segment as mount
		parts := strings.SplitN(cleanPath, "/", 2)
		mount = parts[0]
		if len(parts) > 1 {
			subpath = parts[1]
		} else {
			subpath = ""
		}
	} else if strings.HasPrefix(cleanPath, mount+"/") {
		subpath = strings.TrimPrefix(cleanPath, mount+"/")
	}

	// If subpath starts with "data/", normalize it
	subpath = strings.TrimPrefix(subpath, "data/")

	return v.address + "/v1/" + mount + "/data/" + subpath
}

// buildWriteURL constructs the appropriate HTTP endpoint URL for writing/deleting a secret.
func (v *VaultProvider) buildWriteURL(path string) string {
	return v.buildReadURL(path)
}

// buildListURL constructs the HTTP endpoint URL for listing keys at a path.
// For KV v2: /v1/<mount>/metadata/<subpath>?list=true
// For KV v1: /v1/<mount>/<subpath>?list=true
func (v *VaultProvider) buildListURL(path string) string {
	cleanPath := strings.Trim(path, "/")
	if v.engineVersion == 1 {
		if v.mountPath != "" && !strings.HasPrefix(cleanPath, v.mountPath+"/") && cleanPath != v.mountPath {
			cleanPath = v.mountPath + "/" + cleanPath
		}
		return v.address + "/v1/" + cleanPath + "?list=true"
	}

	mount := v.mountPath
	subpath := cleanPath
	if mount == "" {
		parts := strings.SplitN(cleanPath, "/", 2)
		mount = parts[0]
		if len(parts) > 1 {
			subpath = parts[1]
		} else {
			subpath = ""
		}
	} else if strings.HasPrefix(cleanPath, mount+"/") {
		subpath = strings.TrimPrefix(cleanPath, mount+"/")
	}
	subpath = strings.TrimPrefix(subpath, "metadata/")
	subpath = strings.TrimPrefix(subpath, "data/")

	if subpath == "" {
		return v.address + "/v1/" + mount + "/metadata?list=true"
	}
	return v.address + "/v1/" + mount + "/metadata/" + subpath + "?list=true"
}

// executeRequest sends an authenticated HTTP request to the Vault server.
func (v *VaultProvider) executeRequest(ctx context.Context, method, reqURL string, body io.Reader) (*http.Response, []byte, error) {
	v.mu.RLock()
	token := v.token
	namespace := v.namespace
	client := v.client
	v.mu.RUnlock()

	req, err := http.NewRequestWithContext(ctx, method, reqURL, body)
	if err != nil {
		return nil, nil, err
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("X-Vault-Token", token)
	}
	if namespace != "" {
		req.Header.Set("X-Vault-Namespace", namespace)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("vault provider: request failed: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp, nil, fmt.Errorf("vault provider: failed to read response body: %w", err)
	}

	return resp, respBytes, nil
}

// readSecretData queries Vault and extracts the secret attributes map.
func (v *VaultProvider) readSecretData(ctx context.Context, path string) (map[string]any, error) {
	reqURL := v.buildReadURL(path)
	resp, respBytes, err := v.executeRequest(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}
	defer utils.ZeroBytes(respBytes)

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("vault provider: secret at path %q not found", path)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("vault provider: HTTP %d: %s", resp.StatusCode, sanitizeVaultError(respBytes))
	}

	var vaultResp struct {
		Data   map[string]any `json:"data"`
		Errors []string       `json:"errors"`
	}
	if err := json.Unmarshal(respBytes, &vaultResp); err != nil {
		return nil, fmt.Errorf("vault provider: failed to parse secret JSON response: %w", err)
	}
	if len(vaultResp.Errors) > 0 {
		return nil, fmt.Errorf("vault provider: %s", strings.Join(vaultResp.Errors, ", "))
	}

	if v.engineVersion == 2 {
		// In KV v2, secret payload is inside data.data
		if innerData, ok := vaultResp.Data["data"].(map[string]any); ok {
			return innerData, nil
		}
	}

	// KV v1 (or fallback)
	return vaultResp.Data, nil
}

// GetSecret resolves a secret value from Vault given location ("path:field" or "path").
func (v *VaultProvider) GetSecret(ctx context.Context, location string) (string, error) {
	path, field := v.splitLocation(location)
	if path == "" {
		return "", errors.New("vault provider: empty path in secret location")
	}

	data, err := v.readSecretData(ctx, path)
	if err != nil {
		return "", err
	}

	if field != "" {
		val, ok := data[field]
		if !ok {
			return "", fmt.Errorf("vault provider: field %q not found in secret %q", field, path)
		}
		return formatSecretValue(val), nil
	}

	// If no field specified, look for common standard keys or return single key/json
	if val, ok := data["value"]; ok {
		return formatSecretValue(val), nil
	}
	if val, ok := data["password"]; ok {
		return formatSecretValue(val), nil
	}
	if val, ok := data["data"]; ok {
		return formatSecretValue(val), nil
	}
	if len(data) == 1 {
		for _, val := range data {
			return formatSecretValue(val), nil
		}
	}

	// Multiple fields: serialize dictionary as JSON
	jsonBytes, err := json.Marshal(data)
	if err != nil {
		return "", fmt.Errorf("vault provider: failed to serialize secret attributes: %w", err)
	}
	return string(jsonBytes), nil
}

func formatSecretValue(val any) string {
	switch v := val.(type) {
	case string:
		return v
	case []byte:
		return string(v)
	case nil:
		return ""
	case int:
		return strconv.Itoa(v)
	case int64:
		return strconv.FormatInt(v, 10)
	case float64:
		if v == float64(int64(v)) {
			return strconv.FormatInt(int64(v), 10)
		}
		return strconv.FormatFloat(v, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(v)
	default:
		b, err := json.Marshal(v)
		if err == nil {
			return string(b)
		}
		return fmt.Sprintf("%v", v)
	}
}

// SetSecret writes a secret value to Vault at the given location ("path:field" or "path").
func (v *VaultProvider) SetSecret(ctx context.Context, location string, value string) error {
	path, field := v.splitLocation(location)
	if path == "" {
		return errors.New("vault provider: empty path in secret location")
	}

	var payload any
	if v.engineVersion == 2 {
		// Read existing data if writing a specific field to preserve sibling keys
		existing := make(map[string]any)
		if current, err := v.readSecretData(ctx, path); err == nil && current != nil {
			existing = current
		}
		targetKey := field
		if targetKey == "" {
			targetKey = "value"
		}
		existing[targetKey] = value
		payload = map[string]any{
			"data": existing,
		}
	} else {
		// KV v1
		existing := make(map[string]any)
		if current, err := v.readSecretData(ctx, path); err == nil && current != nil {
			existing = current
		}
		targetKey := field
		if targetKey == "" {
			targetKey = "value"
		}
		existing[targetKey] = value
		payload = existing
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	defer utils.ZeroBytes(bodyBytes)

	reqURL := v.buildWriteURL(path)
	resp, respBytes, err := v.executeRequest(ctx, http.MethodPost, reqURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return err
	}
	defer utils.ZeroBytes(respBytes)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("vault provider: HTTP %d: %s", resp.StatusCode, sanitizeVaultError(respBytes))
	}

	return nil
}

// DeleteSecret removes the secret at the given location from Vault.
func (v *VaultProvider) DeleteSecret(ctx context.Context, location string) error {
	path, _ := v.splitLocation(location)
	if path == "" {
		return errors.New("vault provider: empty path in secret location")
	}

	reqURL := v.buildWriteURL(path)
	// For KV v2, DELETE /v1/<mount>/data/<path> soft deletes the latest version
	resp, respBytes, err := v.executeRequest(ctx, http.MethodDelete, reqURL, nil)
	if err != nil {
		return err
	}
	defer utils.ZeroBytes(respBytes)

	if resp.StatusCode == http.StatusNotFound {
		return nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("vault provider: HTTP %d: %s", resp.StatusCode, sanitizeVaultError(respBytes))
	}

	return nil
}

// GetEntry retrieves a complete structured entry from Vault given location.
func (v *VaultProvider) GetEntry(ctx context.Context, location string) (Entry, error) {
	entry, _, err := v.GetEntryWithPath(ctx, location)
	return entry, err
}

// GetEntryWithPath retrieves a structured entry along with its authoritative path.
func (v *VaultProvider) GetEntryWithPath(ctx context.Context, location string) (Entry, string, error) {
	path, _ := v.splitLocation(location)
	if path == "" {
		return Entry{}, "", errors.New("vault provider: empty path in secret location")
	}

	data, err := v.readSecretData(ctx, path)
	if err != nil {
		return Entry{}, "", err
	}

	// Extract title from path
	title := path
	if idx := strings.LastIndex(path, "/"); idx != -1 {
		title = path[idx+1:]
	}

	// Extract tags if present in data
	var tags []string
	if rawTags, ok := data["tags"]; ok {
		switch t := rawTags.(type) {
		case []string:
			tags = t
		case []any:
			for _, item := range t {
				if s, ok := item.(string); ok {
					tags = append(tags, s)
				}
			}
		case string:
			for _, p := range strings.Split(t, ",") {
				if trimmed := strings.TrimSpace(p); trimmed != "" {
					tags = append(tags, trimmed)
				}
			}
		}
	}

	entry := Entry{
		Title:      title,
		Tags:       tags,
		Attributes: data,
	}

	return entry, path, nil
}

// listKeys recursively lists secret paths starting from a root path.
func (v *VaultProvider) listKeys(ctx context.Context, root string) ([]string, error) {
	reqURL := v.buildListURL(root)
	resp, respBytes, err := v.executeRequest(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}
	defer utils.ZeroBytes(respBytes)

	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("vault provider list HTTP %d: %s", resp.StatusCode, sanitizeVaultError(respBytes))
	}

	var listResp struct {
		Data struct {
			Keys []string `json:"keys"`
		} `json:"data"`
	}
	if err := json.Unmarshal(respBytes, &listResp); err != nil {
		return nil, err
	}

	var results []string
	cleanRoot := strings.Trim(root, "/")

	for _, k := range listResp.Data.Keys {
		var fullPath string
		if cleanRoot == "" {
			fullPath = k
		} else {
			fullPath = cleanRoot + "/" + k
		}

		if strings.HasSuffix(k, "/") {
			// Sub-directory: recurse
			subKeys, err := v.listKeys(ctx, fullPath)
			if err == nil {
				results = append(results, subKeys...)
			}
		} else {
			results = append(results, fullPath)
		}
	}

	return results, nil
}

// Search retrieves entries from Vault matching query criteria.
func (v *VaultProvider) Search(ctx context.Context, query SearchQuery) ([]SearchResult, error) {
	startPath := v.mountPath
	if query.Path != "" {
		startPath = query.Path
	}

	keys, err := v.listKeys(ctx, startPath)
	if err != nil {
		// If listing root or specific path returns an error, fallback to empty search results
		return []SearchResult{}, nil
	}

	var results []SearchResult
	for _, k := range keys {
		entry, fullPath, err := v.GetEntryWithPath(ctx, k)
		if err != nil {
			continue
		}

		// Filter by query.Title
		if query.Title != "" && !strings.Contains(strings.ToLower(entry.Title), strings.ToLower(query.Title)) {
			continue
		}
		// Filter by query.Path
		if query.Path != "" && !strings.Contains(strings.ToLower(fullPath), strings.ToLower(query.Path)) {
			continue
		}
		// Filter by query.Tags
		if len(query.Tags) > 0 {
			match := true
			for _, requiredTag := range query.Tags {
				tagFound := false
				for _, entryTag := range entry.Tags {
					if strings.EqualFold(entryTag, requiredTag) {
						tagFound = true
						break
					}
				}
				if !tagFound {
					match = false
					break
				}
			}
			if !match {
				continue
			}
		}

		results = append(results, SearchResult{
			Path:  fullPath,
			Entry: entry,
		})
	}

	return results, nil
}
