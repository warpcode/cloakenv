# Test Data & Fixtures (`testdata/`)

> [!CAUTION]
> **Testdata credentials are strictly for automated testing and local development.**
> These credentials and fixtures must NEVER be used in production environments or deployed infrastructure.

This directory houses static fixtures used by `cloakenv` unit tests and benchmarks.

---

## Primary KeePass Fixture (`testDB.kdbx`)

The KeePass 4 database fixture at `testdata/testDB.kdbx` provides a deterministic credential hierarchy for testing `keepass` provider resolution, placeholder references, file attachment extraction, and `RuleEngine` transforms.

| Property | Value | Description |
|---|---|---|
| **Database File** | `testdata/testDB.kdbx` | KeePass 4 encrypted database |
| **Master Password** | `password123` | Master password for unlocking the database |
| **Mock Keyring Target** | `service: cloakenv`, `account: provider/testdb` | Keyring entry used by unit tests (`keyring.MockInit`) |
| **Group Hierarchy** | `website` | Top-level group beneath root |
| **Entry Title** | `Test Website` | Entry title attribute |
| **Canonical Path** | `website/Test Website` | Relative path from root |
| **Root-Prefixed Path** | `Root/website/Test Website` | Absolute group path supported by provider |
| **Password (`Password` / `password`)** | `testPassword123!` | Password attribute (exposed under both `Password` and `password`) |
| **Standard Attribute (`UserName`)** | `user@email.com` | Username field |
| **Attachment (`hello.txt`)** | `Hello, World! Extraction Test Successful.` | Raw binary attachment attribute |

---

## Quick Verification

```bash
go test -race ./internal/provider -run TestKeePassProvider
```
