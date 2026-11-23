# Environment Sets (`envs:`)

Top-level `envs:` defines named, reusable environment variable sets (`map[string]string`) composed from one or more vault sources.

---

## Configuration Structure

```yaml
envs:
  <env_name>:
    - vault: <vault_name>          # Required: configured vault, "env", or "keyring"
      query: '<go_template_bool>'  # Optional: Go text/template predicate
      rules:                       # Optional: defaults to a single pass (key_format: env)
        - type: pass | match | set
```

---

## Multi-Vault Composition & Precedence

An `envs:` entry can pull from multiple vaults in sequence. `EnvsBuilder` flattens all matched entries into a single `map[string]string` with strict deterministic precedence:

1. **Within a single entry**: Rules execute sequentially; later rules overwrite keys from earlier rules.
2. **Within a single vault source**: Matched entries are sorted deterministically by `(path, title, uuid)`; later entries overwrite keys from earlier entries.
3. **Across vault sources**: Sources execute in the exact order listed under `envs.<name>`; later sources overwrite keys from earlier sources.

---

## Complete Example: Multi-Vault App Environment

```yaml
envs:
  app_env:
    # 1. Primary backend secrets from KeePass
    - vault: work
      query: '{{ and (hasTag "backend" .tags) (eq .group.name "Production") }}'
      rules:
        - type: pass
          include: ["DB_*", "REDIS_*"]
          exclude: ["*_DEPRECATED"]
          key_format: env
        - type: set
          values:
            DATABASE_URL: 'postgres://{{ .UserName }}:{{ .password }}@{{ default "localhost" .URL }}:5432/app'

    # 2. AI API keys from OS Keyring
    - vault: os_keyring
      rules:
        - type: pass
          include: ["OPENAI_*", "ANTHROPIC_*"]

    # 3. Selective host variables from process environment
    - vault: env
      rules:
        - type: pass
          include: ["DEFAULT_REGION", "USER"]
```

Verify the resolved variable names and matched entry traces at any time with:

```bash
cloakenv inspect env app_env
```
