package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"cloakenv/internal/config"
	"cloakenv/internal/engine"
	"cloakenv/internal/provider"

	"gopkg.in/yaml.v3"
)

var customConfigPath string

func main() {
	// Parse global flags first (e.g. -c <config_path>)
	args := os.Args
	var parsedArgs []string
	parsedArgs = append(parsedArgs, args[0])

	i := 1
	for i < len(args) {
		if args[i] == "--" {
			parsedArgs = append(parsedArgs, args[i:]...)
			break
		}
		if args[i] == "-c" {
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, "Error: -c flag requires a config path argument")
				os.Exit(1)
			}
			customConfigPath = args[i+1]
			i += 2
			continue
		}
		parsedArgs = append(parsedArgs, args[i])
		i++
	}
	os.Args = parsedArgs

	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	if os.Args[1] == "--help" || os.Args[1] == "-h" {
		printUsageStdout()
		os.Exit(0)
	}

	switch os.Args[1] {
	case "run":
		os.Exit(cmdRun(os.Args[2:]))
	case "get":
		os.Exit(cmdGet(os.Args[2:]))
	case "set":
		os.Exit(cmdSet(os.Args[2:]))
	case "delete":
		os.Exit(cmdDelete(os.Args[2:]))
	case "cache":
		if hasHelpFlag(os.Args[2:]) && (len(os.Args) < 3 || os.Args[2] != "clear") {
			printCacheHelp()
			os.Exit(0)
		}
		if len(os.Args) < 3 || os.Args[2] != "clear" {
			fmt.Fprintln(os.Stderr, "Usage: cloakenv cache clear")
			os.Exit(1)
		}
		os.Exit(cmdCacheClear(os.Args[3:]))
	case "show":
		os.Exit(cmdShow(os.Args[2:]))
	case "search":
		os.Exit(cmdSearch(os.Args[2:]))
	case "auth":
		if hasHelpFlag(os.Args[2:]) && (len(os.Args) < 3 || (os.Args[2] != "login" && os.Args[2] != "forget" && os.Args[2] != "status")) {
			printAuthHelp()
			os.Exit(0)
		}
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "Usage: cloakenv auth <login|forget|status> [vault]")
			os.Exit(1)
		}
		switch os.Args[2] {
		case "login":
			os.Exit(cmdAuthLogin(os.Args[3:]))
		case "forget":
			os.Exit(cmdAuthForget(os.Args[3:]))
		case "status":
			os.Exit(cmdAuthStatus(os.Args[3:]))
		default:
			fmt.Fprintf(os.Stderr, "Unknown auth subcommand: %s\n", os.Args[2])
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}
}

// cmdRun handles "cloakenv run [-e KEY=uri ...] [-m entry-uri] [-i KEY ...] -- <cmd> [args]".
func cmdRun(args []string) int {
	if hasHelpFlag(args) {
		printRunHelp()
		return 0
	}
	var (
		explicitEnv   = make(map[string]string)
		merges        []string
		whitelist     []string
		cmdArgs       []string
	)

	// Parse flags manually to support repeated -e, -m, and -i flags + -- separator
	i := 0
	for i < len(args) {
		switch {
		case args[i] == "--":
			cmdArgs = args[i+1:]
			i = len(args) // break out of loop
		case args[i] == "-e" && i+1 < len(args):
			i++
			key, uri, ok := strings.Cut(args[i], "=")
			if !ok || key == "" || uri == "" {
				fmt.Fprintf(os.Stderr, "Invalid -e format: %q (expected KEY=uri)\n", args[i])
				return 1
			}
			explicitEnv[key] = uri
			i++
		case args[i] == "-m" && i+1 < len(args):
			i++
			merges = append(merges, args[i])
			i++
		case args[i] == "-i" && i+1 < len(args):
			i++
			whitelist = append(whitelist, args[i])
			i++
		default:
			// Treat remaining args as the command if no -- separator was used
			cmdArgs = args[i:]
			i = len(args)
		}
	}

	if len(cmdArgs) == 0 {
		fmt.Fprintln(os.Stderr, "No command specified. Usage: cloakenv run [flags] -- <command> [args]")
		return 1
	}

	// Load config and build orchestrator
	cfg, err := loadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Config error: %v\n", err)
		return 1
	}

	orch, err := engine.NewOrchestrator(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Config error: %v\n", err)
		return 1
	}
	ctx := context.Background()

	// Build the environment block (pass-through if no mappings specified)
	env, err := orch.BuildEnv(ctx, explicitEnv, merges, whitelist)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Secret resolution failed: %v\n", err)
		return 1
	}

	// Execute the wrapped command
	cmd := exec.Command(cmdArgs[0], cmdArgs[1:]...)
	cmd.Env = env
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			if status, ok := exitErr.Sys().(syscall.WaitStatus); ok {
				return status.ExitStatus()
			}
		}
		fmt.Fprintf(os.Stderr, "Execution failed: %v\n", err)
		return 1
	}

	return 0
}

