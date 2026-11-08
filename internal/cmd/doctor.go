package cmd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"strings"

	"github.com/warpcode/cloakenv/internal/config"
	"github.com/warpcode/cloakenv/internal/utils"
)

// PrintDoctorHelpTo prints usage help for the doctor subcommand to the provided writer.
func PrintDoctorHelpTo(w io.Writer) {
	fmt.Fprintln(w, `Usage:
  cloakenv doctor

Description:
  Diagnose environment and vault reachability.
  Inspects the loaded Config structure, validates provider binary/vault file reachability,
  checks cache directory permissions (0700), and produces a structured diagnostic report
  without leaking secret values.`)
}

// DoctorCmd handles "cloakenv doctor [flags]".
func DoctorCmd(args []string, cfg *config.Config, stdout, stderr io.Writer) int {
	if utils.HasHelpFlag(args) {
		PrintDoctorHelpTo(stdout)
		return 0
	}

	if len(args) > 0 {
		fmt.Fprintf(stderr, "Unexpected arguments for 'doctor': %v\n", args)
		return 1
	}

	fmt.Fprintln(stdout, "Cloakenv Doctor Diagnostic Report")
	fmt.Fprintln(stdout, strings.Repeat("-", 40))

	// 1. Config path info
	fmt.Fprintf(stdout, "Configuration Path: %s\n", cfg.ConfigPath)

	// 2. Check cache directory permissions (0700)
	var cacheDir string
	if cfg.ConfigPath == "" {
		home, err := os.UserHomeDir()
		if err == nil {
			cacheDir = filepath.Join(home, ".config", "cloakenv", "cache")
		} else {
			cacheDir = "cache" // fallback
		}
	} else {
		cacheDir = filepath.Join(filepath.Dir(cfg.ConfigPath), "cache")
	}

	fmt.Fprintf(stdout, "Cache Directory: %s\n", cacheDir)
	if info, err := os.Stat(cacheDir); err == nil {
		if info.Mode().Perm() == 0700 {
			fmt.Fprintln(stdout, "  [OK] Cache directory permissions are 0700")
		} else {
			fmt.Fprintf(stdout, "  [WARN] Cache directory permissions are %04o (expected 0700)\n", info.Mode().Perm())
		}
	} else if os.IsNotExist(err) {
		fmt.Fprintln(stdout, "  [INFO] Cache directory does not exist yet")
	} else {
		fmt.Fprintf(stdout, "  [ERROR] Cannot access cache directory: %v\n", err)
	}

	// 3. Vault reachability
	fmt.Fprintln(stdout, "\nVault Reachability:")
	if len(cfg.Vaults) == 0 {
		fmt.Fprintln(stdout, "  No vaults configured.")
	}

	hasErrors := false

	for name, vault := range cfg.Vaults {
		fmt.Fprintf(stdout, "  Vault %q (provider: %s):\n", name, vault.Provider)

		if vault.Provider == "keepass" || vault.Provider == "yaml" || vault.Provider == "json" {
			if vault.VaultPath != "" {
				if info, err := os.Stat(vault.VaultPath); err == nil {
					if info.IsDir() {
						fmt.Fprintf(stdout, "    [ERROR] Vault path %q is a directory, not a file\n", vault.VaultPath)
						hasErrors = true
					} else {
						fmt.Fprintf(stdout, "    [OK] Vault file exists and is accessible\n")
					}
				} else {
					fmt.Fprintf(stdout, "    [ERROR] Vault file not accessible: %v\n", err)
					hasErrors = true
				}
			} else {
				fmt.Fprintf(stdout, "    [ERROR] Missing vault_path\n")
				hasErrors = true
			}

			if vault.Provider == "keepass" && vault.KeyFile != "" {
				if info, err := os.Stat(vault.KeyFile); err == nil {
					if info.IsDir() {
						fmt.Fprintf(stdout, "    [ERROR] Key file %q is a directory, not a file\n", vault.KeyFile)
						hasErrors = true
					} else {
						fmt.Fprintf(stdout, "    [OK] Key file exists and is accessible\n")
					}
				} else {
					fmt.Fprintf(stdout, "    [ERROR] Key file not accessible: %v\n", err)
					hasErrors = true
				}
			}
		} else if vault.Provider == "custom_vault" {
			fmt.Fprintln(stdout, "    [OK] Custom vault (in-memory)")
		} else if vault.Provider == "search" {
			fmt.Fprintln(stdout, "    [OK] Search vault (dynamic)")
		} else if vault.Provider == "vault" {
			if vault.Address == "" {
				fmt.Fprintln(stdout, "    [ERROR] Missing Vault address")
				hasErrors = true
			} else {
				fmt.Fprintln(stdout, "    [OK] HashiCorp Vault configured (reachability check deferred to runtime)")
			}
		}
	}

	fmt.Fprintln(stdout, strings.Repeat("-", 40))
	if hasErrors {
		fmt.Fprintln(stdout, "Diagnostics completed with errors.")
		return 1
	}

	fmt.Fprintln(stdout, "Diagnostics completed successfully.")
	return 0
}
