package provider

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"text/template"

	"github.com/warpcode/cloakenv/internal/utils"
)

// AttributesProvider wraps a SecretProvider and injects configured attributes
// into every entry returned by the underlying provider.
type AttributesProvider struct {
	underlying SecretProvider
	attributes map[string]any
}

// Scheme returns the underlying provider's scheme.
func (a *AttributesProvider) Scheme() string {
	return a.underlying.Scheme()
}

// Initialize delegates initialization to the underlying provider.
func (a *AttributesProvider) Initialize(ctx context.Context, cfg ProviderConfig) error {
	return a.underlying.Initialize(ctx, cfg)
}

// SetSecret delegates secret writing to the underlying provider.
func (a *AttributesProvider) SetSecret(ctx context.Context, location, value string) error {
	return a.underlying.SetSecret(ctx, location, value)
}

// DeleteSecret delegates secret deletion to the underlying provider.
func (a *AttributesProvider) DeleteSecret(ctx context.Context, location string) error {
	return a.underlying.DeleteSecret(ctx, location)
}

// Validate delegates configuration validation to the underlying provider.
func (a *AttributesProvider) Validate(settings map[string]string) error {
	return a.underlying.Validate(settings)
}

// Underlying returns the wrapped SecretProvider.
func (a *AttributesProvider) Underlying() SecretProvider {
	return a.underlying
}

// GetSecret resolves a secret. It first queries the underlying provider.
// If the underlying provider does not have the secret and it is searchable,
// it fetches the entry and checks if the requested attribute was injected.
func (a *AttributesProvider) GetSecret(ctx context.Context, location string) (string, error) {
	val, err := a.underlying.GetSecret(ctx, location)
	if err == nil {
		return val, nil
	}

	searchable, isSearchable := a.underlying.(SearchableProvider)
	if !isSearchable {
		return "", err
	}

	var entryLoc, attrName string
	if strings.Contains(location, ":") {
		parts := strings.SplitN(location, ":", 2)
		entryLoc = parts[0]
		attrName = parts[1]
	} else {
		attrName = location
	}

	resolveFromEntry := func(rawVal any) (string, error) {
		strVal, isStr := rawVal.(string)
		if !isStr || !strings.Contains(strVal, "{{") || !strings.Contains(strVal, "}}") {
			if isStr {
				return strVal, nil
			}
			return fmt.Sprintf("%v", rawVal), nil
		}

		// Templated attribute: resolve entry to evaluate against entry metadata and attributes
		var targetEntry Entry
		var entryPath string
		var getErr error
		if pWithPath, ok := a.underlying.(interface {
			GetEntryWithPath(ctx context.Context, location string) (Entry, string, error)
		}); ok {
			targetEntry, entryPath, getErr = pWithPath.GetEntryWithPath(ctx, entryLoc)
		} else {
			targetEntry, getErr = searchable.GetEntry(ctx, entryLoc)
			entryPath = entryLoc
		}
		if getErr != nil {
			return "", getErr
		}

		providerName, vaultName, _ := utils.ParseURI(location)
		if providerName == "" {
			providerName = a.underlying.Scheme()
		}

		rendered, renderErr := renderAttributeTemplate(strVal, targetEntry, providerName, vaultName, entryPath)
		if renderErr != nil {
			return "", renderErr
		}
		return rendered, nil
	}

	// Check if the attribute is in our injected attributes
	if injectedVal, ok := a.attributes[attrName]; ok {
		return resolveFromEntry(injectedVal)
	}

	// Check if the attribute was accessed via dot path within an injected attribute
	// e.g. location is "my-entry:nested.attr", attrName is "nested.attr"
	if strings.Contains(attrName, ".") {
		val, resolveErr := resolveDotPath(a.attributes, attrName)
		if resolveErr == nil {
			return resolveFromEntry(val)
		}
	}

	return "", err
}

func (a *AttributesProvider) injectAttributes(entry Entry, providerName string, vaultName string, entryPath string) map[string]any {
	if a.attributes == nil {
		return entry.Attributes
	}
	attrs := make(map[string]any, len(entry.Attributes)+len(a.attributes))
	for k, v := range entry.Attributes {
		attrs[k] = v
	}
	for k, v := range a.attributes {
		if strVal, isStr := v.(string); isStr && strings.Contains(strVal, "{{") && strings.Contains(strVal, "}}") {
			rendered, err := renderAttributeTemplate(strVal, entry, providerName, vaultName, entryPath)
			if err == nil {
				attrs[k] = rendered
			} else {
				attrs[k] = v // Fallback to raw string on error
			}
		} else {
			attrs[k] = v
		}
	}
	return attrs
}