// cmdGet handles single value secret retrieval (raw to stdout, pipeable).
func cmdGet(args []string) int {
	if hasHelpFlag(args) {
		printGetHelp()
		return 0
	}
	if len(args) != 1 || strings.HasPrefix(args[0], "-") {
		fmt.Fprintln(os.Stderr, "Usage: cloakenv get <uri>")
		return 1
	}

	cfg, err := loadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Config error: %v\n", err)
		return 1
	}

	orch, err := engine.NewOrchestrator(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Config error: %v\n", err)
		return 1
	}
	ctx := context.Background()

	val, err := orch.Resolve(ctx, args[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "Resolution failed: %v\n", err)
		return 1
	}

	fmt.Print(val)
	return 0
}

// cmdList handles multiple secret retrieval and outputs as key=value or JSON.
// cmdSet handles "cloakenv set <uri> <value> [--ttl <duration>]".
func cmdSet(args []string) int {
	if hasHelpFlag(args) {
		printSetHelp()
		return 0
	}
	if len(args) != 2 && len(args) != 4 {
		fmt.Fprintln(os.Stderr, "Usage: cloakenv set <uri> <value> [--ttl <duration>]")
		return 1
	}

	uri := args[0]
	value := args[1]
	var ttl time.Duration

	if len(args) == 4 {
		if args[2] != "--ttl" {
			fmt.Fprintf(os.Stderr, "Unknown flag: %s (expected --ttl)\n", args[2])
			return 1
		}
		var err error
		ttl, err = time.ParseDuration(args[3])
		if err != nil {
			fmt.Fprintf(os.Stderr, "Invalid TTL duration format: %v (examples: 5m, 1h)\n", err)
			return 1
		}
	}

	// Provider-specific validation: --ttl is cache:// only
	parts := strings.SplitN(uri, "://", 2)
	if len(parts) != 2 {
		fmt.Fprintf(os.Stderr, "Invalid URI format: %q (expected scheme://location)\n", uri)
		return 1
	}
	scheme := parts[0]
	if scheme != "cache" && ttl > 0 {
		fmt.Fprintf(os.Stderr, "Error: flag --ttl is only supported by the 'cache' provider, not %q\n", scheme)
		return 1
	}

	cfg, err := loadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Config error: %v\n", err)
		return 1
	}

	// Fallback to default_ttl from global config if CLI flag is omitted
	if scheme == "cache" && ttl == 0 && cfg.Cache.DefaultTTL != "" {
		var err error
		ttl, err = time.ParseDuration(cfg.Cache.DefaultTTL)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Invalid default_ttl in global config: %v (examples: 5m, 1h)\n", err)
			return 1
		}
	}

	orch, err := engine.NewOrchestrator(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Config error: %v\n", err)
		return 1
	}
	ctx := context.Background()
	if ttl > 0 {
		ctx = context.WithValue(ctx, provider.ContextKeyTTL, ttl)
	}

	if err := orch.Write(ctx, uri, value); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to write secret: %v\n", err)
		return 1
	}

	return 0
}

