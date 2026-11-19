package cmd

import (
	"fmt"
	"io"
	"os"
)

const generalUsage = `cloakenv — pluggable secret orchestrator & runtime injector

Usage:
  cloakenv [-c config_path] run [-E] [--no-autoload] [--dry-run] -- <command> [args]
  cloakenv [-c config_path] inspect [env <name> | template <name> | run -- <command> [args]] [-o text|json|yaml]
  cloakenv [-c config_path] auth <login|forget|status> [vault]
  cloakenv [-c config_path] cache <clear|prune>
  cloakenv [-c config_path] doctor [--strict]
  cloakenv [-c config_path] completion <shell>
  cloakenv version
  cloakenv [-c config_path] internal <subcommand> [args]

Commands:
  run         Wrap a binary with config-governed injected environment variables and templates
  inspect     Preview matched vaults, envs, templates, and autoload rules (always <REDACTED>)
  auth        Manage vault credentials and status (subcommands: login, forget, status)
  cache       Manage local encrypted cache (subcommands: clear, prune)
  doctor      Diagnose environment, validate config, and check vault reachability
  completion  Generate shell autocompletion script (bash, zsh, fish, powershell)
  version     Print build and runtime version information
  internal    Developer & hook helpers (subcommands: match-alias, list-vaults)

Flags:
  -c config_path  Custom configuration file path (global flag)
  -h, --help      Display help information (global flag)
  -v, --version   Print version information (global flag)
  -E              Start with an empty environment (run only)
  -o, --output    Output format: text, json, yaml (inspect, auth status)`

// PrintUsage prints the general usage instructions to stderr.
func PrintUsage() {
	fmt.Fprintln(os.Stderr, generalUsage)
}

// PrintUsageStdout prints the general usage instructions to stdout.
func PrintUsageStdout() {
	fmt.Fprintln(os.Stdout, generalUsage)
}

// PrintRunHelp prints usage help for the run subcommand.
func PrintRunHelp() {
	fmt.Fprintln(os.Stdout, `Usage:
  cloakenv run [-E] [--no-autoload] [--dry-run] -- <command> [args]

Description:
  Wrap a binary execution, resolving and injecting secret environment variables
  and rendered templates strictly from matching autoload rules in config.yaml.
  If no -- separator is used, any remaining arguments are treated as the command.

Flags:
  -E              Start with an empty environment (do not inherit from parent)
  --no-autoload   Disable config command autoloading rules for this run execution
  --dry-run       Preview injected environment variable names, templates, and command without executing`)
}

// PrintInspectHelp prints usage help for the inspect subcommand to stdout.
func PrintInspectHelp() {
	PrintInspectHelpTo(os.Stdout)
}

// PrintInspectHelpTo prints usage help for the inspect subcommand to w.
func PrintInspectHelpTo(w io.Writer) {
	fmt.Fprintln(w, `Usage:
  cloakenv inspect [-o text|json|yaml]
  cloakenv inspect env <name> [-o text|json|yaml]
  cloakenv inspect template <name> [-o text|json|yaml]
  cloakenv inspect run -- <command> [args] [-o text|json|yaml]

Description:
  Evaluate config.yaml queries, rules, and autoload matching to preview matched
  entries, generated keys, template drivers, and delivery targets.
  All secret values are unconditionally replaced with <REDACTED>.

Flags:
  -o, --output format  Output format: text, json, or yaml (default: text)`)
}

// PrintCacheHelp prints usage help for the cache subcommand.
func PrintCacheHelp() {
	fmt.Fprintln(os.Stdout, `Usage:
  cloakenv cache <clear|prune>

Description:
  Manage local encrypted cache.

Subcommands:
  clear           Clear all entries in the local encrypted cache
  prune           Remove expired entries from the local encrypted cache`)
}

// PrintCacheClearHelp prints usage help for the cache clear subcommand.
func PrintCacheClearHelp() {
	fmt.Fprintln(os.Stdout, `Usage:
  cloakenv cache clear

Description:
  Clear all entries in the local encrypted cache.`)
}

// PrintCachePruneHelp prints usage help for the cache prune subcommand.
func PrintCachePruneHelp() {
	fmt.Fprintln(os.Stdout, `Usage:
  cloakenv cache prune

Description:
  Remove expired entries from the local encrypted cache.`)
}

// PrintAuthHelp prints usage help for the auth subcommand.
func PrintAuthHelp() {
	fmt.Fprintln(os.Stdout, `Usage:
  cloakenv auth <login|forget|status> [vault]

Description:
  Manage vault authentication and status.

Subcommands:
  login           Authenticate and save credentials for a vault
  forget          Clear credentials for a vault
  status          Check if configured vaults are active and accessible`)
}

// PrintAuthLoginHelp prints usage help for the auth login subcommand.
func PrintAuthLoginHelp() {
	fmt.Fprintln(os.Stdout, `Usage:
  cloakenv auth login <vault>

Description:
  Authenticate and save credentials for a vault.

Arguments:
  <vault>         The name of the vault (e.g., work, home)`)
}

// PrintAuthForgetHelp prints usage help for the auth forget subcommand.
func PrintAuthForgetHelp() {
	fmt.Fprintln(os.Stdout, `Usage:
  cloakenv auth forget <vault>

Description:
  Clear credentials for a vault.

Arguments:
  <vault>         The name of the vault (e.g., work, home)`)
}

// PrintAuthStatusHelp prints usage help for the auth status subcommand.
func PrintAuthStatusHelp() {
	fmt.Fprintln(os.Stdout, `Usage:
  cloakenv auth status [vault] [-o text | json | yaml]

Description:
  Check if configured vaults are active and accessible.

Arguments:
  [vault]         Optional name of the vault to check

Flags:
  -o, --output    Output format: text, json, or yaml (default: text)`)
}

// PrintDoctorHelp prints usage help for the doctor subcommand.
func PrintDoctorHelp() {
	PrintDoctorHelpTo(os.Stdout)
}

// PrintInternalHelp prints usage help for the internal subcommand.
func PrintInternalHelp() {
	fmt.Fprintln(os.Stdout, `Usage:
  cloakenv internal <subcommand> [args]

Description:
  Internal helper commands for cloakenv integrations.

Subcommands:
  match-alias     Check if a command matches any configured autoload/run alias rule
  list-vaults     List configured vault names`)
}
