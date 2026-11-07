# Sentinel's Journal - Critical Security Learnings

## 2026-11-06 - Enforce Execution Validation Equivalence Between Runner and Engine
**Vulnerability:** Discrepancy between process execution validations in `internal/runner` (which validated null bytes in command arguments and blocked `.bat`/`.cmd` batch file execution on Windows) and the template engine's `exec` function in `internal/engine/template.go` (which only validated against empty name and `exec.ErrDot`). This exposed template-driven command execution to null-byte injection and Windows batch script argument injection risks.
**Learning:** When adding auxiliary command execution interfaces (such as template helper functions like `exec`), security validation policies (null byte filtering, Windows batch file restrictions, ErrDot relative path blocking) must be kept strictly equivalent to the primary process runner to maintain uniform defense-in-depth across the entire application.
**Prevention:** Unify or mirror validation rules across all command execution entry points, and write cross-platform test cases asserting null-byte rejection and batch file blocks.