// cmdDelete handles "cloakenv delete <uri>".
func cmdDelete(args []string) int {
	if hasHelpFlag(args) {
		printDeleteHelp()
		return 0
	}
	if len(args) != 1 || strings.HasPrefix(args[0], "-") {
		fmt.Fprintln(os.Stderr, "Usage: cloakenv delete <uri>")
		return 1
	}

	uri := args[0]

	cfg, err := loadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Config error: %v\n", err)
		return 1
	}

	orch, err := engine.NewOrchestrator(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Config error: %v\n", err)
		return 1
	}
	ctx := context.Background()

	if err := orch.Delete(ctx, uri); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to delete secret: %v\n", err)
		return 1
	}

	fmt.Printf("Secret successfully deleted from %s\n", uri)
	return 0
}

// cmdCacheClear handles "cloakenv cache clear".
func cmdCacheClear(args []string) int {
	if hasHelpFlag(args) {
		printCacheClearHelp()
		return 0
	}
	if len(args) != 0 {
		fmt.Fprintln(os.Stderr, "Usage: cloakenv cache clear")
		return 1
	}

	cfg, err := loadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Config error: %v\n", err)
		return 1
	}

	orch, err := engine.NewOrchestrator(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Config error: %v\n", err)
		return 1
	}
	ctx := context.Background()

	if err := orch.ClearCache(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to clear cache: %v\n", err)
		return 1
	}

	fmt.Println("Cache cleared successfully.")
	return 0
}

// cmdAuthLogin handles "cloakenv auth login <scheme>".
func cmdAuthLogin(args []string) int {
	if hasHelpFlag(args) {
		printAuthLoginHelp()
		return 0
	}
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "Usage: cloakenv auth login <scheme>")
		return 1
	}
	scheme := args[0]

	cfg, err := loadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Config error: %v\n", err)
		return 1
	}

	orch, err := engine.NewOrchestrator(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Config error: %v\n", err)
		return 1
	}
	ctx := context.Background()
	if err := orch.Login(ctx, scheme); err != nil {
		fmt.Fprintf(os.Stderr, "Authentication failed: %v\n", err)
		return 1
	}

	fmt.Printf("Successfully authenticated and saved credentials for scheme %q\n", scheme)
	return 0
}

// cmdAuthForget handles "cloakenv auth forget <scheme>".
func cmdAuthForget(args []string) int {
	if hasHelpFlag(args) {
		printAuthForgetHelp()
		return 0
	}
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "Usage: cloakenv auth forget <scheme>")
		return 1
	}
	scheme := args[0]

	cfg, err := loadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Config error: %v\n", err)
		return 1
	}

	orch, err := engine.NewOrchestrator(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Config error: %v\n", err)
		return 1
	}
	ctx := context.Background()
	if err := orch.Forget(ctx, scheme); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to clear credentials: %v\n", err)
		return 1
	}

	fmt.Printf("Successfully cleared credentials for scheme %q\n", scheme)
	return 0
}

// loadConfig reads the global config file.
func loadConfig() (*config.Config, error) {
	path := customConfigPath
	if path == "" {
		var err error
		path, err = config.DefaultConfigPath()
		if err != nil {
			return nil, err
		}
	}
	return config.Load(path)
}

