# cloakenv Secret Pipeline & Security Invariants

This document records critical invariants and historical regression patterns across `cloakenv`'s read-only providers, declarative `RuleEngine`, template renderer, runner delivery, and zero-leak CLI.

> **Historical Note (Epic #467):** The legacy 4-layer provider decorator chain (`FilteringProvider` -> `MappingProvider` -> `KeyFormatProvider` -> `AttributesProvider`), `custom_vault`, `search://`, Expr queries, and URI template interpolation (`get`/`entry`/`search`/`include`/`includeFile`/`exec`) were permanently retired in Epic #467. All transformation logic now lives exclusively in `internal/engine/rules.go` and `internal/engine/output_pipeline.go`.

---

## 1. Pure Read-Only Providers & Flat `Entry`

- Vault backends (`keepass`, `pass`, `vault`, `env`, `keyring`) implement read-only `provider.SecretProvider` (`Scheme`, `Initialize`, `GetSecret`, `Validate`) and `provider.SearchableProvider` (`Search`, `GetEntry`).
- Providers must return raw flat `Entry` (`map[string]any`) maps with canonical metadata (`title`, `path`, `uuid`, `tags`, `group`) untouched—never filtering, mapping, formatting keys, or running templates inside `internal/provider/`.
- **`OSKeyringProvider` Isolation**: `isInternalKeyringAccount` and `GetEntry` must block internal credentials and cache keys (`cache_token`, `cache_disk_token`, `cache-master-key`, `provider/*`, `cache/*`, `cache_v2/*`). Configured `keyring` vaults must clone the underlying `KeyringStore` (`CloneWithStore()`) rather than mutating the shared built-in instance.
- **Error Propagation**: Listing (`VaultProvider.Search` / `listKeys`) and authentication failures must propagate cleanly instead of silently returning empty entries.

---

## 2. Declarative `RuleEngine` Invariants (`internal/engine/rules.go`)

- **Reserved Metadata Protection**: Wildcard rules (`pass`, `match`, `set`) must never emit or overwrite canonical entry metadata (`title`, `path`, `uuid`, `tags`, `group`, `vault`, `provider`) unless explicitly targeted by a rule key.
- **Deterministic Last-Rule-Wins**: Multiple matching rules (`pass` -> `match` -> `set`) compose sequentially on the entry output map; later rules overwrite earlier keys deterministically.
- **Safe Capture Expansion**: `match` rules supporting `$1` / `${1}` captures must evaluate regexes safely before template evaluation and rewrite capture placeholders into `index .__captures N` so capture values containing `{{` or `}}` cannot cause Go `text/template` syntax injection or parse errors.
- **Strict Missing Key Mode**: Every `query:`, `match`, `set`, and template renders via Go `text/template` with `.Option("missingkey=error")` and pure helpers only (`ruleTemplateFuncs()` must match `config.StandardTemplateFuncs()`).

---

## 3. Output Delivery, Redaction & Child Process Security

- **Unconditional `<REDACTED>` Inspection**: `cloakenv inspect` must replace every secret value with `<REDACTED>` and never expose a flag or code path to emit raw secret values.
- **Template File Delivery**: Ephemeral template files prefer `/dev/shm` when no explicit `path` is set, enforce `0700` parent directories / `0600` file mode with symlink traversal checks (`O_NOFOLLOW` / `rejectSymlinkParent`), refuse to clobber pre-existing files unless configured, and overwrite with `0x00` + unlink on child exit.
- **Stream Masking & Verbatim Args**: Child stdout/stderr streams through `MaskingWriter` (`MaskTrie` populated from env values, template contents, and template entry values); child command arguments pass through completely verbatim after autoload matching.
