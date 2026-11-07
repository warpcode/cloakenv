package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/warpcode/cloakenv/internal/config"
	"github.com/warpcode/cloakenv/internal/engine"
	"github.com/warpcode/cloakenv/internal/provider"
	"github.com/warpcode/cloakenv/internal/utils"
)

// Search handles "cloakenv search [query] [--vault <vault> ...] [-i KEY ...] [-o yaml | json] [-f format] [--show-meta]"
func Search(args []string, cfg *config.Config) int {
	if utils.HasHelpFlag(args) {
		PrintSearchHelp()
		return 0
	}

	query, repoScopes, selectedKeys, outputFormat, keyFormat, showMeta, err := parseSearchArgs(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	if keyFormat != "" {
		if cfg == nil {
			cfg = &config.Config{}
		}
		cfg.KeyFormatOverride = keyFormat
	}

	orch, err := engine.NewOrchestrator(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Config error: %v\n", err)
		return 1
	}
	ctx, stop := signalContext()
	defer stop()

	results, err := orch.Search(ctx, query, repoScopes)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Search failed: %v\n", err)
		return 1
	}

	flatResults := flattenSearchResults(results, selectedKeys, showMeta)

	asJSON := (outputFormat == "json")
	if err := utils.RenderOutput(flatResults, asJSON, "results"); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	return 0
}

func parseSearchArgs(args []string) (query string, repoScopes []string, selectedKeys []string, outputFormat string, keyFormat string, showMeta bool, err error) {
	outputFormat = "yaml" // default

	parser := NewFlagParser()
	parser.Var([]string{"-f", "--key-format"}, true, "flag -f/--key-format requires an argument", func(name, val string) error {
		if val != "none" && val != "env" && val != "snake" && val != "kebab" {
			return fmt.Errorf("invalid key format %q (expected none, env, snake, or kebab)", val)
		}
		keyFormat = val
		return nil
	})
	parser.Var([]string{"--show-meta"}, false, "", func(name, val string) error {
		showMeta = true
		return nil
	})
	parser.Var([]string{"-o", "--output"}, true, "flag -o/--output requires an argument", func(name, val string) error {
		if val != "yaml" && val != "json" {
			return fmt.Errorf("invalid output format %q (expected yaml or json)", val)
		}
		outputFormat = val
		return nil
	})
	parser.StringSlice([]string{"--vault"}, &repoScopes, "flag --vault requires an argument")
	parser.StringSlice([]string{"-i"}, &selectedKeys, "flag -i requires an argument")
	parser.UnknownFlagErr = func(flag string) error {
		return fmt.Errorf("unknown flag: %s", flag)
	}
	parser.PositionalHandler = func(arg string) error {
		if query != "" {
			return fmt.Errorf("usage: cloakenv search [query] [--vault <vault> ...] [-i KEY ...] [-o yaml | json]")
		}
		query = arg
		return nil
	}

	if _, err := parser.Parse(args); err != nil {
		return "", nil, nil, "", "", false, err
	}

	return query, repoScopes, selectedKeys, outputFormat, keyFormat, showMeta, nil
}

type selectedKeyKind uint8

const (
	keyKindDefaultAttr selectedKeyKind = iota
	keyKindMeta
	keyKindMetaProvider
	keyKindMetaVault
	keyKindMetaTags
	keyKindProvider
	keyKindVault
	keyKindPath
	keyKindTitle
	keyKindTags
	keyKindUUID
	keyKindGroup
	keyKindGroupName
	keyKindGroupUUID
	keyKindGroupPath
)

type selectedKeySpec struct {
	field string
	kind  selectedKeyKind
}

func parseSelectedKeySpecs(selectedKeys []string) []selectedKeySpec {
	if len(selectedKeys) == 0 {
		return nil
	}
	specs := make([]selectedKeySpec, len(selectedKeys))
	for i, field := range selectedKeys {
		spec := selectedKeySpec{field: field}
		switch strings.ToLower(field) {
		case "_meta_":
			spec.kind = keyKindMeta
		case "_meta_.provider":
			spec.kind = keyKindMetaProvider
		case "_meta_.vault":
			spec.kind = keyKindMetaVault
		case "_meta_.tags":
			spec.kind = keyKindMetaTags
		case "provider":
			spec.kind = keyKindProvider
		case "vault":
			spec.kind = keyKindVault
		case "path":
			spec.kind = keyKindPath
		case "title":
			spec.kind = keyKindTitle
		case "tags":
			spec.kind = keyKindTags
		case "uuid":
			spec.kind = keyKindUUID
		case "group":
			spec.kind = keyKindGroup
		case "group_name":
			spec.kind = keyKindGroupName
		case "group_uuid":
			spec.kind = keyKindGroupUUID
		case "group_path":
			spec.kind = keyKindGroupPath
		default:
			spec.kind = keyKindDefaultAttr
		}
		specs[i] = spec
	}
	return specs
}

func flattenSearchResults(results []provider.SearchResult, selectedKeys []string, showMeta bool) []map[string]any {
	flatResults := make([]map[string]any, len(results))

	// Optimization: Pre-classify selectedKeys once to avoid repeated string conversions
	// and multi-branch string switches per entry across all search results.
	specs := parseSelectedKeySpecs(selectedKeys)

	for i, r := range results {
		flatResults[i] = flattenEntry(r, specs, showMeta)
	}
	return flatResults
}

func flattenEntry(r provider.SearchResult, specs []selectedKeySpec, showMeta bool) map[string]any {
	if len(specs) > 0 {
		return flattenSelectedKeys(r, specs)
	}
	return flattenDefaultEntry(r, showMeta)
}

