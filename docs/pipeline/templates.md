# Template Generators (`templates:`)

Top-level `templates:` renders structured configuration files, `.env` payloads, JSON, or YAML artifacts from vault sources or existing `envs:`.

---

## Supported Drivers

| Driver | `driver:` Value | Input Context | Output Format | Default `key_format` |
|---|---|---|---|---|
| **Go Template** | `template` *(default)* | `.entries` (`[]map[string]string`) | Arbitrary text via Go `text/template` | `none` |
| **JSON** | `json` | `.entries` (`[]map[string]string`) | Pretty-printed JSON array | `none` |
| **YAML** | `yaml` | `.entries` (`[]map[string]string`) | YAML sequence | `none` |
| **Dotenv** | `dotenv` | `from_env:` and/or `vaults:` | Sorted, escaped `KEY="VALUE"` lines | `env` |

---

## 1. `driver: template` (Inline, File Path, or Scalar Shorthand)

Executes a Go `text/template` with `.Option("missingkey=error")` and root data `{ "entries": []map[string]string }` containing the post-rules map for every matched entry.

### Inline Template
```yaml
templates:
  service_ini:
    driver: template
    vaults:
      - vault: work
        query: '{{ hasTag "service" .tags }}'
        rules:
          - type: pass
            include: ["title", "URL", "UserName"]
            key_format: none
    template: |
      {{- range .entries }}
      [{{ .title }}]
      endpoint = {{ default "https://localhost" .URL }}
      user = {{ .UserName }}
      {{ end -}}
```

### External Template File (`path:`)
Relative `path:` values are resolved relative to the directory containing `config.yaml` (with `~/` expansion supported):

```yaml
templates:
  litellm_config:
    driver: template
    path: ~/.config/litellm/config.tmpl
    vaults:
      - vault: work
        query: '{{ hasTag "litellm-model" .tags }}'
        rules:
          - type: pass
            include: ["title", "api_base", "model_name"]
            key_format: none
          - type: set
            values:
              api_key: "os.environ/{{ .title | env }}_API_KEY"
```

### Scalar Shorthand
If a template entry is a plain YAML string, it defaults to `driver: template` with inline `template:`:

```yaml
templates:
  banner: "mode=production"
```

> [!NOTE]
> `template:` and `path:` are mutually exclusive; specifying both fails validation.

---

## 2. `driver: json`

Serializes post-rules `.entries` (`[]map[string]string`) as indented JSON:

```yaml
templates:
  service_json:
    driver: json
    vaults:
      - vault: work
        query: '{{ eq .title "Service Account" }}'
        rules:
          - type: pass
            exclude: ["Notes"]
            key_format: snake
```

---

## 3. `driver: yaml`

Serializes post-rules `.entries` (`[]map[string]string`) as YAML:

```yaml
templates:
  clusters_yaml:
    driver: yaml
    vaults:
      - vault: work
        query: '{{ hasTag "k8s" .tags }}'
        rules:
          - type: pass
            exclude: ["Notes"]
            key_format: kebab
```

---

## 4. `driver: dotenv`

Generates sorted `KEY="VALUE"` lines. It can reference an existing `envs:` definition via `from_env:`, define its own `vaults:` sources, or combine both (where `vaults:` entries override `from_env:` on key collisions).

```yaml
templates:
  app_dotenv:
    driver: dotenv
    from_env: app_env
```

Or with additional overrides:
```yaml
templates:
  compose_dotenv:
    driver: dotenv
    from_env: app_env
    vaults:
      - vault: work
        query: '{{ eq .title "Docker Compose" }}'
        rules:
          - type: pass
            include: ["COMPOSE_*"]
            key_format: env
```

### Dotenv Validation & Escaping
- All keys must match `^[A-Za-z_][A-Za-z0-9_]*$`.
- Values are double-quoted with `\`, `"`, `$`, `` ` ``, `\n`, `\r`, and `\t` safely escaped.

---

## Inspecting Templates Safely

```bash
cloakenv inspect template service_json
```
`cloakenv inspect template` reports the driver, rendered byte count, matched entries, and post-rules keys with all values replaced by `<REDACTED>`.
