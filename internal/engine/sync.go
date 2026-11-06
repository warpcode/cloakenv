package engine

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/warpcode/cloakenv/internal/provider"
	"github.com/warpcode/cloakenv/internal/utils"
)

// SyncOptions configures filtering and behavior for cross-vault secret synchronization.
type SyncOptions struct {
	Tags   []string
	Prefix string
	Regex  string
	DryRun bool
}

// SyncEntryDiff records the synchronization action for a single key location.
// Crucially, it contains only key metadata and statuses, never resolved secret values.
type SyncEntryDiff struct {
	Key    string `json:"key" yaml:"key"`
	Status string `json:"status" yaml:"status"` // "added", "updated", "skipped"
	Reason string `json:"reason,omitempty" yaml:"reason,omitempty"`
}

// SyncSummary aggregates synchronization metrics and individual diffs.
type SyncSummary struct {
	Added   int             `json:"added" yaml:"added"`
	Updated int             `json:"updated" yaml:"updated"`
	Skipped int             `json:"skipped" yaml:"skipped"`
	Diffs   []SyncEntryDiff `json:"diffs" yaml:"diffs"`
}

// Sync performs unidirectional secret synchronization from sourceURI to targetURI in memory.
func (o *Orchestrator) Sync(ctx context.Context, sourceURI, targetURI string, opts SyncOptions) (*SyncSummary, error) {
	if o == nil {
		return nil, fmt.Errorf("orchestrator is nil")
	}

	targetScheme, targetBaseLoc, err := utils.ParseURI(targetURI)
	if err != nil {
		return nil, fmt.Errorf("invalid target URI %q: %w", targetURI, err)
	}

	targetProv, _, err := o.providerManager.GetProvider(ctx, targetScheme)
	if err != nil {
		return nil, fmt.Errorf("failed to get target provider %q: %w", targetScheme, err)
	}

	writableTarget, ok := targetProv.(provider.WritableProvider)
	if !ok {
		return nil, fmt.Errorf("target vault %q is read-only or does not support writing", targetScheme)
	}

	// Validate target by checking scheme; read-only providers are rejected fast
	switch targetScheme {
	case "env", "keepass", "search", "yaml", "json", "custom_vault":
		return nil, fmt.Errorf("target vault %q is read-only or does not support writing", targetScheme)
	}
	if o.config != nil && o.config.Vaults != nil {
		if vCfg, exists := o.config.Vaults[targetScheme]; exists {
			switch vCfg.Provider {
			case "keepass", "search", "yaml", "json", "custom_vault":
				return nil, fmt.Errorf("target vault %q is read-only or does not support writing", targetScheme)
			}
		}
	}

	var compiledRegex *regexp.Regexp
	if opts.Regex != "" {
		cr, err := regexp.Compile(opts.Regex)
		if err != nil {
			return nil, fmt.Errorf("invalid regex filter %q: %w", opts.Regex, err)
		}
		compiledRegex = cr
	}

	sourceKeys, isSingleSecret, err := o.extractSourceKeys(ctx, sourceURI, opts)
	if err != nil {
		return nil, err
	}

	// Filter and sort keys deterministically
	var filteredKeys []string
	for key := range sourceKeys {
		if opts.Prefix != "" && !strings.HasPrefix(key, opts.Prefix) {
			continue
		}
		if compiledRegex != nil && !compiledRegex.MatchString(key) {
			continue
		}
		filteredKeys = append(filteredKeys, key)
	}
	slices.Sort(filteredKeys)

	summary := &SyncSummary{
		Diffs: make([]SyncEntryDiff, 0, len(filteredKeys)),
	}

	for _, key := range filteredKeys {
		srcVal := sourceKeys[key]

		targetLoc := buildTargetLocation(targetBaseLoc, key, isSingleSecret)

		// Compare with target value
		existingVal, getErr := targetProv.GetSecret(ctx, targetLoc)

		var diff SyncEntryDiff
		if getErr != nil {
			diff = SyncEntryDiff{
				Key:    key,
				Status: "added",
			}
			if !opts.DryRun {
				if err := writableTarget.SetSecret(ctx, targetLoc, srcVal); err != nil {
					return nil, fmt.Errorf("failed to write secret to target %s: %w", targetLoc, err)
				}
			}
			summary.Added++
		} else if existingVal != srcVal {
			diff = SyncEntryDiff{
				Key:    key,
				Status: "updated",
			}
			if !opts.DryRun {
				if err := writableTarget.SetSecret(ctx, targetLoc, srcVal); err != nil {
					return nil, fmt.Errorf("failed to update secret in target %s: %w", targetLoc, err)
				}
			}
			summary.Updated++
		} else {
			diff = SyncEntryDiff{
				Key:    key,
				Status: "skipped",
				Reason: "unchanged",
			}
			summary.Skipped++
		}
		summary.Diffs = append(summary.Diffs, diff)
	}

	return summary, nil
}

