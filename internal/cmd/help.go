package cmd

import (
	"fmt"
	"os"
)

const generalUsage = `cloakenv — pluggable secret orchestrator & runtime injector

Usage:
  cloakenv [-c config_path] run [-E] [-e KEY=uri ...] [-t template_path] [-m entry-uri] [-i KEY ...] [-f format] -- <command> [args]
  cloakenv [-c config_path] get <uri>
  cloakenv [-c config_path] set <uri> [--ttl <duration>]
  cloakenv [-c config_path] delete <uri>
  cloakenv [-c config_path] cache <clear|prune>
  cloakenv [-c config_path] sync <source> <target> [flags]
  cloakenv [-c config_path] show <entry-uri> [args]
  cloakenv [-c config_path] search [query] [args]
  cloakenv [-c config_path] auth <login|forget|status> [vault]
  cloakenv [-c config_path] doctor [--strict]
  cloakenv [-c config_path] completion <shell>
  cloakenv version
  cloakenv [-c config_path] internal <subcommand> [args]

Commands:
  run     Wrap a binary with injected environment variables
  get     Retrieve and print a single secret value raw to stdout (no trailing newline)
  set     Store a secret value at a writable URI (keyring://, cache://)
  delete  Remove a secret from a writable URI (keyring://, cache://)
  cache   Manage local encrypted cache (subcommands: clear, prune)
  sync    Unidirectional cross-vault secret synchronization
  show    Retrieve and display a structured entry
  search  Search for structured entries
  auth    Manage vault credentials and status (subcommands: login, forget, status)
  doctor  Diagnose environment, validate config, and check vault reachability
  completion  Generate shell autocompletion script (bash, zsh, fish, powershell)
  version Print build and runtime version information
  internal  Developer & hook helpers (subcommands: match-alias, list-vaults)

Flags:
  -c config_path  Custom configuration file path (global flag)
  -h, --help      Display help information (global flag)
  -v, --version   Print version information (global flag)
  -E              Start with an empty environment (do not inherit from parent)
  -e KEY=uri      Map an environment variable to a secret URI (repeatable)
  -t template     Load template .env file mapping KEY=uri per line (repeatable)
  -m entry-uri    Merge attributes from an entry into the environment. Can target a single attribute using :attribute (repeatable)
  -i KEY          Filter/whitelist keys or select output fields (repeatable)
  -f, --key-format format  Override attribute key formatting (none, env, snake, kebab)
  -o, --output    Output format: keys, json, yaml, env, text (depends on command)
  --vault vault   Scope search to a specific vault (repeatable)
  --ttl duration  Expiration duration for cache entries (e.g. 5m, 1h, set only)

URI schemes:
  keyring://service/account    Built-in: OS keyring (macOS Keychain, Linux D-Bus, Windows Credential Manager)
  env://VARIABLE_NAME          Built-in: read from current process environment (read-only)
  cache://KEY                  Built-in: local file cache (AES-GCM encrypted, key in OS keyring)
  pass://path/to/entry[:attr]  Built-in: Unix password store (read-only)
  search://query/attribute     Built-in: resolve dynamically matched credentials
  <vault>://Path/To/Entry      Config-defined: resolved via ~/.config/cloakenv/config.yaml`

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
  cloakenv run [-E] [-e KEY=uri ...] [-t template_path] [-m entry-uri] [-i KEY ...] [-f format] [--no-autoload] [--dry-run] -- <command> [args]

Description:
  Wrap a binary execution, resolving and injecting secret environment variables.
  If no -- separator is used, any remaining arguments are treated as the command.
  Config autoload rules defined in ~/.config/cloakenv/config.yaml matching <command> will be evaluated automatically.

Flags:
  -E              Start with an empty environment (do not inherit from parent)
  -e KEY=uri      Map an environment variable to a secret URI (repeatable)
  -t template     Load template .env file mapping KEY=uri per line (repeatable)
  -m entry-uri    Merge attributes from an entry into the environment. Can target a single attribute using :attribute (repeatable)
  -i KEY          Whitelist filter key (filters only merged -m keys; repeatable)
  -f, --key-format format  Override attribute key formatting (none, env, snake, kebab)
  --no-autoload   Disable config command autoloading rules for this run execution
  --dry-run       Preview injected environment variable names and transformed command without executing`)
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
  cloakenv set <uri> [--ttl <duration>]

Description:
  Store a secret value at a writable URI. Currently only 'keyring://' and 'cache://' schemes are writable.
  When run in an interactive terminal, prompts for masked input with confirmation; otherwise reads from standard input (stdin).

Arguments:
  <uri>           The secret URI where the value will be stored

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

// PrintShowHelp prints usage help for the show subcommand.
func PrintShowHelp() {
	fmt.Fprintln(os.Stdout, `Usage:
  cloakenv show <entry-uri> [-f format] [-o yaml | json | env | keys] [--raw] [--explain] [--show-meta]
   or: cloakenv show -m <entry-uri> [-e KEY=uri ...] [-t template_path] [-i KEY ...] [-f format] [-o yaml | json | env | keys] [--raw] [--explain] [--show-meta]

