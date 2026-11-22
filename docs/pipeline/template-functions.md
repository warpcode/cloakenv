# Pure Go Template Functions Reference

All template expressions in `query:`, `rules:`, `templates:`, and `autoload:` execute via Go's standard library `text/template` with `.Option("missingkey=error")` and a curated `FuncMap` of pure, side-effect-free functions.

> [!IMPORTANT]
> Legacy impure template functions (`get`, `tryGet`, `entry`, `search`, `include`, `includeFile`, `exec`) and `${...}` interpolation are permanently removed. Templates cannot read arbitrary files, spawn subprocesses, or perform ad-hoc vault lookups.

---

## Tag & Conditional Helpers

| Function | Signature | Description & Example |
|---|---|---|
| `hasTag` | `hasTag <tag> <tags>` | Case-insensitive membership check against `[]string`, `[]any`, or comma/space-delimited string.<br>`{{ hasTag "prod" .tags }}` |
| `default` | `default <fallback> <val...>` | Returns the first non-empty value in `<val...>`, or `<fallback>` if all are nil/zero/empty.<br>`{{ default "localhost" .URL }}` |
| `coalesce` | `coalesce <val...>` | Returns the first non-empty string representation from `<val...>`, or `""`.<br>`{{ coalesce .CUSTOM_HOST .URL "127.0.0.1" }}` |

*(Go built-in functions `eq`, `ne`, `lt`, `le`, `gt`, `ge`, `and`, `or`, `not`, `index`, `len`, `printf` are also available.)*

---

## Serialization & Parsing Helpers

| Function | Aliases | Description & Example |
|---|---|---|
| `toEnv` | `toKv` | Formats a `map[string]any` or `map[string]string` as sorted `KEY=VALUE` lines, skipping reserved metadata keys (`title`, `path`, `uuid`, `tags`, `group`, `vault`, `provider`, `meta`, `entries`, `attributes`, `original`, `output`).<br>`{{ . | toEnv }}` |
| `toJSON` | `toJson`, `json` | Serializes any value to compact JSON.<br>`{{ .entries \| toJSON }}` |
| `fromJSON` | `fromJson` | Parses a JSON string into Go values (`map[string]any`, `[]any`, etc.).<br>`{{ ($cfg := fromJSON .raw_json) }}{{ $cfg.host }}` |
| `toYAML` | `toYaml`, `yaml` | Serializes any value to YAML.<br>`{{ .entries \| toYAML }}` |
| `fromYAML` | `fromYaml` | Parses a YAML string into Go values.<br>`{{ ($y := fromYAML .raw_yaml) }}{{ $y.endpoint }}` |

---

## Key & String Formatting Helpers

| Function | Description & Example |
|---|---|
| `upper` | Converts string to uppercase (`strings.ToUpper`).<br>`{{ .env \| upper }}` |
| `lower` | Converts string to lowercase (`strings.ToLower`).<br>`{{ .region \| lower }}` |
| `env` | Formats identifier as `UPPER_SNAKE_CASE`.<br>`{{ .title \| env }}_API_KEY` |
| `snake` | Formats identifier as `lower_snake_case`.<br>`{{ .title \| snake }}` |
| `kebab` | Formats identifier as `lower-kebab-case`.<br>`{{ .title \| kebab }}` |
| `shquote` *(alias `shellQuote`)* | Escapes a string in POSIX single quotes (`'...'` with internal `'` escaped as `'\''`).<br>`TOKEN={{ .password \| shquote }}` |

---

## Map & Slice Helpers

| Function | Signature | Description & Example |
|---|---|---|
| `keys` | `keys <map>` | Returns sorted `[]string` of keys from `map[string]any` or `map[string]string`. |
| `values` | `values <map>` | Returns `[]any` of values ordered by sorted keys. |
| `join` | `join <sep> <slice>` | Joins a `[]string` or `[]any` with `<sep>`.<br>`{{ join "," .tags }}` |
