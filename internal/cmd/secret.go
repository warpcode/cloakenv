package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/warpcode/cloakenv/internal/config"
	"github.com/warpcode/cloakenv/internal/engine"
	"github.com/warpcode/cloakenv/internal/provider"
	"github.com/warpcode/cloakenv/internal/utils"

	"golang.org/x/term"
)

// Cache handles routing for the "cloakenv cache" subcommands.
func Cache(args []string, cfg *config.Config) int {
	if utils.HasHelpFlag(args) && (len(args) < 1 || args[0] != "clear") {
		PrintCacheHelp()
		return 0
	}
	if len(args) < 1 || args[0] != "clear" {
		fmt.Fprintln(os.Stderr, "Usage: cloakenv cache clear")
		return 1
	}
	return cacheClear(args[1:], cfg)
}

// Get handles single value secret retrieval (raw to stdout, pipeable).
func Get(args []string, cfg *config.Config) int {
	if utils.HasHelpFlag(args) {
		PrintGetHelp()
		return 0
	}
	if len(args) != 1 || strings.HasPrefix(args[0], "-") {
		fmt.Fprintln(os.Stderr, "Usage: cloakenv get <uri>")
		return 1
	}

	orch, err := engine.NewOrchestrator(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Config error: %v\n", err)
		return 1
	}
	ctx := context.Background()

	uri := args[0]
	if !strings.Contains(uri, "${") && strings.Contains(uri, "://") {
		uri = "${" + uri + "}"
	}

	val, err := orch.Resolve(ctx, uri)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Resolution failed: %v\n", err)
		return 1
	}

	fmt.Print(val)
	return 0
}

// Set handles "cloakenv set <uri> [value] [--ttl <duration>]".
func Set(args []string, cfg *config.Config) int {
	if utils.HasHelpFlag(args) {
		PrintSetHelp()
		return 0
	}

	var posArgs []string
	var ttl time.Duration
	for i := 0; i < len(args); i++ {
		if args[i] == "--ttl" {
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, "Error: missing value for --ttl flag")
				return 1
			}
			var err error
			ttl, err = time.ParseDuration(args[i+1])
			if err != nil {
				fmt.Fprintf(os.Stderr, "Invalid TTL duration format: %v (examples: 5m, 1h)\n", err)
				return 1
			}
			i++
		} else {
			posArgs = append(posArgs, args[i])
		}
	}

	if len(posArgs) < 1 || len(posArgs) > 2 {
		fmt.Fprintln(os.Stderr, "Usage: cloakenv set <uri> [value] [--ttl <duration>]")
		return 1
	}

	uri := posArgs[0]
	var value string

	if len(posArgs) == 2 && posArgs[1] != "-" {
		value = posArgs[1]
	} else {
		var b []byte
		var err error
		if term.IsTerminal(int(os.Stdin.Fd())) {
			fmt.Fprint(os.Stderr, "Enter secret value: ")
			b, err = term.ReadPassword(int(os.Stdin.Fd()))
			fmt.Fprintln(os.Stderr)
		} else {
			b, err = io.ReadAll(os.Stdin)
		}

		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to read from stdin: %v\n", err)
			return 1
		}

		value = string(b)
		if strings.HasSuffix(value, "\r\n") {
			value = strings.TrimSuffix(value, "\r\n")
		} else if strings.HasSuffix(value, "\n") {
			value = strings.TrimSuffix(value, "\n")
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

// Delete handles "cloakenv delete <uri>".
func Delete(args []string, cfg *config.Config) int {
	if utils.HasHelpFlag(args) {
		PrintDeleteHelp()
		return 0
	}
	if len(args) != 1 || strings.HasPrefix(args[0], "-") {
		fmt.Fprintln(os.Stderr, "Usage: cloakenv delete <uri>")
		return 1
	}

	uri := args[0]

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

// cacheClear handles "cloakenv cache clear".
func cacheClear(args []string, cfg *config.Config) int {
	if utils.HasHelpFlag(args) {
		PrintCacheClearHelp()
		return 0
	}
	if len(args) != 0 {
		fmt.Fprintln(os.Stderr, "Usage: cloakenv cache clear")
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
