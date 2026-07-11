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
	i := 0
	for i < len(args) {
		if args[i] == "-o" || args[i] == "--output" {
			if i+1 >= len(args) {
				return "", nil, nil, "", fmt.Errorf("flag -o/--output requires an argument")
			}
			format := args[i+1]
			if format != "yaml" && format != "json" {
				return "", nil, nil, "", fmt.Errorf("invalid output format %q (expected yaml or json)", format)
			}
			outputFormat = format
			i += 2
		} else if args[i] == "--vault" {
			if i+1 >= len(args) {
				return "", nil, nil, "", fmt.Errorf("flag --vault requires an argument")
			}
			repoScopes = append(repoScopes, args[i+1])
			i += 2
		} else if args[i] == "-i" {
			if i+1 >= len(args) {
				return "", nil, nil, "", fmt.Errorf("flag -i requires an argument")
			}
			selectedKeys = append(selectedKeys, args[i+1])
			i += 2
		} else if strings.HasPrefix(args[i], "-") {
			return "", nil, nil, "", fmt.Errorf("unknown flag: %s", args[i])
		} else {
			if query != "" {
				return "", nil, nil, "", fmt.Errorf("usage: cloakenv search [query] [--vault <vault> ...] [-i KEY ...] [-o yaml | json]")
			}
			query = args[i]
			i++
		}
	}
	return query, repoScopes, selectedKeys, outputFormat, nil
}

func flattenSearchResults(results []provider.SearchResult, selectedKeys []string) []map[string]any {
	flatResults := make([]map[string]any, len(results))
	for i, r := range results {
		flatRes := make(map[string]any)
		if len(selectedKeys) > 0 {
			for _, field := range selectedKeys {
				fieldLower := strings.ToLower(field)
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
					found := false
					for k, v := range r.Entry.Attributes {
						if strings.ToLower(k) == fieldLower {
							flatRes[utils.FormatKey(k)] = v
							found = true
							break
						}
					}
					if !found {
						if v, ok := r.Entry.Attributes[field]; ok {
							flatRes[utils.FormatKey(field)] = v
						} else {
							flatRes[utils.FormatKey(field)] = nil
						}
					}
				}
			}
		} else {
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
		}
		flatResults[i] = flatRes
	}
	return flatResults
}
