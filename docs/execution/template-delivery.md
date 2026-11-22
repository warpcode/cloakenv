# Ephemeral Template Delivery

`autoload[].templates[]` delivers rendered `templates:` payloads to child commands using four distinct delivery modes—ranging from pure in-memory environment variables to ephemeral `0600` files in `/dev/shm` that are zeroed and unlinked on exit.

---

## The 4 Delivery Modes

| Mode | Fields Set | Behavior | Cleanup on Exit |
|---|---|---|---|
| **1. In-Memory Env** | `env: VAR` | Sets `$VAR` directly to the rendered template string. Zero disk writes. | N/A (in-memory) |
| **2. Auto-Ephemeral File** | `env_path: VAR` | Creates a `0600` temp file (`cloakenv-ephemeral-*` in `/dev/shm` on Linux, or `os.TempDir()`), sets `$VAR` to its path. | **Yes** (`0x00` zero + unlink) |
| **3. Explicit / Templated Path + Env** | `env_path: VAR` + `path: "..."` | Templates `path:` against resolved `envs`, writes `0600` file, sets `$VAR` to the resolved path. | **Yes** unless `persist: true` |
| **4. Explicit / Templated Path Only** | `path: "..."` | Templates `path:` against resolved `envs`, writes `0600` file without setting an env var. | **Yes** unless `persist: true` |

### Configuration Examples

```yaml
autoload:
  - match: '^aws(\s|$)'
    templates:
      # Mode 1: Pure in-memory env var
      - template: service_json
        env: SERVICE_ACCOUNT_JSON

  - match: '^deploy\s+'
    templates:
      # Mode 2: Auto-ephemeral file in /dev/shm (or TempDir)
      - template: app_dotenv
        env_path: APP_ENV_FILE

  - match: '^kubectl(\s|$)'
    envs:
      - app_env
    templates:
      # Mode 3: Templated path + env_path (zeroed & unlinked on exit)
      - template: clusters_yaml
        env_path: KUBECONFIG
        path: "/tmp/kubeconfig-{{ .USER }}.yaml"
        persist: false

  - match: '^render-config$'
    templates:
      # Mode 4: Persistent build artifact (no env var, kept after exit)
      - template: inline_config
        path: "./build/generated.conf"
        persist: true
```

---

## Validation Constraints

`cloakenv doctor` / `config.Validate` enforces the following rules:
- At least one of `env`, `env_path`, or `path` must be set.
- `env` cannot be combined with `env_path` or `path`.
- `persist: true` cannot be combined with `env` (no file is written).
- Duplicate `env` / `env_path` variable names or duplicate literal `path` targets within the same `autoload` rule are rejected.

---

## Filesystem Safety & Zero-on-Exit Lifecycle

When materializing explicit `path:` targets (`WriteTemplateFile`), `cloakenv` enforces strict filesystem hardening:

1. **No Overwrites**: Refuses to overwrite any pre-existing file or directory at `path`.
2. **Symlink Rejection**: Walks every ancestor directory from root downward (`rejectSymlinkParent`) and refuses symlinked directories or symlinked target files (while permitting OS-managed macOS root symlinks `/var`, `/tmp`, `/etc`).
3. **Strict Permissions**: Creates missing parent directories with `0700` and opens the target file exclusively (`O_CREATE|O_EXCL|O_NOFOLLOW`) with `0600` permissions.
4. **Zero-on-Exit & Signal Cleanup**:
   - On normal child exit, `SIGINT`, or `SIGTERM`, `SecureZeroAndRemove` opens every non-persistent file (`O_NOFOLLOW`), overwrites its exact byte length with `0x00`, calls `Sync()`, and unlinks it.
   - If a process is terminated abruptly with `SIGKILL`, `cloakenv doctor` detects leftover `cloakenv-ephemeral-*` files in `/dev/shm` and `os.TempDir()`.

> [!NOTE]
> Overwriting with `0x00` is best-effort on copy-on-write (CoW), journalled, or SSD-backed filesystems. On Linux, `/dev/shm` (tmpfs in RAM) is preferred automatically for Mode 2 (`env_path` without `path`).
