package engine

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/warpcode/cloakenv/internal/config"
	"github.com/warpcode/cloakenv/internal/provider"
	"github.com/warpcode/cloakenv/internal/utils"
)

// StageOriginal represents Stage 1 (Raw/Original) of the entry processing pipeline.
type StageOriginal struct {
	Attributes map[string]any `json:"attributes" yaml:"attributes"`
}

// StageFiltered represents Stage 2 (Field Filtering) of the entry processing pipeline.
type StageFiltered struct {
	Attributes   map[string]any `json:"attributes" yaml:"attributes"`
	ExcludedKeys []string       `json:"excluded_keys" yaml:"excluded_keys"`
}

// StageMapped represents Stage 3 (Key Mapping) of the entry processing pipeline.
type StageMapped struct {
	Attributes  map[string]any    `json:"attributes" yaml:"attributes"`
	RenamedKeys map[string]string `json:"renamed_keys" yaml:"renamed_keys"`
}

// StageKeyFormatted represents Stage 4 (Key Formatting) of the entry processing pipeline.
type StageKeyFormatted struct {
	Attributes    map[string]any    `json:"attributes" yaml:"attributes"`
	FormattedKeys map[string]string `json:"formatted_keys" yaml:"formatted_keys"`
}

// StageFinalOutput represents Stage 5 (Computed Attributes) of the entry processing pipeline.
type StageFinalOutput struct {
	Attributes      map[string]any `json:"attributes" yaml:"attributes"`
	AddedAttributes []string       `json:"added_attributes" yaml:"added_attributes"`
}

// PipelineExplanation provides a structured breakdown of an entry across each of the 5 pipeline stages.
type PipelineExplanation struct {
	Original     StageOriginal     `json:"original" yaml:"original"`
	Filtered     StageFiltered     `json:"filtered" yaml:"filtered"`
	Mapped       StageMapped       `json:"mapped" yaml:"mapped"`
	KeyFormatted StageKeyFormatted `json:"key_formatted" yaml:"key_formatted"`
	FinalOutput  StageFinalOutput  `json:"final_output" yaml:"final_output"`
}

// ExplainEntry evaluates and traces an entry across each of the 5 entry processing pipeline stages:
// Stage 1 (original) -> Stage 2 (filtered) -> Stage 3 (mapped) -> Stage 4 (key_formatted) -> Stage 5 (final_output).
func (o *Orchestrator) ExplainEntry(ctx context.Context, uri string) (*PipelineExplanation, error) {
	scheme, location, err := utils.ParseURI(uri)
	if err != nil {
		return nil, err
	}

	rawEntry, err := o.resolver.GetRawEntry(ctx, uri)
	if err != nil {
		return nil, err
	}

	finalEntry, err := o.resolver.GetEntry(ctx, uri)
	if err != nil {
		return nil, err
	}

	var vCfg config.VaultConfig
	if o.config != nil && o.config.Vaults != nil {
		vCfg = o.config.Vaults[scheme]
	}

	var rootPrefixes []string
	if p, _, err := o.providerManager.GetProvider(ctx, scheme); err == nil {
		if rpProvider, ok := p.(interface{ RootPrefixes() []string }); ok {
			rootPrefixes = rpProvider.RootPrefixes()
		}
	}

	// Stage 1: original
	origAttrs := copyAttributesMap(rawEntry.Attributes)
	explanation := &PipelineExplanation{
		Original: StageOriginal{
			Attributes: origAttrs,
		},
	}

	// Stage 2: filtered
	filteredAttrs := copyAttributesMap(origAttrs)
	var excludedKeys []string
	if len(vCfg.IncludeFields) > 0 || len(vCfg.ExcludeFields) > 0 {
		filteredEntry := provider.FilterEntryWithPath(rawEntry, location, rootPrefixes, vCfg.IncludeFields, vCfg.ExcludeFields)
		filteredAttrs = copyAttributesMap(filteredEntry.Attributes)
		excludedKeys = findExcludedKeys(origAttrs, filteredAttrs, "")
	}
	if excludedKeys == nil {
		excludedKeys = []string{}
	}
	explanation.Filtered = StageFiltered{
		Attributes:   filteredAttrs,
		ExcludedKeys: excludedKeys,
	}

	// Stage 3: mapped
	mappedAttrs := copyAttributesMap(filteredAttrs)
	renamedKeys := make(map[string]string)
	if len(vCfg.Mapping) > 0 {
		mapped, mapErr := provider.ApplyMappingAndFilteringToAttributesWithContextAndRenderer(
			ctx,
			o.providerManager.renderTemplateFunc,
			rawEntry.Attributes,
			vCfg.Mapping,
			vCfg.IncludeFields,
			vCfg.ExcludeFields,
			location,
			rootPrefixes,
			rawEntry,
			scheme,
			scheme,
		)
		if mapErr != nil {
			return nil, fmt.Errorf("mapping stage failed: %w", mapErr)
		}
		mappedAttrs = copyAttributesMap(mapped)
		findRenamedKeysInMap(rawEntry.Attributes, rawEntry, vCfg.Mapping, scheme, location, o.providerManager.renderTemplateFunc, ctx, renamedKeys)
	}
	explanation.Mapped = StageMapped{
		Attributes:  mappedAttrs,
		RenamedKeys: renamedKeys,
	}

	// Stage 4: key_formatted
	kf := vCfg.KeyFormat
	if o.config != nil && o.config.KeyFormatOverride != "" {
		kf = o.config.KeyFormatOverride
	}
	keyFormattedAttrs := copyAttributesMap(mappedAttrs)
	formattedKeys := make(map[string]string)
	if kf != "" && kf != "none" {
		keyFormattedAttrs = make(map[string]any, len(mappedAttrs))
		for k, v := range mappedAttrs {
			fmtKey := utils.FormatKeyWithFormat(k, kf)
			keyFormattedAttrs[fmtKey] = provider.FormatValue(v, kf)
		}
		findFormattedKeys(mappedAttrs, kf, "", formattedKeys)
	}
	explanation.KeyFormatted = StageKeyFormatted{
		Attributes:    keyFormattedAttrs,
		FormattedKeys: formattedKeys,
	}

	// Stage 5: final_output
	finalAttrs := copyAttributesMap(finalEntry.Attributes)
	var addedAttributes []string
	if len(vCfg.Attributes) > 0 {
		for k := range vCfg.Attributes {
			addedAttributes = append(addedAttributes, k)
		}
		sort.Strings(addedAttributes)
	}
	if addedAttributes == nil {
		addedAttributes = []string{}
	}
	explanation.FinalOutput = StageFinalOutput{
		Attributes:      finalAttrs,
		AddedAttributes: addedAttributes,
	}

	return explanation, nil
}