// flattenSelectedKeys extracts selected keys for a result entry using pre-classified key specs.
func flattenSelectedKeys(r provider.SearchResult, specs []selectedKeySpec) map[string]any {
	flatRes := make(map[string]any, len(specs))

	for _, spec := range specs {
		switch spec.kind {
		case keyKindMeta:
			metaMap := make(map[string]any)
			if r.Provider != "" {
				metaMap["provider"] = r.Provider
			}
			if r.Vault != "" {
				metaMap["vault"] = r.Vault
			}
			if len(r.Entry.Tags) > 0 {
				metaMap["tags"] = r.Entry.Tags
			}
			if len(metaMap) > 0 {
				flatRes["_meta_"] = metaMap
			}
		case keyKindMetaProvider:
			if r.Provider != "" {
				flatRes["_meta_.provider"] = r.Provider
			}
		case keyKindMetaVault:
			if r.Vault != "" {
				flatRes["_meta_.vault"] = r.Vault
			}
		case keyKindMetaTags:
			if len(r.Entry.Tags) > 0 {
				flatRes["_meta_.tags"] = r.Entry.Tags
			}
		case keyKindProvider:
			if r.Provider != "" {
				flatRes["provider"] = r.Provider
			}
		case keyKindVault:
			if r.Vault != "" {
				flatRes["vault"] = r.Vault
			}
		case keyKindPath:
			if r.Path != "" {
				flatRes["path"] = r.Path
			}
		case keyKindTitle:
			if r.Entry.Title != "" {
				flatRes["title"] = r.Entry.Title
			} else if _, val, ok := findAttrCaseInsensitive(r.Entry.Attributes, "title"); ok {
				flatRes["title"] = val
			}
		case keyKindTags:
			if len(r.Entry.Tags) > 0 {
				flatRes["tags"] = r.Entry.Tags
			} else if _, val, ok := findAttrCaseInsensitive(r.Entry.Attributes, "tags"); ok {
				flatRes["tags"] = val
			}
		case keyKindUUID:
			if r.Entry.UUID != "" {
				flatRes["uuid"] = r.Entry.UUID
			} else if _, val, ok := findAttrCaseInsensitive(r.Entry.Attributes, "uuid"); ok {
				flatRes["uuid"] = val
			}
		case keyKindGroup:
			if r.Entry.Group != nil {
				flatRes["group"] = r.Entry.Group
			} else if _, val, ok := findAttrCaseInsensitive(r.Entry.Attributes, "group"); ok {
				flatRes["group"] = val
			}
		case keyKindGroupName:
			if r.Entry.Group != nil && r.Entry.Group.Name != "" {
				flatRes["group_name"] = r.Entry.Group.Name
			} else if _, val, ok := findAttrCaseInsensitive(r.Entry.Attributes, "group_name"); ok {
				flatRes["group_name"] = val
			}
		case keyKindGroupUUID:
			if r.Entry.Group != nil && r.Entry.Group.UUID != "" {
				flatRes["group_uuid"] = r.Entry.Group.UUID
			} else if _, val, ok := findAttrCaseInsensitive(r.Entry.Attributes, "group_uuid"); ok {
				flatRes["group_uuid"] = val
			}
		case keyKindGroupPath:
			if r.Entry.Group != nil && r.Entry.Group.Path != "" {
				flatRes["group_path"] = r.Entry.Group.Path
			} else if _, val, ok := findAttrCaseInsensitive(r.Entry.Attributes, "group_path"); ok {
				flatRes["group_path"] = val
			}
		default:
			key, val := resolveSelectedAttribute(r.Entry.Attributes, spec.field)
			flatRes[key] = val
		}
	}
	return flatRes
}

func resolveSelectedAttribute(attributes map[string]any, field string) (string, any) {
	if origKey, val, ok := findAttrCaseInsensitive(attributes, field); ok {
		return origKey, val
	}
	return field, nil
}

// findAttrCaseInsensitive performs a direct lookup or case-insensitive search over attributes
// without string allocations or creating temporary maps.
func findAttrCaseInsensitive(attributes map[string]any, field string) (string, any, bool) {
	if len(attributes) == 0 {
		return "", nil, false
	}
	if val, ok := attributes[field]; ok {
		return field, val, true
	}
	for k, val := range attributes {
		if strings.EqualFold(k, field) {
			return k, val, true
		}
	}
	return "", nil, false
}

func flattenDefaultEntry(r provider.SearchResult, showMeta bool) map[string]any {
	flatRes := make(map[string]any, 7+len(r.Entry.Attributes))
	if showMeta {
		metaMap := make(map[string]any)
		if r.Provider != "" {
			metaMap["provider"] = r.Provider
		}
		if r.Vault != "" {
			metaMap["vault"] = r.Vault
		}
		if len(r.Entry.Tags) > 0 {
			metaMap["tags"] = r.Entry.Tags
		}
		if len(metaMap) > 0 {
			flatRes["_meta_"] = metaMap
		}
	}
	if r.Path != "" {
		flatRes["path"] = r.Path
	}
	if r.Entry.Title != "" {
		flatRes["title"] = r.Entry.Title
	}
	if r.Entry.UUID != "" {
		flatRes["uuid"] = r.Entry.UUID
	}
	if r.Entry.Group != nil {
		flatRes["group"] = r.Entry.Group
	}

	for k, v := range r.Entry.Attributes {
		if r.Entry.Title != "" && strings.EqualFold(k, "title") {
			continue
		}
		if r.Entry.UUID != "" && strings.EqualFold(k, "uuid") {
			continue
		}
		if r.Entry.Group != nil && strings.EqualFold(k, "group") {
			continue
		}
		flatRes[k] = v
	}
	return flatRes
}
