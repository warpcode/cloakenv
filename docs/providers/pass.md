# Password Store (`pass`)

The `pass` provider reads GPG-encrypted `.gpg` entries from a standard [Unix Password Store](https://www.passwordstore.org/) directory.

---

## Prerequisites

- The `gpg` binary must be installed and available in `PATH`.
- Your GPG private key must be accessible (via `gpg-agent` / pinentry) to decrypt `.gpg` files.
- Verify readiness with `cloakenv doctor`.

---

## Configuration Options

```yaml
vaults:
  pass_store:
    provider: pass
    vault_path: ~/.password-store   # Optional: defaults to $PASSWORD_STORE_DIR or ~/.password-store
    query: work/                    # Optional: scope lookups to a subdirectory prefix
    cache: "10m"                    # Optional: transparent caching
```

| Field | Required | Default | Description |
|---|---|---|---|
| `provider` | Yes | — | Must be `pass`. |
| `vault_path` | No | `$PASSWORD_STORE_DIR` or `~/.password-store` | Path to the password store directory. |
| `query` | No | `""` | Subdirectory/entry prefix to scope lookups (rejects `..` path traversal). |
| `cache` | No | disabled | Per-vault [caching configuration](caching.md). |

---

## `.gpg` Entry Parsing Rules

Each `.gpg` file inside `vault_path` (excluding hidden `.git` / `.gpg-id` files) is decrypted in memory via `gpg --decrypt --quiet --batch` and parsed into a flat `Entry`:

1. **Line 1 (Password)**:
   - Always stored as both `Password` and `password`.
2. **Subsequent Lines (`Key: Value`)**:
   - Lines formatted as `Key: Value` (split on the first `:`) are exposed as top-level attributes (e.g., `username: alice`, `api_key: sk-...`).
   - Lines starting with `tags:` or `Tags:` are parsed into the canonical `tags` slice (splitting on commas or whitespace).
3. **Canonical Metadata**:
   - `path`: Relative path without `.gpg` extension (e.g., `"work/openai"`)
   - `title`: File basename without `.gpg` (e.g., `"openai"`)
   - `group`: `{ name, path }` derived from the parent directory (e.g., `name: "work"`, `path: "work"`)

### Example `.gpg` File Content
```text
sk-live-1234567890
username: svc-ai
api_base: https://api.openai.com/v1
tags: ai, litellm, prod
```

### Resulting `Entry` Map
- `title`: `"openai"`
- `path`: `"work/openai"`
- `group`: `{ name: "work", path: "work" }`
- `tags`: `["ai", "litellm", "prod"]`
- `Password` / `password`: `"sk-live-1234567890"`
- `username`: `"svc-ai"`
- `api_base`: `"https://api.openai.com/v1"`

---

## Example Configuration

```yaml
vaults:
  unix_pass:
    provider: pass
    vault_path: ~/.password-store
    query: work/
    cache:
      enabled: true
      ttl: "10m"
      mode: "keyring"

envs:
  ai_env:
    - vault: unix_pass
      query: '{{ hasTag "litellm" .tags }}'
      rules:
        - type: set
          values:
            "{{ .title | env }}_API_KEY": "{{ .password }}"
            "{{ .title | env }}_API_BASE": "{{ .api_base }}"
```
