## 2026-07-03 - Bounded Secret Stdin Reading & Pipe Testing
**Vulnerability:** Reading secret data from piped `os.Stdin` without length bounds (`io.ReadAll(os.Stdin)`) exposes the CLI to memory exhaustion DoS attacks if piped an infinite or large stream.
**Learning:** Piped stdin reads for secrets must be wrapped with `io.LimitReader(os.Stdin, maxSecretSize+1)` to enforce size limits before allocating memory or heap strings. In tests mocking `os.Stdin` via `os.Pipe`, payloads larger than the OS pipe buffer size (~64 KB) must be written asynchronously in a goroutine to avoid deadlocking `Write`.
**Prevention:** Always use `io.LimitReader` when reading unbuffered streams into memory and write pipe inputs asynchronously in unit tests.
