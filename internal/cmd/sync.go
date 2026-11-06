package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/warpcode/cloakenv/internal/config"
	"github.com/warpcode/cloakenv/internal/engine"
	"github.com/warpcode/cloakenv/internal/utils"
)

// Sync handles "cloakenv sync <source> <target> [--dry-run] [--tags <tags>] [--prefix <prefix>] [--regex <pattern>]".
func Sync(args []string, cfg *config.Config) int {
	if utils.HasHelpFlag(args) {
		PrintSyncHelp()
		return 0
	}

	var dryRun bool
	var tags []string
	var prefix string
	var regexPattern string
	var positional []string

	parser := NewFlagParser()
	parser.Bool([]string{"--dry-run"}, &dryRun)
	parser.Var([]string{"--tags"}, true, "flag --tags requires an argument", func(name, val string) error {
		for _, part := range strings.Split(val, ",") {
			part = strings.TrimSpace(part)
			if part != "" {
				tags = append(tags, part)
			}
		}
		return nil
	})
	parser.Var([]string{"--prefix"}, true, "flag --prefix requires an argument", func(name, val string) error {
		prefix = val
		return nil
	})
	parser.Var([]string{"--regex", "-r"}, true, "flag --regex/-r requires an argument", func(name, val string) error {
		regexPattern = val
		return nil
	})
	parser.PositionalHandler = func(arg string) error {
		positional = append(positional, arg)
		return nil
	}

	if _, err := parser.Parse(args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	if len(positional) != 2 {
		fmt.Fprintln(os.Stderr, "Usage: cloakenv sync <source> <target> [flags]")
		return 1
	}

	source := positional[0]
	target := positional[1]

	orch, err := engine.NewOrchestrator(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Config error: %v\n", err)
		return 1
	}

	opts := engine.SyncOptions{
		Tags:   tags,
		Prefix: prefix,
		Regex:  regexPattern,
		DryRun: dryRun,
	}

	summary, err := orch.Sync(context.Background(), source, target, opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Sync failed: %v\n", err)
		return 1
	}

	renderSyncSummary(summary, dryRun)
	return 0
}

func renderSyncSummary(summary *engine.SyncSummary, dryRun bool) {
	if dryRun {
		fmt.Println("[DRY RUN] Secret synchronization preview:")
	} else {
		fmt.Println("Secret synchronization complete:")
	}

	for _, diff := range summary.Diffs {
		switch diff.Status {
		case "added":
			fmt.Printf("  + %s (added)\n", diff.Key)
		case "updated":
			fmt.Printf("  ~ %s (updated)\n", diff.Key)
		case "skipped":
			if diff.Reason != "" {
				fmt.Printf("    %s (skipped: %s)\n", diff.Key, diff.Reason)
			} else {
				fmt.Printf("    %s (skipped)\n", diff.Key)
			}
		}
	}

	actionWord := "synchronized"
	if dryRun {
		actionWord = "to synchronize"
	}
	fmt.Printf("\nSummary: %d added, %d updated, %d skipped (%s)\n",
		summary.Added, summary.Updated, summary.Skipped, actionWord)
}