func renderAttributeTemplate(templateStr string, rawEntry Entry, providerName string, vaultName string, entryPath string) (string, error) {
	funcs := template.FuncMap{
		"upper": strings.ToUpper,
		"lower": strings.ToLower,
		"snake": func(s string) string { return utils.FormatKeyWithFormat(s, utils.KeyFormatSnake) },
		"kebab": func(s string) string { return utils.FormatKeyWithFormat(s, utils.KeyFormatKebab) },
		"env":   func(s string) string { return utils.FormatKeyWithFormat(s, utils.KeyFormatEnv) },
	}

	tmpl, err := template.New("tmpl").Option("missingkey=error").Funcs(funcs).Parse(templateStr)
	if err != nil {
		return "", err
	}

	meta := map[string]any{
		"Title": rawEntry.Title, "title": rawEntry.Title,
		"Tags": rawEntry.Tags, "tags": rawEntry.Tags,
		"UUID": rawEntry.UUID, "uuid": rawEntry.UUID,
		"Path": entryPath, "path": entryPath,
		"Vault": vaultName, "vault": vaultName,
		"Provider": providerName, "provider": providerName,
	}
	if rawEntry.Group != nil {
		grp := map[string]any{
			"Name": rawEntry.Group.Name, "name": rawEntry.Group.Name,
			"UUID": rawEntry.Group.UUID, "uuid": rawEntry.Group.UUID,
			"Path": rawEntry.Group.Path, "path": rawEntry.Group.Path,
		}
		meta["Group"] = grp
		meta["group"] = grp
	} else {
		meta["Group"] = nil
		meta["group"] = nil
	}
	data := map[string]any{
		"Meta":       meta,
		"meta":       meta,
		"Attributes": rawEntry.Attributes,
		"attributes": rawEntry.Attributes,
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// SearchableAttributesProvider implements SearchableProvider when the underlying provider is searchable.
type SearchableAttributesProvider struct {
	AttributesProvider
}

// RootPrefixes delegates root prefix reporting if the underlying provider supports it.
func (s *SearchableAttributesProvider) RootPrefixes() []string {
	if rp, ok := s.underlying.(interface{ RootPrefixes() []string }); ok {
		return rp.RootPrefixes()
	}
	return nil
}

// GetEntryWithPath retrieves a structured entry and injects configured attributes.
func (s *SearchableAttributesProvider) GetEntryWithPath(ctx context.Context, location string) (Entry, string, error) {
	providerName, vaultName, _ := utils.ParseURI(location)
	if providerName == "" {
		providerName = s.underlying.Scheme()
	}

	if pWithPath, ok := s.underlying.(interface {
		GetEntryWithPath(ctx context.Context, location string) (Entry, string, error)
	}); ok {
		entry, path, err := pWithPath.GetEntryWithPath(ctx, location)
		if err != nil {
			return Entry{}, "", err
		}
		entry.Attributes = s.injectAttributes(entry, providerName, vaultName, path)
		return entry, path, nil
	}
	entry, err := s.GetEntry(ctx, location)
	return entry, location, err
}

// GetEntry retrieves a structured entry and injects configured attributes.
func (s *SearchableAttributesProvider) GetEntry(ctx context.Context, location string) (Entry, error) {
	providerName, vaultName, _ := utils.ParseURI(location)
	if providerName == "" {
		providerName = s.underlying.Scheme()
	}

	entry, err := s.underlying.(SearchableProvider).GetEntry(ctx, location)
	if err != nil {
		return Entry{}, err
	}
	entry.Attributes = s.injectAttributes(entry, providerName, vaultName, location)
	return entry, nil
}

// Search queries the underlying provider and injects configured attributes on all returned entries.
func (s *SearchableAttributesProvider) Search(ctx context.Context, query SearchQuery) ([]SearchResult, error) {
	results, err := s.underlying.(SearchableProvider).Search(ctx, query)
	if err != nil {
		return nil, err
	}
	for i := range results {
		results[i].Entry.Attributes = s.injectAttributes(results[i].Entry, results[i].Provider, results[i].Vault, results[i].Path)
	}
	return results, nil
}

// ValueResolvableAttributesProvider implements ValueResolvableProvider when underlying implements it.
type ValueResolvableAttributesProvider struct {
	AttributesProvider
}

// SupportsValueResolution delegates to the underlying ValueResolvableProvider.
func (v *ValueResolvableAttributesProvider) SupportsValueResolution() bool {
	if vr, ok := v.underlying.(ValueResolvableProvider); ok {
		return vr.SupportsValueResolution()
	}
	return false
}

// SearchableValueResolvableAttributesProvider implements both SearchableProvider and ValueResolvableProvider.
type SearchableValueResolvableAttributesProvider struct {
	SearchableAttributesProvider
}

// SupportsValueResolution delegates to the underlying ValueResolvableProvider.
func (s *SearchableValueResolvableAttributesProvider) SupportsValueResolution() bool {
	if vr, ok := s.underlying.(ValueResolvableProvider); ok {
		return vr.SupportsValueResolution()
	}
	return false
}

// NewAttributesProvider constructs an AttributesProvider wrapper preserving optional provider interfaces.
func NewAttributesProvider(p SecretProvider, attributes map[string]any) SecretProvider {
	if len(attributes) == 0 {
		return p
	}

	base := AttributesProvider{
		underlying: p,
		attributes: attributes,
	}

	_, isSearchable := p.(SearchableProvider)
	_, isValueResolvable := p.(ValueResolvableProvider)

	if isSearchable && isValueResolvable {
		return &SearchableValueResolvableAttributesProvider{
			SearchableAttributesProvider: SearchableAttributesProvider{
				AttributesProvider: base,
			},
		}
	}
	if isSearchable {
		return &SearchableAttributesProvider{
			AttributesProvider: base,
		}
	}
	if isValueResolvable {
		return &ValueResolvableAttributesProvider{
			AttributesProvider: base,
		}
	}
	return &base
}
