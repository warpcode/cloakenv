---
name: cloakenv-add-provider
description: SOP, standards, and verification checklist for adding a new secret provider backend to cloakenv. Use when implementing, testing, or registering a new secret provider backend.
---

# Adding a Secret Provider to cloakenv

Standard Operating Procedure for implementing, registering, testing, documenting, and mechanically verifying a new secret provider backend in `cloakenv`.

---

## 1. Overview & Interface Contracts

Every secret backend in `cloakenv` implements the core `SecretProvider` interface defined in `internal/provider/provider.go`.

### 1.1 Core Interface (`provider.SecretProvider`)

```go
type SecretProvider interface {
    Scheme() string
    Initialize(ctx context.Context, config ProviderConfig) error
    GetSecret(ctx context.Context, location string) (string, error)
    SetSecret(ctx context.Context, location string, value string) error
    DeleteSecret(ctx context.Context, location string) error
    Validate(settings map[string]string) error
}
```

- **`Scheme()`**: Returns the URI scheme handled by this provider (e.g., `"keepass"`, `"yaml"`, `"json"`, `"vault"`).
- **`Initialize(ctx, config)`**: Prepares the provider instance using backend-specific settings from `config.Settings`. For stateless backends, this may be a no-op; for stateful backends, it initializes connections or decrypts databases.
- **`GetSecret(ctx, location)`**: Resolves a secret value given a location string (the path following `scheme://`). Must return an error if the location does not resolve.
- **`SetSecret(ctx, location, value)`**: Writes a secret value at the given location. Returns an error if the provider is read-only.
- **`DeleteSecret(ctx, location)`**: Removes a secret from the given location. Returns an error if read-only or if deletion fails.
- **`Validate(settings)`**: Validates configuration parameters before initialization.

### 1.2 Optional Interfaces

- **`SearchableProvider` (`internal/provider/provider.go`)**:
  Implement if the backend supports listing or searching multiple structured entries:
  - `Search(ctx context.Context, query SearchQuery) ([]SearchResult, error)`
  - `GetEntry(ctx context.Context, location string) (Entry, error)`
- **`ValueResolvableProvider` (`internal/provider/provider.go`)**:
  Implement if the provider supports the `resolve_values` configuration flag:
  - `SupportsValueResolution() bool` (returns `true` when honoring `resolve_values`).

---

## 2. Step-by-Step Implementation SOP

### Step 1: Implement the Provider in `internal/provider/<name>.go`

1. Create `internal/provider/<name>.go` in `package provider`.
2. Define the provider struct and constructor:
   ```go
   type MyProvider struct {
       // backend-specific fields
   }

   func NewMyProvider() *MyProvider {
       return &MyProvider{}
   }
   ```
3. Implement all six methods of `provider.SecretProvider`.
4. Adhere to non-negotiable security & coding invariants:
   - **Zero-persistence**: Never write unencrypted secrets to disk.
   - **No secret logging**: Error messages and logs must never include secret values.
   - **Entry & Tag Immutability**: `Entry.Tags` must NEVER be modified (e.g. lowercased) at parse time. Preserve original casing, and perform case-insensitive tag matching dynamically inside `Search`.
   - **Cross-platform**: Any OS-specific syscalls or imports (`golang.org/x/sys/...`, `syscall`) must be gated with Go build tags (`//go:build <os>`).

### Step 2: Implement Unit Tests in `internal/provider/<name>_test.go`

1. Create `internal/provider/<name>_test.go` in `package provider`.
2. Write table-driven unit tests testing:
   - Successful initialization and configuration validation.
   - Successful secret retrieval across expected location formats.
   - Error handling for invalid configuration, missing keys, or unauthorized paths.
   - Read-only protection on `SetSecret` / `DeleteSecret` when applicable.
3. Test isolation:
   - If interacting with keyring or cache, invoke `keyring.MockInit()` in test setup.
   - Use `t.TempDir()` for all temporary files or environment mocks (`HOME`, `XDG_CACHE_HOME`).

### Step 3: Register in Engine (`internal/engine/provider_manager.go`)

1. Open `internal/engine/provider_manager.go`.
2. In `initVaultProvider(ctx context.Context, vaultName string, vault config.VaultConfig)`, add a case to the provider switch statement:
   ```go
   case "<name>":
       p, err = pm.init<Name>(ctx, vaultName, vault)
   ```
3. Implement the helper `init<Name>(ctx context.Context, vaultName string, vault config.VaultConfig) (provider.SecretProvider, error)` to instantiate and initialize the provider.
4. **Decorator Invariant**: Ensure the provider instance `p` is wrapped by the standard decorator chain:
   - `NewFilteringProvider(p, vault.IncludeFields, vault.ExcludeFields)`
   - `NewMappingProvider(p, vault.Mapping)`
   - `NewKeyFormatProvider(p, vault.KeyFormat)`
   - `NewAttributesProvider(p, vault.Attributes)`

### Step 4: Update Documentation

1. **`README.md`**:
   - Add a configuration snippet under the configured vaults section illustrating provider usage.
   - Document any custom settings, location syntax, or attributes supported.
2. **`examples/config.yaml`**:
   - Add a commented example vault block demonstrating the provider's configuration options.

### Step 5: Dependency & Build Audit

1. Prefer Go standard library over third-party dependencies.
2. Do not introduce new dependencies in `go.mod` without explicit review.
3. Ensure the project builds cleanly across platforms.

### Step 6: Automated Verification

1. Run the mechanical checklist helper script:
   ```bash
   .github/skills/cloakenv-add-provider/scripts/provider_checklist.sh <name>
   ```
2. Run the repository verification pipeline:
   ```bash
   go build ./... && go vet ./... && test -z "$(gofmt -l .)" && go test -race ./...
   ```

---

## 3. Mechanical Checklist Script

The skill provides an automated verification script:

```bash
.github/skills/cloakenv-add-provider/scripts/provider_checklist.sh <provider_name>
```

### Checks Performed

1. **Source File**: Confirms `internal/provider/<name>.go` exists.
2. **Test File**: Confirms `internal/provider/<name>_test.go` exists.
3. **Interface Compliance**: Verifies declarations for `Scheme`, `Initialize`, `GetSecret`, `SetSecret`, `DeleteSecret`, and `Validate`.
4. **Engine Registration**: Checks `internal/engine/provider_manager.go` for `case "<name>":` in `initVaultProvider`.
5. **README Documentation**: Verifies `README.md` documents `<name>`.
6. **Example Configuration**: Verifies `examples/config.yaml` includes `<name>`.
7. **Clean Dependencies**: Verifies `go.mod` contains no uncommitted dependency modifications.
8. **Cross-Platform Safety**: Scans for ungated OS-specific imports in `<name>.go`.

The script emits machine-readable JSON to stdout and exits `0` if all checks pass, or `1` if any check fails.
