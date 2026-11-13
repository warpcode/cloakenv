# ADR-0002: Cross-Vault Secret Synchronization and Secure Transparent Retrieval Caching

| Metadata | Details |
|---|---|
| **Status** | Accepted |
| **Date** | 2026-11-04 |
| **Deciders** | warpcode |
| **Depends On** | ADR-0001 |
| **Reversibility** | One-way |

---

## 1. Context and Problem Statement

`cloakenv` serves as a dynamic secret orchestrator and runtime environment injector. In practical usage, two operational challenges emerge:
1. **Cross-Vault Synchronization**: Users frequently maintain primary secrets in secure offline vaults (e.g. KeePass `.kdbx` with Argon2) or local password stores (e.g. Unix `pass`), but need to synchronize a subset of these secrets into fast local storage (e.g. OS Keyring) or centralized team secret stores (e.g. HashiCorp Vault). Currently, cross-vault copying requires manual shell scripts calling `cloakenv get` and `cloakenv set` in loops.
2. **Expensive Retrievals**: Certain secret lookups involve high latency or heavy CPU cost (e.g. Argon2 KDBX key derivation on every invocation, remote HashiCorp Vault network latency, or slow multi-vault regex searches). `cloakenv` requires a caching mechanism to avoid repeating expensive retrievals, but must strictly uphold the invariant of **zero plaintext disk persistence** and prevent secret leakage in process memory.

---

## 2. Decision Drivers

- **P0 (Security & Non-Negotiables)**:
  - Zero plaintext disk persistence under all circumstances.
  - Intermediate byte buffers must be scrubbed (`utils.ZeroBytes`).
  - Secret payloads must never be leaked into logs, debug output, or stdout/stderr.
  - Safe against race conditions and cross-vault cache collisions.
- **P1 (Architectural Integrity)**:
  - Unidirectional synchronization (`source -> target`) only; avoid complex split-brain bidirectional reconciliation.
  - Explicit capability interfaces: providers capable of writing implement `provider.WritableProvider`. Read-only providers remain immutable.
  - Caching must be transparent and opt-in per vault, never global.
- **P2 (Ergonomics & Observability)**:
  - Mandatory `--dry-run` for synchronization commands to preview additions, updates, and skips before mutation.
  - Explicit cache lifecycle management: TTL expiration, lazy shredding on read, and explicit `cloakenv cache prune` / `clear`.

---

## 3. Considered Options

### For Cross-Vault Synchronization:
- **Option 1 (Unidirectional `cloakenv sync` with `WritableProvider` Interface)**: Introduce a dedicated subcommand `cloakenv sync <source> <target>` operating strictly in memory, filtering by tags/patterns, requiring explicit `--dry-run` preview.
- **Option 2 (Bidirectional two-way sync)**: Attempt two-way synchronization with timestamp conflict resolution. Rejected due to complexity, lack of reliable modification timestamps across all providers, and risk of accidental secret overwrites.

### For Retrieval Caching:
- **Option 1 (Dual-Mode Caching: OS Keyring or AES-256-GCM Encrypted Disk)**: Provide opt-in caching configurable per vault. Mode 1 (Keyring/Secure Enclave) stores secrets in memory via OS Keyring with zero disk I/O. Mode 2 (Encrypted Disk) encrypts cached payloads with AES-256-GCM using a dynamic key stored in OS Keyring.
- **Option 2 (Plaintext temporary disk caching)**: Strictly rejected; violates core security invariants.
- **Option 3 (Global unencrypted in-memory daemon)**: Rejected; requires long-running daemon infrastructure and breaks `syscall.Exec` process replacement.

---

## 4. Decision Outcome

**Chosen Options**:
1. **Unidirectional `cloakenv sync` via `provider.WritableProvider`**.
2. **Transparent Per-Vault Dual-Mode Caching (Keyring or AES-256-GCM Encrypted Disk)**.

### 4.1 Cross-Vault Synchronization Architecture

#### The `WritableProvider` Contract
Backends capable of receiving writes implement:
```go
package provider

type WritableProvider interface {
    SetSecret(ctx context.Context, location string, value string) error
    DeleteSecret(ctx context.Context, location string) error
}
```

- Providers implementing writable operations: `os_keyring` (`keyring://`) and `cache` (`cache://`) are active sync targets today. `vault` (HashiCorp Vault in `internal/provider/vault.go`) also implements `WritableProvider`, pending registration in `ProviderManager`.
- Read-only providers (`keepass`, `env`, `yaml`, `json`, `custom_vault`, `pass`, `search`) reject writes; `cloakenv sync` validates destinations up front and fails fast with clear errors when targeting read-only vaults.

#### Sync Command Semantics
```bash
cloakenv sync <source-uri-or-vault> <target-uri-or-vault> [flags]
```
- Flags:
  - `--tags <tag-filter>`: Only sync entries matching specified tags (comma-separated, e.g. `sync:keyring`).
  - `--prefix <prefix>`: Filter source secret keys by prefix (`strings.HasPrefix`).
  - `-r, --regex <pattern>`: Filter source secret keys using a regular expression.
  - `--dry-run`: Preview keys to be added/updated without executing writes.
- Future (deferred) options: `--overwrite=false` and `--prune` are not yet implemented.

---

### 4.2 Secure Retrieval Caching Architecture

#### Per-Vault Configuration Schema
In `config.yaml`, caching is configured per vault (supporting boolean `cache: true`, duration string `cache: "15m"`, or an explicit mapping):
```yaml
vaults:
  work:
    provider: "keepass"
    vault_path: "~/secrets/work.kdbx"
    cache:
      enabled: true
      ttl: "15m"
      mode: "keyring"   # Options: "keyring" (default) or "encrypted_disk"
```

#### Storage Modes & Security Invariants
1. **Mode: `keyring` (Default & Recommended)**:
   - Secret payloads are stored directly in the OS Keyring under service `<prefix>_vault_cache` and account `cache_v2/<sha256(vaultName + "\x00" + location)>`.
   - Zero disk persistence. Managed by OS secure enclave / credential service.
2. **Mode: `encrypted_disk`**:
   - Payloads are encrypted via AES-256-GCM before writing to `<UserCacheDir>/<prefix>/vaults/<vault_hash>/<entry_hash>` (typically `~/.cache/cloakenv/vaults/...`) with `0700` directory and `0600` file permissions.
   - The encryption key is stored exclusively in the OS Keyring under service `<prefix>` and account `cache_disk_token`.
   - Payloads contain an 8-byte big-endian write timestamp and an 8-byte TTL header.
   - Intermediate byte buffers are scrubbed immediately using `utils.ZeroBytes()`.
   - Expired entries are deleted on read; `cloakenv cache prune` removes expired `cache://` entries under `<UserCacheDir>/<prefix>`.

---

## 5. Consequences & Architectural Invariants

### Positive Consequences
- Developers can warm fast local stores (Keyring) from heavy cold vaults (KeePass) using safe, tag-filtered synchronization.
- Expensive remote calls and key derivations are cached transparently without code changes in consuming applications.
- Zero plaintext disk persistence invariant is strictly preserved.

### Trade-offs & Limitations
- Secrets cached in OS Keyring remain until TTL expiry or manual purge.
- `cloakenv sync` is strictly unidirectional; deletions in source are not propagated to target (deletion pruning such as `--prune` and non-overwriting mode `--overwrite=false` remain future enhancements).
