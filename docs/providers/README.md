# Secret Providers Overview

Every secret backend in `cloakenv` implements the read-only `provider.SecretProvider` and `provider.SearchableProvider` interfaces. Providers never filter, rename, or transform secret attributes—they return raw, flat `Entry` (`map[string]any`) maps to the declarative pipeline.

---

## Provider Comparison Matrix

| Provider | `provider:` Value | Entry Topology | Implicitly Available? | Authentication | Vault-Level `query:` Scope | Caching Support |
|---|---|---|---|---|---|---|
| **KeePass** | `keepass` | Multi-entry (1 `Entry` per record) | No (declare in `vaults:`) | OS keyring (`auth login`) and/or `key_file` | Group/entry path prefix (e.g., `Production/`) | Yes (`keyring` / `encrypted_disk`) |
| **Password Store** | `pass` | Multi-entry (1 `Entry` per `.gpg` file) | No (declare in `vaults:`) | GPG keyring / `gpg-agent` | Subdirectory path prefix (e.g., `work/`) | Yes (`keyring` / `encrypted_disk`) |
| **HashiCorp Vault** | `vault` | Multi-entry (1 `Entry` per KV secret) | No (declare in `vaults:`) | Token (`VAULT_TOKEN` / `auth login`) or AppRole | KV path prefix (e.g., `prod/`) | Yes (`keyring` / `encrypted_disk`) |
| **Process Env** | `env` | Single-entry (all env vars in 1 `Entry`) | **Yes** (`vault: env`) | Process environment (in-memory) | Ignored | Yes |
| **OS Keyring** | `keyring` | Single-entry (requested keys in 1 `Entry`) | **Yes** (`vault: keyring`) | Native OS credential store | Comma-separated key names (e.g., `KEY1,KEY2`) | Yes |

---

## Canonical Flat `Entry` Structure

Every provider normalizes record metadata into canonical lowercase keys at the top level of each `Entry`:

```yaml
# Conceptual flat Entry map returned by a multi-entry provider
title: "OpenAI"
path: "AI/OpenAI"
uuid: "a1b2c3d4..."
tags: ["ai", "litellm"]
group:
  name: "AI"
  path: "AI"
  uuid: "f0e1d2c3..."
vault: "work"
provider: "keepass"

# Raw entry fields sit alongside metadata at the top level:
UserName: "api-service"
Password: "sk-..."
password: "sk-..."       # Convenience lowercase alias provided by keepass & pass
URL: "https://api.openai.com"
API_KEY: "sk-..."
```

> [!IMPORTANT]
> Reserved metadata keys (`title`, `path`, `uuid`, `tags`, `group`, `vault`, `provider`) are automatically skipped by `type: pass` catch-all `*` wildcards so they don't accidentally pollute environment variables. You can still reference them in `query:`, `type: set`, or explicitly name them in `type: pass` `include: ["title", "path"]`.

---

## Built-In Implicit Vaults (`env` and `keyring`)

`env` and `keyring` are available by name in `envs:` and `templates:` without any entry under `vaults:`:

```yaml
envs:
  ci_env:
    - vault: env
      rules:
        - type: pass
          include: ["CI_*", "GITHUB_*"]
```

If you declare `env:` or `keyring:` explicitly under `vaults:`, its `provider:` must match its name (`env` must use `provider: env`, `keyring` must use `provider: keyring`). You can also define multiple custom-named `env` or `keyring` vaults (e.g., `os_keyring: { provider: keyring, query: "OPENAI_API_KEY" }`).

---

## Provider Guides

- [KeePass (`keepass`)](keepass.md)
- [Password Store (`pass`)](pass.md)
- [HashiCorp Vault (`vault`)](vault.md)
- [Process Environment (`env`)](env.md)
- [OS Keyring (`keyring`)](keyring.md)
- [Transparent Per-Vault Caching (`cache`)](caching.md)
