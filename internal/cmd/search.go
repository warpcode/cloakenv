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

// Search handles "cloakenv search [query] [--vault <vault> ...] [-i KEY ...] [-o yaml | json]"
func Search(args []string, cfg *config.Config) int {
	if utils.HasHelpFlag(args) {
		PrintSearchHelp()
		return 0
	}

	query, repoScopes, selectedKeys, outputFormat, err := parseSearchArgs(args)
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

	flatResults := flattenSearchResults(results, selectedKeys)

	asJSON := (outputFormat == "json")
	if err := utils.RenderOutput(flatResults, asJSON, "results"); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	return 0
}

func parseSearchArgs(args []string) (query string, repoScopes []string, selectedKeys []string, outputFormat string, err error) {
	outputFormat = "yaml" // default

	parser := NewFlagParser()
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
		return "", nil, nil, "", err
	}

	return query, repoScopes, selectedKeys, outputFormat, nil
}

func flattenSearchResults(results []provider.SearchResult, selectedKeys []string) []map[string]any {
	flatResults := make([]map[string]any, len(results))

	selectedKeysLower := make([]string, len(selectedKeys))
	for i, field := range selectedKeys {
		selectedKeysLower[i] = strings.ToLower(field)
	}

	for i, r := range results {
		flatResults[i] = flattenEntry(r, selectedKeys, selectedKeysLower)
	}
	return flatResults
}

func flattenEntry(r provider.SearchResult, selectedKeys []string, selectedKeysLower []string) map[string]any {
	if len(selectedKeys) > 0 {
		return flattenSelectedKeys(r, selectedKeys, selectedKeysLower)
	}
	return flattenDefaultEntry(r)
}

func flattenSelectedKeys(r provider.SearchResult, selectedKeys []string, selectedKeysLower []string) map[string]any {
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
				flatRes["title"] = r.Entry.Attributes[origKey]
			}
		case "tags":
			if len(r.Entry.Tags) > 0 {
				flatRes["tags"] = r.Entry.Tags
			} else if origKey, ok := lowerAttrs["tags"]; ok {
				flatRes["tags"] = r.Entry.Attributes[origKey]
			}
		case "uuid":
			if r.Entry.UUID != "" {
				flatRes["uuid"] = r.Entry.UUID
			} else if origKey, ok := lowerAttrs["uuid"]; ok {
				flatRes["uuid"] = r.Entry.Attributes[origKey]
			}
		case "group":
			if r.Entry.Group != nil {
				flatRes["group"] = r.Entry.Group
			} else if origKey, ok := lowerAttrs["group"]; ok {
				flatRes["group"] = r.Entry.Attributes[origKey]
			}
		case "group_name":
			if r.Entry.Group != nil && r.Entry.Group.Name != "" {
				flatRes["group_name"] = r.Entry.Group.Name
			} else if origKey, ok := lowerAttrs["group_name"]; ok {
				flatRes["group_name"] = r.Entry.Attributes[origKey]
			}
		case "group_uuid":
			if r.Entry.Group != nil && r.Entry.Group.UUID != "" {
				flatRes["group_uuid"] = r.Entry.Group.UUID
			} else if origKey, ok := lowerAttrs["group_uuid"]; ok {
				flatRes["group_uuid"] = r.Entry.Attributes[origKey]
			}
		case "group_path":
			if r.Entry.Group != nil && r.Entry.Group.Path != "" {
				flatRes["group_path"] = r.Entry.Group.Path
			} else if origKey, ok := lowerAttrs["group_path"]; ok {
				flatRes["group_path"] = r.Entry.Attributes[origKey]
			}
		default:
			key, val := resolveSelectedAttribute(r.Entry.Attributes, lowerAttrs, field, fieldLower)
			flatRes[key] = val
		}
	}
	return flatRes
}

func resolveSelectedAttribute(attributes map[string]any, lowerAttrs map[string]string, field, fieldLower string) (string, any) {
	if origKey, ok := lowerAttrs[fieldLower]; ok {
		return origKey, attributes[origKey]
	}
	if v, ok := attributes[field]; ok {
		return field, v
	}
	return field, nil
}

func flattenDefaultEntry(r provider.SearchResult) map[string]any {
	flatRes := make(map[string]any, 7+len(r.Entry.Attributes))
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
	if r.Entry.UUID != "" {
		flatRes["uuid"] = r.Entry.UUID
	}
	if r.Entry.Group != nil {
		flatRes["group"] = r.Entry.Group
	}

	for k, v := range r.Entry.Attributes {
		kLower := strings.ToLower(k)
		if kLower == "title" && r.Entry.Title != "" {
			continue
		}
		if kLower == "tags" && len(r.Entry.Tags) > 0 {
			continue
		}
		if kLower == "uuid" && r.Entry.UUID != "" {
			continue
		}
		if kLower == "group" && r.Entry.Group != nil {
			continue
		}
		flatRes[k] = v
	}
	return flatRes
}