Description:
  Retrieve and display a structured entry, or merge multiple entries/explicit mapping values.
  KeePass entries expose virtual metadata attributes (:UUID/:uuid, :group/:group_name, :group_uuid, :group_path) and fields (uuid, group, group_name, group_uuid, group_path).

Arguments:
  <entry-uri>     The structured entry URI to retrieve (e.g. keepass://website/Test Website, work://website/Test Website:UUID)

Flags:
  -m entry-uri    Merge attributes from an entry. Can target a single attribute using :attribute (repeatable)
  -e KEY=uri      Explicit environment/key override (can be specified multiple times)
  -t template     Load template .env file mapping KEY=uri per line (repeatable)
  -i KEY          Whitelist filter key (filters only merged -m keys; repeatable)
  -f, --key-format format  Override attribute key formatting (none, env, snake, kebab)
  --raw           Display unmodified raw entry attributes before filtering, mapping, formatting, and computed attributes
  --explain       Display structured transformation trace across all 5 entry processing pipeline stages (yaml or json only)
  --show-meta     Include vault, provider, and tags metadata in a _meta_ object (yaml and json output only)
  -o, --output    Output format: yaml, json, env, or keys (default: yaml)`)
}

// PrintSearchHelp prints usage help for the search subcommand.
func PrintSearchHelp() {
	fmt.Fprintln(os.Stdout, `Usage:
  cloakenv search [query] [--vault <vault> ...] [-i KEY ...] [-o yaml | json] [-f format] [--show-meta]

Description:
  Search for structured entries matching the query (supporting query expressions on title, tags, _meta_.tags, uuid, group.name, group.path, etc.).

Arguments:
  [query]         Optional query string to filter entries by title, tags, uuid, group, or fields

Flags:
  --vault vault   Scope search to a specific vault (repeatable)
  -i KEY          Select output fields/keys to return (e.g. uuid, group_name, group_uuid, group_path) (repeatable)
  -f, --key-format format  Override attribute key formatting (none, env, snake, kebab)
  --show-meta     Include vault, provider, and tags metadata in a _meta_ object (yaml and json output only)
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
  cloakenv auth status [vault] [-o text | json | yaml]

Description:
  Check if configured vaults are active and accessible.

Arguments:
  [vault]         Optional name of the vault to check

Flags:
  -o, --output    Output format: text, json, or yaml (default: text)`)
}

// PrintSyncHelp prints usage help for the sync subcommand.
func PrintSyncHelp() {
	fmt.Fprintln(os.Stdout, `Usage:
  cloakenv sync <source> <target> [--dry-run] [--tags <tags>] [--prefix <prefix>] [--regex <pattern>]

Description:
  Unidirectional cross-vault secret synchronization from a source vault/URI to a destination writable vault.
  All synchronization operations occur strictly in memory with zero secret leakage.

Arguments:
  <source>        Source vault URI or entry URI (e.g. work://, keepass://website/Test Website, env://)
  <target>        Destination writable vault URI (e.g. keyring://my_service, cache://sync)

Flags:
  --dry-run       Preview additions, updates, and skips without writing
  --tags tags     Filter source entries by tag (comma-separated, e.g. sync:keyring)
  --prefix prefix Filter keys by prefix
  -r, --regex pat Filter keys by regular expression`)
}

// PrintInternalHelp prints usage help for the internal subcommand.
// PrintDoctorHelp prints usage help for the doctor subcommand.
func PrintDoctorHelp() {
	PrintDoctorHelpTo(os.Stdout)
}

func PrintInternalHelp() {
	fmt.Fprintln(os.Stdout, `Usage:
  cloakenv internal <subcommand> [args]

Description:
  Internal helper commands for cloakenv integrations.

Subcommands:
  match-alias     Check if a command matches any configured autoload/run alias rule
  list-vaults     List configured vault names`)
}
