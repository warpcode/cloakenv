package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/warpcode/cloakenv/internal/config"
	"github.com/warpcode/cloakenv/internal/engine"
	"github.com/warpcode/cloakenv/internal/runner"
	"github.com/warpcode/cloakenv/internal/utils"
)

// Run handles "cloakenv run [-e KEY=uri ...] [-m entry-uri] [-i KEY ...] -- <cmd> [args]".
func Run(args []string, cfg *config.Config) int {
	if utils.HasHelpFlag(args) {
		PrintRunHelp()
		return 0
	}
	var (
		explicitEnv = make(map[string]string)
		merges      []string
		whitelist   []string
		cmdArgs     []string
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
		case args[i] == "-t" && i+1 < len(args):
			i++
			templatePath := args[i]
			envs, err := utils.ParseTemplateFile(templatePath)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error parsing template file %s: %v\n", templatePath, err)
				return 1
			}
			for k, v := range envs {
				explicitEnv[k] = v
			}
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
	return runner.RunCommand(cmdArgs, env)
}
