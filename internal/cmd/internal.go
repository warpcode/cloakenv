package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/warpcode/cloakenv/internal/config"
	"github.com/warpcode/cloakenv/internal/engine"
	"github.com/warpcode/cloakenv/internal/utils"
)

// MatchAliasResult represents the structured JSON output for match-alias.
type MatchAliasResult struct {
	Matched   bool              `json:"matched"`
	Match     string            `json:"match,omitempty"`
	Command   string            `json:"command,omitempty"`
	Vaults    []string          `json:"vaults,omitempty"`
	Merge     []string          `json:"merge,omitempty"`
	Env       map[string]string `json:"env,omitempty"`
	Whitelist []string          `json:"whitelist,omitempty"`
}

// Internal handles internal helper commands, specifically "cloakenv internal match-alias [--json] -- <command> [args]".
func Internal(args []string, cfg *config.Config) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Usage: cloakenv internal <subcommand> [args]")
		return 1
	}

	subcmd := args[0]
	subArgs := args[1:]

	switch subcmd {
	case "match-alias":
		return runMatchAlias(subArgs, cfg)
	default:
		fmt.Fprintf(os.Stderr, "Unknown internal subcommand: %s\n", subcmd)
		return 1
	}
}

func runMatchAlias(args []string, cfg *config.Config) int {
	if utils.HasHelpFlag(args) {
		fmt.Fprintln(os.Stdout, `Usage:
  cloakenv internal match-alias [--json] -- <command> [args]

Description:
  Internal helper to check if a command matches any configured autoload/run alias rule.
  Exits with code 0 if matched, exit code 1 if no match.`)
		return 0
	}

	var jsonOutput bool
	var cmdArgs []string

	i := 0
	for i < len(args) {
		switch {
		case args[i] == "--":
			cmdArgs = args[i+1:]
			i = len(args)
		case args[i] == "--json":
			jsonOutput = true
			i++
		default:
			cmdArgs = args[i:]
			i = len(args)
		}
	}

	if len(cmdArgs) == 0 {
		if jsonOutput {
			res := MatchAliasResult{Matched: false}
			data, _ := json.MarshalIndent(res, "", "  ")
			fmt.Println(string(data))
		} else {
			fmt.Println("false")
		}
		return 1
	}

	rule, matched := engine.MatchRunAlias(cfg, cmdArgs)

	if jsonOutput {
		res := MatchAliasResult{
			Matched: matched,
		}
		if matched {
			res.Match = rule.Match
			res.Command = rule.Command
			res.Vaults = rule.Vaults
			res.Merge = rule.Merge
			res.Env = rule.Env
			res.Whitelist = rule.Whitelist
		}
		data, err := json.MarshalIndent(res, "", "  ")
		if err != nil {
			fmt.Fprintf(os.Stderr, "JSON encoding error: %v\n", err)
			return 1
		}
		fmt.Println(string(data))
	} else {
		if matched {
			if rule.Match != "" {
				fmt.Println(rule.Match)
			} else {
				fmt.Println("true")
			}
		} else {
			fmt.Println("false")
		}
	}

	if matched {
		return 0
	}
	return 1
}
