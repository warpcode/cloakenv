# Process Environment (`env`)

The `env` provider exposes variables from the current process environment as a single flat `Entry` (`map[string]any`). It is available **implicitly** by name (`vault: env`) without needing a `vaults:` declaration, or can be declared explicitly under `vaults:`.

---

## Configuration Options

### Implicit Usage (No `vaults:` Block Needed)
```yaml
envs:
  aws_from_host:
    - vault: env
      rules:
        - type: pass
          include: ["AWS_*", "DEFAULT_REGION"]
          key_format: env
```

### Explicit Declaration
```yaml
vaults:
  process_env:
    provider: env
```

> [!WARNING]
> If you use `vault: env` without `rules:` or with a `type: pass` rule that omits `include:`, `cloakenv doctor` emits a warning because a wildcard pass would re-inject the entire host environment. Always specify explicit `include:` globs or `type: match` rules on `env` and `keyring` sources.

---

## Common Patterns

### 1. Pass Specific Host Variables into `-E` (Empty Environment) Runs
When running `cloakenv run -E -- <cmd>` to start with a clean environment, selectively forward only required host variables:

```yaml
envs:
  sandboxed_env:
    - vault: env
      rules:
        - type: pass
          include: ["PATH", "HOME", "LANG", "TERM"]
          key_format: none
    - vault: work
      query: '{{ hasTag "app" .tags }}'
      rules:
        - type: pass
          include: ["API_KEY"]
          key_format: env
```

### 2. Match Tokens by Key Pattern and Value Prefix
Use `type: match` with `value_match` to selectively capture API keys from the environment without knowing their exact names ahead of time:

```yaml
envs:
  llm_keys:
    - vault: env
      rules:
        - type: match
          match: "^(.*_API_KEY)$"
          value_match: "^sk-.*"
          key: "$1"
```
