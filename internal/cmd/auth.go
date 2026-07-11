package cmd

import (
	"context"
	"fmt"
	"os"

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

// authLogin handles "cloakenv auth login <scheme>".
func authLogin(args []string, cfg *config.Config) int {
	if utils.HasHelpFlag(args) {
		PrintAuthLoginHelp()
		return 0
	}
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "Usage: cloakenv auth login <scheme>")
		return 1
	}
	scheme := args[0]

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

// authForget handles "cloakenv auth forget <scheme>".
func authForget(args []string, cfg *config.Config) int {
	if utils.HasHelpFlag(args) {
		PrintAuthForgetHelp()
		return 0
	}
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "Usage: cloakenv auth forget <scheme>")
		return 1
	}
	scheme := args[0]

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

// authStatus handles "cloakenv auth status [vault]".
func authStatus(args []string, cfg *config.Config) int {
	if utils.HasHelpFlag(args) {
		PrintAuthStatusHelp()
		return 0
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
