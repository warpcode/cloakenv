package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/warpcode/cloakenv/internal/config"
	"github.com/warpcode/cloakenv/internal/utils"
)

// PrintDoctorHelpTo prints usage help for the doctor subcommand to the provided writer.
func PrintDoctorHelpTo(w io.Writer) {
	fmt.Fprintln(w, `Usage:
  cloakenv doctor [--strict]

Description:
  Diagnose environment, configuration validity, and vault reachability.
  Validates YAML syntax, schema conformity, file existence, regexes, and templates,
  checks cache directory permissions (0700), and inspects provider dependencies and
  vault reachability without leaking secret values.

Flags:
  --strict    Enforce strict schema checking (reject unknown fields) and fail on warnings`)
}

// DoctorCmd handles "cloakenv doctor [--strict]".
func DoctorCmd(args []string, cfg *config.Config, stdout, stderr io.Writer) int {
	if utils.HasHelpFlag(args) {
		PrintDoctorHelpTo(stdout)
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
		fmt.Fprintf(stderr, "Unexpected arguments for 'doctor': %v\n", rem)
		return 1
	}

	if cfg == nil {
		cfg = &config.Config{}
	}

	hasErrors := false
	hasWarnings := false

	fmt.Fprintln(stdout, "Cloakenv Doctor Diagnostic Report")
	fmt.Fprintln(stdout, strings.Repeat("-", 40))

	// 1. Config path & full validation
	cfgPath := cfg.ConfigPath
	if cfgPath == "" {
		if defaultPath, pathErr := config.DefaultConfigPath(); pathErr == nil {
			cfgPath = defaultPath
		}
	}

	fmt.Fprintf(stdout, "Configuration Path: %s\n", cfgPath)
	if cfgPath == "" {
		fmt.Fprintln(stdout, "  [WARN] Configuration path could not be resolved")
		hasWarnings = true
	} else if _, statErr := os.Stat(cfgPath); statErr != nil {
		if errors.Is(statErr, os.ErrNotExist) {
			fmt.Fprintf(stdout, "  [WARN] Configuration file does not exist: %s\n", cfgPath)
			hasWarnings = true
		} else {
			fmt.Fprintf(stderr, "  [FAIL] Cannot access configuration file %s: %v\n", cfgPath, statErr)
			hasErrors = true
		}
	} else {
		res, valErr := config.Validate(cfgPath, strict)
		if valErr != nil {
			fmt.Fprintf(stderr, "  [FAIL] Configuration check failed: %v\n", valErr)
			hasErrors = true
		} else {
			for _, w := range res.Warnings {
				fmt.Fprintf(stdout, "  [WARN] %s\n", w)
				hasWarnings = true
			}
			for _, e := range res.Errors {
				fmt.Fprintf(stderr, "  [FAIL] %s\n", e)
				hasErrors = true
			}
			if res.IsValid() {
				fmt.Fprintln(stdout, "  [OK] Configuration is valid.")
			}
		}
	}

	// 2. Check cache directory permissions (0700)
	prefix := cfg.KeyringPrefix()
	var cacheDir string
	if userCache, err := os.UserCacheDir(); err == nil {
		cacheDir = filepath.Join(userCache, prefix)
	} else if home, err := os.UserHomeDir(); err == nil {
		cacheDir = filepath.Join(home, ".cache", prefix)
	} else {
		cacheDir = prefix // fallback
	}

	fmt.Fprintf(stdout, "Cache Directory: %s\n", cacheDir)
	if info, err := os.Stat(cacheDir); err == nil {
		if info.Mode().Perm() == 0700 {
			fmt.Fprintln(stdout, "  [OK] Cache directory permissions are 0700")
		} else {
			fmt.Fprintf(stdout, "  [WARN] Cache directory permissions are %04o (expected 0700)\n", info.Mode().Perm())
			hasWarnings = true
		}
	} else if errors.Is(err, os.ErrNotExist) {
		fmt.Fprintln(stdout, "  [INFO] Cache directory does not exist yet")
	} else {
		fmt.Fprintf(stdout, "  [ERROR] Cannot access cache directory: %v\n", err)
		hasErrors = true
	}

	// 3. Vault reachability & provider dependencies
	fmt.Fprintln(stdout, "\nVault Reachability:")
	if len(cfg.Vaults) == 0 {
		fmt.Fprintln(stdout, "  No vaults configured.")
	} else {
		vaultNames := make([]string, 0, len(cfg.Vaults))
		for name := range cfg.Vaults {
			vaultNames = append(vaultNames, name)
		}
		slices.Sort(vaultNames)

		for _, name := range vaultNames {
			vault := cfg.Vaults[name]
			fmt.Fprintf(stdout, "  Vault %q (provider: %s):\n", name, vault.Provider)

			switch vault.Provider {
			case "keepass", "yaml", "json":
				if vault.VaultPath != "" {
					if info, err := os.Stat(vault.VaultPath); err == nil {
						if info.IsDir() {
							fmt.Fprintf(stdout, "    [ERROR] Vault path %q is a directory, not a file\n", vault.VaultPath)
							hasErrors = true
						} else {
							fmt.Fprintln(stdout, "    [OK] Vault file exists and is accessible")
						}
					} else {
						fmt.Fprintf(stdout, "    [ERROR] Vault file not accessible: %v\n", err)
						hasErrors = true
					}
				} else {
					fmt.Fprintln(stdout, "    [ERROR] Missing vault_path")
					hasErrors = true
				}

				if vault.Provider == "keepass" && vault.KeyFile != "" {
					if info, err := os.Stat(vault.KeyFile); err == nil {
						if info.IsDir() {
							fmt.Fprintf(stdout, "    [ERROR] Key file %q is a directory, not a file\n", vault.KeyFile)
							hasErrors = true
						} else {
							fmt.Fprintln(stdout, "    [OK] Key file exists and is accessible")
						}
					} else {
						fmt.Fprintf(stdout, "    [ERROR] Key file not accessible: %v\n", err)
						hasErrors = true
					}
				}
			case "pass":
				storeDir := vault.VaultPath
				if storeDir == "" {
					storeDir = os.Getenv("PASSWORD_STORE_DIR")
				}
				if storeDir == "" {
					if home, err := os.UserHomeDir(); err == nil {
						storeDir = filepath.Join(home, ".password-store")
					}
				}
				if storeDir == "" {
					fmt.Fprintln(stdout, "    [ERROR] Cannot determine password store directory")
					hasErrors = true
				} else if info, err := os.Stat(storeDir); err == nil {
					if !info.IsDir() {
						fmt.Fprintf(stdout, "    [ERROR] Password store path %q is not a directory\n", storeDir)
						hasErrors = true
					} else {
						fmt.Fprintln(stdout, "    [OK] Password store directory exists and is accessible")
					}
				} else {
					fmt.Fprintf(stdout, "    [ERROR] Password store directory not accessible: %v\n", err)
					hasErrors = true
				}

				if _, err := exec.LookPath("gpg"); err != nil {
					fmt.Fprintln(stdout, "    [WARN] 'gpg' binary not found in PATH (required to decrypt pass entries)")
					hasWarnings = true
				} else {
					fmt.Fprintln(stdout, "    [OK] 'gpg' binary available in PATH")
				}
			case "env":
				fmt.Fprintln(stdout, "    [OK] Process environment provider (in-memory)")
			case "custom_vault":
				fmt.Fprintln(stdout, "    [OK] Custom vault (in-memory)")
			case "search":
				fmt.Fprintln(stdout, "    [OK] Search vault (dynamic)")
			case "vault":
				if vault.Address == "" {
					fmt.Fprintln(stdout, "    [ERROR] Missing Vault address")
					hasErrors = true
				} else {
					fmt.Fprintln(stdout, "    [OK] HashiCorp Vault configured (reachability check deferred to runtime)")
				}
			default:
				fmt.Fprintf(stdout, "    [ERROR] Unknown provider %q\n", vault.Provider)
				hasErrors = true
			}
		}
	}

	fmt.Fprintln(stdout, strings.Repeat("-", 40))
	if hasErrors {
		fmt.Fprintln(stdout, "Diagnostics completed with errors.")
		return 1
	}
	if strict && hasWarnings {
		fmt.Fprintln(stdout, "Diagnostics completed with warnings (strict mode enabled).")
		return 1
	}

	fmt.Fprintln(stdout, "Diagnostics completed successfully.")
	return 0
}
