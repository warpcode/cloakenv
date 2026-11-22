# CLI Command Reference

`cloakenv` exposes a focused, zero-leak CLI surface. No command or flag can print raw secret values to `stdout`.

---

## Global Flags

Global flags must appear **before** the subcommand:

```bash
cloakenv [-c <config_path>] [-h|--help] [-v|--version] <subcommand> [args]
```

| Flag | Description |
|---|---|
| `-c <config_path>` | Load an alternate YAML configuration file (default: `~/.config/cloakenv/config.yaml`). |
| `-h`, `--help` | Display help information. |
| `-v`, `--version` | Print build and runtime version information. |

---

## `run` — Execute Commands with Autoloaded Secrets

Wraps a child command with declarative `envs:`, `templates:`, ephemeral `0600` file delivery, and real-time `stdout`/`stderr` masking.

```bash
cloakenv [-c config_path] run [-E] [--no-autoload] [--dry-run] -- <command> [args]
```

### Flags
| Flag | Description |
|---|---|
| `-E` | Start with an empty environment (do not inherit parent process environment variables). |
| `--no-autoload` | Disable `autoload:` rule matching for this invocation. |
| `--dry-run` | Preview the command, injected variable names, and template delivery modes (with secrets masked) without executing. |

### Examples
```bash
# Execute with autoloaded envs, ephemeral files, and output masking
cloakenv run -- python app.py

# Start with a clean environment (-E)
cloakenv run -E -- ./scripts/deploy.sh prod

# Preview without running
cloakenv run --dry-run -- python app.py
```

---

## `inspect` — Preview Resolved Envs & Templates (`<REDACTED>`)

Evaluates queries, `RuleEngine` transforms, template drivers, and `autoload:` matches without executing commands. **Every secret value is unconditionally replaced with `<REDACTED>`**.

```bash
cloakenv [-c config_path] inspect [-o text|json|yaml]
cloakenv [-c config_path] inspect env <name> [-o text|json|yaml]
cloakenv [-c config_path] inspect template <name> [-o text|json|yaml]
cloakenv [-c config_path] inspect [-o text|json|yaml] run -- <command> [args]
```

*(If `<name>` is unambiguous across `envs:` and `templates:`, `cloakenv inspect <name>` resolves it automatically.)*

### Flags
| Flag | Description |
|---|---|
| `-o`, `--output <format>` | Output format: `text` (default), `json`, or `yaml`. |

### Example Output
```bash
$ cloakenv inspect env app_env
Env: app_env
Matched entries (1):
  - [work] OpenAI tags=[ai] keys=[API_KEY]
Generated variables:
  API_KEY=<REDACTED>
```

---

## `auth` — Manage Vault Authentication

Stores, verifies, or removes vault unlock credentials in the OS keyring (`provider/<vault>`).

```bash
cloakenv [-c config_path] auth login <vault>
cloakenv [-c config_path] auth forget <vault>
cloakenv [-c config_path] auth status [vault] [-o text|json|yaml]
```

### Subcommands
- `login <vault>`: Prompts securely (or reads from stdin) to store unlock credentials in the OS keyring and validates vault access.
- `forget <vault>`: Removes stored unlock credentials for `<vault>` from the OS keyring.
- `status [vault]`: Checks vault unlock and connection readiness for one vault or all configured vaults (`ACTIVE` / `ERROR`, with latency in `json`/`yaml` output).

---

## `cache` — Local Encrypted Cache Management

Manages entries stored by per-vault [transparent caching](../providers/caching.md).

```bash
cloakenv [-c config_path] cache clear
cloakenv [-c config_path] cache prune
```

- `clear`: Deletes all cached entries across all vaults.
- `prune`: Removes only expired cache entries.

---

## `doctor` — Diagnose Environment & Config

Validates `config.yaml` syntax and schema, legacy key rejections, template syntax, autoload regexes, cache directory permissions (`0700`), provider dependencies (`gpg`, `.kdbx` / key file accessibility), and stale ephemeral files.

```bash
cloakenv [-c config_path] doctor [--strict]
```

### Flags
| Flag | Description |
|---|---|
| `--strict` | Reject unknown YAML fields (`KnownFields(true)` + strict template/rule/cache key checks) and exit non-zero on warnings. |

---

## `completion` — Generate Shell Autocompletion Scripts

Outputs autocompletion scripts for supported shells:

```bash
cloakenv completion <bash|zsh|fish|powershell>
```

---

## `version` — Print Build & Runtime Version

```bash
cloakenv version
```

---

## `internal` — Developer & Shell Hook Helpers

```bash
cloakenv [-c config_path] internal match-alias [--json] -- <command> [args]
cloakenv [-c config_path] internal list-vaults
```

- `match-alias`: Exits `0` if `<command>` matches an `autoload:` rule (printing the matched pattern, or full match metadata with `--json`), and exits `1` otherwise. Designed for [shell auto-wrapping hooks](../guides/shell-integration.md).
- `list-vaults`: Prints configured vault names sorted alphabetically (used by shell completions).
