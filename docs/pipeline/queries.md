# Entry Queries (`query:`)

`cloakenv` uses `query:` at two levels:
1. **Vault-level `query:`** (`vaults.<name>.query`): A provider-native scope (e.g., KeePass group prefix, `pass` subdirectory, Vault KV path prefix, or `keyring` key list).
2. **Source-level `query:`** (`envs.<name>[].query` or `templates.<name>.vaults[].query`): A Go `text/template` boolean predicate evaluated against each candidate `Entry`.

---

## Source-Level `query:` Semantics

When `query:` is omitted on a source, **all** entries returned by the vault are selected.

When `query:` is specified, `cloakenv` evaluates it as a Go `text/template` with `.Option("missingkey=error")` against each candidate `Entry`:

- **Selection rule**: An entry is selected **if and only if** the trimmed template output equals `"true"`.
- **Missing field handling in `query:`**: If a predicate references a custom attribute (like `.env`) that is absent on a particular entry, `EvaluateQueryPredicate` treats that missing-key lookup as non-matching (`false`) rather than aborting the scan—so heterogeneous vaults work seamlessly.
- **Defaulted canonical metadata**: Canonical keys (`title`, `path`, `uuid`, `vault`, `provider`, `tags`, `group`) are always present in the query context (defaulting to `""`, `[]string{}`, or `nil` if absent).
- **Syntax & type errors**: Invalid template syntax or type errors fail the run immediately with a clear error.

---

## Common `query:` Examples

### Match by Exact Entry Title
```yaml
query: '{{ eq .title "OpenAI" }}'
```

### Match by Tag (Case-Insensitive)
```yaml
query: '{{ hasTag "litellm" .tags }}'
```

### Match by Group Name and Tag
```yaml
query: '{{ and (hasTag "backend" .tags) (eq .group.name "Production") }}'
```

### Match Multiple Titles or Tags (`or`)
```yaml
query: '{{ or (hasTag "aws" .tags) (eq .title "Cloudflare") }}'
```

### Exclude Entries by Tag (`not`)
```yaml
query: '{{ and (hasTag "db" .tags) (not (hasTag "deprecated" .tags)) }}'
```

### Match by Custom Entry Attribute
```yaml
query: '{{ eq .environment "staging" }}'
```
*(Entries that do not have an `environment` field safely evaluate to `false`.)*
