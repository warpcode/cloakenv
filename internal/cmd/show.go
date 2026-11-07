package cmd

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strings"

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
	keyFormat     string
	positionalURI string
	showMeta      bool
	raw           bool
	explain       bool
}

func parseShowFlags(args []string) (*showOptions, error) {
	opts := &showOptions{
		explicit:     make(map[string]string),
		outputFormat: "yaml",
	}

	usageMsg := "Usage: cloakenv show <entry-uri> [-f format] [-o yaml | json | env | keys] [--raw] [--explain] [--show-meta]\n   or: cloakenv show -m <entry-uri> [-e KEY=uri ...] [-t template_path] [-i KEY ...] [-f format] [-o yaml | json | env | keys] [--raw] [--show-meta]"

	parser := NewFlagParser()
	parser.Var([]string{"-f", "--key-format"}, true, "flag -f/--key-format requires an argument", func(name, val string) error {
		if val != "none" && val != "env" && val != "snake" && val != "kebab" {
			return fmt.Errorf("invalid key format %q (expected none, env, snake, or kebab)", val)
		}
		opts.keyFormat = val
		return nil
	})
	parser.Var([]string{"--raw"}, false, "", func(name, val string) error {
		opts.raw = true
		return nil
	})
	parser.Var([]string{"--explain"}, false, "", func(name, val string) error {
		opts.explain = true
		return nil
	})
	parser.Var([]string{"--show-meta"}, false, "", func(name, val string) error {
		opts.showMeta = true
		return nil
	})
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

	if opts.raw && opts.explain {
		return nil, fmt.Errorf("cannot use --raw and --explain together")
	}
	if opts.explain && (opts.outputFormat == "env" || opts.outputFormat == "keys") {
		return nil, fmt.Errorf("invalid output format %q for --explain (expected yaml or json)", opts.outputFormat)
	}
	if opts.explain && (len(opts.explicit) > 0 || len(opts.whitelist) > 0 || len(opts.merges) > 1) {
		return nil, fmt.Errorf("--explain can only be used with a single entry URI")
	}
	if opts.explain && opts.positionalURI == "" && len(opts.merges) == 1 {
		opts.positionalURI = opts.merges[0]
		opts.merges = nil
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

// Show handles "cloakenv show <entry-uri> [-m <entry-uri> ...] [-e KEY=uri ...] [-t template_path] [-i KEY ...] [-o yaml | json | env | keys] [--raw] [--explain]"
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

	if opts.explain {
		if opts.positionalURI == "" {
			fmt.Fprintln(os.Stderr, "--explain requires a single entry URI")
			return 1
		}
		if opts.keyFormat != "" {
			if cfg == nil {
				cfg = &config.Config{}
			}
			cfg.KeyFormatOverride = opts.keyFormat
		}
		orch, err := engine.NewOrchestrator(cfg)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Config error: %v\n", err)
			return 1
		}
		ctx := context.Background()
		explanation, err := orch.ExplainEntry(ctx, opts.positionalURI)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to retrieve entry: %v\n", err)
			return 1
		}
		asJSON := (opts.outputFormat == "json")
		if err := utils.RenderOutput(explanation, asJSON, "pipeline"); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		return 0
	}

	if opts.keyFormat != "" && !opts.raw {
		if cfg == nil {
			cfg = &config.Config{}
		}
		cfg.KeyFormatOverride = opts.keyFormat
	}

	orch, err := engine.NewOrchestrator(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Config error: %v\n", err)
		return 1
	}
	ctx := context.Background()

	var entry provider.Entry

	if opts.positionalURI != "" {
		if opts.raw {
			entry, err = orch.GetRawEntry(ctx, opts.positionalURI)
		} else {
			entry, err = orch.GetEntry(ctx, opts.positionalURI)
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to retrieve entry: %v\n", err)
			return 1
		}
	} else {
		mergedCtx := ctx
		if opts.raw {
			mergedCtx = provider.WithRawKeys(ctx)
		}
		entry, err = orch.BuildMergedEntry(mergedCtx, engine.MergedEntryConfig{
			Merges:    opts.merges,
			Whitelist: opts.whitelist,
			Explicit:  opts.explicit,
		})
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
	}

	if !opts.raw {
		// Strip internal metadata keys from entry.Attributes
		formattedAttributes := make(map[string]any)
		for k, v := range entry.Attributes {
			if strings.EqualFold(k, "title") {
				continue
			}
			formattedAttributes[k] = v
		}
		entry.Attributes = formattedAttributes
	}

	if opts.outputFormat == "keys" {
		printKeysFormat(entry.Attributes)
		return 0
	}

	if opts.outputFormat == "env" {
		printEnvFormat(entry.Attributes)
		return 0
	}

	out := make(map[string]any)
	for k, v := range entry.Attributes {
		out[k] = v
	}
	if entry.UUID != "" {
		if _, exists := out["uuid"]; !exists {
			out["uuid"] = entry.UUID
		}
	}
	if entry.Group != nil {
		if _, exists := out["group"]; !exists {
			out["group"] = entry.Group
		}
	}

	var showMetaTags bool
	var showTopTags bool
	for _, w := range opts.whitelist {
		switch strings.ToLower(w) {
		case "tags":
			showTopTags = true
		case "_meta_", "_meta_.tags", "_meta_:tags":
			showMetaTags = true
		}
	}

	if (opts.showMeta || showMetaTags) && (opts.outputFormat == "yaml" || opts.outputFormat == "json") {
		primaryURI := opts.positionalURI
		if primaryURI == "" && len(opts.merges) > 0 {
			primaryURI = opts.merges[0]
		}
		metaMap := make(map[string]any)
		if primaryURI != "" {
			if scheme, _, err := utils.ParseURI(primaryURI); err == nil {
				if cfg != nil && cfg.Vaults != nil {
					if vCfg, exists := cfg.Vaults[scheme]; exists {
						metaMap["vault"] = scheme
						if vCfg.Provider != "" {
							metaMap["provider"] = vCfg.Provider
						}
					} else {
						metaMap["provider"] = scheme
					}
				} else {
					metaMap["provider"] = scheme
				}
			}
		}
		if len(entry.Tags) > 0 {
			metaMap["tags"] = entry.Tags
		}
		if len(metaMap) > 0 {
			out["_meta_"] = metaMap
		}
	}

	if showTopTags && !opts.showMeta {
		if _, exists := out["tags"]; !exists && len(entry.Tags) > 0 {
			out["tags"] = entry.Tags
		}
	}

	asJSON := (opts.outputFormat == "json")
	if err := utils.RenderOutput(out, asJSON, "entry"); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	return 0
}

func printEnvFormat(attributes map[string]any) {
	keys := make([]string, 0, len(attributes))
	for k := range attributes {
		if strings.EqualFold(k, "title") || strings.EqualFold(k, "tags") {
			continue
		}
		keys = append(keys, k)
	}
	slices.Sort(keys)

	for _, k := range keys {
		v := attributes[k]
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
	keys := make([]string, 0, len(attributes))
	for k := range attributes {
		if strings.EqualFold(k, "title") || strings.EqualFold(k, "tags") {
			continue
		}
		keys = append(keys, k)
	}
	slices.Sort(keys)

	for _, k := range keys {
		fmt.Println(k)
	}
}
