package cmd

import (
	"fmt"
	"os"
)

// PrintUsage prints the general usage instructions to stderr.
func PrintUsage() {
	fmt.Fprintln(os.Stderr, `cloakenv — pluggable secret orchestrator & runtime injector

Usage:
  cloakenv [-c config_path] run [-e KEY=uri ...] [-m entry-uri] [-i KEY ...] -- <command> [args]
  cloakenv [-c config_path] get <uri>
  cloakenv [-c config_path] set <uri> <value> [--ttl <duration>]
  cloakenv [-c config_path] delete <uri>
  cloakenv [-c config_path] cache clear
  cloakenv [-c config_path] show <entry-uri> [args]
  cloakenv [-c config_path] search [query] [args]
  cloakenv [-c config_path] auth <login|forget|status> [vault]

Commands:
  run     Wrap a binary with injected environment variables
  get     Retrieve and print a single secret value raw to stdout (no trailing newline)
  set     Store a secret value at a writable URI (keyring://, cache://)
  delete  Remove a secret from a writable URI (keyring://, cache://)
  cache   Manage local encrypted cache (subcommand: clear)
  show    Retrieve and display a structured entry
  search  Search for structured entries
  auth    Manage vault credentials and status (subcommands: login, forget, status)

Flags:
  -c config_path  Custom configuration file path (global flag)
  -e KEY=uri      Map an environment variable to a secret URI (repeatable)
  -m entry-uri    Merge all attributes from an entry into the environment (repeatable)
  -i KEY          Filter/whitelist keys/variables (repeatable)
  -o, --output    Output format: plain, json, yaml, env (depends on command)
  --vault vault   Scope search to a specific vault (repeatable)
  --ttl duration  Expiration duration for cache entries (e.g. 5m, 1h, set only)

URI schemes:
  keyring://service/account    Built-in: OS keyring (macOS Keychain, Linux D-Bus, Windows Credential Manager)
  env://VARIABLE_NAME          Built-in: read from current process environment (read-only)
  cache://KEY                  Built-in: local file cache (AES-GCM encrypted, key in OS keyring)
  search://query/attribute     Built-in: resolve dynamically matched credentials
  <vault>://Path/To/Entry      Config-defined: resolved via ~/.config/cloakenv/config.yaml`)
}

// PrintUsageStdout prints the general usage instructions to stdout.
func PrintUsageStdout() {
	fmt.Fprintln(os.Stdout, `cloakenv — pluggable secret orchestrator & runtime injector

Usage:
  cloakenv [-c config_path] run [-e KEY=uri ...] [-m entry-uri] [-i KEY ...] -- <command> [args]
  cloakenv [-c config_path] get <uri>
  cloakenv [-c config_path] set <uri> <value> [--ttl <duration>]
  cloakenv [-c config_path] delete <uri>
  cloakenv [-c config_path] cache clear
  cloakenv [-c config_path] show <entry-uri> [args]
  cloakenv [-c config_path] search [query] [args]
  cloakenv [-c config_path] auth <login|forget|status> [vault]

Commands:
  run     Wrap a binary with injected environment variables
  get     Retrieve and print a single secret value raw to stdout (no trailing newline)
  set     Store a secret value at a writable URI (keyring://, cache://)
  delete  Remove a secret from a writable URI (keyring://, cache://)
  cache   Manage local encrypted cache (subcommand: clear)
  show    Retrieve and display a structured entry
  search  Search for structured entries
  auth    Manage vault credentials and status (subcommands: login, forget, status)

Flags:
  -c config_path  Custom configuration file path (global flag)
  -e KEY=uri      Map an environment variable to a secret URI (repeatable)
  -m entry-uri    Merge all attributes from an entry into the environment (repeatable)
  -i KEY          Filter/whitelist keys/variables (repeatable)
  -o, --output    Output format: plain, json, yaml, env (depends on command)
  --vault vault   Scope search to a specific vault (repeatable)
  --ttl duration  Expiration duration for cache entries (e.g. 5m, 1h, set only)

URI schemes:
  keyring://service/account    Built-in: OS keyring (macOS Keychain, Linux D-Bus, Windows Credential Manager)
  env://VARIABLE_NAME          Built-in: read from current process environment (read-only)
  cache://KEY                  Built-in: local file cache (AES-GCM encrypted, key in OS keyring)
  search://query/attribute     Built-in: resolve dynamically matched credentials
  <vault>://Path/To/Entry      Config-defined: resolved via ~/.config/cloakenv/config.yaml`)
}

