# cloakenv

[![Go Report Card](https://goreportcard.com/badge/github.com/warpcode/cloakenv)](https://goreportcard.com/report/github.com/warpcode/cloakenv)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

**Pluggable secret orchestrator and dynamic runtime environment injector.**

`cloakenv` eliminates plaintext secret sprawl. Instead of writing credentials to unencrypted `.env` files or committing secrets to git, `cloakenv` resolves secret URIs on demand from multiple backends (KeePass, OS Keyring, encrypted cache, YAML, JSON, environment) and injects them strictly into temporary execution memory right when your command runs.

---

## 📑 Table of Contents

- [Why cloakenv?](#why-cloakenv)
- [How It Works](#how-it-works)
- [Prerequisites](#prerequisites)
- [Installation](#installation)
- [Quick Start (Zero to Running in 2 Minutes)](#quick-start-zero-to-running-in-2-minutes)
- [Core Concepts](#core-concepts)
  - [URI Addressing](#uri-addressing)
  - [Template & Secret Expansion Syntax (`{{ ... }}`)](#template--secret-expansion-syntax-)
  - [Vault Key Formatting (`key_format`)](#vault-key-formatting-key_format)
- [CLI Command Reference](#cli-command-reference)
  - [`run` — Wrap Processes & Inject Secrets](#run--wrap-processes--inject-secrets)
  - [`get` — Extract Single Secret Raw to Stdout](#get--extract-single-secret-raw-to-stdout)
  - [`set` — Write to Keyring or Cache](#set--write-to-keyring-or-cache)
  - [`delete` — Remove from Keyring or Cache](#delete--remove-from-keyring-or-cache)
  - [`cache` — Local Encrypted Cache Management](#cache--local-encrypted-cache-management)
  - [`show` — Inspect & Export Structured Records](#show--inspect--export-structured-records)
  - [`search` — Dynamic Querying Across Vaults](#search--dynamic-querying-across-vaults)
  - [`auth` — Manage Vault Authentication](#auth--manage-vault-authentication)
  - [`internal` — Developer & Hook Helpers](#internal--developer--hook-helpers)
- [Secret Providers & Schemes](#secret-providers--schemes)
  - [Built-In Providers (Zero Setup)](#built-in-providers-zero-setup)
  - [Configured Vaults](#configured-vaults)
    - [Custom Vault (`custom_vault`)](#3-custom-vault-custom_vault)
    - [Universal Per-Entry Attributes Decorator (`attributes`)](#8-universal-per-entry-attributes-decorator-attributes)
    - [KeePass Placeholders & Cross-Entry References (`resolve_references`)](#9-keepass-placeholders--cross-entry-references-resolve_references)
- [Template Engine & FuncMap Reference](#template-engine--funcmap-reference)
- [Command Autoloading & Alias Masking](#command-autoloading--alias-masking)
- [Search Query Language Reference](#search-query-language-reference)
- [Configuration Reference (`config.yaml`)](#configuration-reference-configyaml)
- [Headless & CI/CD Usage](#headless--cicd-usage)
- [Development & Testing](#development--testing)

---

## Why cloakenv?

Managing application credentials usually forces bad trade-offs:
- **Plaintext `.env` files**: Vulnerable to accidental git commits, log file leaks, and disk snooping.
- **Multiple password managers & stores**: Secrets scattered across KeePass `.kdbx` files, OS keychains, cloud vaults, and config files with no unified access pattern.
- **Verbose boilerplate**: Writing custom scripts just to decrypt a password and export it as an environment variable before starting a service.

`cloakenv` provides a single, uniform abstraction:
- **Zero Disk Persistence**: Secrets are decrypted into process memory only for the duration of the child process.
- **Universal URI Scheme**: Access any backend with the same syntax (`keyring://...`, `keepass://...`, `yaml://...`, `env://...`, `cache://...`).
- **Template Hydration**: Inject whole `.env` templates containing `{{ ... }}` secret expansions and Go templates on the fly.
- **Command Autoloading**: Automatically attach the right credentials whenever specific CLI commands (like `aws`, `kubectl`, or custom tools) run.
- **Cross-Platform**: Full support for Linux, macOS, and Windows.

---

## How It Works

```mermaid
flowchart TD
    subgraph CLI ["1. You run a command wrapped with cloakenv"]
        Cmd["cloakenv run -e DB_PASS='{{ get \"keyring://db/prod\" }}' -- ./my-app"]
    end

    subgraph Engine ["2. cloakenv Orchestrator"]
        Router["URI Router & Template Parser"]
        AuthCheck["Keyring / Cache Auth"]
    end

    subgraph Backends ["3. Pluggable Secret Providers"]
        OSKeyring["OS Keyring (Keychain / Secret Service / Credential Mgr)"]
        KeePass["KeePass .kdbx (AES Decryption)"]
        EncCache["Local AES-256 GCM Cache"]
        YAML_JSON["Static YAML / JSON Vaults"]
        Env["Process Environment"]
    end

    subgraph ChildProcess ["4. Target Application Execution"]
        Memory["Process Memory: DB_PASS=supersecret ./my-app"]
    end

    Cmd --> Router
    Router --> AuthCheck
    AuthCheck --> Backends
    Backends --> Router
    Router --> Memory
```

---

## Prerequisites

- [Go](https://go.dev/) 1.26.2 or higher.
- A supported operating system credential store:
  - **macOS**: Keychain (built-in)
  - **Linux**: D-Bus Secret Service (`gnome-keyring`, `ksecretsservice`, or `keepassxc`)
  - **Windows**: Windows Credential Manager (built-in)

---

## Installation

### Using `go install` (Recommended)

```bash
go install github.com/warpcode/cloakenv@latest
```

### Direct Execution with `go run`

```bash
go run github.com/warpcode/cloakenv@latest --help
```

### Building from Source

```bash
git clone https://github.com/warpcode/cloakenv.git
cd cloakenv
make build
# Binary is compiled to bin/cloakenv
```

---

## Quick Start (Zero to Running in 2 Minutes)

You can start using `cloakenv` immediately with the built-in providers—**no configuration file required**!

### 1. Store a secret securely in your OS Keyring

```bash
# Store an API key in your OS keyring (reads securely from stdin or interactive prompt)
echo -n "sk-proj-1234567890abcdef" | cloakenv set keyring://openai/dev_key
```

### 2. Retrieve a secret for a script or subshell

```bash
# Retrieve raw value directly to stdout (no trailing newline)
cloakenv get keyring://openai/dev_key
```

### 3. Run an application with injected secrets

```bash
# Injects OPENAI_API_KEY into the Python script's memory
cloakenv run -e OPENAI_API_KEY=keyring://openai/dev_key -- python app.py
```

`app.py` reads `os.environ["OPENAI_API_KEY"]` normally. When `app.py` exits, the secret is gone from memory, and nothing was ever written to disk!

---

## Core Concepts

### URI Addressing

Every secret is referenced through a typed URI formatted as:

$$\text{scheme://path/to/entry[:attribute]}$$

- **Scheme**: Identifies either a built-in provider (`keyring`, `env`, `cache`, `search`) or a user-defined vault name from `config.yaml` (`work`, `home`, `staging_db`).
- **Path**: Location of the credential within that provider (e.g. `service/account` for keyrings, `Group/Entry` for KeePass, `key.subkey` for JSON/YAML).
- **Attribute** *(optional)*: Specific field to retrieve (e.g., `:Password`, `:UserName`, `:api_token`, or `:attachment.txt`). Defaults to `:Password` if omitted for KeePass and custom vaults.

Examples:
```text
keyring://github/personal_access_token
env://CI_JOB_TOKEN
cache://session_token
my_vault://Production/Database:Password
my_vault://Production/Database:UserName
my_vault://Production/SSH_Keys:id_rsa.pub
json_db://servers.bastion.hostname
```

---

### Template & Secret Expansion Syntax (`{{ ... }}`)

`cloakenv` provides a unified templating engine powered by Go's standard library `text/template` with `.Option("missingkey=error")`. When `resolve_values: true` is configured or when evaluating template contexts, secret URIs and expressions can be embedded directly into connection strings, URLs, configuration files, and attribute templates:

```bash
cloakenv run -e DATABASE_URL='postgres://{{ get "env://DB_USER" }}:{{ get "keyring://pg/password" }}@localhost:5432/mydb' -- ./server
```

#### Core Template Mechanics:
1. **Unified Go `text/template` Engine**: Expressions use standard Go template syntax `{{ ... }}` with strict key checking (`missingkey=error`). Referencing an undefined attribute or field aborts with a descriptive error instead of outputting `<no value>`.
2. **Explicit Secret Retrieval**: Use `{{ get "scheme://..." }}` to fetch a secret string from any vault or built-in provider.
3. **Structured Context (`.`)**: Attribute and composed vault templates receive a structured dot context containing:
   - **`.meta` / `.Meta`**: Entry metadata map (`Title`/`title`, `Tags`/`tags`, `UUID`/`uuid`, `Vault`/`vault`, `Provider`/`provider`, `Path`/`path`, `Group`/`group`).
   - **`.attributes` / `.Attributes`**: Entry attributes after field filtering, mapping, and key formatting.
   - **`.entries` / `.Entries`**: Slice of upstream source entry contexts when resolving composed `custom_vault` entities.
4. **Child Command Argument Pass-Through Safety (`resolveCmdArgs`)**:
   When `cloakenv run` executes child processes, command-line arguments are safely inspected:
   - Arguments that are bare known `scheme://...` URIs or templates invoking `cloakenv` functions (`get`, `entry`, `search`, `include`, `includeFile`) are resolved before process execution.
   - Foreign templates (such as Docker format strings `'{{.State.Status}}'`, Helm templates, or Kubernetes format strings) pass through to the child command untouched without evaluation errors.
5. **Recursion Limit**: Dynamic resolution and template inclusion are capped at depth 5 (`maxDepth = 5`) to prevent circular evaluation or infinite recursion.
6. **Legacy `${...}` Removed**: The legacy `${scheme://...}` syntax has been completely removed in favor of `{{ get "scheme://..." }}`. Bare `scheme://...` URIs remain supported in explicit CLI contexts (`get`, `show`, `-e`, `-m`), `merge:`, and `autoload.env`.

---

### Vault Key Formatting (`key_format`)

By default (`none`), `cloakenv` passes through original attribute key names unmodified as stored in the vault backend.

Per vault, you can configure `key_format` in `config.yaml` to standardize attribute keys:
- **`none`** (default): Passes through the original key name unmodified.
- **`env`**: Uppercase with non-alphanumeric runs collapsed to a single underscore (`_`).
- **`snake`**: Lowercase with non-alphanumeric runs collapsed to a single underscore (`_`).
- **`kebab`**: Lowercase with non-alphanumeric runs collapsed to a single hyphen (`-`).

| Original Attribute Name | `none` (Default) | `env` | `snake` | `kebab` |
|---|---|---|---|---|
| `api-key` | `api-key` | `API_KEY` | `api_key` | `api-key` |
| `database.user_name` | `database.user_name` | `DATABASE_USER_NAME` | `database_user_name` | `database-user-name` |
| `UserName` | `UserName` | `USERNAME` | `username` | `username` |
| `custom---token` | `custom---token` | `CUSTOM_TOKEN` | `custom_token` | `custom-token` |

---

## CLI Command Reference

Global flags:
- `-c <config_path>`: Path to custom configuration file (defaults to `~/.config/cloakenv/config.yaml`).
- `-h`, `--help`: Display usage instructions.

```
Usage:
  cloakenv [-c config_path] <command> [flags] [args]

Commands:
  run     Wrap a binary with injected environment variables
  get     Retrieve and print a single secret value raw to stdout
  set     Store a secret value at a writable URI (keyring://, cache://)
  delete  Remove a secret from a writable URI (keyring://, cache://)
  cache   Manage local encrypted cache (subcommand: clear)
  show    Retrieve and display a structured entry
  search  Search for structured entries across vaults
  auth    Manage vault credentials and status (login, forget, status)
  internal  Developer & hook helpers (match-alias)
```

---

### `run` — Wrap Processes & Inject Secrets

Wraps and executes a target command, injecting resolved environment variables directly into its process environment.

```bash
cloakenv run [-E] [flags] -- <command> [args...]
```

> [!TIP]
> If the `--` separator is omitted, all remaining positional arguments are treated as the command.

#### Flags:
- `-E`: Start with an empty environment (do not inherit from parent process). Useful for wrapping untrusted processes or preventing secret leakage from the parent shell.
- `-e KEY=uri`: Explicitly map an environment variable to a secret URI (repeatable).
- `-t template_path`: Load a `.env` template file containing `KEY=uri` or literal definitions (repeatable).
- `-m entry-uri`: Merge all attributes from a structured entry into the environment (repeatable). Can target a single attribute using `:attribute`.
- `-i KEY`: Whitelist key filter (repeatable). When `-i` is specified, only the whitelisted keys from `-m` merges are injected. (`-e` mappings always bypass whitelist filtering).
- `--no-autoload`: Bypass configuration `autoload:` rules for this execution.

#### Examples:

```bash
# 1. Map single environment variables explicitly
cloakenv run -e API_KEY=keyring://stripe/secret_key -- node server.js

# 2. Merge an entire KeePass entry (injects all attributes)
cloakenv run -m "work://Production/PostgreSQL" -- ./migrate-db

# 3. Merge an entry but whitelist only specific fields
cloakenv run -m "work://Production/PostgreSQL" -i USERNAME -i PASSWORD -- ./migrate-db

# 4. Load a template .env file
cloakenv run -t ./deploy.env -- terraform apply

# 5. Combine template, merge, and explicit overrides
cloakenv run \
  -t .env.template \
  -m "work://Cloud/AWS_Production" \
  -e REGION="us-east-1" \
  -- aws s3 ls
```

---

### `get` — Extract Single Secret Raw to Stdout

Resolves and outputs a single secret value raw to standard output with **no trailing newline**. Ideal for piping into clipboards, shells, and other CLI utilities.

```bash
cloakenv get <uri>
```

#### Examples:

```bash
# Copy secret directly to macOS clipboard
cloakenv get keyring://github/token | pbcopy

# Capture into shell variable in a bash script
export GITHUB_TOKEN=$(cloakenv get keyring://github/token)

# Retrieve a specific KeePass attribute
cloakenv get "work://Servers/Bastion:UserName"

# Stream a KeePass file attachment (e.g. private key or certificate)
cloakenv get "work://Infrastructure/SSH:id_rsa" > ~/.ssh/id_rsa_temp
```

---

### `set` — Write to Keyring or Cache

Writes a secret to a writable provider (`keyring://` or `cache://`). The secret value is read securely from standard input (stdin) or an interactive masked prompt, preventing exposure in shell history or process lists.

```bash
cloakenv set <uri> [--ttl <duration>]
```

- The secret value is securely read from standard input (stdin) or prompted interactively if running in a terminal.
- `--ttl <duration>`: Optional Time-To-Live expiration (e.g. `5m`, `1h`, `24h`) for `cache://` secrets.

#### Examples:

```bash
# 1. Interactive masked terminal prompt (no secret in bash history!)
cloakenv set keyring://aws/secret_access_key

# 2. Pipe secret from another CLI tool or file
cat ~/.my_token | cloakenv set keyring://app/token

# 3. Store a temporary session token in cache for 30 minutes
echo -n "temp_jwt_token_xyz" | cloakenv set cache://session_token --ttl 30m
```

---

### `delete` — Remove from Keyring or Cache

Deletes a secret from a writable backend (`keyring://` or `cache://`).

```bash
cloakenv delete <uri>
```

#### Examples:

```bash
cloakenv delete keyring://aws/secret_access_key
cloakenv delete cache://session_token
```

---

### `cache` — Local Encrypted Cache Management

`cloakenv` provides a local, file-based encrypted cache (`cache://`). Cached files are encrypted on disk with **AES-256-GCM**, using an encryption key generated and stored inside your native OS keyring.

```bash
# Clear all cached items immediately
cloakenv cache clear
```

---

### `show` — Inspect & Export Structured Records

Retrieves and formats a structured credential record (or multiple merged records).
KeePass entries include `uuid` and `group` (`name`, `uuid`, `path`) in search results and `show -o yaml|json` outputs, and support selection via `-i uuid`, `-i group_name`, `-i group_uuid`, `-i group_path`.

```bash
cloakenv show <entry-uri> [flags]
# or
cloakenv show -m <entry-uri> [-m <entry-uri> ...] [flags]
```

#### Flags:
- `-o, --output`: Output format: `yaml` (default), `json`, `env` (dotenv), or `keys`.
- `-m entry-uri`: Merge additional entries (repeatable).
- `-e KEY=uri`: Explicit key override (repeatable).
- `-t template_path`: Load template `.env` file (repeatable).
- `-i KEY`: Whitelist filter keys (repeatable).

#### Output Formats:
1. **`yaml`** (Default): Formatted YAML document.
2. **`json`**: Formatted, indented JSON object.
3. **`env`**: Valid dotenv formatted lines with appropriate escaping and quotes (`KEY="value"`).
4. **`keys`**: Plain list of exported environment variable names (one per line).

#### Examples:

```bash
# Inspect KeePass record as YAML
cloakenv show "work://Production/Database"

# Export record as a .env file
cloakenv show "work://Production/Database" -o env > .env.local

# View all available key names in an entry
cloakenv show "work://Production/Database" -o keys

# Merge multiple sources and export as JSON
cloakenv show \
  -m "work://Production/Database" \
  -e DB_PORT="5432" \
  -o json
```

---

### `search` — Dynamic Querying Across Vaults

Searches for structured entries across all configured searchable vaults using Go `expr` query syntax. `uuid` and `group` (`name`, `uuid`, `path`) are included in search results, can be selected via `-i uuid`, `-i group_name`, `-i group_uuid`, `-i group_path`, and can be queried in search expressions (e.g. `uuid == '...'`, `group.name == '...'`, `group.path == '...'`).

```bash
cloakenv search "[query_expression]" [flags]
```

#### Flags:
- `--vault <vault_name>`: Scope the search to one or more specific vaults (repeatable).
- `-i KEY`: Select only specific attributes/fields in the output (repeatable).
- `-o, --output`: Output format: `yaml` (default) or `json`.

#### Examples:

```bash
# Search for entries with a specific tag
cloakenv search '"auth:ssh" in tags'

# Search for RSA keys with 4096-bit strength
cloakenv search 'bit_strength == 4096'

# Search by title prefix across a specific vault
cloakenv search 'title startsWith "Staging"' --vault work

# Return only the hostnames and usernames from matching entries
cloakenv search '"env:prod" in tags' -i title -i hostname -i username -o yaml
```

---

### `auth` — Manage Vault Authentication

Manages master passwords and access verification for vaults requiring credentials (such as KeePass databases).

```bash
cloakenv auth <login|forget|status> [vault_name]
```

- **`login <vault>`**: Prompts for the vault master password and securely stores it in your OS Keyring under the `cloakenv` service prefix.
- **`forget <vault>`**: Clears the saved master password from the OS Keyring.
- **`status [vault]`**: Tests if configured vaults are currently unlocked and accessible.

#### Examples:

```bash
# 1. Unlock your work KeePass database once
cloakenv auth login work

# 2. Verify all vaults are active
cloakenv auth status
# Output:
# work: ACTIVE
# home: ACTIVE

# 3. Clear credentials when locking down
cloakenv auth forget work
```

---

### `internal` — Developer & Hook Helpers

Internal helper commands for scripting and automation. These are not intended for direct end-user usage.

```
Usage:
  cloakenv internal <subcommand> [args]
```

#### `match-alias` — Check Autoload Rule Matches

Tests whether a command matches any configured `autoload:` rule without resolving secrets or triggering side effects. Useful for external shell hooks that need to detect whether `cloakenv run` would apply autoload rules to a given command.

```bash
cloakenv internal match-alias [--json] -- <command> [args]
```

- Exits with code **0** if a match is found, **1** if no match.
- `--json`: Output structured JSON with the matched rule details (`match`, `command`, `vaults`, `merge`, `env`, `whitelist`).

#### Examples:

```bash
# Check if "aws s3 ls" matches any autoload rule
cloakenv internal match-alias -- aws s3 ls

# Get structured JSON output for scripting
cloakenv internal match-alias --json -- kubectl get pods
```

---

## Secret Providers & Schemes

### Built-In Providers (Zero Setup)

These providers are always available and do not require registration in `config.yaml`:

| URI Scheme | Type | Description |
|---|---|---|
| `keyring://service/account` | Read / Write | OS native secure credential store (macOS Keychain, Linux D-Bus, Windows Credential Manager). |
| `env://VAR_NAME` | Read-Only | Values from the parent process environment. Great for CI/CD pipelines. |
| `cache://KEY` | Read / Write | Local AES-256-GCM encrypted filesystem cache with optional TTL. Encryption key is in OS Keyring. |
| `search://query/attribute` | Read-Only | Dynamic URI resolution! Executes a search query on the fly and retrieves the matching attribute. |

#### Dynamic `search://` URI Scheme Example:

```bash
# Dynamically finds the entry with tags "env:prod" and "service:redis" and extracts its Password
cloakenv get "search://tags=env:prod,service:redis&title=Cluster/Password"
```

---

### Configured Vaults

Configure external databases in `~/.config/cloakenv/config.yaml`:

```yaml
vaults:
  work:
    provider: "keepass"
    vault_path: "~/secrets/work.kdbx"

  infra_yaml:
    provider: "yaml"
    vault_path: "~/secrets/infra.yaml"
    entities_root_key: "entries"

  hosts_json:
    provider: "json"
    vault_path: "~/secrets/hosts.json"
    entities_root_key: "hosts"

  static_vault:
    provider: "custom_vault"
    entities:
      database:
        username: "postgres"
        Password: '{{ get "keyring://db/prod_password" }}'
```

#### 1. KeePass (`keepass`)
- Points to any KeePass 2.x `.kdbx` file.
- Path traversal: `vault://Folder/SubFolder/EntryName[:Attribute]`.
- Defaults to `:Password` attribute if omitted.
- **Virtual Metadata Attributes**: KeePass entries expose virtual metadata attributes via `GetSecret` (e.g. `work://website/Test Website:UUID`, `work://website/Test Website:uuid`, `work://website/Test Website:group`, `work://website/Test Website:group_name`, `work://website/Test Website:group_uuid`, `work://website/Test Website:group_path`). Custom string fields take precedence over virtual metadata attributes.
- **File Attachments**: Directly stream binary attachments by name (e.g. `vault://SSH/Keys:id_rsa`).
- **Placeholder & Reference Resolution**: Set `resolve_references: true` in `config.yaml` to enable `{TITLE}`, `{REF:...}`, `{T-CONV:...}`, `{URL:...}`, and date/time expansion (see [KeePass Placeholders & Cross-Entry References](#9-keepass-placeholders--cross-entry-references-resolve_references)).

##### Placeholder Expansion & Field References
`cloakenv` supports resolving field placeholders and cross-entry references embedded inside KeePass field values:

1. **Standard KeePass Placeholders** (Upstream Compatible):
   - **Standard Fields**: `{TITLE}`, `{USERNAME}`, `{PASSWORD}`, `{URL}`, `{NOTES}`.
   - **Custom String Attributes**: `{S:<AttributeName>}` (e.g., `{S:api_key}`).
   - **Date/Time Placeholders**: Local (`{DT_SIMPLE}`, `{DT_YEAR}`, `{DT_MONTH}`, `{DT_DAY}`, `{DT_HOUR}`, `{DT_MINUTE}`, `{DT_SECOND}`) and UTC (`{DT_UTC_SIMPLE}`, `{DT_UTC_YEAR}`, `{DT_UTC_MONTH}`, `{DT_UTC_DAY}`, `{DT_UTC_HOUR}`, `{DT_UTC_MINUTE}`, `{DT_UTC_SECOND}`).
   - **Cross-Entry References**: `{REF:<WantedField>@<SearchIn>:<SearchText>}` where `WantedField` uses single-letter codes (`T` Title, `U` Username, `P` Password, `A` URL, `N` Notes) or `{REF:S:<custom>@...}`. `SearchIn` can be `I` (UUID), `T`, `U`, `P`, `A`, `N`, or `O` (Custom/Other fields).

2. **`cloakenv` Placeholder Extensions** (Divergence from Upstream KeePass):
   - **Text Transformation**: `{T-CONV:<sep><text><sep><method><sep>}` (e.g., `{T-CONV:/{USERNAME}/UPPER/}`). The character following `{T-CONV:` acts as a configurable delimiter (such as `/` or `!`). Supported methods include `UPPER` (`U`), `LOWER` (`L`), `BASE64`, `HEX`, `URI`, `URI-DEC`, and `RAW`.
   - **Same-Entry URL Component Extraction**: `{URL:<component>}` (e.g., `{URL:HOST}`, `{URL:PORT}`, `{URL:SCHEME}`, `{URL:PATH}`).
   - **Cross-Entry URL Component Extraction**: `{REF:A:<component>@<SearchIn>:<SearchText>}` or `{REF:URL:<component>@<SearchIn>:<SearchText>}` (e.g., `{REF:A:HOST@T:DatabaseEntry}`).

3. **Field Disambiguation Rule (`URL` vs `S:URL`)**:
   - In cross-references, `{REF:URL@<SearchIn>:<SearchText>}` (or `{REF:A@...}`) always resolves to the standard KeePass `URL` field.
   - If a KeePass entry defines a custom string attribute whose name is literally `URL`, you **must** use `{REF:S:URL@<SearchIn>:<SearchText>}` to target the custom attribute.

> [!WARNING]
> **Portability Warning & Authoring Tips (see #234)**:
> `cloakenv` extensions (`{T-CONV:...}`, `{URL:<component>}`, `{REF:A:<component>@...}`, `{REF:URL:<component>@...}`) are non-standard constructs with no upstream counterpart in KeePass or KeePassXC.
> - **Silent Expansion Failure**: Databases containing these extensions will silently fail to expand them when opened in KeePass or KeePassXC, leaving the literal placeholder text.
> - **Accidental Overwrite Risk**: Users opening the database in KeePass/KeePassXC may see unexpanded text and attempt to "correct" or overwrite the field, accidentally destroying the placeholder logic.
> - **Authoring Recommendation**: Keep `cloakenv`-specific extension placeholders out of fields that you also view or edit interactively within KeePass or KeePassXC.

#### 2. YAML (`yaml`) & JSON (`json`)
- Reads static structured YAML or JSON files.
- Supports dot-separated object navigation (`vault://servers.bastion.hostname`) and array index addressing (`vault://servers.bastion.public_keys.0`).
- Supports multi-entity registries (under `entities_root_key`) or flat single-entity databases (`single_entity: true`).

#### 3. Custom Vault (`custom_vault`)
Defined directly inside `config.yaml` with no external files required. Supports both static attribute definitions and multi-entry composed entities.

- **`resolve_values: true`**: Enables recursive resolution of `{{ ... }}` Go template expressions and `{{ get "scheme://..." }}` lookups within attribute values (up to recursion depth 5).
- **Legacy Flat Entities**: Fully backward-compatible. Any entity defined as a flat key-value map of attributes resolves attributes directly:
  ```yaml
  vaults:
    custom_static:
      provider: "custom_vault"
      entities:
        database:
          username: "admin"
          password: "plain-secret"
  ```
- **Composed Custom Vault Entities**: When an entity configures composition keys (`merge`, `attributes`, `mapping`, `include_fields`, `exclude_fields`, `key_format`, `resolve_values`), `cloakenv` treats it as a composed entity:
  - **`merge`**: A list of source URIs to combine into this entity. Can target specific entries (`work://infra/db`) or multi-entry vaults (`openrouter://`, which automatically expands all entries returned by `Search` in deterministic order).
  - **Per-Source Transformation**: Each upstream entry is transformed in sequence: `include_fields` $\rightarrow$ `exclude_fields` $\rightarrow$ `mapping` $\rightarrow$ `key_format`.
  - **Deterministic Fold**: Transformed attributes are folded into `.attributes`, while upstream entry contexts are preserved in `.entries`.
  - **`attributes`**: Configures static attributes or `{{ ... }}` templates evaluated against the merged `.meta`, `.attributes`, and `.entries` context.
  - **Strict Validation**: Unrecognized top-level fields on composed entities are rejected to catch typos.
  - **Queryable**: Composed entities can be inspected via `cloakenv show <vault>://<entity>`, resolved via `cloakenv get <vault>://<entity>:<attribute>`, or injected into processes via `cloakenv run -m <vault>://<entity>`.
  - Example composed entity:
  ```yaml
  vaults:
    config:
      provider: "custom_vault"
      resolve_values: true
      entities:
        jules:
          title: "Jules Agent Configuration"
          tags: ["ai", "composed"]
          merge:
            - "openrouter://"
            - "work://ai/credentials"
          include_fields:
            - "API_*"
            - "DATABASE_*"
          exclude_fields:
            - "API_SECRET_KEY"
          mapping:
            - match: "^API_(.*)"
              key: "{{ .meta.Title | env }}_API_$1"
          key_format: "env"
          attributes:
            DOTENV: "{{ .attributes | toEnv }}"
            SUMMARY: "{{ .meta.Title }} contains {{ len .entries }} upstream models"
  ```

#### 4. Stored Search Vault (`search`)
- Acts as a stored search definition over one or more source vaults, returning a filtered synthetic entry view.
- Non-searchable and read-only by design (does not support the `searchable` flag).
- Resolves entries matching a stored `query` against configured `source_vaults`.
- Example config:
```yaml
vaults:
  openrouter:
    provider: "search"
    source_vaults: ["work"]
    query: '"service:openrouter" in tags'
```

#### 5. Field Filtering (`include_fields` and `exclude_fields`)
- Supported across structured vault providers (`keepass`, `yaml`, `json`, `custom_vault`, `search`).
- **Default Metadata Fields**: Default metadata fields (`title`, `tags`, `uuid`, `group`, `group_name`, `group_uuid`, `group_path`, `path`) are treated as default metadata fields and are included by default alongside entry attributes. Vault and provider metadata can be requested via `--show-meta` or explicitly via `-i _meta_.provider` or `-i _meta_.vault`.
- **`include_fields`**: When configured, restricts returned entry attributes to fields matching at least one glob pattern. **Include rules always take precedence over exclude rules** (evaluated first). An **empty list means filtering is disabled** (all unexcluded attributes allowed), not deny-all.
- **`exclude_fields`**: Drops attributes matching any configured glob pattern before returning secrets when not explicitly permitted by include rules.
- **Case-Sensitivity**: Glob patterns are evaluated **case-sensitively** (via Go's `path.Match`). A pattern like `"password"` will not match an attribute named `"Password"`. Ensure pattern casing matches the provider's attribute names.
- **Candidate Matching Scope**: Pattern matching is not limited to bare attribute names; it evaluates multiple candidate representations:
  - **Bare field names**: e.g., `UserName`, `password`, `notes`.
  - **Full static dot paths**: e.g., `entities.db.password`, `db.password`.
  - **Numeric array indices**: e.g., `users.0.token`, `config.servers.1.host`.
  - **Entry titles & qualified attributes**: e.g., `Test Website.notes`, `Test Website:notes`.
  - **Subtree exclusions via ancestor segments**: e.g., `exclude_fields: ["db"]` or `["entities.db"]` blocks all attributes beneath that container. Directly requesting and including a container grants its unexcluded nested attributes.
- **Path Separators (`/` vs `.`) and Wildcards**:
  - In Go's `path.Match`, the `*` wildcard does not match across `/` directory separators. However, for hierarchical vaults (`keepass`, `custom_vault`), each `/`-separated group prefix is also generated as a candidate path (`filter.go:679-682`, `749-752`). For example, resolving `keepass://website/Test Website:Password` generates the candidate prefix `website`. Because `path.Match("*", "website")` is true, an exclude pattern like `exclude_fields: ["*"]` matches the top-level group prefix and **excludes every secret in all top-level groups**.
  - In contrast, the `*.notes` pattern below targets title-qualified attribute candidates (e.g. `Test Website.notes`), matching any entry's notes attribute without matching slashed group prefixes. To exclude specific hierarchical groups or subtrees safely without blocking entire vaults, use qualified patterns (e.g. `website/*` or `group/subgroup/*`).
- Filters apply consistently across `GetEntry`, `Search`, and scalar `GetSecret` resolution.
- Example config:
```yaml
vaults:
  work:
    provider: "keepass"
    vault_path: "~/secrets/work_vault.kdbx"
    include_fields:
      - "env:*"
      - "UserName"
    exclude_fields:
      - "*.notes"
      - "secret:*"
```

#### 6. Field Key Mapping (`mapping`)
- Supported across vault providers (`keepass`, `yaml`, `json`, `custom_vault`, `search`).
- **`match`**: Regex pattern matched against raw attribute field names. Supports capture groups (e.g., `env:(.*)`).
- **`key`**: Target output field name template using capture group references (e.g., `$1` or `\1`).
- **Metadata Interpolation in `key`**: Target key names support Go `text/template` syntax referencing the entry's `.meta` context (such as `{{ .meta.Title | env }}_$1`). Case helper functions (`env`, `snake`, `kebab`, `upper`, `lower`) can be piped into capture groups and metadata values.
- **Preserved Values**: Mapped fields retain the original field value.
- **Filter Exemption**: Mapped fields always show regardless of `include_fields` or `exclude_fields` configured on the same vault. Field filtering (`include_fields` and `exclude_fields`) applies to unmapped fields.
- Example config:
```yaml
vaults:
  mapped_work:
    provider: "keepass"
    vault_path: "~/secrets/work_vault.kdbx"
    mapping:
      - match: "^Password$"
        key: "{{ .meta.Title | env }}_API_KEY"
      - match: "env:(.*)"
        key: "\\1"
```

#### 7. Key Formatting (`key_format`)
Supported across all configured vault providers (`keepass`, `yaml`, `json`, `custom_vault`, `search`). Transforms entry attribute keys to a consistent casing and separator format:
- `none` (default): Passes through the original key name unmodified.
- `env`: UPPERCASE_WITH_UNDERSCORES.
- `snake`: lowercase_with_underscores.
- `kebab`: lowercase-with-hyphens.
- Example config:
```yaml
vaults:
  snake_vault:
    provider: "keepass"
    vault_path: "~/secrets/work_vault.kdbx"
    key_format: "snake"
```

#### 8. Universal Per-Entry Attributes Decorator (`attributes`)
Supported across all configured vault providers (`keepass`, `yaml`, `json`, `custom_vault`, `search`). Computes or overlays templated attributes onto every entry.

- **Execution Order**: Wrapped strictly after `FilteringProvider` (`include_fields`, `exclude_fields`), `MappingProvider` (`mapping`), and `KeyFormatProvider` (`key_format`).
- **Post-Filter Guarantee**: Templates in `attributes:` only see authorized, formatted attributes in `.attributes`, ensuring field filters cannot be bypassed by template evaluation (`RSK-002`, `REQ-005`).
- **Template Context**: Templates have access to `.meta` (`Title`, `Tags`, `UUID`, `Vault`, `Provider`, `Path`, `Group`) and post-filter `.attributes`.
- **Queryable**: Computed attributes are immediately available via `cloakenv get <vault>://<entry>:<attribute>`, `cloakenv show <vault>://<entry>`, and `cloakenv search`.
- Example config:
```yaml
vaults:
  decorated_keepass:
    provider: "keepass"
    vault_path: "~/secrets/work.kdbx"
    key_format: "env"
    attributes:
      DOTENV: "{{ .attributes | toEnv }}"
      SERVICE_NAME: "{{ .meta.Title | lower }}"
```

#### 9. KeePass Placeholders & Cross-Entry References (`resolve_references`)
Supported on `keepass` vaults when `resolve_references: true` is configured in `config.yaml`. Allows KeePass entries to dynamically reference fields within the same entry, calculate timestamps, perform text transformations, parse URL components, or pull attributes from other entries across the database.

##### Enabling Placeholder Resolution
Placeholder resolution is disabled by default (`false`) to ensure string literals containing `{...}` remain untouched. Enable it per-vault in `config.yaml`:
```yaml
vaults:
  work:
    provider: "keepass"
    vault_path: "~/secrets/work_vault.kdbx"
    resolve_references: true
```

##### Same-Entry Placeholders
Interpolates standard or custom fields from the current entry:
| Placeholder | Description | Example Output |
|---|---|---|
| `{TITLE}` | Entry Title | `Database Server` |
| `{USERNAME}` | Entry UserName | `admin_user` |
| `{PASSWORD}` | Entry Password | `s3cr3t_p@ss` |
| `{URL}` | Entry URL | `https://db.example.com:5432/mydb` |
| `{NOTES}` | Entry Notes | `Production DB notes` |
| `{S:<custom>}` | Custom string attribute named `<custom>` | `{S:api_key}` $\rightarrow$ `sk_live_12345` |

##### Date/Time Placeholders
Generates current timestamp components. Local variants (`DT_*`) use the host system's local timezone (`time.Now()`), while `DT_UTC_*` variants use UTC.
| Local Placeholder | UTC Placeholder | Format / Description | Example Output |
|---|---|---|---|
| `{DT_SIMPLE}` | `{DT_UTC_SIMPLE}` | Full timestamp (`YYYYMMDDHHMMSS`) | `20260330143000` |
| `{DT_YEAR}` | `{DT_UTC_YEAR}` | 4-digit year (`YYYY`) | `2026` |
| `{DT_MONTH}` | `{DT_UTC_MONTH}` | 2-digit month (`MM`) | `03` |
| `{DT_DAY}` | `{DT_UTC_DAY}` | 2-digit day (`DD`) | `30` |
| `{DT_HOUR}` | `{DT_UTC_HOUR}` | 2-digit 24-hour (`HH`) | `14` |
| `{DT_MINUTE}` | `{DT_UTC_MINUTE}` | 2-digit minute (`MM`) | `30` |
| `{DT_SECOND}` | `{DT_UTC_SECOND}` | 2-digit second (`SS`) | `00` |

##### Text Transformations (`T-CONV`)
Transforms inner text using a specified method.
- **Syntax**: `{T-CONV:<sep><text><sep><METHOD><sep>}`
- **Configurable Separator**: The character `<sep>` following `T-CONV:` defines the delimiter (e.g. `/`, `!`, `#`). This allows transforming text containing slashes without syntax errors (e.g., `{T-CONV:!usr/bin!UPPER!}`).
- **Examples**:
  - `{T-CONV:/hello world/UPPER/}` $\rightarrow$ `HELLO WORLD`
  - `{T-CONV:!admin:secret!BASE64!}` $\rightarrow$ `YWRtaW46c2VjcmV0`

| Method / Aliases | Description | Example Input $\rightarrow$ Output |
|---|---|---|
| `UPPER`, `U` | Convert text to uppercase | `foo` $\rightarrow$ `FOO` |
| `LOWER`, `L` | Convert text to lowercase | `FOO` $\rightarrow$ `foo` |
| `BASE64` | Standard Base64 encode | `admin:pass` $\rightarrow$ `YWRtaW46cGFzcw==` |
| `HEX` | Lowercase hexadecimal encode | `abc` $\rightarrow$ `616263` |
| `URI` | Percent-encode for URI queries | `user@host` $\rightarrow$ `user%40host` |
| `URI-DEC`, `URIDEC`, `URI-DECODE` | Percent-decode URI query text | `user%40host` $\rightarrow$ `user@host` |
| `RAW` | Return raw text unchanged | `text` $\rightarrow$ `text` |

##### URL Component Extraction
Extracts individual URL components from an entry's `URL` field using `{URL:<component>}` for the current entry, or `{REF:A:<component>@...}` / `{REF:URL:<component>@...}` for cross-entry references.

Given an entry URL `https://admin:secret123@db.example.com:5432/app_db?ssl=true#main`:
| Component / Aliases | Extracted Part | Example Output |
|---|---|---|
| `SCM`, `SCHEME` | Scheme | `https` |
| `HOST` | Hostname (excluding port) | `db.example.com` |
| `PORT` | Port number | `5432` |
| `PATH` | Path | `/app_db` |
| `QUERY` | Query string | `ssl=true` |
| `FRAGMENT` | Fragment / anchor | `main` |
| `USERINFO` | User info (`username:password`) | `admin:secret123` |
| `USERNAME` | Username from userinfo | `admin` |
| `PASSWORD` | Password from userinfo | `secret123` |
| `RMVSCM`, `WITHOUTSCHEME` | URL with `scheme:` prefix removed | `//admin:secret123@db.example.com:5432/app_db?ssl=true#main` |

##### Cross-Entry Field References (`REF`)
References field values from another entry in the database.
- **Syntax**: `{REF:<WantedField>@<SearchIn>:<SearchText>}`

**`WantedField` Codes (Field to Extract)**:
- `T`: Title
- `U`: UserName
- `P`: Password
- `A` or `URL`: URL (or `A:<component>` / `URL:<component>`, e.g. `A:HOST`, `URL:PORT`)
- `N`: Notes
- `S:<custom>`: Custom string attribute named `<custom>`

**`SearchIn` Codes (Field to Search In)**:
- `I`: Entry UUID (32-character hex string, with or without hyphens)
- `T`: Title
- `U`: UserName
- `P`: Password
- `A`: URL
- `N`: Notes
- `O`: Custom attributes ("Other")

**Entry Search Matching**:
`cloakenv` performs a two-pass search across all entries in the database:
1. **Pass 1 (Exact Match)**: Checks for an exact string match in `SearchIn`.
2. **Pass 2 (Case-Insensitive Substring Match)**: If no exact match is found, falls back to a case-insensitive substring search in `SearchIn`.

**Examples**:
- `{REF:P@T:PostgreSQL Production}`: Fetches Password of entry titled `PostgreSQL Production`.
- `{REF:U@I:10203040-5060-7080-90a0-b0c0d0e0f001}`: Fetches UserName of entry with specified UUID.
- `{REF:S:api_key@U:service_acct}`: Fetches custom attribute `api_key` from entry with UserName `service_acct`.
- `{REF:A:HOST@T:Staging DB}`: Extracts `HOST` component from URL of `Staging DB` entry.

##### Recursion Depth Limits & Circular Reference Protection
- **Max Depth Limit**: References and placeholders can be chained up to a maximum recursion depth of **10**. Exceeding depth 10 returns an error (`maximum recursion depth (10) exceeded`).
- **Circular Reference Detection**: `cloakenv` tracks visited entry fields (`<UUID>:<fieldKey>`). If circular dependencies are detected (e.g. Entry A $\rightarrow$ Entry B $\rightarrow$ Entry A), an error is raised immediately.

##### Security Warning & Secret Exposure Caveat
> [!CAUTION]
> **Secret Exposure Warning**:
> Extracting sensitive URL components (such as `{URL:PASSWORD}`, `{URL:USERINFO}`), transforming passwords (e.g. `{T-CONV:/{PASSWORD}/BASE64/}`), or referencing passwords across entries (`{REF:P@...}`) in non-sensitive or exposed fields (such as `Notes`, `URL`, or custom fields) causes entry secrets to be evaluated and stored in those derived fields.
>
> When entries are exported or merged (e.g. via `cloakenv show` or `cloakenv run -m`), all attributes are exposed by default.
>
> **Mitigation**: Combine `resolve_references: true` with [Field Filtering](#5-field-filtering-include_fields-and-exclude_fields) (`exclude_fields` / `include_fields`) to constrain the blast radius and prevent secret fields from being exported:
> ```yaml
> vaults:
>   work:
>     provider: "keepass"
>     vault_path: "~/secrets/work_vault.kdbx"
>     resolve_references: true
>     exclude_fields:
>       - "*.notes"
>       - "raw_*"
> ```

---

## Template Engine & FuncMap Reference

`cloakenv` embeds Go's standard library `text/template` engine with `.Option("missingkey=error")` and recursion depth tracking. When `resolve_values: true` is enabled on a `custom_vault` or `custom_vault` entity, string attribute values containing `{{ ... }}` are dynamically rendered with access to entry metadata, attributes, and 17 built-in functions.

### Top-Level Templates (`templates:`)

Centralize reusable templates in `~/.config/cloakenv/config.yaml`. Templates can be defined inline or point to external files:

```yaml
templates:
  # Inline template definition:
  dotenv:
    template: "{{ .attributes | toEnv }}"

  # External file template (resolves relative to config.yaml directory or expands ~/):
  app_config:
    path: "~/.config/cloakenv/templates/app.tmpl"
```

Evaluate configured templates using `{{ include "name" . }}` or evaluate external template files directly using `{{ includeFile "path/to/file.tmpl" . }}`. Template authors are the configuration owner, and template file paths are unrestricted by design.

### Built-In Template Functions (`FuncMap`)

| Function | Signature | Description | Example |
|---|---|---|---|
| `get` | `(uri string) (string, error)` | Resolves a single secret value from any vault or provider | `{{ get "keyring://pg/pass" }}` |
| `entry` | `(uri string) (map[string]any, error)` | Resolves a complete entry into a structured context map (`.meta`, `.attributes`) | `{{ (entry "work://db").attributes.Password }}` |
| `search` | `(query string) ([]map[string]any, error)` | Executes a search query across vaults and returns a slice of entry context maps | `{{ range search "tag=db" }}{{ .meta.Title }}: {{ .attributes.HOST }}{{ end }}` |
| `toEnv` | `(attrs any) (string, error)` | Serializes an attributes map into dotenv key-value format (sorted, excluding metadata) | `{{ .attributes \| toEnv }}` |
| `toKv` | `(attrs any) (string, error)` | Alias for `toEnv` | `{{ .attributes \| toKv }}` |
| `toJson` | `(v any) (string, error)` | Serializes a value or map to a JSON string | `{{ .attributes \| toJson }}` |
| `toYaml` | `(v any) (string, error)` | Serializes a value or map to a YAML string | `{{ .attributes \| toYaml }}` |
| `upper` | `(s string) string` | Converts a string to UPPERCASE | `{{ .meta.Title \| upper }}` |
| `lower` | `(s string) string` | Converts a string to lowercase | `{{ .meta.Title \| lower }}` |
| `env` | `(s string) string` | Formats a string to `UPPERCASE_WITH_UNDERSCORES` | `{{ .meta.Title \| env }}_API_KEY` |
| `snake` | `(s string) string` | Formats a string to `lowercase_with_underscores` | `{{ .meta.Title \| snake }}` |
| `kebab` | `(s string) string` | Formats a string to `lowercase-with-hyphens` | `{{ .meta.Title \| kebab }}` |
| `keys` | `(m map[string]any) []string` | Returns a sorted slice of map keys | `{{ range keys .attributes }}{{ . }}{{ end }}` |
| `values` | `(m map[string]any) []any` | Returns a slice of map values in sorted key order | `{{ range values .attributes }}{{ . }}{{ end }}` |
| `join` | `(sep string, slice any) (string, error)` | Joins slice elements with a separator string | `{{ join "," .meta.Tags }}` |
| `include` | `(name string, ctx any) (string, error)` | Renders a named top-level template defined under `templates:` | `{{ include "dotenv" . }}` |
| `includeFile` | `(path string, ctx any) (string, error)` | Renders an external template file relative to `config.yaml` or `~/` | `{{ includeFile "templates/dotenv.tmpl" . }}` |

### Template Context (`.`)

Templates rendered within vaults, per-entry `attributes:`, and composed entities receive a root dot context (`.`):

| Context Variable | Description |
|---|---|
| `.meta` / `.Meta` | Entry metadata: `Title`/`title`, `Tags`/`tags`, `UUID`/`uuid`, `Vault`/`vault`, `Provider`/`provider`, `Path`/`path`, `Group`/`group` (with `.group.name`, `.group.uuid`, `.group.path`). Both lowercase and PascalCase access are supported. |
| `.attributes` / `.Attributes` | Post-filter, post-map, post-format entry attributes map. Excluded fields are never accessible in `.attributes`. |
| `.entries` / `.Entries` | Ordered slice of upstream entry context maps when evaluating composed `custom_vault` entities. |

### Error Handling & Safety
- **Strict Key Checking (`missingkey=error`)**: Accessing an unmapped or misspelled attribute key (e.g. `{{ .attributes.MissingKey }}`) triggers an immediate evaluation error, preventing silent generation of broken configurations.
- **Bounded Recursion (`maxDepth = 5`)**: Resolution through `get`, `entry`, `search`, `include`, and `includeFile` tracks execution depth and aborts if recursion exceeds 5 levels.

---

## Command Autoloading & Alias Masking

`cloakenv` can automatically match commands executed via `cloakenv run -- <command>` and inject credentials, merge vaults, or transform the command line before execution.

Define `autoload:` rules in `~/.config/cloakenv/config.yaml`:

```yaml
autoload:
  # Example 1: Command Transformation / Alias Masking
  # Running: `cloakenv run -- litellm --config config.yaml`
  # Expands to: `uvx --with 'litellm[proxy]' litellm --config config.yaml`
  # while injecting LITELLM_MASTER_KEY!
  - match: "^litellm\\s+(.*)$"
    command: "uvx --with 'litellm[proxy]' litellm \\1"
    env:
      LITELLM_MASTER_KEY: "keyring://litellm/master_key"

  # Example 2: AWS CLI Autoloading
  - match: "aws"
    vaults:
      - "work://Cloud/AWS_Production"
    env:
      AWS_DEFAULT_REGION: "env://DEFAULT_REGION"
    whitelist:
      - "AWS_ACCESS_KEY_ID"
      - "AWS_SECRET_ACCESS_KEY"
      - "AWS_DEFAULT_REGION"

  # Example 3: Wildcard / Glob Matching
  - match: "kubectl*"
    merge:
      - "infra_yaml://clusters/staging"
    env:
      KUBECONFIG_TOKEN: "keyring://k8s/token"
```

> [!WARNING]
> **Windows Batch Script Restriction**: Execution of `.bat` and `.cmd` scripts is explicitly blocked on Windows for security reasons due to vulnerabilities in how arguments are escaped by `cmd.exe`. If you must run a batch wrapper (e.g., `npm.cmd`, `gradlew.bat`), please invoke it explicitly using a shell, such as `cloakenv run -- bash -c "npm ..."` or invoke the underlying executable directly.

### Pattern Matching Types:
1. **Regular Expressions**: `match: "^(aws|terraform)\\s+(.*)$"` with capture group substitutions (`\1`..`\9` or `$1`..`$9`) in `command:`.
2. **Executable Basename & Path**: `match: "aws"` matches `aws`, `/usr/local/bin/aws`, or `aws s3 ls`.
3. **Glob Patterns**: `match: "npm run *"` or `match: "*.sh"`.

---

## Search Query Language Reference

`cloakenv search` evaluates queries using the fast, safe [Go expr](https://github.com/expr-lang/expr) engine. Queries run against flattened entry objects containing `title`, `path`, `tags`, and all custom attributes.

### 1. Tag & Array Membership (`in`, `not in`)

```bash
# Entry must contain tag "auth:ssh"
cloakenv search '"auth:ssh" in tags'

# Entry must NOT contain tag "deprecated"
cloakenv search 'not ("deprecated" in tags)'

# Combined tags
cloakenv search '"env:prod" in tags and "role:db" in tags'
```

### 2. Numbers & Comparisons

```bash
cloakenv search 'bit_strength == 4096'
cloakenv search 'port >= 8000 and port <= 9000'
```

### 3. String Functions

```bash
# Substring search
cloakenv search 'title contains "Bastion"'

# Case-insensitive substring search
cloakenv search 'lower(title) contains "bastion"'

# Prefix and Suffix matching
cloakenv search 'title startsWith "Prod-"'
cloakenv search 'hostname endsWith ".internal.net"'

# Regular expression matching
cloakenv search 'hostname matches "db-[0-9]+\\.example\\.com"'
```

### 4. Boolean Combinations

```bash
cloakenv search '("auth:ssh" in tags or "auth:tls" in tags) and bit_strength >= 2048'
```

> [!NOTE]
> If an entry lacks a field referenced in the search query (e.g. searching `bit_strength == 4096` when some entries have no `bit_strength` attribute), `cloakenv` **gracefully skips** non-matching entries without raising runtime errors.

> [!NOTE]
> The search engine uses a **strict allowlist** for expression nodes. Only safe primitives, builtins, comparisons, and boolean logic are permitted. Arbitrary function calls and method invocations on entries are blocked for security.

---

## Configuration Reference (`config.yaml`)

Default path: `~/.config/cloakenv/config.yaml` (override with `-c <path>`).

```yaml
# ==========================================
# Reusable Templates
# ==========================================
templates:
  dotenv:
    template: "{{ .attributes | toEnv }}"
  # app_config:
  #   path: "~/.config/cloakenv/templates/app.tmpl"

# ==========================================
# Cache Configuration
# ==========================================
cache:
  # Default TTL duration for cache:// sets if --ttl is omitted
  default_ttl: "10m"

# ==========================================
# Keyring Configuration
# ==========================================
keyring:
  # Service prefix used in the OS keyring
  prefix: "cloakenv"

# ==========================================
# Configured Vaults
# ==========================================
vaults:
  # KeePass .kdbx Vault
  work:
    provider: "keepass"
    vault_path: "~/secrets/work.kdbx"
    searchable: true
    resolve_references: true # Enable KeePass entry placeholder & cross-entry reference resolution

  # YAML File Vault (multiple entities)
  infra_yaml:
    provider: "yaml"
    vault_path: "./examples/providers/yaml/database.yaml"
    entities_root_key: "entries" # Use "." for document root

  # JSON File Vault
  hosts_json:
    provider: "json"
    vault_path: "./examples/providers/json/database.json"
    entities_root_key: "entries"

  # Flat Single-Entity Dotenv Vault
  dotenv_dev:
    provider: "yaml"
    vault_path: "./examples/providers/yaml/dotenv.yaml"
    single_entity: true
    entity_name: "Local Development Config"
    tags: ["env:dev", "local"]

  # Inline Custom Vault (with recursive URI and template resolution)
  custom_static:
    provider: "custom_vault"
    resolve_values: true
    entities:
      api_gateway:
        username: "admin"
        Password: '{{ get "keyring://gateway/admin_pass" }}'
        tags: ["static", "gateway"]

  # Composed Custom Vault (multi-source merge, filtering, mapping, and templates)
  composed_vault:
    provider: "custom_vault"
    resolve_values: true
    entities:
      jules_stack:
        title: "Jules AI Stack"
        tags: ["composed", "ai"]
        merge:
          - "openrouter://"
          - "work://ai/keys"
        include_fields:
          - "API_*"
        mapping:
          - match: "^API_(.*)"
            key: "{{ .meta.Title | env }}_API_$1"
        key_format: "env"
        attributes:
          DOTENV: "{{ .attributes | toEnv }}"
          SUMMARY: "{{ .meta.Title }} contains {{ len .entries }} models"

  # Stored Search Vault (filtered view over source vaults)
  openrouter:
    provider: "search"
    source_vaults: ["work"]
    query: '"service:openrouter" in tags'

  # Vault with Regex Field Key Mapping & Key Formatting
  mapped_vault:
    provider: "keepass"
    vault_path: "~/secrets/work.kdbx"
    key_format: "env"
    mapping:
      - match: "env:(.*)"
        key: "\\1"

  # Vault with Universal Per-Entry Attributes Decorator
  decorated_vault:
    provider: "keepass"
    vault_path: "~/secrets/work.kdbx"
    key_format: "env"
    attributes:
      DOTENV: "{{ .attributes | toEnv }}"
      TITLE_UPPER: "{{ .meta.Title | upper }}"

# ==========================================
# Command Autoloading Rules
# ==========================================
autoload:
  - match: "^litellm\\s+(.*)$"
    command: "uvx --with 'litellm[proxy]' litellm \\1"
    env:
      LITELLM_MASTER_KEY: "keyring://litellm/master_key"

  - match: "aws"
    vaults:
      - "work://Cloud/AWS_Production"
    whitelist:
      - "AWS_ACCESS_KEY_ID"
      - "AWS_SECRET_ACCESS_KEY"
```

---

## Headless & CI/CD Usage

`cloakenv` is built to run seamlessly in continuous integration (GitHub Actions, GitLab CI) and headless Docker containers:

1. **Use `env://` mappings**: Point secrets to CI environment variables:
   ```yaml
   # .github/workflows/deploy.yml
   - name: Deploy
     run: |
       cloakenv run \
         -e DB_PASS=env://SECRET_DB_PASSWORD \
         -e API_KEY=env://SECRET_API_KEY \
         -- ./deploy.sh
   ```
2. **Static JSON/YAML or custom vaults**: Mount encrypted or pipeline-generated files without requiring interactive master password prompts.
3. **Zero Interactive Prompts**: When secrets are supplied via `env://` or files, `cloakenv` never prompts for terminal input.

---

## Development & Testing

### Common Makefile Targets

```bash
make build       # Compile binary to bin/cloakenv
make test        # Run tests with race detector (go test -v -race ./...)
make bench        # Run performance benchmarks
make fmt         # Format Go source code (go fmt ./...)
make vet         # Run Go static analysis (go vet ./...)
make test-all    # Run fmt, vet, test, and bench in sequence
make install     # Install binary to $GOBIN or $GOPATH/bin
make uninstall   # Remove installed binary
```

> [!NOTE]
> CI runs lint, cross-platform tests (Linux, macOS, Windows), and [Semgrep](https://semgrep.dev/) static analysis for code scanning. All checks must pass before merge.

### Running Test Verification with Test Fixture

A pre-built KeePass database fixture is available at `testdata/testDB.kdbx`:
- **Master Password**: `password123`
- **Group**: `website` $\rightarrow$ **Entry**: `Test Website`
- **Attributes**: `Password` (`testPassword123!`), `UserName` (`user@email.com`), Attachment `hello.txt`

```bash
# Test KeePass authentication and attribute resolution
echo "password123" | ./bin/cloakenv -c testdata/test_config.yaml auth login testdb
./bin/cloakenv -c testdata/test_config.yaml get "testdb://website/Test Website:UserName"
./bin/cloakenv -c testdata/test_config.yaml auth forget testdb
```

---

## License

MIT License. See [LICENSE](LICENSE) for details.
