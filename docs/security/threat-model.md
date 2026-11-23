# Threat Model & Security Guarantees

`cloakenv` is designed around a **zero-leak policy boundary**: secrets live in read-only vaults, are transformed exclusively in memory according to `config.yaml`, and are delivered strictly to authorized child processes while scrubbing terminal output.

---

## 1. Zero-Leak CLI Policy Boundary

In many workflows—especially when AI coding agents or automated scripts run in your terminal—traditional secret managers expose `get`, `show`, or `export` commands that print plaintext credentials to `stdout`.

`cloakenv` enforces `config.yaml` as the single security policy boundary:
- **No plaintext CLI getters**: `get`, `show`, `search`, `set`, `delete`, and `sync` subcommands do not exist.
- **No ad-hoc CLI secret binding**: `cloakenv run` does not accept `-e`, `-m`, `-t`, `-i`, or `-f` flags to inject arbitrary vault URIs from the command line.
- **Unconditional redaction**: `cloakenv inspect` replaces every secret value with `<REDACTED>` and provides no `--raw` or `--unredacted` flag.
- **Verbatim command arguments**: Child command arguments are never scanned or expanded for templates or secret URIs.

---

## 2. Protection Against Disk Persistence & Symlink Attacks

- **In-memory first**: `envs:` and `templates:` delivered via `env: VAR` never touch disk.
- **Tmpfs preference (`/dev/shm`)**: Auto-ephemeral template files (`env_path: VAR` without `path:`) are created in `/dev/shm` on Linux when writable, keeping payloads in RAM-backed tmpfs rather than persistent storage.
- **Strict file creation (`WriteTemplateFile`)**:
  - Ancestor directories are walked from root downward (`rejectSymlinkParent`) to reject symlinked directories.
  - Missing parent directories are created with `0700` permissions.
  - Target files are opened exclusively (`O_CREATE|O_EXCL|O_NOFOLLOW`) with `0600` permissions, refusing to overwrite pre-existing files or follow symlinks.
- **Best-effort zero-on-exit**: Non-persistent files are overwritten with `0x00` bytes, synced (`fsync`), and unlinked on normal exit, `SIGINT`, or `SIGTERM`. (Note: `SIGKILL`, CoW filesystems, and SSD wear-leveling cannot guarantee physical block erasure; `/dev/shm` mitigates this on Linux.)

---

## 3. Process Memory & Buffer Hygiene

- Passwords read during `cloakenv auth login` and vault unlock are scrubbed from heap slices via `utils.ZeroBytes`.
- Pooled `bytes.Buffer` instances used during Go template evaluation are cleared (`clear(b[:cap(b)])`) before returning to `sync.Pool`.
- AES-256-GCM disk cache operations scrub intermediate plaintext buffers immediately after encryption/decryption.

---

## 4. Keyring Isolation

- `cloakenv` isolates its internal keyring entries (`cache_token`, `cache_disk_token`, `cache-master-key`, `provider/*`, `cache/*`, `cache_v2/*`) so the built-in `keyring` provider can never read or expose them.
- Setting `keyring.isolate: true` scopes keyring entries per `-c <config_path>` using a 10-character SHA-256 hash suffix.

---

## 5. Supply Chain Security

Official releases ship with SHA-256 `checksums.txt`, keyless Sigstore/Cosign bundles (`.sigstore.json`), and SLSA Level 3 build provenance attestations. See [Installation & Verification](../getting-started/installation.md#cryptographic-release-verification).