// PrintRunHelp prints usage help for the run subcommand.
func PrintRunHelp() {
	fmt.Fprintln(os.Stdout, `Usage:
  cloakenv run [-e KEY=uri ...] [-m entry-uri] [-i KEY ...] -- <command> [args]

Description:
  Wrap a binary execution, resolving and injecting secret environment variables.
  If no -- separator is used, any remaining arguments are treated as the command.

Flags:
  -e KEY=uri      Map an environment variable to a secret URI (repeatable)
  -m entry-uri    Merge all attributes from an entry into the environment (repeatable)
  -i KEY          Whitelist filter key (filters only merged -m keys; repeatable)`)
}

// PrintGetHelp prints usage help for the get subcommand.
func PrintGetHelp() {
	fmt.Fprintln(os.Stdout, `Usage:
  cloakenv get <uri>

Description:
  Retrieve and print a single secret value raw to stdout (no trailing newline).

Arguments:
  <uri>           The secret URI to retrieve (e.g., keyring://service/account, env://VAR)`)
}

// PrintSetHelp prints usage help for the set subcommand.
func PrintSetHelp() {
	fmt.Fprintln(os.Stdout, `Usage:
  cloakenv set <uri> <value> [--ttl <duration>]

Description:
  Store a secret value at a writable URI. Currently only 'keyring://' and 'cache://' schemes are writable.

Arguments:
  <uri>           The secret URI where the value will be stored
  <value>         The secret value to write

Flags:
  --ttl duration  Expiration duration for cache entries (e.g. 5m, 1h).
                  Only supported by the 'cache' provider.`)
}

// PrintDeleteHelp prints usage help for the delete subcommand.
func PrintDeleteHelp() {
	fmt.Fprintln(os.Stdout, `Usage:
  cloakenv delete <uri>

Description:
  Remove a secret from a writable URI.

Arguments:
  <uri>           The secret URI to delete from (e.g., keyring://service/account, cache://KEY)`)
}

// PrintCacheHelp prints usage help for the cache subcommand.
func PrintCacheHelp() {
	fmt.Fprintln(os.Stdout, `Usage:
  cloakenv cache clear

Description:
  Manage local encrypted cache.

Subcommands:
  clear           Clear all entries in the local encrypted cache`)
}

// PrintCacheClearHelp prints usage help for the cache clear subcommand.
func PrintCacheClearHelp() {
	fmt.Fprintln(os.Stdout, `Usage:
  cloakenv cache clear

Description:
  Clear all entries in the local encrypted cache.`)
}

// PrintShowHelp prints usage help for the show subcommand.
func PrintShowHelp() {
	fmt.Fprintln(os.Stdout, `Usage:
  cloakenv show <entry-uri> [-m <entry-uri> ...] [-e KEY=uri ...] [-i KEY ...] [-o yaml | json | env]
  cloakenv show -m <entry-uri> [-e KEY=uri ...] [-i KEY ...] [-o yaml | json | env]

Description:
  Retrieve and display a structured entry, or merge multiple entries/explicit mapping values.

Arguments:
  <entry-uri>     The structured entry URI to retrieve

Flags:
  -m <entry-uri>  Merge entry attributes (can be specified multiple times)
  -e KEY=uri      Explicit environment/key override (can be specified multiple times)
  -i KEY          Whitelist filter key (filters only merged -m keys; repeatable)
  -o, --output    Output format: yaml, json, or env (default: yaml)`)
}

// PrintSearchHelp prints usage help for the search subcommand.
func PrintSearchHelp() {
	fmt.Fprintln(os.Stdout, `Usage:
  cloakenv search [query] [--vault <vault> ...] [-i KEY ...] [-o yaml | json]

Description:
  Search for structured entries matching the query.

Arguments:
  [query]         Optional query string to filter entries by title, tags, or fields

Flags:
  --vault vault   Scope search to a specific vault (repeatable)
  -i KEY          Select output fields/keys to return (repeatable)
  -o, --output    Output format: yaml or json (default: yaml)`)
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
  cloakenv auth status [vault]

Description:
  Check if configured vaults are active and accessible.

Arguments:
  [vault]         Optional name of the vault to check`)
}
