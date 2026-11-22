# HashiCorp Vault (`vault`)

The `vault` provider reads secrets from HashiCorp Vault KV secrets engines (v1 and v2) over HTTP/HTTPS in read-only mode.

---

## Configuration Options

```yaml
vaults:
  hcp_vault:
    provider: vault
    address: https://vault.example.com:8200   # Or set VAULT_ADDR
    mount_path: secret                        # KV engine mount path
    engine_version: 2                         # 1 or 2 (default: 2)
    namespace: admin                          # Optional: Vault Enterprise namespace
    query: prod/                              # Optional: path prefix scope
    # token: "hvs.EXAMPLE"                    # Optional: or set VAULT_TOKEN
    # role_id: "..."                          # Optional: AppRole role_id (or VAULT_ROLE_ID)
    # secret_id: "..."                        # Optional: AppRole secret_id (or VAULT_SECRET_ID)
    cache: "5m"
```

| Field | Required | Default | Description |
|---|---|---|---|
| `provider` | Yes | — | Must be `vault`. |
| `address` | Yes* | `$VAULT_ADDR` | Vault server URL (`http://` or `https://`). |
| `mount_path` | No | `secret` | KV secrets engine mount point. |
| `engine_version` | No | `2` | KV engine version (`1` or `2`). |
| `namespace` | No | `""` | Vault Enterprise namespace header (`X-Vault-Namespace`). |
| `query` | No | `""` | Path prefix to scope recursive secret listing (e.g., `prod/`). |
| `token` | No* | `$VAULT_TOKEN` | Static/session Vault token. |
| `role_id` | No* | `$VAULT_ROLE_ID` | AppRole Role ID (must be paired with `secret_id`). |
| `secret_id` | No* | `$VAULT_SECRET_ID` | AppRole Secret ID (must be paired with `role_id`). |
| `cache` | No | disabled | Per-vault [caching configuration](caching.md). |

---

## Authentication Methods

1. **Environment Variables (Recommended for CI/CD)**:
   Set `VAULT_ADDR` and either `VAULT_TOKEN` or `VAULT_ROLE_ID` + `VAULT_SECRET_ID`.
2. **AppRole Authentication**:
   Configure `role_id` and `secret_id` (or `VAULT_ROLE_ID` and `VAULT_SECRET_ID`). `cloakenv` exchanges them via `POST /v1/auth/approle/login` for a short-lived client token in memory.
3. **Static Token in Config**:
   Configure `token` directly in `config.yaml` (discouraged for shared repositories).

> [!WARNING]
> Never commit plaintext `token` or `secret_id` values into a shared `config.yaml`. Prefer `VAULT_TOKEN` or `VAULT_ROLE_ID`/`VAULT_SECRET_ID`.

---

## KV Engine Traversal & Flat `Entry` Mapping

- **KV v2** (`engine_version: 2`, default): Lists keys via `?list=true` under `<mount_path>/metadata/<query>` and reads secret payloads from `<mount_path>/data/<path>`.
- **KV v1** (`engine_version: 1`): Lists keys via `?list=true` under `<mount_path>/<query>` and reads secret payloads from `<mount_path>/<path>`.
- Each secret path becomes one flat `Entry`:
  - `path`: Relative secret path under the mount
  - `title`: Leaf secret name
  - `group`: Parent folder metadata (`name`, `path`)
  - `tags`: Extracted from `custom_metadata.tags` or secret `tags` field when present
  - All secret K/V pairs from `data` sit at the top level of the `Entry`.

---

## Example Configuration

```yaml
vaults:
  prod_vault:
    provider: vault
    address: https://vault.internal:8200
    mount_path: kv
    engine_version: 2
    query: apps/payments/
    cache:
      enabled: true
      ttl: "5m"
      mode: "keyring"

envs:
  payments_env:
    - vault: prod_vault
      query: '{{ eq .title "stripe" }}'
      rules:
        - type: pass
          include: ["STRIPE_*"]
          key_format: env
```
