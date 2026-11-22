# Structured Audit Logging

`cloakenv` includes optional structured audit logging to record every vault access attempt (without ever logging plaintext secret values or entry contents).

---

## Configuration

Enable audit logging in `~/.config/cloakenv/config.yaml`:

```yaml
audit:
  enabled: true
  log_path: "~/.config/cloakenv/audit.jsonl"   # Default when omitted
  format: "jsonl"                              # "jsonl" (default) or "json"
```

| Field | Default | Description |
|---|---|---|
| `enabled` | `false` | Enables or disables structured audit logging. |
| `log_path` | `~/.config/cloakenv/audit.jsonl` | Destination file path (`~/` and relative paths anchored to `config.yaml` directory). |
| `format` | `"jsonl"` | Must be `"jsonl"` or `"json"`. |

---

## Security & File Permissions

- Parent directories are created with `0700` permissions.
- The log file is opened append-only (`O_CREATE|O_WRONLY|O_APPEND`) with `0600` permissions (`chmod 0600` enforced on open) and synced (`fsync`) after every event.
- **Zero-secret guarantee**: Audit records only contain the vault target URI (`<vault>://`), caller binary, process/user metadata, and outcome—never secret keys, entry titles, or secret values.

---

## Event Schema

Each line is a JSON object (`audit.Event`):

```json
{
  "timestamp": "2026-11-22T19:30:12.481920Z",
  "uri": "work://",
  "target": "work://",
  "caller": "cloakenv",
  "pid": 48120,
  "uid": 1000,
  "subcommand": "run",
  "outcome": "SUCCESS"
}
```

| Field | Type | Description |
|---|---|---|
| `timestamp` | `string` | UTC RFC3339Nano timestamp. |
| `uri` | `string` | Vault URI accessed (e.g., `work://`). |
| `target` | `string` | Target vault URI. |
| `caller` | `string` | Basename of invoking binary (`os.Args[0]`). |
| `pid` | `int` | Process ID. |
| `uid` | `int` | Numeric user ID (omitted on Windows). |
| `subcommand` | `string` | CLI subcommand (`run`, `inspect`, etc.). |
| `outcome` | `string` | `"SUCCESS"`, `"DENIED"`, or `"ERROR"`. |