// extractSourceKeys extracts flattened secret keys and resolved values from sourceURI.
func (o *Orchestrator) extractSourceKeys(ctx context.Context, sourceURI string, opts SyncOptions) (map[string]string, bool, error) {
	scheme, location, err := utils.ParseURI(sourceURI)
	if err != nil {
		return nil, false, fmt.Errorf("invalid source URI %q: %w", sourceURI, err)
	}

	srcProv, _, err := o.providerManager.GetProvider(ctx, scheme)
	if err != nil {
		return nil, false, fmt.Errorf("failed to get source provider %q: %w", scheme, err)
	}

	results := make(map[string]string)

	searchable, isSearchable := srcProv.(provider.SearchableProvider)

	if !isSearchable {
		val, err := o.Resolve(ctx, sourceURI)
		if err != nil {
			return nil, false, fmt.Errorf("failed to resolve source secret %q: %w", sourceURI, err)
		}
		key := location
		if idx := strings.LastIndex(location, "/"); idx >= 0 {
			key = location[idx+1:]
		}
		results[key] = val
		return results, true, nil
	}

	if strings.Contains(location, ":") {
		val, err := o.Resolve(ctx, sourceURI)
		if err != nil {
			return nil, false, fmt.Errorf("failed to resolve source secret %q: %w", sourceURI, err)
		}
		results[location] = val
		return results, true, nil
	}

	if location != "" {
		entry, err := o.GetEntry(ctx, sourceURI)
		if err == nil {
			if len(entry.Attributes) == 1 {
				if _, hasKey := entry.Attributes[location]; hasKey {
					strVal, _ := utils.SerializeAttrValue(entry.Attributes[location])
					results[location] = strVal
					return results, true, nil
				}
			}
			if matchesTags(entry.Tags, opts.Tags) {
				extractEntryAttributes(location, entry, results)
			}
			return results, false, nil
		}
	}

	entries, err := searchable.Search(ctx, provider.SearchQuery{})
	if err != nil {
		return nil, false, fmt.Errorf("failed to search source vault %q: %w", scheme, err)
	}

	for _, sr := range entries {
		if !matchesTags(sr.Entry.Tags, opts.Tags) {
			continue
		}
		entryPath := sr.Path
		if entryPath == "" {
			entryPath = sr.Entry.Title
		}
		extractEntryAttributes(entryPath, sr.Entry, results)
	}

	return results, false, nil
}

func extractEntryAttributes(basePath string, entry provider.Entry, results map[string]string) {
	for attrKey, attrVal := range entry.Attributes {
		if strings.EqualFold(attrKey, "title") || strings.EqualFold(attrKey, "tags") {
			continue
		}
		keyName := attrKey
		if basePath != "" {
			keyName = basePath + "/" + attrKey
		}
		strVal, _ := utils.SerializeAttrValue(attrVal)
		results[keyName] = strVal
	}
}

func matchesTags(entryTags []string, filterTags []string) bool {
	if len(filterTags) == 0 {
		return true
	}
	tagSet := make(map[string]bool, len(entryTags))
	for _, t := range entryTags {
		tagSet[strings.ToLower(t)] = true
	}
	for _, reqTag := range filterTags {
		if !tagSet[strings.ToLower(reqTag)] {
			return false
		}
	}
	return true
}

func buildTargetLocation(targetBaseLoc, key string, isSingleSecret bool) string {
	if targetBaseLoc == "" {
		return key
	}
	targetBaseLoc = strings.TrimSuffix(targetBaseLoc, "/")
	if isSingleSecret && strings.Contains(targetBaseLoc, "/") {
		return targetBaseLoc
	}
	key = strings.TrimPrefix(key, "/")
	return targetBaseLoc + "/" + key
}
