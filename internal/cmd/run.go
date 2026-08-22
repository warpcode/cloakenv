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

// Run handles "cloakenv run [-E] [-e KEY=uri ...] [-m entry-uri] [-i KEY ...] [--no-autoload] -- <cmd> [args]".
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
		noAutoload  bool
		emptyEnv    bool
	)

	parser := NewFlagParser()
	parser.StopAtNonFlag = true
	parser.Bool([]string{"-E"}, &emptyEnv)
	parser.Bool([]string{"--no-autoload", "--skip-autoload"}, &noAutoload)
	parser.Var([]string{"-e"}, true, "", func(name, val string) error {
		key, uri, ok := strings.Cut(val, "=")
		if !ok || key == "" || uri == "" {
			return fmt.Errorf("Invalid -e format: %q (expected KEY=uri)", val)
		}
		explicitEnv[key] = uri
		return nil
	})
	parser.Var([]string{"-t"}, true, "", func(name, val string) error {
		envs, err := utils.ParseTemplateFile(val)
		if err != nil {
			return fmt.Errorf("Error parsing template file %s: %v", val, err)
		}
		for k, v := range envs {
			explicitEnv[k] = v
		}
		return nil
	})
	parser.StringSlice([]string{"-m"}, &merges, "")
	parser.StringSlice([]string{"-i"}, &whitelist, "")

	rem, err := parser.Parse(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	cmdArgs = rem

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

	var activeCmdArgs []string
	if !noAutoload {
		activeCmdArgs = cmdArgs
	}

	// Build the environment block and evaluate command transformations
	finalCmdArgs, env, err := orch.BuildEnvForCommand(ctx, activeCmdArgs, explicitEnv, merges, whitelist, emptyEnv)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Secret resolution failed: %v\n", err)
		return 1
	}

	if len(finalCmdArgs) == 0 {
		finalCmdArgs = cmdArgs
	}

	// Execute the wrapped command
	return runner.RunCommand(finalCmdArgs, env)
}
