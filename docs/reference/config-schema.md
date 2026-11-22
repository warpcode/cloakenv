# Configuration Schema Reference (`config.yaml`)

By default, `cloakenv` loads `~/.config/cloakenv/config.yaml`. Override this path with the global `-c <path>` flag.

---

## Path Resolution Rules

- Leading `~/` expands to the current user's home directory (`os.UserHomeDir`).
- Relative paths (in `vault_path`, `key_file`, `audit.log_path`, and `templates.<name>.path`) are resolved relative to the directory containing `config.yaml`, then cleaned with `filepath.Clean`.

---

## Top-Level Sections

| Key | Type | Description |
|---|---|---|
| `cache` | `CacheConfig` | Global cache defaults (`default_ttl`). |
| `keyring` | `KeyringConfig` | OS keyring namespace (`prefix`, `isolate`). |
| `audit` | `AuditConfig` | Non-secret structured access logging (`enabled`, `log_path`, `format`). |
| `vaults` | `map[string]VaultConfig` | Named read-only secret backends (`keepass`, `pass`, `vault`, `env`, `keyring`). |
| `envs` | `map[string][]VaultSourceConfig` | Named flat environment variable pipelines. |
| `templates` | `map[string]TemplateConfig` | Named template/artifact generators (`template`, `json`, `yaml`, `dotenv`). |
| `autoload` | `[]AutoloadRule` | Command-to-env/template bindings for `cloakenv run`. |

---

## 1. `cache`, `keyring`, and `audit`

```yaml
cache:
  default_ttl: "10m"     # Go duration string (e.g., "30s", "15m", "1h")

keyring:
  prefix: "cloakenv"     # Default: "cloakenv"
  isolate: false         # If true, appends 10-char SHA-256 of non-default `-c` config path

audit:
  enabled: false         # Default: false
  log_path: "~/.config/cloakenv/audit.jsonl"
  format: "jsonl"        # "jsonl" (default) or "json"
```

---

## 2. `vaults.<name>` (`VaultConfig`)

| Field | Type | Providers | Description |
|---|---|---|---|
| `provider` | `string` | All | Required: `"keepass"`, `"pass"`, `"vault"`, `"env"`, or `"keyring"`. |
| `vault_path` | `string` | `keepass`, `pass` | Path to `.kdbx` file or password store directory. |
| `key_file` | `string` | `keepass` | Optional path to `.key` / `.keyx` / raw key file. |
| `key_file_env` | `string` | `keepass` | Env var holding a key file path (overrides `key_file` when non-empty). |
| `resolve_references` | `bool` | `keepass` | Expand KeePass `{REF:...}` and `{TITLE}`/`{PASSWORD}` placeholders (`false` default). |
| `address` | `string` | `vault` | Vault server URL (or `VAULT_ADDR`). |
| `mount_path` | `string` | `vault` | KV secrets engine mount path (default `"secret"`). |
| `engine_version` | `int` | `vault` | `1` or `2` (default `2`). |
| `namespace` | `string` | `vault` | Vault Enterprise namespace. |
| `token` | `string` | `vault` | Vault token (or `VAULT_TOKEN`). |
| `role_id` | `string` | `vault` | AppRole `role_id` (must pair with `secret_id`). |
| `secret_id` | `string` | `vault` | AppRole `secret_id` (must pair with `role_id`). |
| `query` | `string` | `keepass`, `pass`, `vault`, `keyring` | Provider-native scope (path prefix, or comma-separated keys for `keyring`). |
| `cache` | `VaultCacheConfig` / `bool` / `string` | All | `true`, `"15m"`, or `{ enabled: bool, ttl: string, mode: "keyring"|"encrypted_disk" }`. |

---

## 3. `envs.<name>[]` & `templates.<name>.vaults[]` (`VaultSourceConfig`)

| Field | Type | Required | Description |
|---|---|---|---|
| `vault` | `string` | Yes | Vault name from `vaults:`, or built-in `"env"` / `"keyring"`. |
| `query` | `string` | No | Go `text/template` boolean predicate (`trimmed output == "true"`). |
| `rules` | `[]RuleConfig` | No | Ordered rules (`pass`, `match`, `set`). Defaults to a single `type: pass`. |

### `RuleConfig`
| Field | Rule Type | Description |
|---|---|---|
| `type` | All | Required: `"pass"`, `"match"`, or `"set"`. |
| `include` | `pass` | `[]string` glob patterns (`path.Match`). Defaults to `["*"]`. |
| `exclude` | `pass` | `[]string` glob patterns (overrides `include`). |
| `key_format` | `pass` | `"env"`, `"snake"`, `"kebab"`, or `"none"`. |
| `match` | `match` | Key regex pattern. |
| `value_match` | `match` | Serialized value regex pattern. |
| `key` | `match` | Required target key expression (`$1`..`$N` / Go template). |
| `value` | `match` | Optional target value expression (`$1`..`$N` / Go template). |
| `values` | `set` | `map[string]string` of templated keys to templated values. |

---

## 4. `templates.<name>` (`TemplateConfig`)

| Field | Type | Description |
|---|---|---|
| `driver` | `string` | `"template"` (default), `"json"`, `"yaml"`, or `"dotenv"`. |
| `template` | `string` | Inline Go `text/template` (`driver: template` only; mutually exclusive with `path`). |
| `path` | `string` | Path to Go `text/template` file (`driver: template` only; mutually exclusive with `template`). |
| `from_env` | `string` | Name of an `envs:` entry to seed (`driver: dotenv` only). |
| `vaults` | `[]VaultSourceConfig` | Vault sources producing `.entries` (`[]map[string]string`). |

---

## 5. `autoload[]` (`AutoloadRule`)

| Field | Type | Description |
|---|---|---|
| `match` | `string` | Required regex, command prefix, basename, or `*` wildcard glob. |
| `command` | `string` | Optional command replacement with `\1`/`$1`/`${name}` capture groups. |
| `envs` | `[]AutoloadEnvItem` | Ordered list of named `envs:` strings or inline `map[string]string`. |
| `templates` | `[]AutoloadTemplateConfig` | List of `{ template, env, env_path, path, persist }`. |

See [`examples/config.yaml`](../../examples/config.yaml) for a complete, validated example file.
