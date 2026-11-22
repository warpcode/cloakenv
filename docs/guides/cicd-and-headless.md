# Guide: Headless & CI/CD Usage

`cloakenv` runs cleanly in non-interactive CI/CD runners, Docker containers, and automated servers without needing an interactive terminal or OS keyring.

---

## 1. Non-Interactive Authentication Options

Choose one of these headless-friendly patterns:

1. **HashiCorp Vault AppRole or Token**:
   Set `VAULT_ADDR` and `VAULT_ROLE_ID` + `VAULT_SECRET_ID` (or `VAULT_TOKEN`) in CI environment secrets.
2. **Process Environment (`vault: env`)**:
   Use `vault: env` with explicit `include:` globs to filter and transform CI/CD runner secrets.
3. **Key-File-Only KeePass Database**:
   Mount a `.keyx` file and pass its path via `key_file_env: CI_KEEPASS_KEYFILE` (without requiring a master password in the OS keyring).

---

## 2. Project-Scoped Configs & Keyring Isolation

Use `-c <path>` to point `cloakenv` at a repository-committed configuration file (which contains zero secrets—only vault pointers, queries, rules, and autoload bindings):

```yaml
# ./ci/cloakenv.yaml
keyring:
  prefix: "cloakenv"
  isolate: true

audit:
  enabled: true
  log_path: "./ci-audit.jsonl"
  format: "jsonl"

vaults:
  ci_vault:
    provider: vault
    address: https://vault.internal:8200
    mount_path: kv
    engine_version: 2
    query: ci/

envs:
  deploy_env:
    - vault: ci_vault
      query: '{{ hasTag "deploy" .tags }}'
      rules:
        - type: pass
          include: ["DEPLOY_*", "REGISTRY_*"]
          key_format: env
    - vault: env
      rules:
        - type: pass
          include: ["GITHUB_SHA", "GITHUB_REF_NAME"]

autoload:
  - match: '^deploy$'
    command: "./scripts/deploy.sh"
    envs:
      - deploy_env
```

---

## 3. GitHub Actions Example

```yaml
jobs:
  deploy:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - name: Validate cloakenv config
        run: cloakenv -c ./ci/cloakenv.yaml doctor --strict

      - name: Audit resolved keys (redacted)
        run: cloakenv -c ./ci/cloakenv.yaml inspect run -- deploy

      - name: Run deployment with masked output
        env:
          VAULT_ADDR: ${{ secrets.VAULT_ADDR }}
          VAULT_ROLE_ID: ${{ secrets.VAULT_ROLE_ID }}
          VAULT_SECRET_ID: ${{ secrets.VAULT_SECRET_ID }}
        run: cloakenv -c ./ci/cloakenv.yaml run -- deploy
```

> [!TIP]
> Running `cloakenv -c ./ci/cloakenv.yaml doctor --strict` in PR checks catches YAML typos, invalid regexes, template syntax errors, and legacy config keys before merge.
