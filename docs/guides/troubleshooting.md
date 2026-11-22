# Guide: Troubleshooting & Diagnostics

Start every troubleshooting session with `cloakenv doctor --strict` and `cloakenv inspect`—both are read-only and never expose plaintext secrets.

---

## 1. `map has no entry for key` (Template Execution Failure)

**Symptom**:
```text
Secret resolution failed: ... map has no entry for key "URL"
```

**Cause**:
`cloakenv` compiles all Go `text/template` expressions with `.Option("missingkey=error")` so missing attributes fail loudly instead of rendering `<no value>`.

**Resolution**:
Use `default` for optional attributes:
```yaml
DATABASE_URL: 'postgres://{{ .UserName }}:{{ .password }}@{{ default "localhost" .URL }}:5432/app'
```
Or check `.output` if you are referencing a key produced by an earlier rule in the same entry.

---

## 2. Legacy Vault Key Rejected (`config.Validate`)

**Symptom**:
```text
vault "work": legacy key "include_fields" is no longer supported (use 'rules:' with 'type: pass' and 'include:' inside 'envs:' or 'templates:')
```

**Cause**:
Vaults are pure read-only retrievers. Filtering, mapping, formatting, and computed attributes moved from `vaults:` to `rules:` inside `envs:` and `templates:`.

**Resolution**:
Follow the [Migration Guide](../reference/migration-guide.md) to move `include_fields`, `exclude_fields`, `mapping`, `key_format`, and `attributes` into `envs:` / `templates:`.

---

## 3. Linux Keyring / D-Bus Errors

**Symptom**:
`cloakenv auth login` or `keyring` lookup fails with `dbus` / `org.freedesktop.secrets` errors.

**Resolution**:
- Ensure a Secret Service daemon (`gnome-keyring-daemon`, `ksecretsservice`, or KeePassXC Secret Service) is running and unlocked.
- Verify `DBUS_SESSION_BUS_ADDRESS` is exported in your shell session.
- In headless environments (SSH without D-Bus, Docker, CI), use `VAULT_TOKEN` / AppRole, `vault: env`, or key-file-only KeePass (`key_file_env`).

---

## 4. Template Delivery File Errors (`WriteTemplateFile`)

**Symptom**:
- `refusing to overwrite pre-existing file "/path/to/file"`
- `refusing to write template through symlink "/path/to/dir"`

**Cause**:
`cloakenv` refuses to overwrite existing files or traverse symlinked directories/targets when materializing `path:` templates.

**Resolution**:
- Omit `path:` and use `env_path: MY_VAR` alone so `cloakenv` creates a fresh ephemeral file in `/dev/shm` or `os.TempDir()`.
- Or remove the stale target file / ensure the target path does not pass through a symlink.

---

## 5. Stale Ephemeral Files Reported by `cloakenv doctor`

**Symptom**:
```text
[WARN] Stale ephemeral file found: /dev/shm/cloakenv-ephemeral-12345 (from an aborted run; remove manually if no cloakenv process is active)
```

**Cause**:
A previous `cloakenv run` process was terminated with `SIGKILL` (`kill -9`) or crashed before its exit cleanup could run.

**Resolution**:
Verify no `cloakenv` process is currently using the file, then remove it:
```bash
rm -f /dev/shm/cloakenv-ephemeral-*
```

---

## 6. Ambiguous `inspect <name>` Target

**Symptom**:
```text
Ambiguous inspect target "app" exists in both envs and templates; specify 'inspect env app' or 'inspect template app'
```

**Resolution**:
Specify the explicit kind:
```bash
cloakenv inspect env app
cloakenv inspect template app
```
