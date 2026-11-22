# OS Keyring (`keyring`)

The `keyring` provider reads secrets directly from your operating system's native credential store:
- **macOS**: Keychain Services
- **Linux**: D-Bus Secret Service (`gnome-keyring`, `ksecretsservice`, KeePassXC)
- **Windows**: Windows Credential Manager

It is available **implicitly** as `vault: keyring` or can be declared explicitly under `vaults:`.

---

## Configuration Options

```yaml
# Optional global keyring settings
keyring:
  prefix: "cloakenv"   # Default service namespace
  isolate: false       # Set true to isolate keyring entries per `-c <config_path>`

vaults:
  os_secrets:
    provider: keyring
    query: "OPENAI_API_KEY,ANTHROPIC_API_KEY"  # Comma-separated attribute/account names
```

| Field | Required | Default | Description |
|---|---|---|---|
| `provider` | Yes | — | Must be `keyring`. |
| `query` | Recommended | `""` | Comma-separated list of keyring account/key names to fetch. |
| `cache` | No | disabled | Per-vault [caching configuration](caching.md). |

---

## Why `query:` is Needed on Native OS Keyrings

Standard OS credential stores (macOS Keychain, Linux Secret Service via `go-keyring`, Windows Credential Manager) do not provide a cross-platform `List` API. By specifying `query: "OPENAI_API_KEY,ANTHROPIC_API_KEY"`, `cloakenv` fetches those exact keys under your configured `keyring.prefix` service and returns them as a single flat `Entry`.

---

## Config Isolation (`keyring.isolate`)

When working across multiple projects with `-c ./project-a/cloakenv.yaml` and `-c ./project-b/cloakenv.yaml`, set `keyring.isolate: true`:

```yaml
keyring:
  prefix: "cloakenv"
  isolate: true
```

When `isolate: true` and `-c` points to a non-default config file, `cloakenv` computes a 10-character SHA-256 hash of the absolute config path and scopes the keyring service to `<prefix>_<hash10>`.

---

## Internal Keyring Isolation (Security Boundary)

To prevent accidental or malicious exposure of `cloakenv`'s own internal credentials, the `keyring` provider **strictly blocks** access to internal accounts:
- `cache_token`
- `cache_disk_token`
- `cache-master-key`
- `provider/*` (vault unlock passwords saved by `cloakenv auth login`)
- `cache/*` and `cache_v2/*`

Even if requested via `query:`, internal keys are never returned in any `Entry`.

---

## Example Configuration

```yaml
vaults:
  os_keyring:
    provider: keyring
    query: "OPENAI_API_KEY,ANTHROPIC_API_KEY"

envs:
  ai_keys:
    - vault: os_keyring
      rules:
        - type: pass
          include: ["OPENAI_*", "ANTHROPIC_*"]
          key_format: env
```
