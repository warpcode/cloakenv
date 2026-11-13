# Feature Specification & Plan: Cross-Vault Secret Synchronization & Secure Retrieval Caching

**Plan Date**: 2026-11-04  
**Target Architecture**: Go 1.26+  
**Relevant ADR**: [ADR-0002](../adr/0002-cross-vault-sync-and-secure-retrieval-caching.md)  
**Status**: Completed  

---

## 1. Overview & Goals

This charter specifies two high-priority capabilities for `cloakenv`:
1. **Unidirectional Secret Synchronization (`cloakenv sync`)**: Copy entries between vaults (e.g. KeePass -> OS Keyring, or local vault -> HashiCorp Vault) with tag-based filtering, key mapping, and pre-execution dry-run verification.
2. **Transparent Retrieval Caching**: Opt-in caching for expensive secret retrievals (Argon2 derivation, remote network calls) with zero plaintext disk persistence, strict TTL enforcement, and immediate buffer zeroization.

---

## 2. Work Breakdown Structure

### Phase 1: Writable Provider Contract & Sync Command
- **TSK-101**: Define `provider.WritableProvider` interface in `internal/provider/provider.go`.
- **TSK-102**: Implement `WritableProvider` on `internal/provider/os_keyring.go` (`SetSecret`, `DeleteSecret`).
- **TSK-103**: Build `internal/cmd/sync.go` implementing `cloakenv sync <source> <target> [flags]`.
- **TSK-104**: Implement `--dry-run`, `--tags`, `--prefix`, and `-r`/`--regex` flags for `cloakenv sync` (`--overwrite` deferred).
- **TSK-105**: Add unit and integration tests in `internal/cmd/sync_test.go` and `internal/provider/`.

### Phase 2: Transparent Retrieval Caching Layer
- **TSK-201**: Extend `internal/config/config.go` with per-vault `cache` configuration (`enabled`, `ttl`, `mode`).
- **TSK-202**: Implement `internal/provider/caching.go` wrapping any `SecretProvider` with opt-in caching.
- **TSK-203**: Implement `keyring` storage mode (storing short-lived cached secrets in OS Keyring with TTL metadata).
- **TSK-204**: Implement `encrypted_disk` storage mode (AES-256-GCM encryption with key in Keyring and zeroized buffers).
- **TSK-205**: Add `cloakenv cache prune` to purge expired cached secrets from disk and keyring.
- **TSK-206**: Benchmarking and test suite verification with `-race`.

---

## 3. Acceptance Criteria & Fitness Functions

- [x] `cloakenv sync` operates strictly in memory without intermediate plaintext disk writes.
- [x] Read-only destination vaults fail fast with clear diagnostic messages.
- [x] `--dry-run` accurately previews all key mutations without executing writes.
- [x] Secret values are never printed to terminal, logs, or error messages during sync.
- [x] Cached secrets on disk are always AES-256-GCM encrypted; zero plaintext files exist under `~/.cache/cloakenv/`.
- [x] Expired cache entries are immediately evicted on read.
- [x] `make verify` and `make agents-check` pass with zero regressions.
