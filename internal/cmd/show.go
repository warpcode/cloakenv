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

	usageMsg := "Usage: cloakenv show <entry-uri> [-o yaml | json | env | keys]\n   or: cloakenv show -m <entry-uri> [-e KEY=uri ...] [-t template_path] [-i KEY ...] [-o yaml | json | env | keys]"

	parser := NewFlagParser()
	parser.Var([]string{"-o", "--output"}, true, "", func(name, val string) error {
		if val != "yaml" && val != "json" && val != "env" && val != "keys" {
			return fmt.Errorf("invalid output format %q (expected yaml, json, env, or keys)", val)
		}
		opts.outputFormat = val
		return nil
	})
	parser.StringSlice([]string{"-m"}, &opts.merges, "")
	parser.Var([]string{"-e"}, true, "", func(name, val string) error {
		key, uri, ok := strings.Cut(val, "=")
		if !ok || key == "" || uri == "" {
			return fmt.Errorf("invalid -e format: %q (expected KEY=uri)", val)
		}
		opts.explicit[key] = uri
		return nil
	})
	parser.Var([]string{"-t"}, true, "", func(name, val string) error {
		envs, err := utils.ParseTemplateFile(val)
		if err != nil {
			return fmt.Errorf("error parsing template file %s: %w", val, err)
		}
		for k, v := range envs {
			opts.explicit[k] = v
		}
		return nil
	})
	parser.StringSlice([]string{"-i"}, &opts.whitelist, "")
	parser.PositionalHandler = func(arg string) error {
		if opts.positionalURI != "" {
			return fmt.Errorf("%s", usageMsg)
		}
		opts.positionalURI = arg
		return nil
	}

	if _, err := parser.Parse(args); err != nil {
		return nil, err
	}

	hasFlags := len(opts.merges) > 0 || len(opts.explicit) > 0 || len(opts.whitelist) > 0
	if hasFlags && opts.positionalURI != "" {
		opts.merges = append([]string{opts.positionalURI}, opts.merges...)
		opts.positionalURI = ""
	}
	if !hasFlags && opts.positionalURI == "" {
		return nil, fmt.Errorf("%s", usageMsg)
	}

	return opts, nil
}

func buildMergedEntry(ctx context.Context, orch *engine.Orchestrator, merges []string, whitelist []string, explicit map[string]string) (provider.Entry, error) {
	entries, err := resolveMergedEntries(ctx, orch, merges)
	if err != nil {
		return provider.Entry{}, err
	}

	tags, attributes := mergeTagsAndAttributes(entries, whitelist)

	explicitResolved, err := resolveExplicitMappings(ctx, orch, explicit)
	if err != nil {
		return provider.Entry{}, err
	}

	for k, v := range explicitResolved {
		attributes[k] = v
	}

	if tags == nil {
		tags = []string{}
	}

	return provider.Entry{
		Tags:       tags,
		Attributes: attributes,
	}, nil
}

func resolveMergedEntries(ctx context.Context, orch *engine.Orchestrator, merges []string) ([]provider.Entry, error) {
	if len(merges) == 0 {
		return nil, nil
	}

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

	entries := make([]provider.Entry, len(merges))
	for i, lm := range loadedMerges {
		if lm.err != nil {
			return nil, fmt.Errorf("failed to retrieve entry: %w", lm.err)
		}
		entries[i] = lm.entry
	}
	return entries, nil
}

func mergeTagsAndAttributes(entries []provider.Entry, whitelist []string) ([]string, map[string]any) {
	whitelistSet := make(map[string]bool)
	for _, k := range whitelist {
		whitelistSet[utils.FormatKey(k)] = true
	}
	hasWhitelist := len(whitelist) > 0

	tagSet := make(map[string]bool)
	attributes := make(map[string]any)

	for _, e := range entries {
		for _, tag := range e.Tags {
			tagSet[tag] = true
		}
		for k, v := range e.Attributes {
			kLower := strings.ToLower(k)
			if kLower == "title" || kLower == "tags" {
				continue
			}
			formattedKey := utils.FormatKey(k)
			if hasWhitelist && !whitelistSet[formattedKey] {
				continue
			}
			attributes[k] = v
		}
	}

	var uniqueTags []string
	for tag := range tagSet {
		uniqueTags = append(uniqueTags, tag)
	}

	return uniqueTags, attributes
}

func resolveExplicitMappings(ctx context.Context, orch *engine.Orchestrator, explicit map[string]string) (map[string]string, error) {
	if len(explicit) == 0 {
		return nil, nil
	}

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

	results := make(map[string]string, len(explicit))
	for _, rm := range resolvedList {
		if rm.err != nil {
			return nil, fmt.Errorf("failed to resolve mapping %s=%s: %w", rm.key, explicit[rm.key], rm.err)
		}
		results[rm.key] = rm.val
	}
	return results, nil
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
		strVal, _ := utils.SerializeAttrValue(v)
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

func printKeysFormat(attributes map[string]any) {
	for k := range attributes {
		kLower := strings.ToLower(k)
		if kLower == "title" || kLower == "tags" {
			continue
		}
		fmt.Println(k)
	}
}
