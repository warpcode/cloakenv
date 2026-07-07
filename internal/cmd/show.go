package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/warpcode/cloakenv/internal/config"
	"github.com/warpcode/cloakenv/internal/engine"
	"github.com/warpcode/cloakenv/internal/provider"
	"github.com/warpcode/cloakenv/internal/utils"
	"gopkg.in/yaml.v3"
)

// Show handles "cloakenv show <entry-uri> [--yaml | --json]"
func Show(args []string, cfg *config.Config) int {
	if utils.HasHelpFlag(args) {
		PrintShowHelp()
		return 0
	}
	var (
		merges        []string
		explicit      = make(map[string]string)
		whitelist     []string
		outputFormat  = "yaml" // default
		positionalURI string
	)

	i := 0
	for i < len(args) {
		switch {
		case (args[i] == "-o" || args[i] == "--output") && i+1 < len(args):
			i++
			format := args[i]
			if format != "yaml" && format != "json" && format != "env" && format != "keys" {
				fmt.Fprintf(os.Stderr, "Invalid output format %q (expected yaml, json, env, or keys)\n", format)
				return 1
			}
			outputFormat = format
			i++
		case args[i] == "-m" && i+1 < len(args):
			i++
			merges = append(merges, args[i])
			i++
		case args[i] == "-e" && i+1 < len(args):
			i++
			key, uri, ok := strings.Cut(args[i], "=")
			if !ok || key == "" || uri == "" {
				fmt.Fprintf(os.Stderr, "Invalid -e format: %q (expected KEY=uri)\n", args[i])
				return 1
			}
			explicit[key] = uri
			i++
		case args[i] == "-i" && i+1 < len(args):
			i++
			whitelist = append(whitelist, args[i])
			i++
		case strings.HasPrefix(args[i], "-"):
			fmt.Fprintf(os.Stderr, "Unknown flag: %s\n", args[i])
			return 1
		default:
			if positionalURI != "" {
				fmt.Fprintln(os.Stderr, "Usage: cloakenv show <entry-uri> [-o yaml | json | env | keys]")
				fmt.Fprintln(os.Stderr, "   or: cloakenv show -m <entry-uri> [-e KEY=uri ...] [-i KEY ...] [-o yaml | json | env | keys]")
				return 1
			}
			positionalURI = args[i]
			i++
		}
	}

	hasFlags := len(merges) > 0 || len(explicit) > 0 || len(whitelist) > 0
	if hasFlags && positionalURI != "" {
		merges = append([]string{positionalURI}, merges...)
		positionalURI = ""
	}
	if !hasFlags && positionalURI == "" {
		fmt.Fprintln(os.Stderr, "Usage: cloakenv show <entry-uri> [-o yaml | json | env | keys]")
		fmt.Fprintln(os.Stderr, "   or: cloakenv show -m <entry-uri> [-e KEY=uri ...] [-i KEY ...] [-o yaml | json | env | keys]")
		return 1
	}

	orch, err := engine.NewOrchestrator(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Config error: %v\n", err)
		return 1
	}
	ctx := context.Background()

	var entry provider.Entry

	if positionalURI != "" {
		entry, err = orch.GetEntry(ctx, positionalURI)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to retrieve entry: %v\n", err)
			return 1
		}
	} else {
		// Initialize a merged entry.
		entry = provider.Entry{
			Tags:       []string{},
			Attributes: make(map[string]any),
		}

		whitelistSet := make(map[string]bool)
		for _, k := range whitelist {
			whitelistSet[utils.FormatKey(k)] = true
		}
		hasWhitelist := len(whitelist) > 0

		// Resolve the entries concurrently, then merge them in order
		type loadedEntry struct {
			entry provider.Entry
			err   error
		}
		loadedMerges := make([]loadedEntry, len(merges))
		var mWg sync.WaitGroup
		for idx, mURI := range merges {
			mWg.Add(1)
			go func(i int, uri string) {
				defer mWg.Done()
				e, err := orch.GetEntry(ctx, uri)
				loadedMerges[i] = loadedEntry{entry: e, err: err}
			}(idx, mURI)
		}
		mWg.Wait()

		// Check if any merge failed
		for _, lm := range loadedMerges {
			if lm.err != nil {
				fmt.Fprintf(os.Stderr, "Failed to retrieve entry: %v\n", lm.err)
				return 1
			}
		}

		// Merge tags and attributes in order
		tagSet := make(map[string]bool)
		for _, lm := range loadedMerges {
			for _, tag := range lm.entry.Tags {
				tagSet[tag] = true
			}
			for k, v := range lm.entry.Attributes {
				kLower := strings.ToLower(k)
				if kLower == "title" || kLower == "tags" {
					continue
				}
				formattedKey := utils.FormatKey(k)
				if hasWhitelist && !whitelistSet[formattedKey] {
					continue
				}
				entry.Attributes[k] = v
			}
		}

		// Build the tags slice from tagSet
		var uniqueTags []string
		for tag := range tagSet {
			uniqueTags = append(uniqueTags, tag)
		}
		entry.Tags = uniqueTags

		// Resolve explicit -e mappings (highest priority, not subject to whitelist)
		if len(explicit) > 0 {
			type resolvedMapping struct {
				key string
				val string
				err error
			}
			resolvedList := make([]resolvedMapping, len(explicit))
			var eWg sync.WaitGroup
			idx := 0
			for k, uri := range explicit {
				eWg.Add(1)
				go func(i int, key, u string) {
					defer eWg.Done()
					val, err := orch.Resolve(ctx, u)
					resolvedList[i] = resolvedMapping{key: key, val: val, err: err}
				}(idx, k, uri)
				idx++
			}
			eWg.Wait()

			for _, rm := range resolvedList {
				if rm.err != nil {
					fmt.Fprintf(os.Stderr, "Failed to resolve mapping %s=%s: %v\n", rm.key, explicit[rm.key], rm.err)
					return 1
				}
				entry.Attributes[rm.key] = rm.val
			}
		}
	}

	// Format all keys in entry.Attributes by default
	formattedAttributes := make(map[string]any)
	for k, v := range entry.Attributes {
		kLower := strings.ToLower(k)
		if kLower == "title" || kLower == "tags" {
			continue
		}
		formattedAttributes[utils.FormatKey(k)] = v
	}
	entry.Attributes = formattedAttributes

	if outputFormat == "keys" {
		printKeysFormat(entry.Attributes)
		return 0
	}

	if outputFormat == "env" {
		printEnvFormat(entry.Attributes)
		return 0
	}

	asJSON := (outputFormat == "json")
	if err := utils.RenderOutput(entry.Attributes, asJSON, "entry"); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	return 0
}

func printEnvFormat(attributes map[string]any) {
	for k, v := range attributes {
		kLower := strings.ToLower(k)
		if kLower == "title" || kLower == "tags" {
			continue
		}
		strVal, _ := serializeEntryAttrValue(v)
		if shouldQuoteDotenvValue(strVal) {
			escaped := strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(strVal)
			fmt.Printf("%s=\"%s\"\n", k, escaped)
		} else {
			fmt.Printf("%s=%s\n", k, strVal)
		}
	}
}

func shouldQuoteDotenvValue(s string) bool {
	return strings.ContainsAny(s, " \n\r#\"")
}

func serializeEntryAttrValue(val any) (string, error) {
	switch v := val.(type) {
	case string:
		return v, nil
	case []any, map[string]any, map[any]any, []string:
		data, err := yaml.Marshal(v)
		if err != nil {
			return "", err
		}
		return strings.TrimSuffix(string(data), "\n"), nil
	default:
		return fmt.Sprintf("%v", v), nil
	}
}

func printKeysFormat(attributes map[string]any) {
	for k := range attributes {
		kLower := strings.ToLower(k)
		if kLower == "title" || kLower == "tags" {
			continue
		}
		fmt.Println(k)
	}
}
