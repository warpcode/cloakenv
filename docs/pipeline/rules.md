# Transformation Rules (`rules:`)

Inside `envs:` and `templates:`, each vault source transforms every selected `Entry` through an ordered `rules:` list using `RuleEngine`.

---

## Execution & Precedence Model

- **Omitted `rules:`**: If `rules:` is omitted or empty, a single `type: pass` rule runs automatically using the pipeline's default `key_format` (`env` for `envs:` and `dotenv`; `none` for `template`, `json`, and `yaml`).
- **Explicit `rules:`**: When you specify `rules:`, **only** the listed rules execute, in sequential order. Raw fields do **not** pass through unless you include a `type: pass` rule.
- **Last-rule-wins**: Every rule writes into the entry's output map (`map[string]string`). If a later rule writes the same key as an earlier rule, the later rule overwrites it.

---

## Rule Type 1: `pass`

Copies fields from the raw `Entry` matching `include` and `exclude` globs (`path.Match` syntax), formatting keys with `key_format`.

```yaml
- type: pass
  include: ["DB_*", "REDIS_*"]   # Optional: defaults to ["*"]
  exclude: ["*_NOTES", "*_TMP"]  # Optional: exclude overrides include
  key_format: env                # Optional: "env", "snake", "kebab", or "none"
```

### `key_format` Options
| Value | Example Input | Output |
|---|---|---|
| `env` | `apiKey` / `db-host` | `API_KEY` / `DB_HOST` |
| `snake` | `ApiKey` / `DB-Host` | `api_key` / `db_host` |
| `kebab` | `ApiKey` / `DB_Host` | `api-key` / `db-host` |
| `none` | `UserName` | `UserName` (unchanged) |

### Reserved Metadata Protection
Catch-all wildcards (`*`, `**`) automatically **skip** reserved metadata keys (`title`, `path`, `uuid`, `tags`, `group`, `vault`, `provider`) so they do not leak as `TITLE` or `UUID` environment variables. To include a metadata key via `pass`, name it explicitly in `include:`:

```yaml
- type: pass
  include: ["title", "URL", "UserName"]
  key_format: none
```

---

## Rule Type 2: `match`

Filters raw entry fields by key regex (`match`) and/or serialized value regex (`value_match`), rewriting target `key` and optional `value` using capture groups and Go `text/template`.

```yaml
- type: match
  match: "^env:(.*)$"          # Regex against raw field key (optional if value_match set)
  value_match: "^sk-.*"        # Regex against serialized field value (optional if match set)
  key: "$1"                    # Required: target key expression
  value: "{{ .value }}"        # Optional: target value expression (defaults to raw value)
```

### Available Variables & Capture Groups
- `$0`..`$N` and `${1}`..`${N}` (or `.captures`): Regex capture groups from `match` (for `key`) or `value_match` (for `value`, falling back to `match` captures if `value_match` is unset).
- `.key`: Original raw field key.
- `.value`: Original serialized field value.
- `.output`: Map of keys written by earlier rules on the same entry.
- Top-level entry fields: `.title`, `.path`, `.uuid`, `.tags`, `.group`, `.vault`, `.provider`, and any entry attribute.

> [!NOTE]
> Capture placeholders (`$1`, `${1}`) inside Go templates are safely rewritten before template parsing (`RewriteCapturePlaceholdersForTemplate`) so runtime values containing `{{` can never cause template injection.

### Examples
```yaml
# Strip "env:" prefix from KeePass custom fields
- type: match
  match: "^env:(.*)$"
  key: "$1"

# Rename Password using the entry title
- type: match
  match: "^Password$"
  value_match: "^(.+)$"
  key: "{{ .title | env }}_SECRET"
  value: "{{ .value }}"
```

---

## Rule Type 3: `set`

Evaluates explicit `key: value` template pairs against the entry and `.output`, ideal for computing composite connection strings or standardized variable names.

```yaml
- type: set
  values:
    "{{ .title | env }}_API_KEY": "{{ .password }}"
    DATABASE_URL: 'postgres://{{ .UserName }}:{{ .password }}@{{ default "localhost" .URL }}:5432/app'
    APP_ENV: "production"
```

### Rules for `set`
- Both keys and values in `values:` are evaluated in deterministic sorted key order.
- Templates can reference any raw entry field (`.password`, `.UserName`), canonical metadata (`.title`, `.tags`), and `.output` (keys produced by earlier `pass`/`match` rules on the same entry).
- If a `set` key evaluates to `""`, it is skipped.

---

## Complete Multi-Rule Trace Example

```yaml
rules:
  # 1. Pass DB_* fields as UPPER_SNAKE_CASE
  - type: pass
    include: ["DB_*"]
    exclude: ["DB_NOTES"]
    key_format: env
  # 2. Extract any "tf_var:foo" field as "TF_VAR_foo"
  - type: match
    match: "^tf_var:(.*)$"
    key: "TF_VAR_$1"
  # 3. Synthesize DATABASE_URL using raw fields and overwrite DB_HOST if needed
  - type: set
    values:
      DATABASE_URL: 'postgres://{{ .UserName }}:{{ .password }}@{{ .output.DB_HOST }}:5432/app'
```
