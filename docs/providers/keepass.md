# KeePass (`keepass`)

The `keepass` provider reads entries directly from KeePass `.kdbx` databases (KDBX v3 and v4, including Argon2 and AES-KDF) in read-only mode.

---

## Configuration Options

```yaml
vaults:
  work:
    provider: keepass
    vault_path: ~/secrets/work.kdbx
    key_file: ~/secrets/work.keyx        # Optional: .key, .keyx, or raw key file
    key_file_env: WORK_KEEPASS_KEYFILE   # Optional: env var overriding key_file when set
    resolve_references: false            # Optional: expand KeePass {REF:...} & placeholders (default: false)
    query: "Production/"                 # Optional: group/entry path prefix scope
    cache:                               # Optional: transparent caching
      enabled: true
      ttl: "15m"
      mode: "keyring"
```

| Field | Required | Default | Description |
|---|---|---|---|
| `provider` | Yes | — | Must be `keepass`. |
| `vault_path` | Yes | — | Path to the `.kdbx` file (`~/` and relative paths anchored to `config.yaml` directory). |
| `key_file` | No | `""` | Path to a KeePass key file (`.key`, `.keyx`, or raw key). |
| `key_file_env` | No | `""` | Environment variable name containing a key file path; overrides `key_file` when non-empty. |
| `resolve_references` | No | `false` | Enables expansion of KeePass `{REF:...}` cross-entry references and `{TITLE}`/`{USERNAME}`/`{PASSWORD}`/`{URL}`/`{NOTES}`/`{S:...}` placeholders. |
| `query` | No | `""` | Optional group/entry path prefix to scope which entries are returned. |
| `cache` | No | disabled | Per-vault [caching configuration](caching.md). |

---

## Authentication Flows

### 1. Master Password via OS Keyring (Recommended)
Store the database master password in your OS keyring once:

```bash
cloakenv auth login work
```

`cloakenv` stores the password under keyring service `<keyring.prefix>` (default `cloakenv`) and account `provider/<vault_name>` (e.g., `provider/work`). You can also pipe the password via stdin in automated setups.

Verify or remove stored credentials:
```bash
cloakenv auth status work
cloakenv auth forget work
```

### 2. Key-File-Only Database
If `key_file` (or `key_file_env`) is configured and the `.kdbx` database has no master password (key file only), `cloakenv` unlocks it automatically without requiring `cloakenv auth login`.

### 3. Composite Master Password + Key File
Configure `key_file` (or `key_file_env`) in `config.yaml` **and** run `cloakenv auth login <vault>` to store the master password. Both are combined during KDBX decryption.

---

## Entry Fields & Metadata

Each non-deleted KeePass entry (recycle-bin entries are excluded) is returned as a flat `Entry` containing:

- **Canonical metadata**:
  - `title` (`string`): Entry `Title`
  - `path` (`string`): Full group + title path (e.g., `"Production/Database"`)
  - `uuid` (`string`): Base64/hex entry UUID
  - `tags` (`[]string`): KeePass entry tags (case-preserved)
  - `group` (`map[string]any`): `{ name, path, uuid }`
- **Standard & custom fields**:
  - `Title`, `UserName`, `Password`, `URL`, `Notes`
  - `password` (automatic lowercase alias of `Password` when present, so `{{ .password }}` works alongside `{{ .Password }}`)
  - All custom string fields defined on the entry
  - Binary attachments (loaded into memory as string fields by attachment name)

---

## Reference Resolution (`resolve_references: true`)

When `resolve_references: true` is set, `cloakenv` expands KeePass placeholders and cross-entry references natively:
- Entry placeholders: `{TITLE}`, `{USERNAME}`, `{PASSWORD}`, `{URL}`, `{NOTES}`, `{S:CustomField}`
- Cross-entry references: `{REF:<WantedField>@<SearchIn>:<Text>}` (for example, `{REF:P@T:Shared DB}` or `{REF:U@I:<UUID>}`)

Circular references are detected and fail safely with an error.

---

## Example: Multi-Environment KeePass Configuration

```yaml
vaults:
  corp:
    provider: keepass
    vault_path: ~/vaults/corp.kdbx
    key_file_env: CORP_KDBX_KEY
    resolve_references: true
    query: "Services/"
    cache: "15m"

envs:
  backend_prod:
    - vault: corp
      query: '{{ and (hasTag "prod" .tags) (eq .group.name "Backend") }}'
      rules:
        - type: pass
          include: ["DB_*", "REDIS_*"]
          key_format: env
        - type: set
          values:
            DATABASE_URL: 'postgres://{{ .UserName }}:{{ .password }}@{{ .URL }}/prod'
```