// cmdShow handles "cloakenv show <entry-uri> [--yaml | --json]"
func cmdShow(args []string) int {
	if hasHelpFlag(args) {
		printShowHelp()
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
			if format != "yaml" && format != "json" && format != "env" {
				fmt.Fprintf(os.Stderr, "Invalid output format %q (expected yaml, json, or env)\n", format)
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
				fmt.Fprintln(os.Stderr, "Usage: cloakenv show <entry-uri> [-o yaml | json | env]")
				fmt.Fprintln(os.Stderr, "   or: cloakenv show -m <entry-uri> [-e KEY=uri ...] [-i KEY ...] [-o yaml | json | env]")
				return 1
			}
			positionalURI = args[i]
			i++
		}
	}

	hasFlags := len(merges) > 0 || len(explicit) > 0 || len(whitelist) > 0
	if hasFlags && positionalURI != "" {
		fmt.Fprintln(os.Stderr, "Error: cannot mix positional entry URI with -m, -e, or -i flags.")
		return 1
	}
	if !hasFlags && positionalURI == "" {
		fmt.Fprintln(os.Stderr, "Usage: cloakenv show <entry-uri> [-o yaml | json | env]")
		fmt.Fprintln(os.Stderr, "   or: cloakenv show -m <entry-uri> [-e KEY=uri ...] [-i KEY ...] [-o yaml | json | env]")
		return 1
	}

	cfg, err := loadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Config error: %v\n", err)
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
			Title:      "Merged Entry",
			Tags:       []string{},
			Attributes: make(map[string]any),
		}

		whitelistSet := make(map[string]bool)
		for _, k := range whitelist {
			whitelistSet[k] = true
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
				if hasWhitelist && !whitelistSet[k] {
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

	if outputFormat == "env" {
		printEnvFormat(entry.Attributes)
		return 0
	}

	asJSON := (outputFormat == "json")
	if err := renderOutput(entry.Attributes, asJSON, "entry"); err != nil {
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

// cmdSearch handles "cloakenv search [query] [--vault <vault> ...] [-i KEY ...] [--json | --yaml]"
func cmdSearch(args []string) int {
	if hasHelpFlag(args) {
		printSearchHelp()
		return 0
	}

	query, repoScopes, selectedKeys, outputFormat, err := parseSearchArgs(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	cfg, err := loadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Config error: %v\n", err)
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
	if err := renderOutput(flatResults, asJSON, "results"); err != nil {
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

func flattenEntry(entry provider.Entry) map[string]any {
	flatEntry := make(map[string]any)
	flatEntry["title"] = entry.Title
	flatEntry["tags"] = entry.Tags
	for k, v := range entry.Attributes {
		kLower := strings.ToLower(k)
		if kLower == "title" || kLower == "tags" {
			continue
		}
		flatEntry[k] = v
	}
	return flatEntry
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
							flatRes[k] = v
							found = true
							break
						}
					}
					if !found {
						if v, ok := r.Entry.Attributes[field]; ok {
							flatRes[field] = v
						} else {
							flatRes[field] = nil
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
				flatRes[k] = v
			}
		}
		flatResults[i] = flatRes
	}
	return flatResults
}

func renderOutput(data any, asJSON bool, errorLabel string) error {
	if asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(data); err != nil {
			return fmt.Errorf("failed to serialize %s to JSON: %w", errorLabel, err)
		}
	} else {
		enc := yaml.NewEncoder(os.Stdout)
		enc.SetIndent(2)
		if err := enc.Encode(data); err != nil {
			return fmt.Errorf("failed to serialize %s to YAML: %w", errorLabel, err)
		}
	}
	return nil
}

func printUsage() {
	fmt.Fprintln(os.Stderr, `cloakenv — pluggable secret orchestrator & runtime injector

Usage:
  cloakenv [-c config_path] run [-e KEY=uri ...] [-m entry-uri] [-i KEY ...] -- <command> [args]
  cloakenv [-c config_path] get <uri>
  cloakenv [-c config_path] set <uri> <value> [--ttl <duration>]
  cloakenv [-c config_path] delete <uri>
  cloakenv [-c config_path] cache clear
  cloakenv [-c config_path] show <entry-uri> [args]
  cloakenv [-c config_path] search [query] [args]
  cloakenv [-c config_path] auth <login|forget|status> [vault]

Commands:
  run     Wrap a binary with injected environment variables
  get     Retrieve and print a single secret value raw to stdout (no trailing newline)
  set     Store a secret value at a writable URI (keyring://, cache://)
  delete  Remove a secret from a writable URI (keyring://, cache://)
  cache   Manage local encrypted cache (subcommand: clear)
  show    Retrieve and display a structured entry
  search  Search for structured entries
  auth    Manage vault credentials and status (subcommands: login, forget, status)

Flags:
  -c config_path  Custom configuration file path (global flag)
  -e KEY=uri      Map an environment variable to a secret URI (repeatable)
  -m entry-uri    Merge all attributes from an entry into the environment (repeatable)
  -i KEY          Filter/whitelist keys/variables (repeatable)
  -o, --output    Output format: plain, json, yaml, env (depends on command)
  --vault vault   Scope search to a specific vault (repeatable)
  --ttl duration  Expiration duration for cache entries (e.g. 5m, 1h, set only)

URI schemes:
  keyring://service/account    Built-in: OS keyring (macOS Keychain, Linux D-Bus, Windows Credential Manager)
  env://VARIABLE_NAME          Built-in: read from current process environment (read-only)
  cache://KEY                  Built-in: local file cache (AES-GCM encrypted, key in OS keyring)
  search://query/attribute     Built-in: resolve dynamically matched credentials
  <vault>://Path/To/Entry      Config-defined: resolved via ~/.config/cloakenv/config.yaml`)
}

func printUsageStdout() {
	fmt.Fprintln(os.Stdout, `cloakenv — pluggable secret orchestrator & runtime injector

Usage:
  cloakenv [-c config_path] run [-e KEY=uri ...] [-m entry-uri] [-i KEY ...] -- <command> [args]
  cloakenv [-c config_path] get <uri>
  cloakenv [-c config_path] set <uri> <value> [--ttl <duration>]
  cloakenv [-c config_path] delete <uri>
  cloakenv [-c config_path] cache clear
  cloakenv [-c config_path] show <entry-uri> [args]
  cloakenv [-c config_path] search [query] [args]
  cloakenv [-c config_path] auth <login|forget|status> [vault]

Commands:
  run     Wrap a binary with injected environment variables
  get     Retrieve and print a single secret value raw to stdout (no trailing newline)
  set     Store a secret value at a writable URI (keyring://, cache://)
  delete  Remove a secret from a writable URI (keyring://, cache://)
  cache   Manage local encrypted cache (subcommand: clear)
  show    Retrieve and display a structured entry
  search  Search for structured entries
  auth    Manage vault credentials and status (subcommands: login, forget, status)

Flags:
  -c config_path  Custom configuration file path (global flag)
  -e KEY=uri      Map an environment variable to a secret URI (repeatable)
  -m entry-uri    Merge all attributes from an entry into the environment (repeatable)
  -i KEY          Filter/whitelist keys/variables (repeatable)
  -o, --output    Output format: plain, json, yaml, env (depends on command)
  --vault vault   Scope search to a specific vault (repeatable)
  --ttl duration  Expiration duration for cache entries (e.g. 5m, 1h, set only)

URI schemes:
  keyring://service/account    Built-in: OS keyring (macOS Keychain, Linux D-Bus, Windows Credential Manager)
  env://VARIABLE_NAME          Built-in: read from current process environment (read-only)
  cache://KEY                  Built-in: local file cache (AES-GCM encrypted, key in OS keyring)
  search://query/attribute     Built-in: resolve dynamically matched credentials
  <vault>://Path/To/Entry      Config-defined: resolved via ~/.config/cloakenv/config.yaml`)
}

func hasHelpFlag(args []string) bool {
	for _, arg := range args {
		if arg == "--" {
			break
		}
		if arg == "--help" || arg == "-h" {
			return true
		}
	}
	return false
}

func printRunHelp() {
	fmt.Fprintln(os.Stdout, `Usage:
  cloakenv run [-e KEY=uri ...] [-m entry-uri] [-i KEY ...] -- <command> [args]

Description:
  Wrap a binary execution, resolving and injecting secret environment variables.
  If no -- separator is used, any remaining arguments are treated as the command.

Flags:
  -e KEY=uri      Map an environment variable to a secret URI (repeatable)
  -m entry-uri    Merge all attributes from an entry into the environment (repeatable)
  -i KEY          Whitelist filter key (filters only merged -m keys; repeatable)`)
}

func printGetHelp() {
	fmt.Fprintln(os.Stdout, `Usage:
  cloakenv get <uri>

Description:
  Retrieve and print a single secret value raw to stdout (no trailing newline).

Arguments:
  <uri>           The secret URI to retrieve (e.g., keyring://service/account, env://VAR)`)
}

func printSetHelp() {
	fmt.Fprintln(os.Stdout, `Usage:
  cloakenv set <uri> <value> [--ttl <duration>]

Description:
  Store a secret value at a writable URI. Currently only 'keyring://' and 'cache://' schemes are writable.

Arguments:
  <uri>           The secret URI where the value will be stored
  <value>         The secret value to write

Flags:
  --ttl duration  Expiration duration for cache entries (e.g. 5m, 1h).
                  Only supported by the 'cache' provider.`)
}

func printDeleteHelp() {
	fmt.Fprintln(os.Stdout, `Usage:
  cloakenv delete <uri>

Description:
  Remove a secret from a writable URI.

Arguments:
  <uri>           The secret URI to delete from (e.g., keyring://service/account, cache://KEY)`)
}

func printCacheHelp() {
	fmt.Fprintln(os.Stdout, `Usage:
  cloakenv cache clear

Description:
  Manage local encrypted cache.

Subcommands:
  clear           Clear all entries in the local encrypted cache`)
}

func printCacheClearHelp() {
	fmt.Fprintln(os.Stdout, `Usage:
  cloakenv cache clear

Description:
  Clear all entries in the local encrypted cache.`)
}

func printShowHelp() {
	fmt.Fprintln(os.Stdout, `Usage:
  cloakenv show <entry-uri> [-o yaml | json | env]
  cloakenv show -m <entry-uri> [-e KEY=uri ...] [-i KEY ...] [-o yaml | json | env]

Description:
  Retrieve and display a structured entry, or merge multiple entries/explicit mapping values.

Arguments:
  <entry-uri>     The structured entry URI to retrieve

Flags:
  -m <entry-uri>  Merge entry attributes (can be specified multiple times)
  -e KEY=uri      Explicit environment/key override (can be specified multiple times)
  -i KEY          Whitelist filter key (filters only merged -m keys; repeatable)
  -o, --output    Output format: yaml, json, or env (default: yaml)`)
}

func printSearchHelp() {
	fmt.Fprintln(os.Stdout, `Usage:
  cloakenv search [query] [--vault <vault> ...] [-i KEY ...] [-o yaml | json]

Description:
  Search for structured entries matching the query.

Arguments:
  [query]         Optional query string to filter entries by title, tags, or fields

Flags:
  --vault vault   Scope search to a specific vault (repeatable)
  -i KEY          Select output fields/keys to return (repeatable)
  -o, --output    Output format: yaml or json (default: yaml)`)
}

func printAuthHelp() {
	fmt.Fprintln(os.Stdout, `Usage:
  cloakenv auth <login|forget|status> [vault]

Description:
  Manage vault authentication and status.

Subcommands:
  login           Authenticate and save credentials for a vault
  forget          Clear credentials for a vault
  status          Check if configured vaults are active and accessible`)
}

func printAuthLoginHelp() {
	fmt.Fprintln(os.Stdout, `Usage:
  cloakenv auth login <vault>

Description:
  Authenticate and save credentials for a vault.

Arguments:
  <vault>         The name of the vault (e.g., work, home)`)
}

func printAuthForgetHelp() {
	fmt.Fprintln(os.Stdout, `Usage:
  cloakenv auth forget <vault>

Description:
  Clear credentials for a vault.

Arguments:
  <vault>         The name of the vault (e.g., work, home)`)
}

func printAuthStatusHelp() {
	fmt.Fprintln(os.Stdout, `Usage:
  cloakenv auth status [vault]

Description:
  Check if configured vaults are active and accessible.

Arguments:
  [vault]         Optional name of the vault to check`)
}

// cmdAuthStatus handles "cloakenv auth status [vault]".
func cmdAuthStatus(args []string) int {
	if hasHelpFlag(args) {
		printAuthStatusHelp()
		return 0
	}

	cfg, err := loadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Config error: %v\n", err)
		return 1
	}

	orch, err := engine.NewOrchestrator(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Config error: %v\n", err)
		return 1
	}
	ctx := context.Background()

	if len(args) > 0 {
		vaultName := args[0]
		err := orch.CheckAccess(ctx, vaultName)
		if err != nil {
			fmt.Printf("%s: ERROR: %v\n", vaultName, err)
			return 1
		}
		fmt.Printf("%s: ACTIVE\n", vaultName)
		return 0
	}

	if len(cfg.Vaults) == 0 {
		fmt.Println("No vaults configured.")
		return 0
	}

	hasError := false
	for vaultName := range cfg.Vaults {
		err := orch.CheckAccess(ctx, vaultName)
		if err != nil {
			fmt.Printf("%s: ERROR: %v\n", vaultName, err)
			hasError = true
		} else {
			fmt.Printf("%s: ACTIVE\n", vaultName)
		}
	}

	if hasError {
		return 1
	}
	return 0
}
