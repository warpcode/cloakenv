# cloakenv Provider Filtering & Decorator Traps

This document records critical institutional memory of filter bypasses, decorator chain ordering bugs, and resolution regressions observed across `cloakenv` development and pull requests.

Reviewers must consult these patterns whenever evaluating changes to `internal/provider/` or `internal/engine/`.

---

## 1. The Decorator Chain Order

Vault providers in `internal/engine/provider_manager.go:initVaultProvider` must be wrapped in a deterministic sequence:

```
Base Provider (keepass / yaml / json / custom_vault / search)
  └── 1. FilteringProvider   (vault.IncludeFields, vault.ExcludeFields)
        └── 2. MappingProvider     (vault.Mapping)
              └── 3. KeyFormatProvider   (vault.KeyFormat)
                    └── 4. AttributesProvider  (vault.Attributes)
```

### Invariants
- **All Providers Decorated**: Both direct providers (`keepass`, `yaml`, `json`, `custom_vault`) and virtual providers (`search`) must be wrapped by this decorator chain.
- **Attributes Last**: `AttributesProvider` must wrap the provider **strictly after** filtering, mapping, and key formatting. This ensures `.attributes` and `.entries` exposed to text templates contain only authorized, post-filter fields (RSK-002, REQ-005).

---

## 2. Wrapper-Replaces-Filter Regression (PR #210)

### What Happened
In PR #210, regex key mapping was introduced. `initVaultProvider` was modified to:
```go
// INCORRECT REGRESSION PATTERN:
if len(vault.Mapping) > 0 {
    p = provider.NewMappingProvider(p, vault.Mapping)
} else if len(vault.IncludeFields) > 0 || len(vault.ExcludeFields) > 0 {
    p = provider.NewFilteringProvider(p, vault.IncludeFields, vault.ExcludeFields)
}
```

### The Failure Mode
Whenever `mapping:` was configured on a vault, `FilteringProvider` was dropped completely. The new wrapper attempted to re-implement filtering on bare leaf keys, but its `GetSecret` fell through to `underlying.GetSecret` unfiltered. As a result, excluded keys and original unmapped keys remained readable.

### Invariants
1. Provider wrappers must **compose with `FilteringProvider`**, never replace it.
2. There must be **no fallback to the unfiltered underlying `GetSecret`** after a miss.
3. Mapped-key collisions must be deterministic and cannot earn an unauthorized filter exemption.

---

## 3. The Unfiltered Fallback Trap & Over-Correction (PR #212)

### What Happened
To fix the PR #210 fallback leak, the follow-up PR (#212) completely removed:
```go
return m.underlying.GetSecret(ctx, location)
```
from `MappingProvider.GetSecret`.

### The Failure Mode
While removing the fallback prevented secret leaks, deleting it outright broke two documented resolution paths:
1. **Bare `provider://entity` URIs**: Lost per-scheme default attribute resolution (`effectiveAttr := "Password"` for `keepass` at `filter.go:649` and `custom_vault` at `:713`).
2. **Multi-Entity YAML/JSON `GetSecret`**: Could not resolve at all, because `getRawEntryWithPath` reaches entity-keyed `GetEntryWithPath` while `staticProvider.GetSecret` resolves dot paths against `rawContent`.

### Invariants
- When removing a passthrough or fallback, explicitly enumerate all resolution paths it previously handled (default attributes, multi-entity dot paths, virtual metadata).
- Write regression tests verifying each documented syntax variant before removing fallback code.

---

## 4. Colon-Qualified vs Dot-Path Filter Candidates

### What Happened
In `internal/provider/filter.go`, `FilteringProvider.GetSecret` builds colon-qualified candidates:
- `entryPath + ":" + effectiveAttr` (`filter.go:674`)
- `entryTitle + ":" + effectiveAttr` (`filter.go:690`)
- `entityLoc + ":" + effectiveAttr` (`filter.go:740`)

However, generic helper `FilterEntryWithPath` and `projectRecursive` construct dot paths only (`entity.attribute`).

### The Failure Mode
If a provider wrapper authorizes access through the generic filter instead of the colon-qualified chain, the colon form of `exclude_fields` (documented at `README.md:587`, such as `website/Test Website:*` or `website/*:Password`) is silently unenforced the moment any wrapper is active.

### Invariants
- Any provider wrapper must either:
  1. Emit the same colon-qualified candidate forms for path-based authorization, or
  2. Retain `FilteringProvider.GetSecret` directly in the evaluation chain and map only the requested attribute name.

---

## 5. Include Precedence & Mapped Field Visibility

### Include Over Exclude Precedence
When both `include_fields` and `exclude_fields` are configured on a vault:
- `include_fields` rules are evaluated first and always take precedence over `exclude_fields` ("include should always be above exclude").
- Any field explicitly matched by an include rule is permitted regardless of broad wildcard or glob exclude rules.

### Mapped Field Visibility
- When `mapping` is configured alongside `include_fields` or `exclude_fields`, mapped fields (and mapped default metadata fields) must always remain visible. Field filtering applies strictly to unmapped fields.
- When `mapping` is configured without include/exclude rules, default metadata fields (`title`, `tags`, `uuid`, `group`, `group_name`, `group_uuid`, `group_path`, `path`, `vault`, `provider`) remain visible and are not stripped.

---

## 6. Virtual Search Vault Invariants

1. **Mapping Propagation**: When a `search` vault configures `mapping` with filter rules, `SearchProvider` must initialize with and propagate `mapping` into `FieldPolicy`.
2. **Pre-Mapping Preservation**: In `ApplyToEntry`, attributes matching mapping rules must be preserved so mapped fields are not stripped before `MappingProvider` receives them.
3. **Search Query Matching**: Candidate entry metadata (`Title`, `Tags`) must be preserved during search execution so that stored queries (`"tag" in tags`, `title == ...`) match as expected.

---

## 7. Template Engine & Command Injection Invariants

1. **Decorator Sequence**: Universal decorator `NewAttributesProvider` must wrap vaults strictly after filtering and mapping so that `.attributes` and `.entries` exposed to templates contain only authorized, post-filter fields.
2. **Composed Entities**: `CustomVaultProvider.GetSecret` must check `entity.IsComposed` and evaluate via `resolveEntity` so dynamic merges, filters, mappings, and templates are computed rather than returning raw static maps.
3. **Child Command Safety**: `resolveCmdArgs` in `internal/engine/env_builder.go` selectively evaluates only bare known URIs or templates containing cloakenv function invocations (`get`, `entry`, `search`, `include`, `includeFile`). Foreign templates (e.g. Docker format flags `'{{.State.Status}}'`, Helm templates, or kubectl queries) must pass through completely untouched.
