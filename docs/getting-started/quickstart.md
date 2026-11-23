# Quick Start

This 5-minute guide walks you through creating a `config.yaml`, authenticating a vault, auditing resolved keys without exposing secrets, and running a command with masked output.

---

## Step 1: Create `config.yaml`

By default, `cloakenv` reads `~/.config/cloakenv/config.yaml` (you can override this with `-c <path>`).

Create `~/.config/cloakenv/config.yaml`:

```yaml
vaults:
  work:
    provider: keepass
    vault_path: ~/secrets/work.kdbx
    cache:
      enabled: true
      ttl: "15m"
      mode: "keyring"

envs:
  app_env:
    - vault: work
      query: '{{ eq .title "OpenAI" }}'
      rules:
        - type: pass
          include: ["API_KEY"]
          key_format: env

templates:
  app_dotenv:
    driver: dotenv
    from_env: app_env

autoload:
  - match: "^python app\\.py$"
    envs:
      - app_env
      - APP_MODE: "development"
    templates:
      - template: app_dotenv
        env_path: APP_ENV_FILE
```

> [!TIP]
> Don't have a KeePass database handy? You can test immediately with the built-in `env` or `keyring` vaults—for example, `vault: env` with `include: ["USER", "HOME"]`.

---

## Step 2: Authenticate Your Vault

Store the KeePass master password securely in your OS keyring so `cloakenv` can unlock it without prompting on every command:

```bash
cloakenv auth login work
```

Verify vault readiness:

```bash
cloakenv auth status
```

Expected output:
```text
work: ACTIVE
```

---

## Step 3: Validate Configuration with `doctor`

Run `cloakenv doctor --strict` to verify YAML syntax, schema fields, template syntax, regex compilation, cache directory permissions, and vault reachability:

```bash
cloakenv doctor --strict
```

---

## Step 4: Inspect Without Exposing Secrets

Before running any command, audit what `cloakenv` will resolve. `cloakenv inspect` unconditionally replaces every secret value with `<REDACTED>`:

```bash
# Overview of configured vaults, envs, templates, and autoload rules
cloakenv inspect

# Inspect a specific env definition
cloakenv inspect env app_env

# Inspect a specific template definition
cloakenv inspect template app_dotenv

# Inspect what a specific command invocation will receive
cloakenv inspect run -- python app.py
```

Example output of `cloakenv inspect run -- python app.py`:
```text
Command: python app.py
Injected variables:
  API_KEY=<REDACTED>
  APP_MODE=<REDACTED>
Templates:
  app_dotenv (driver=dotenv, mode=env_path, env_path=APP_ENV_FILE, ephemeral=true, persist=false)
```

---

## Step 5: Preview and Execute

Run a dry-run preview or execute the command directly:

```bash
# Dry-run execution preview
cloakenv run --dry-run -- python app.py

# Execute with in-memory env injection, ephemeral /dev/shm file delivery, and stdout/stderr masking
cloakenv run -- python app.py
```

When `python app.py` exits (or receives `SIGINT`/`SIGTERM`), `cloakenv` automatically overwrites the ephemeral `$APP_ENV_FILE` with `0x00` bytes, unlinks it, and scrubs any secret values printed to `stdout`/`stderr` with `<REDACTED>`.

---

## Next Steps

- Understand the architecture in [Core Concepts](core-concepts.md)
- Explore all [Secret Providers](../providers/README.md)
- Learn how `pass`, `match`, and `set` work in [Transformation Rules](../pipeline/rules.md)
