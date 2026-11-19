package cmd

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"os"
	"slices"
	"strings"

	"github.com/warpcode/cloakenv/internal/config"
	"github.com/warpcode/cloakenv/internal/engine"
	"github.com/warpcode/cloakenv/internal/runner"
)

// Run handles "cloakenv run [-E] [-e KEY=uri ...] [-t template_path] [-m entry-uri] [-i KEY ...] [--no-autoload] [--dry-run] -- <cmd> [args]".
func Run(args []string, cfg *config.Config) int {
	var (
		explicitEnv = make(map[string]string)
		merges      []string
		whitelist   []string
		cmdArgs     []string
		noAutoload  bool
		emptyEnv    bool
		dryRun      bool
		showHelp    bool
		keyFormat   string
	)

	parser := NewFlagParser()
	parser.Bool([]string{"-h", "--help"}, &showHelp)
	parser.Var([]string{"-f", "--key-format"}, true, "flag -f/--key-format requires an argument", func(name, val string) error {
		if val != "none" && val != "env" && val != "snake" && val != "kebab" {
			return fmt.Errorf("invalid key format %q (expected none, env, snake, or kebab)", val)
		}
		keyFormat = val
		return nil
	})
	parser.StopAtNonFlag = true
	parser.Bool([]string{"-E"}, &emptyEnv)
	parser.Bool([]string{"--no-autoload"}, &noAutoload)
	parser.HiddenBool([]string{"--skip-autoload"}, &noAutoload)
	parser.Bool([]string{"--dry-run"}, &dryRun)
	parser.Var([]string{"-e"}, true, "", func(name, val string) error {
		key, uri, ok := strings.Cut(val, "=")
		if !ok || key == "" || uri == "" {
			return fmt.Errorf("invalid -e format: %q (expected KEY=uri)", val)
		}
		explicitEnv[key] = uri
		return nil
	})
	parser.Var([]string{"-t"}, true, "", func(name, val string) error {
		envs, err := config.ParseTemplateFile(val)
		if err != nil {
			return fmt.Errorf("error parsing template file %s: %w", val, err)
		}
		for k, v := range envs {
			explicitEnv[k] = v
		}
		return nil
	})
	parser.StringSlice([]string{"-m"}, &merges, "")
	parser.StringSlice([]string{"-i"}, &whitelist, "")

	dashDashIdx := -1
	for i, arg := range args {
		if arg == "--" {
			dashDashIdx = i
			break
		}
	}

	if dashDashIdx >= 0 {
		parser.StopAtNonFlag = false
		flagArgs := args[:dashDashIdx]
		rem, err := parser.Parse(flagArgs)
		if showHelp {
			PrintRunHelp()
			return 0
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		if len(rem) > 0 {
			fmt.Fprintf(os.Stderr, "unexpected argument: %s\n", rem[0])
			return 1
		}
		cmdArgs = args[dashDashIdx+1:]
	} else {
		rem, err := parser.Parse(args)
		if showHelp {
			PrintRunHelp()
			return 0
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		cmdArgs = rem
	}

	if len(cmdArgs) == 0 {
		fmt.Fprintln(os.Stderr, "No command specified. Usage: cloakenv run [flags] -- <command> [args]")
		return 1
	}

	if keyFormat != "" {
		if cfg == nil {
			cfg = &config.Config{}
		}
		cfg.KeyFormatOverride = keyFormat
	}

	orch, err := engine.NewOrchestrator(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Config error: %v\n", err)
		return 1
	}
	ctx, stop := signalContext()
	defer stop()

	var activeCmdArgs []string
	if !noAutoload {
		activeCmdArgs = cmdArgs
	}

	// Build the environment block and evaluate command transformations
	envDetails, err := orch.BuildEnvForCommandDetails(ctx, engine.EnvConfig{
		CmdArgs:   activeCmdArgs,
		Explicit:  explicitEnv,
		Merges:    merges,
		Whitelist: whitelist,
		EmptyEnv:  emptyEnv,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Secret resolution failed: %v\n", err)
		return exitCodeForContext(ctx, 1)
	}

	finalCmdArgs := envDetails.Command
	if len(finalCmdArgs) == 0 {
		finalCmdArgs = cmdArgs
	}

	if dryRun {
		cmdToDisplay := envDetails.RawCommand
		if len(cmdToDisplay) == 0 {
			cmdToDisplay = cmdArgs
		}
		printDryRunPreview(cmdToDisplay, envDetails.InjectedKeys, envDetails.RenderedTemplates, envDetails.MaskSecrets)
		return 0
	}

	var deliveries []runner.TemplateDelivery
	for _, rt := range envDetails.RenderedTemplates {
		// Mode 1 (EnvVar) is already injected into envDetails.Env; materialize file deliveries (Modes 2-4).
		if rt.EnvVar != "" {
			continue
		}
		deliveries = append(deliveries, runner.TemplateDelivery{
			TemplateName: rt.TemplateName,
			EnvVar:       rt.EnvVar,
			EnvPathVar:   rt.EnvPathVar,
			Path:         rt.Path,
			Persist:      rt.Persist,
			Content:      rt.Content,
		})
	}

	var cleanup func()
	finalEnv := envDetails.Env
	if len(deliveries) > 0 {
		envOverrides, matCleanup, matErr := runner.MaterializeTemplates(deliveries)
		if matErr != nil {
			fmt.Fprintf(os.Stderr, "Template delivery failed: %v\n", matErr)
			return 1
		}
		cleanup = matCleanup
		defer cleanup()
		if ctx.Err() != nil {
			cleanup()
			return exitCodeForContext(ctx, 1)
		}
		finalEnv = mergeEnvOverrides(finalEnv, envOverrides)
	}

	if ctx.Err() != nil {
		return exitCodeForContext(ctx, 1)
	}

	// Transfer signal ownership to runner without a gap: runner installs its own
	// signal handler before invoking OnSignalsRegistered (stop).
	return runner.RunCommandWithOptions(context.Background(), finalCmdArgs, finalEnv, runner.RunOptions{
		MaskSecrets:         envDetails.MaskSecrets,
		Cleanup:             cleanup,
		OnSignalsRegistered: stop,
	})
}

func mergeEnvOverrides(baseEnv []string, overrides map[string]string) []string {
	if len(overrides) == 0 {
		return baseEnv
	}
	capHint := len(baseEnv)
	if len(overrides) <= math.MaxInt-capHint {
		capHint += len(overrides)
	}
	merged := make(map[string]string, capHint)
	for _, kv := range baseEnv {
		if k, v, ok := strings.Cut(kv, "="); ok && k != "" {
			merged[k] = v
		}
	}
	for k, v := range overrides {
		merged[k] = v
	}
	keys := make([]string, 0, len(merged))
	for k := range merged {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, k+"="+merged[k])
	}
	return out
}

func maskDryRunString(s string, trie *runner.MaskTrie) string {
	if trie == nil || s == "" {
		return s
	}
	var buf bytes.Buffer
	mw := runner.NewMaskingWriter(&buf, trie)
	_, _ = mw.Write([]byte(s))
	_ = mw.Flush()
	return buf.String()
}

func printDryRunPreview(cmdArgs []string, injectedKeys []string, templates []engine.RenderedTemplateDelivery, maskSecrets []string) {
	trie := runner.NewMaskTrie(maskSecrets)
	fmt.Println("[DRY RUN] Command execution preview:")
	fmt.Printf("Command: %s\n", maskDryRunString(strings.Join(cmdArgs, " "), trie))
	if len(injectedKeys) == 0 {
		fmt.Println("Injected variables: (none)")
	} else {
		fmt.Println("Injected variables:")
		for _, k := range injectedKeys {
			fmt.Printf("  %s\n", k)
		}
	}
	if len(templates) > 0 {
		fmt.Println("Templates:")
		for _, tpl := range templates {
			switch {
			case tpl.EnvVar != "":
				fmt.Printf("  %s (driver=%s, env=%s)\n", tpl.TemplateName, tpl.Driver, tpl.EnvVar)
			case tpl.EnvPathVar != "" && tpl.Path == "":
				fmt.Printf("  %s (driver=%s, env_path=%s, ephemeral=true, persist=%t)\n", tpl.TemplateName, tpl.Driver, tpl.EnvPathVar, tpl.Persist)
			case tpl.EnvPathVar != "" && tpl.Path != "":
				fmt.Printf("  %s (driver=%s, env_path=%s, path=%s, persist=%t)\n", tpl.TemplateName, tpl.Driver, tpl.EnvPathVar, maskDryRunString(tpl.Path, trie), tpl.Persist)
			default:
				fmt.Printf("  %s (driver=%s, path=%s, persist=%t)\n", tpl.TemplateName, tpl.Driver, maskDryRunString(tpl.Path, trie), tpl.Persist)
			}
		}
	}
}
