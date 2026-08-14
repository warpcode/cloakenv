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

type showOptions struct {
	merges        []string
	explicit      map[string]string
	whitelist     []string
	outputFormat  string
	positionalURI string
}

func parseShowFlags(args []string) (*showOptions, error) {
	opts := &showOptions{
		explicit:     make(map[string]string),
		outputFormat: "yaml",
	}

	i := 0
	for i < len(args) {
		switch {
		case (args[i] == "-o" || args[i] == "--output") && i+1 < len(args):
			i++
			format := args[i]
			if format != "yaml" && format != "json" && format != "env" && format != "keys" {
				return nil, fmt.Errorf("Invalid output format %q (expected yaml, json, env, or keys)", format)
			}
			opts.outputFormat = format
			i++
		case args[i] == "-m" && i+1 < len(args):
			i++
			opts.merges = append(opts.merges, args[i])
			i++
		case args[i] == "-e" && i+1 < len(args):
			i++
			key, uri, ok := strings.Cut(args[i], "=")
			if !ok || key == "" || uri == "" {
				return nil, fmt.Errorf("Invalid -e format: %q (expected KEY=uri)", args[i])
			}
			opts.explicit[key] = uri
			i++
		case args[i] == "-t" && i+1 < len(args):
			i++
			templatePath := args[i]
			envs, err := utils.ParseTemplateFile(templatePath)
			if err != nil {
				return nil, fmt.Errorf("Error parsing template file %s: %v", templatePath, err)
			}
			for k, v := range envs {
				opts.explicit[k] = v
			}
			i++
		case args[i] == "-i" && i+1 < len(args):
			i++
			opts.whitelist = append(opts.whitelist, args[i])
			i++
		case strings.HasPrefix(args[i], "-"):
			return nil, fmt.Errorf("Unknown flag: %s", args[i])
		default:
			if opts.positionalURI != "" {
				return nil, fmt.Errorf("Usage: cloakenv show <entry-uri> [-o yaml | json | env | keys]\n   or: cloakenv show -m <entry-uri> [-e KEY=uri ...] [-t template_path] [-i KEY ...] [-o yaml | json | env | keys]")
			}
			opts.positionalURI = args[i]
			i++
		}
	}

	hasFlags := len(opts.merges) > 0 || len(opts.explicit) > 0 || len(opts.whitelist) > 0
	if hasFlags && opts.positionalURI != "" {
		opts.merges = append([]string{opts.positionalURI}, opts.merges...)
		opts.positionalURI = ""
	}
	if !hasFlags && opts.positionalURI == "" {
		return nil, fmt.Errorf("Usage: cloakenv show <entry-uri> [-o yaml | json | env | keys]\n   or: cloakenv show -m <entry-uri> [-e KEY=uri ...] [-t template_path] [-i KEY ...] [-o yaml | json | env | keys]")
	}

	return opts, nil
}

func buildMergedEntry(ctx context.Context, orch *engine.Orchestrator, merges []string, whitelist []string, explicit map[string]string) (provider.Entry, error) {
	entry := provider.Entry{
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
			return provider.Entry{}, fmt.Errorf("Failed to retrieve entry: %v", lm.err)
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
				return provider.Entry{}, fmt.Errorf("Failed to resolve mapping %s=%s: %v", rm.key, explicit[rm.key], rm.err)
			}
			entry.Attributes[rm.key] = rm.val
		}
	}

	return entry, nil
}

// Show handles "cloakenv show <entry-uri> [--yaml | --json]"
func Show(args []string, cfg *config.Config) int {
	if utils.HasHelpFlag(args) {
		PrintShowHelp()
		return 0
	}

	opts, err := parseShowFlags(args)
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

	var entry provider.Entry

	if opts.positionalURI != "" {
		entry, err = orch.GetEntry(ctx, opts.positionalURI)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to retrieve entry: %v\n", err)
			return 1
		}
	} else {
		entry, err = buildMergedEntry(ctx, orch, opts.merges, opts.whitelist, opts.explicit)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
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

	if opts.outputFormat == "keys" {
		printKeysFormat(entry.Attributes)
		return 0
	}

	if opts.outputFormat == "env" {
		printEnvFormat(entry.Attributes)
		return 0
	}

	asJSON := (opts.outputFormat == "json")
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
