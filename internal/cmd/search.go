package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/warpcode/cloakenv/internal/config"
	"github.com/warpcode/cloakenv/internal/engine"
	"github.com/warpcode/cloakenv/internal/provider"
	"github.com/warpcode/cloakenv/internal/utils"
)

// Search handles "cloakenv search [query] [--vault <vault> ...] [-i KEY ...] [-o yaml | json] [-f none | env | snake | kebab]"
func Search(args []string, cfg *config.Config) int {
	if utils.HasHelpFlag(args) {
		PrintSearchHelp()
		return 0
	}

	query, repoScopes, selectedKeys, outputFormat, keyFormat, err := parseSearchArgs(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	orch, err := engine.NewOrchestrator(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Config error: %v\n", err)
		return 1
	}
	ctx := context.Background()

	results, err := orch.Search(ctx, query, repoScopes)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Search failed: %v\n", err)
		return 1
	}

	flatResults := flattenSearchResults(results, selectedKeys, keyFormat)

	asJSON := (outputFormat == "json")
	if err := utils.RenderOutput(flatResults, asJSON, "results"); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	return 0
}

func parseSearchArgs(args []string) (query string, repoScopes []string, selectedKeys []string, outputFormat string, keyFormat string, err error) {
	outputFormat = "yaml" // default
	keyFormat = "env"     // default

	parser := NewFlagParser()
	parser.Var([]string{"-o", "--output"}, true, "flag -o/--output requires an argument", func(name, val string) error {
		if val != "yaml" && val != "json" {
			return fmt.Errorf("invalid output format %q (expected yaml or json)", val)
		}
		outputFormat = val
		return nil
	})
	parser.Var([]string{"-f", "--key-format"}, true, "flag -f/--key-format requires an argument", func(name, val string) error {
		if err := utils.ValidateKeyFormat(val); err != nil {
			return err
		}
		keyFormat = val
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
		return "", nil, nil, "", "", err
	}

	return query, repoScopes, selectedKeys, outputFormat, keyFormat, nil
}

func flattenSearchResults(results []provider.SearchResult, selectedKeys []string, keyFormat string) []map[string]any {
	flatResults := make([]map[string]any, len(results))

	selectedKeysLower := make([]string, len(selectedKeys))
	for i, field := range selectedKeys {
		selectedKeysLower[i] = strings.ToLower(field)
	}

	for i, r := range results {
		flatResults[i] = flattenEntry(r, selectedKeys, selectedKeysLower, keyFormat)
	}
	return flatResults
}

func flattenEntry(r provider.SearchResult, selectedKeys []string, selectedKeysLower []string, keyFormat string) map[string]any {
	if len(selectedKeys) > 0 {
		return flattenSelectedKeys(r, selectedKeys, selectedKeysLower, keyFormat)
	}
	return flattenDefaultEntry(r, keyFormat)
}

func isMetadataKey(key string) bool {
	switch strings.ToLower(key) {
	case "title", "tags", "uuid", "group", "path", "vault", "provider":
		return true
	default:
		return false
	}
}

func flattenSelectedKeys(r provider.SearchResult, selectedKeys []string, selectedKeysLower []string, keyFormat string) map[string]any {
	flatRes := make(map[string]any, len(selectedKeys))

	var lowerAttrs map[string]string
	if len(r.Entry.Attributes) > 0 {
		lowerAttrs = make(map[string]string, len(r.Entry.Attributes))
		for k := range r.Entry.Attributes {
			lowerAttrs[strings.ToLower(k)] = k
		}
	}

	for j, field := range selectedKeys {
		fieldLower := selectedKeysLower[j]
		switch fieldLower {
		case "provider":
			if r.Provider != "" {
				flatRes["provider"] = r.Provider
			}
		case "vault":
			if r.Vault != "" {
				flatRes["vault"] = r.Vault
			}
		case "path":
			if r.Path != "" {
				flatRes["path"] = r.Path
			}
		case "title":
			if r.Entry.Title != "" {
				flatRes["title"] = r.Entry.Title
			} else if origKey, ok := lowerAttrs["title"]; ok {
				flatRes["title"] = utils.FormatValueWithKeyFormat(r.Entry.Attributes[origKey], keyFormat)
			}
		case "tags":
			if len(r.Entry.Tags) > 0 {
				flatRes["tags"] = r.Entry.Tags
			} else if origKey, ok := lowerAttrs["tags"]; ok {
				flatRes["tags"] = utils.FormatValueWithKeyFormat(r.Entry.Attributes[origKey], keyFormat)
			}
		case "uuid":
			if origKey, ok := lowerAttrs["uuid"]; ok {
				flatRes["uuid"] = utils.FormatValueWithKeyFormat(r.Entry.Attributes[origKey], keyFormat)
			}
		case "group":
			if origKey, ok := lowerAttrs["group"]; ok {
				flatRes["group"] = utils.FormatValueWithKeyFormat(r.Entry.Attributes[origKey], keyFormat)
			}
		default:
			key, val := resolveSelectedAttribute(r.Entry.Attributes, lowerAttrs, field, fieldLower, keyFormat)
			flatRes[key] = val
		}
	}
	return flatRes
}

func resolveSelectedAttribute(attributes map[string]any, lowerAttrs map[string]string, field, fieldLower string, keyFormat string) (string, any) {
	if isMetadataKey(field) {
		if origKey, ok := lowerAttrs[fieldLower]; ok {
			return fieldLower, utils.FormatValueWithKeyFormat(attributes[origKey], keyFormat)
		}
		if v, ok := attributes[field]; ok {
			return fieldLower, utils.FormatValueWithKeyFormat(v, keyFormat)
		}
		return fieldLower, nil
	}

	if origKey, ok := lowerAttrs[fieldLower]; ok {
		return utils.FormatKeyWithFormat(origKey, keyFormat), utils.FormatValueWithKeyFormat(attributes[origKey], keyFormat)
	}
	if v, ok := attributes[field]; ok {
		return utils.FormatKeyWithFormat(field, keyFormat), utils.FormatValueWithKeyFormat(v, keyFormat)
	}
	return utils.FormatKeyWithFormat(field, keyFormat), nil
}

func flattenDefaultEntry(r provider.SearchResult, keyFormat string) map[string]any {
	flatRes := make(map[string]any, 5+len(r.Entry.Attributes))
	if r.Provider != "" {
		flatRes["provider"] = r.Provider
	}
	if r.Vault != "" {
		flatRes["vault"] = r.Vault
	}
	if r.Path != "" {
		flatRes["path"] = r.Path
	}
	if r.Entry.Title != "" {
		flatRes["title"] = r.Entry.Title
	}
	if len(r.Entry.Tags) > 0 {
		flatRes["tags"] = r.Entry.Tags
	}

	for k, v := range r.Entry.Attributes {
		kLower := strings.ToLower(k)
		if kLower == "title" && r.Entry.Title != "" {
			continue
		}
		if kLower == "tags" && len(r.Entry.Tags) > 0 {
			continue
		}
		if isMetadataKey(k) {
			flatRes[kLower] = utils.FormatValueWithKeyFormat(v, keyFormat)
		} else {
			flatRes[utils.FormatKeyWithFormat(k, keyFormat)] = utils.FormatValueWithKeyFormat(v, keyFormat)
		}
	}
	return flatRes
}
