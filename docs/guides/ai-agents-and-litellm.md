# Guide: AI Agents & LiteLLM Proxy

`cloakenv` was built with AI coding agents (Claude Code, Aider, Codex) and LLM proxies (LiteLLM) in mind. This guide shows how to inject API keys and render an ephemeral `litellm_config.yaml` without exposing plaintext credentials on disk or in agent logs.

---

## 1. Vault Organization

In your KeePass database (or `pass` / HashiCorp Vault), tag LLM provider entries with `litellm`:
- Entry `OpenAI`: `Password: sk-proj-...`, `tags: ["litellm"]`
- Entry `Anthropic`: `Password: sk-ant-...`, `tags: ["litellm"]`
- Entry `Model GPT-4o`: `model_name: "gpt-4o"`, `litellm_model: "openai/gpt-4o"`, `api_base: "https://api.openai.com/v1"`, `provider_key: "OPENAI_API_KEY"`, `tags: ["litellm-model"]`

---

## 2. Create the Template (`~/.config/litellm/config.tmpl`)

```yaml
model_list:
{{- range .entries }}
  - model_name: {{ .model_name }}
    litellm_params:
      model: {{ .litellm_model }}
      api_base: {{ default "https://api.openai.com/v1" .api_base }}
      api_key: {{ .api_key }}
{{- end }}
```

Notice that `api_key` references `os.environ/...` (synthesized below by `rules:`), so no raw secret is even written into the rendered config file.

---

## 3. Configure `config.yaml`

```yaml
vaults:
  work:
    provider: keepass
    vault_path: ~/secrets/work.kdbx
    cache: "15m"

envs:
  litellm_env:
    - vault: work
      query: '{{ hasTag "litellm" .tags }}'
      rules:
        - type: set
          values:
            "{{ .title | env }}_API_KEY": "{{ .password }}"
    - vault: env
      rules:
        - type: match
          match: "^(.*_API_KEY)$"
          value_match: "^sk-.*"
          key: "$1"

templates:
  litellm_config:
    driver: template
    path: ~/.config/litellm/config.tmpl
    vaults:
      - vault: work
        query: '{{ hasTag "litellm-model" .tags }}'
        rules:
          - type: pass
            include: ["model_name", "litellm_model", "api_base"]
            key_format: none
          - type: set
            values:
              api_key: "os.environ/{{ .provider_key }}"

autoload:
  - match: '^litellm(\s+(.*))?$'
    envs:
      - litellm_env
      - LITELLM_LOG: "INFO"
    templates:
      - template: litellm_config
        env_path: LITELLM_CONFIG_PATH
    command: "litellm --config $LITELLM_CONFIG_PATH $2"

  - match: '^(aider|claude|codex)(\s|$)'
    envs:
      - litellm_env
```

---

## 4. Verify & Run

```bash
# Audit without exposing any API keys
cloakenv inspect run -- litellm --port 4000

# Run LiteLLM with ephemeral /dev/shm config and masked stdout/stderr
cloakenv run -- litellm --port 4000
```

If a traceback or debug print inside the child process attempts to print `OPENAI_API_KEY`, `MaskingWriter` automatically replaces it with `<REDACTED>`.
