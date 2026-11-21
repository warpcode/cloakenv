package cmd

import (
	"fmt"
	"os"

	"github.com/warpcode/cloakenv/internal/config"
	"github.com/warpcode/cloakenv/internal/engine"
	"github.com/warpcode/cloakenv/internal/utils"
)

// Cache handles routing for the "cloakenv cache <clear|prune>" subcommands.
func Cache(args []string, cfg *config.Config) int {
	if len(args) > 0 && args[0] == "prune" {
		return cachePrune(args[1:], cfg)
	}
	if len(args) > 0 && args[0] == "clear" {
		return cacheClear(args[1:], cfg)
	}
	if utils.HasHelpFlag(args) {
		PrintCacheHelp()
		return 0
	}
	fmt.Fprintln(os.Stderr, "Usage: cloakenv cache <clear|prune>")
	return 1
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
	defer func() { _ = orch.Close() }()
	ctx, stop := signalContext()
	defer stop()

	if err := orch.ClearCache(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to clear cache: %v\n", err)
		return exitCodeForContext(ctx, 1)
	}

	fmt.Println("Cache cleared successfully.")
	return 0
}

// formatBytes converts a byte count into a human-readable string.
func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	units := []string{"KB", "MB", "GB", "TB"}
	for n := b / unit; n >= unit && exp < len(units)-1; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.2f %s", float64(b)/float64(div), units[exp])
}

// cachePrune handles "cloakenv cache prune".
func cachePrune(args []string, cfg *config.Config) int {
	if utils.HasHelpFlag(args) {
		PrintCachePruneHelp()
		return 0
	}
	if len(args) != 0 {
		fmt.Fprintln(os.Stderr, "Usage: cloakenv cache prune")
		return 1
	}

	orch, err := engine.NewOrchestrator(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Config error: %v\n", err)
		return 1
	}
	defer func() { _ = orch.Close() }()
	ctx, stop := signalContext()
	defer stop()

	stats, err := orch.PruneCache(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to prune cache: %v\n", err)
		return exitCodeForContext(ctx, 1)
	}

	if stats.Count == 1 {
		fmt.Printf("Cache pruned successfully: %d expired entry removed (%s reclaimed).\n", stats.Count, formatBytes(stats.ReclaimedBytes))
	} else {
		fmt.Printf("Cache pruned successfully: %d expired entries removed (%s reclaimed).\n", stats.Count, formatBytes(stats.ReclaimedBytes))
	}
	return 0
}
