# Declarative Transformation Pipeline

`cloakenv` separates raw secret retrieval (`vaults:`) from projection and formatting (`envs:` and `templates:`). Every vault source inside `envs:` or `templates:` executes the same deterministic four-stage pipeline:

```mermaid
flowchart TD
    Fetch["1. VaultEntryFetcher\nRetrieve raw []Entry maps"] --> Query["2. query: Predicate\nGo text/template boolean filter\n(trimmed output == 'true')"]
    Query --> Sort["3. Deterministic Sort\n(path -> title -> uuid)"]
    Sort --> Rules["4. RuleEngine.Apply\nOrdered pass -> match -> set\n(last-rule-wins per entry)"]
    Rules --> Envs["EnvsBuilder\nFlatten to map[string]string\n(default key_format: env)"]
    Rules --> Tpls["TemplatesRenderer\nRender .entries via\ntemplate | json | yaml | dotenv"]
```

---

## Key Pipeline Guarantees

1. **Deterministic Ordering**: Matched entries within a source are always sorted by `(path, title, uuid)`. Multi-source `envs:` and `templates:` evaluate sources in list order.
2. **Non-Mutating Source Entries**: `RuleEngine` never mutates raw `Entry` maps in place, allowing the same vault entry to be projected differently by multiple `envs:` and `templates:`.
3. **Fail-Closed Templates**: All Go `text/template` expressions compile with `.Option("missingkey=error")` so typos in field names fail immediately instead of silently rendering `<no value>`.
4. **Secret-Free Tracing**: Every transformation records `RuleTrace` (`RuleIndex`, `RuleType`, `KeysAdded`) without recording secret values, powering `cloakenv inspect`.

---

## Pipeline Guides

- [Entry Queries (`query:`)](queries.md) — Selecting entries with Go `text/template` predicates
- [Transformation Rules (`rules:`)](rules.md) — Filtering, regex mapping, and templating with `pass`, `match`, and `set`
- [Environment Sets (`envs:`)](envs.md) — Composing flat environment variable maps
- [Template Generators (`templates:`)](templates.md) — Generating files and payloads with `template`, `json`, `yaml`, and `dotenv`
- [Template Functions Reference](template-functions.md) — All built-in Go `text/template` helpers
