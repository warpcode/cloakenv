package cmd

import (
	"context"
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/warpcode/cloakenv/internal/config"
	"github.com/warpcode/cloakenv/internal/engine"
	"github.com/warpcode/cloakenv/internal/utils"
)

// Auth handles routing for "cloakenv auth" subcommands.
func Auth(args []string, cfg *config.Config) int {
	if utils.HasHelpFlag(args) && (len(args) < 1 || (args[0] != "login" && args[0] != "forget" && args[0] != "status")) {
		PrintAuthHelp()
		return 0
	}
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "Usage: cloakenv auth <login|forget|status> [vault]")
		return 1
	}

	switch args[0] {
	case "login":
		return authLogin(args[1:], cfg)
	case "forget":
		return authForget(args[1:], cfg)
	case "status":
		return authStatus(args[1:], cfg)
	default:
		fmt.Fprintf(os.Stderr, "Unknown auth subcommand: %s\n", args[0])
		return 1
	}
}

// authLogin handles "cloakenv auth login <vault>".
func authLogin(args []string, cfg *config.Config) int {
	if utils.HasHelpFlag(args) {
		PrintAuthLoginHelp()
		return 0
	}
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "Usage: cloakenv auth login <vault>")
		return 1
	}
	vault := args[0]

	orch, err := engine.NewOrchestrator(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Config error: %v\n", err)
		return 1
	}
	ctx := context.Background()
	if err := orch.Login(ctx, vault); err != nil {
		fmt.Fprintf(os.Stderr, "Authentication failed: %v\n", err)
		return 1
	}

	fmt.Printf("Successfully authenticated and saved credentials for vault %q\n", vault)
	return 0
}

// authForget handles "cloakenv auth forget <vault>".
func authForget(args []string, cfg *config.Config) int {
	if utils.HasHelpFlag(args) {
		PrintAuthForgetHelp()
		return 0
	}
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "Usage: cloakenv auth forget <vault>")
		return 1
	}
	vault := args[0]

	orch, err := engine.NewOrchestrator(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Config error: %v\n", err)
		return 1
	}
	ctx := context.Background()
	if err := orch.Forget(ctx, vault); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to clear credentials: %v\n", err)
		return 1
	}

	fmt.Printf("Successfully cleared credentials for vault %q\n", vault)
	return 0
}

// AuthStatusRecord represents the status of a single vault for structured output.
type AuthStatusRecord struct {
	Vault     string `json:"vault" yaml:"vault"`
	Status    string `json:"status" yaml:"status"`
	VaultPath string `json:"vault_path,omitempty" yaml:"vault_path,omitempty"`
	Latency   string `json:"latency" yaml:"latency"`
	Error     string `json:"error,omitempty" yaml:"error,omitempty"`
}

type authStatusOptions struct {
	vault        string
	outputFormat string
}

func parseAuthStatusArgs(args []string) (*authStatusOptions, error) {
	opts := &authStatusOptions{
		outputFormat: "text",
	}

	parser := NewFlagParser()
	parser.Var([]string{"-o", "--output"}, true, "flag -o/--output requires an argument", func(name, val string) error {
		if val != "text" && val != "json" && val != "yaml" {
			return fmt.Errorf("invalid output format %q (expected text, json, or yaml)", val)
		}
		opts.outputFormat = val
		return nil
	})
	parser.UnknownFlagErr = func(flag string) error {
		return fmt.Errorf("unknown flag: %s", flag)
	}
	parser.PositionalHandler = func(arg string) error {
		if opts.vault != "" {
			return fmt.Errorf("usage: cloakenv auth status [vault] [-o text|json|yaml]")
		}
		opts.vault = arg
		return nil
	}

	if _, err := parser.Parse(args); err != nil {
		return nil, err
	}

	return opts, nil
}

// authStatus handles "cloakenv auth status [vault]".
func authStatus(args []string, cfg *config.Config) int {
	if utils.HasHelpFlag(args) {
		PrintAuthStatusHelp()
		return 0
	}

	opts, err := parseAuthStatusArgs(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	orch, err := engine.NewOrchestrator(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Config error: %v\n", err)
		return 1
	}
	ctx := context.Background()

	vaultNames := make([]string, 0)
	if opts.vault != "" {
		vaultNames = append(vaultNames, opts.vault)
	} else if cfg != nil && len(cfg.Vaults) > 0 {
		for name := range cfg.Vaults {
			vaultNames = append(vaultNames, name)
		}
		sort.Strings(vaultNames)
	}

	if len(vaultNames) == 0 {
		if opts.outputFormat == "json" || opts.outputFormat == "yaml" {
			asJSON := (opts.outputFormat == "json")
			if err := utils.RenderOutput([]AuthStatusRecord{}, asJSON, "status"); err != nil {
				fmt.Fprintln(os.Stderr, err)
				return 1
			}
			return 0
		}
		fmt.Println("No vaults configured.")
		return 0
	}

	records := make([]AuthStatusRecord, 0, len(vaultNames))
	hasError := false

	for _, vaultName := range vaultNames {
		var vaultPath string
		if cfg != nil && cfg.Vaults != nil {
			if vCfg, exists := cfg.Vaults[vaultName]; exists {
				vaultPath = vCfg.VaultPath
			}
		}

		start := time.Now()
		checkErr := orch.CheckAccess(ctx, vaultName)
		latency := time.Since(start).String()

		record := AuthStatusRecord{
			Vault:     vaultName,
			VaultPath: vaultPath,
			Latency:   latency,
		}

		if checkErr != nil {
			record.Status = "ERROR"
			record.Error = checkErr.Error()
			hasError = true
		} else {
			record.Status = "ACTIVE"
		}
		records = append(records, record)
	}

	if opts.outputFormat == "json" || opts.outputFormat == "yaml" {
		asJSON := (opts.outputFormat == "json")
		if err := utils.RenderOutput(records, asJSON, "status"); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
	} else {
		for _, rec := range records {
			if rec.Status == "ERROR" {
				fmt.Printf("%s: ERROR: %s\n", rec.Vault, rec.Error)
			} else {
				fmt.Printf("%s: ACTIVE\n", rec.Vault)
			}
		}
	}

	if hasError {
		return 1
	}
	return 0
}
