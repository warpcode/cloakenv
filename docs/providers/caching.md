# Transparent Per-Vault Caching (`cache`)

Opening KeePass databases (Argon2 KDF) or querying remote HashiCorp Vault servers on every CLI invocation can add noticeable latency. `cloakenv` provides opt-in, per-vault retrieval caching with zero plaintext disk persistence.

---

## Configuration

### Global Default TTL
```yaml
cache:
  default_ttl: "10m"   # Used when a vault enables caching without specifying ttl
```

### Per-Vault Shorthand & Full Mapping
`cache:` on any vault accepts three forms:

```yaml
vaults:
  # 1. Boolean shorthand (uses cache.default_ttl and mode: "keyring")
  work_bool:
    provider: keepass
    vault_path: ~/secrets/work.kdbx
    cache: true

  # 2. Duration string shorthand (enables cache with custom TTL and mode: "keyring")
  work_ttl:
    provider: pass
    vault_path: ~/.password-store
    cache: "15m"

  # 3. Full mapping
  work_full:
    provider: vault
    address: https://vault.example.com:8200
    cache:
      enabled: true
      ttl: "15m"
      mode: "encrypted_disk"   # "keyring" (default) or "encrypted_disk"
```

---

## Storage Modes

| Mode | Storage Location | Encryption & Key Management | Disk Footprint |
|---|---|---|---|
| **`keyring`** (default) | OS Keyring service `<prefix>_vault_cache`, account `cache_v2/<sha256(vaultName + "\x00" + location)>` | Managed by OS Keychain / Secret Service / Credential Manager | **Zero** disk files |
| **`encrypted_disk`** | `<UserCacheDir>/<prefix>/vaults/<vault_hash>/<entry_hash>` (typically `~/.cache/cloakenv/vaults/...`) | **AES-256-GCM** using a 32-byte random master key stored in OS keyring (`cache_disk_token`). Files created with `0600` inside `0700` dirs. | Encrypted blobs only |

### `encrypted_disk` Binary Layout & Scrubbing
- Each encrypted cache file stores a 16-byte header (`[8-byte big-endian write timestamp][8-byte TTL]`) followed by the AES-256-GCM ciphertext.
- Intermediate plaintext buffers are scrubbed in memory using `utils.ZeroBytes`.
- Expired entries are automatically deleted on read.

---

## Managing the Cache

### Prune Expired Entries
Remove only expired cache entries:
```bash
cloakenv cache prune
```

### Clear All Cached Entries
Purge all cached entries across all vaults:
```bash
cloakenv cache clear
```

### Verify Cache Directory Permissions
`cloakenv doctor` checks that `<UserCacheDir>/<prefix>` has `0700` permissions and flags any discrepancies.
