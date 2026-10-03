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

	var selectedKeysLower, selectedKeysFormatted []string
	if len(selectedKeys) > 0 {
		// Precompute lowercased and formatted selected keys once outside the results loop
		// to avoid per-entry allocations.
		selectedKeysLower = make([]string, len(selectedKeys))
		selectedKeysFormatted = make([]string, len(selectedKeys))
		for i, field := range selectedKeys {
			selectedKeysLower[i] = strings.ToLower(field)
			selectedKeysFormatted[i] = utils.FormatKey(field)
		}
	}

	for i, r := range results {
		flatResults[i] = flattenEntry(r, selectedKeys, selectedKeysLower, selectedKeysFormatted)
	}
	return flatResults
}

func flattenEntry(r provider.SearchResult, selectedKeys []string, selectedKeysLower []string, selectedKeysFormatted []string) map[string]any {
	if len(selectedKeys) > 0 {
		return flattenSelectedKeys(r, selectedKeys, selectedKeysLower, selectedKeysFormatted)
	}
	return flattenDefaultEntry(r)
}

func flattenSelectedKeys(r provider.SearchResult, selectedKeys []string, selectedKeysLower []string, selectedKeysFormatted []string) map[string]any {
	flatRes := make(map[string]any, len(selectedKeys))

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
			val := resolveSelectedAttributeVal(r.Entry.Attributes, field)
			flatRes[selectedKeysFormatted[j]] = val
		}
	}
	return flatRes
}

func resolveSelectedAttributeVal(attributes map[string]any, field string) any {
	if len(attributes) == 0 {
		return nil
	}
	if v, ok := attributes[field]; ok {
		return v
	}
	for k, v := range attributes {
		if strings.EqualFold(k, field) {
			return v
		}
	}
	return nil
}

func flattenDefaultEntry(r provider.SearchResult) map[string]any {
	flatRes := make(map[string]any, 5+len(r.Entry.Attributes))
	flatRes["provider"] = r.Provider
	flatRes["vault"] = r.Vault
	flatRes["path"] = r.Path
	flatRes["title"] = r.Entry.Title
	flatRes["tags"] = r.Entry.Tags

	for k, v := range r.Entry.Attributes {
		if strings.EqualFold(k, "title") || strings.EqualFold(k, "tags") {
			continue
		}
		flatRes[utils.FormatKey(k)] = v
	}
	return flatRes
}
