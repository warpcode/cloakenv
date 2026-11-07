package cmd

import (
	"fmt"
	"io"
	"os"

	"github.com/warpcode/cloakenv/internal/config"
	"github.com/warpcode/cloakenv/internal/utils"
)

// PrintConfigHelp prints usage help for the config subcommand to stdout.
func PrintConfigHelp() {
	PrintConfigHelpTo(os.Stdout)
}

// PrintConfigHelpTo prints usage help for the config subcommand to the provided writer.
func PrintConfigHelpTo(w io.Writer) {
	fmt.Fprintln(w, `Usage:
  cloakenv config <subcommand> [flags]

Description:
  Manage, inspect, and validate cloakenv configuration.

Subcommands:
  check [--strict]    Validate configuration syntax, schema conformity, paths, and templates`)
}

// ConfigCmd handles "cloakenv config <subcommand> [flags]".
func ConfigCmd(args []string, customConfigPath string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		PrintConfigHelpTo(stdout)
		return 1
	}

	subcmd := args[0]
	switch subcmd {
	case "check":
		return runConfigCheck(args[1:], customConfigPath, stdout, stderr)
	default:
		if utils.HasHelpFlag(args) {
			PrintConfigHelpTo(stdout)
			return 0
		}
		fmt.Fprintf(stderr, "Unknown config subcommand: %s\n", subcmd)
		return 1
	}
}

func runConfigCheck(args []string, customConfigPath string, stdout, stderr io.Writer) int {
	if utils.HasHelpFlag(args) {
		fmt.Fprintln(stdout, `Usage:
  cloakenv config check [--strict]

Description:
  Validate YAML syntax, schema conformity, file existence, regex compilation, and template syntax.

Flags:
  --strict    Enforce strict schema checking and reject unknown fields`)
		return 0
	}

	var strict bool
	parser := NewFlagParser()
	parser.Bool([]string{"--strict"}, &strict)

	rem, err := parser.Parse(args)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}

	if len(rem) > 0 {
		fmt.Fprintf(stderr, "Unexpected arguments for 'config check': %v\n", rem)
		return 1
	}

	path := customConfigPath
	if path == "" {
		var err error
		path, err = config.DefaultConfigPath()
		if err != nil {
			fmt.Fprintf(stderr, "Error resolving config path: %v\n", err)
			return 1
		}
	}

	fmt.Fprintf(stdout, "Checking configuration at: %s\n", path)

	res, err := config.Validate(path, strict)
	if err != nil {
		fmt.Fprintf(stderr, "Configuration check failed: %v\n", err)
		return 1
	}

	for _, w := range res.Warnings {
		fmt.Fprintf(stdout, "  [WARN] %s\n", w)
	}

	if !res.IsValid() {
		fmt.Fprintf(stderr, "Configuration validation failed with %d error(s):\n", len(res.Errors))
		for _, e := range res.Errors {
			fmt.Fprintf(stderr, "  [FAIL] %s\n", e)
		}
		return 1
	}

	fmt.Fprintln(stdout, "Configuration is valid.")
	return 0
}