func copyAttributesMap(m map[string]any) map[string]any {
	if m == nil {
		return make(map[string]any)
	}
	cp := make(map[string]any, len(m))
	for k, v := range m {
		cp[k] = provider.CloneValue(v)
	}
	return cp
}

func findExcludedKeys(orig, filtered map[string]any, prefix string) []string {
	var excluded []string
	for k, origV := range orig {
		path := k
		if prefix != "" {
			path = prefix + "." + k
		}
		filteredV, exists := filtered[k]
		if !exists {
			excluded = append(excluded, path)
			continue
		}
		origChildMap, isOrigMap := origV.(map[string]any)
		filteredChildMap, isFilteredMap := filteredV.(map[string]any)
		if isOrigMap && isFilteredMap {
			excluded = append(excluded, findExcludedKeys(origChildMap, filteredChildMap, path)...)
		}
	}
	sort.Strings(excluded)
	return excluded
}

func findRenamedKeysInMap(
	attrs map[string]any,
	rawEntry provider.Entry,
	rules []config.MappingRule,
	scheme, location string,
	renderFn provider.TemplateRenderFunc,
	ctx context.Context,
	renamedKeys map[string]string,
) {
	for k, v := range attrs {
		for _, rule := range rules {
			if rule.CompiledRegex == nil {
				_ = rule.Compile()
			}
			if rule.CompiledRegex == nil {
				continue
			}
			if rule.CompiledRegex.MatchString(k) {
				tmplStr := provider.ConvertBackslashGroups(rule.Key)
				newKey := rule.CompiledRegex.ReplaceAllString(k, tmplStr)
				if strings.Contains(newKey, "{{") && strings.Contains(newKey, "}}") {
					if renderFn != nil {
						data := provider.BuildTemplateData(rawEntry, scheme, scheme, location)
						if rendered, err := renderFn(ctx, newKey, data); err == nil {
							newKey = rendered
						}
					}
				}
				if newKey != "" && newKey != k {
					renamedKeys[k] = newKey
				}
			}
		}
		if childMap, ok := v.(map[string]any); ok {
			findRenamedKeysInMap(childMap, rawEntry, rules, scheme, location, renderFn, ctx, renamedKeys)
		}
	}
}

func findFormattedKeys(m map[string]any, kf string, prefix string, formattedKeys map[string]string) {
	for k, v := range m {
		fmtKey := utils.FormatKeyWithFormat(k, kf)
		path := k
		fmtPath := fmtKey
		if prefix != "" {
			path = prefix + "." + k
			fmtPath = prefix + "." + fmtKey
		}
		if fmtKey != k {
			formattedKeys[path] = fmtPath
		}
		if childMap, ok := v.(map[string]any); ok {
			findFormattedKeys(childMap, kf, fmtPath, formattedKeys)
		}
	}
}
