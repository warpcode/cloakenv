# Test Data & Fixtures (`testdata/`)

> [!CAUTION]
> **Testdata credentials are strictly for automated testing and local development.**
> These credentials and certificates must NEVER be used in production environments or deployed infrastructure.

This directory houses static database fixtures, YAML/JSON configuration files, and environment templates used across `cloakenv` unit tests, integration tests, and performance benchmarks.

---

## 🔑 Primary KeePass Fixture (`testDB.kdbx`)

The KeePass 4 database fixture at `testdata/testDB.kdbx` provides a deterministic credential hierarchy for testing `keepass` provider resolution, placeholder references, file attachment extraction, and field filtering.

### Fixture Credentials & Attributes

| Property | Value | Description |
|---|---|---|
| **Database File** | `testdata/testDB.kdbx` | KeePass 4 encrypted database |
| **Master Password** | `password123` | Master password for unlocking the database |
| **Mock Keyring Target** | `service: cloakenv`, `account: provider/testdb` | Keyring entry used by unit tests (`keyring.Set`) |
| **Group Hierarchy** | `website` | Top-level group beneath root |
| **Entry Title** | `Test Website` | Entry title attribute |
| **Canonical Path** | `website/Test Website` | Relative path from root |
| **Root-Prefixed Path** | `Root/website/Test Website` | Absolute group path supported by provider |
| **Default Secret (`Password`)**| `testPassword123!` | Resolved when attribute selector is omitted |
| **Standard Attribute (`UserName`)** | `user@email.com` | Resolved via `website/Test Website:UserName` |
| **Attachment (`hello.txt`)** | `Hello, World! Extraction Test Successful.` | Resolved via `website/Test Website:hello.txt` |

### Provider-Generated Metadata Fields

When queried via `GetEntry` or templated in `.meta`, the provider dynamically exposes:
- **`uuid` / `UUID`**: 32-character hexadecimal entry UUID.
- **`group` / `group_name`**: `website`
- **`group_uuid`**: 32-character hexadecimal group UUID.
- **`group_path`**: `website`

---

## 🧪 Quick Test Verification Commands

You can verify local installation and authentication against `testDB.kdbx` using the CLI:

```bash
# 1. Unlock the test database in keyring
echo "password123" | cloakenv -c testdata/test_config.yaml auth login testdb

# 2. Resolve default Password attribute
cloakenv -c testdata/test_config.yaml get "testdb://website/Test Website"

# 3. Resolve UserName attribute
cloakenv -c testdata/test_config.yaml get "testdb://website/Test Website:UserName"

# 4. Extract attached file
cloakenv -c testdata/test_config.yaml get "testdb://website/Test Website:hello.txt"

# 5. Clear credentials
cloakenv -c testdata/test_config.yaml auth forget testdb
```

---

## 📁 Additional Fixture Files

| File | Purpose |
|---|---|
| `test_config.yaml` | Sample configuration mapping `testdb` vault to `testDB.kdbx`. |
| `test_keepass_config.yaml` | Minimal configuration for KeePass provider tests. |
| `test_search_config.yaml` | Search provider configuration testing stored queries over `testdb`. |
| `test_custom_config.yaml` | Composed and flat custom vault entity test definitions. |
| `test_custom_json_config.yaml` | Custom vault entity definitions referencing JSON sources. |
| `test_entries.yaml` | Static YAML provider fixture containing structured records. |
| `test_hosts.yaml` | YAML multi-host fixture for static path and filter tests. |
| `test_hosts.json` | JSON equivalent of `test_hosts.yaml`. |
| `test_root.yaml` | Root-keyed static fixture for `entities_root_key` testing. |
| `test_commands_config.yaml` | Autoload command matching and alias test rules. |
| `test_invalid_key_config.yaml` | Configuration fixture verifying error handling on invalid keys. |
| `invalid_config.yaml` | Malformed YAML fixture testing config unmarshaling errors. |
| `test_env.txt` | Environment variable template fixture. |
| `test_env_override.txt` | Environment override test fixture. |
| `test_template.env` | Template `.env` file testing variable interpolation. |
| `test_tags_check.go` | Inspection script verifying KeePass entry hierarchy and tags in `testDB.kdbx`. |
