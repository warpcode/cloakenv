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

// Search handles "cloakenv search [query] [--vault <vault> ...] [-i KEY ...] [--json | --yaml]"
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
			flatRes["provider"] = r.Provider
		case "vault":
			flatRes["vault"] = r.Vault
		case "path":
			flatRes["path"] = r.Path
		case "title":
			flatRes["title"] = r.Entry.Title
		case "tags":
			flatRes["tags"] = r.Entry.Tags
		default:
			key, val := resolveSelectedAttribute(r.Entry.Attributes, lowerAttrs, field, fieldLower)
			flatRes[key] = val
		}
	}
	return flatRes
}

func resolveSelectedAttribute(attributes map[string]any, lowerAttrs map[string]string, field, fieldLower string) (string, any) {
	if origKey, ok := lowerAttrs[fieldLower]; ok {
		return utils.FormatKey(origKey), attributes[origKey]
	}
	if v, ok := attributes[field]; ok {
		return utils.FormatKey(field), v
	}
	return utils.FormatKey(field), nil
}

func flattenDefaultEntry(r provider.SearchResult) map[string]any {
	flatRes := make(map[string]any, 5+len(r.Entry.Attributes))
	flatRes["provider"] = r.Provider
	flatRes["vault"] = r.Vault
	flatRes["path"] = r.Path
	flatRes["title"] = r.Entry.Title
	flatRes["tags"] = r.Entry.Tags

	for k, v := range r.Entry.Attributes {
		kLower := strings.ToLower(k)
		if kLower == "title" || kLower == "tags" {
			continue
		}
		flatRes[utils.FormatKey(k)] = v
	}
	return flatRes
}
