# Core Concepts

`cloakenv` is built around five strict architectural pillars designed to keep secrets out of plaintext files, terminal logs, and AI agent contexts.

---

## 1. Pure Read-Only Vaults & Uniform Flat `Entry` Model

Every backend under `vaults:` (`keepass`, `pass`, `vault`, `env`, `keyring`) is **strictly read-only**. Vaults never filter, rename, or transform fields—they simply retrieve raw entries as uniform flat maps (`Entry`, `map[string]any`).

Every `Entry` exposes raw secret attributes alongside canonical lowercase metadata keys:

| Canonical Key | Type | Description |
|---|---|---|
| `title` | `string` | Entry title (e.g., `"OpenAI"`) |
| `path` | `string` | Canonical path inside the vault (e.g., `"AI/OpenAI"`) |
| `uuid` | `string` | Entry unique identifier (when supported by backend) |
| `tags` | `[]string` | Case-preserved tag list (matched case-insensitively by `hasTag`) |
| `group` | `map[string]any` | Group metadata (`name`, `path`, `uuid`) when supported |
| `vault` | `string` | Configured vault name (e.g., `"work"`) |
| `provider` | `string` | Provider scheme (e.g., `"keepass"`) |

### Multi-Entry vs. Flat K/V Providers
- **Multi-entry providers** (`keepass`, `pass`, `vault`): Emit one `Entry` per record in the vault.
- **Flat K/V providers** (`env`, `keyring`): Emit a **single** `Entry` containing all retrieved key-value pairs. Both `env` and `keyring` are available **implicitly** by name in `envs:` and `templates:` without needing a `vaults:` block.

---

## 2. Two-Stage Entry Selection (`query:`)

`cloakenv` supports `query:` at two distinct levels:

1. **Vault-level `query:`** (`vaults.<name>.query`): Passed directly to the provider as a native scope (e.g., a KeePass group prefix, a `pass` subdirectory, a HashiCorp Vault path prefix, or a comma-separated list of `keyring` key names).
2. **Source-level `query:`** (`envs.<name>[].query` or `templates.<name>.vaults[].query`): A Go `text/template` boolean predicate evaluated against each candidate `Entry`. An entry is selected **only** when the trimmed output equals `"true"`.

```yaml
envs:
  backend_prod:
    - vault: work
      query: '{{ and (hasTag "backend" .tags) (eq .group.name "Production") }}'
```

---

## 3. Declarative `RuleEngine` (`pass`, `match`, `set`)

Once entries are selected, each vault source runs an ordered `rules:` list against each matched `Entry`:

1. **`type: pass`**: Copies raw fields matching `include` globs while excluding `exclude` globs (`exclude` wins), formatting target keys with `key_format` (`env`, `snake`, `kebab`, `none`). Catch-all `*` wildcards automatically skip reserved metadata keys (`title`, `path`, `uuid`, `tags`, `group`, `vault`, `provider`) unless explicitly named.
2. **`type: match`**: Filters fields by key regex (`match`) and/or serialized value regex (`value_match`), rewriting target `key` and `value` using `$1`/`${1}` capture groups and Go `text/template`.
3. **`type: set`**: Evaluates a `values:` map of Go `text/template` keys and values with access to raw entry fields and `.output` (keys written by earlier rules).

**Precedence**:
- If `rules:` is omitted, a single `type: pass` rule runs by default.
- If `rules:` is specified, **only** the listed rules execute in order.
- **Last-rule-wins**: Later rules overwrite keys written by earlier rules.

---

## 4. Separated `envs:` and `templates:` Pipelines

`cloakenv` strictly separates flat environment variable injection from structured file/payload generation:

| Pipeline | Output Type | Default `key_format` | Use Cases |
|---|---|---|---|
| **`envs:`** | Flat `map[string]string` | `env` (`UPPER_SNAKE_CASE`) | Process environment variables (`API_KEY`, `DATABASE_URL`) |
| **`templates:`** | Rendered string | `none` (or `env` for `dotenv`) | Config files (`template`), `json`, `yaml`, `.env` (`dotenv`) |

In `envs:`, entries within a source are deterministically sorted by `(path, title, uuid)` and flattened across sources in declaration order (later entries/sources override colliding keys).

In `templates:`, `driver: template | json | yaml` receive `.entries` (`[]map[string]string`), while `driver: dotenv` flattens entries (optionally seeded from `from_env:`) into validated `KEY="VALUE"` lines.

---

## 5. Zero-Leak Policy Boundary & Masked Execution

`config.yaml` is the **sole security policy boundary**:

- **No plaintext extraction commands**: There are no `get`, `show`, `search`, `set`, `delete`, or `sync` subcommands, and no ad-hoc `run -e/-m` URI flags.
- **Always-redacted inspection**: `cloakenv inspect` lets you audit matched entries, rule traces, generated variable names, and delivery modes while unconditionally replacing every secret value with `<REDACTED>`.
- **Ephemeral delivery**: File-backed templates (`env_path`, `path`) prefer `/dev/shm` on Linux, are created with `0600` permissions (`O_NOFOLLOW`, `0700` parent dirs, no symlink traversal, no overwriting existing files), and are zeroed (`0x00`) and unlinked on exit or `SIGINT`/`SIGTERM` unless `persist: true`.
- **Streaming output masking**: Child `stdout` and `stderr` pass through an Aho-Corasick `MaskingWriter` that replaces any occurrence of injected secrets with `<REDACTED>` in real time.
