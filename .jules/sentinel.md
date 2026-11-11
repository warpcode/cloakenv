# Sentinel's Journal - Critical Security Learnings

## 2026-11-10 - Raw Keyring Input and Null-Byte Target Validation
**Vulnerability:** OS keyring operations accepting raw target parameters (`SetRawSecret` and `DeleteRawSecret`) did not validate `service` and `account` strings, allowing null bytes (`\x00`) and empty strings to pass to native OS keyring APIs (Keychain, Secret Service via D-Bus, Windows Credential Manager).
**Learning:** URI parsing layers often enforce security sanitization (such as `parseKeyringLocation`), but lower-level raw accessor methods that bypass URI parsing may remain unvalidated if not guarded independently.
**Prevention:** Enforce input and null-byte validation at the low-level provider API boundary (`validateRawKeyringTarget`) to ensure defense-in-depth across both URI-driven and direct programmatic credential interactions.

## 2026-11-06 - Enforce Execution Validation Equivalence Between Runner and Engine
**Vulnerability:** Discrepancy between process execution validations in `internal/runner` (which validated null bytes in command arguments and blocked `.bat`/`.cmd` batch file execution on Windows) and the template engine's `exec` function in `internal/engine/template.go` (which only validated against empty name and `exec.ErrDot`). This exposed template-driven command execution to null-byte injection and Windows batch script argument injection risks.
**Learning:** When adding auxiliary command execution interfaces (such as template helper functions like `exec`), security validation policies (null byte filtering, Windows batch file restrictions, ErrDot relative path blocking) must be kept strictly equivalent to the primary process runner to maintain uniform defense-in-depth across the entire application.
**Prevention:** Unify or mirror validation rules across all command execution entry points, and write cross-platform test cases asserting null-byte rejection and batch file blocks.
