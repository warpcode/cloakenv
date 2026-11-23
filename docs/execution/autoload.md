# Command Autoloading (`autoload:`)

`autoload:` binds shell commands executed via `cloakenv run -- <command> [args]` to declarative `envs:` sets, inline variables, rendered `templates:`, and optional command rewrites.

---

## Configuration Schema

```yaml
autoload:
  - match: '^deploy\s+(.*)$'
    command: "./scripts/deploy.sh \\1"
    envs:
      - "app_env"
      - DEPLOY_PORT: "8080"
        HEALTHCHECK_URL: 'http://127.0.0.1:{{ default "8080" .DEPLOY_PORT }}/health'
    templates:
      - template: "app_dotenv"
        env_path: "APP_ENV_FILE"
        persist: false
```

---

## How Command Matching Works

When `cloakenv run -- <command> [args]` runs (unless `--no-autoload` is passed), `EvaluateAutoload` tests every rule in `autoload:` **in declaration order**:

1. **Regex Match (Primary)**:
   - `match:` is compiled as a Go regular expression and tested against the full joined command string (`strings.Join(cmdArgs, " ")`).
2. **Case-Insensitive Fallback**:
   - If the regex does not match (or `match:` is a plain string/glob), `cloakenv` falls back to case-insensitive matching against:
     - Executable basename (e.g., `aws`, `kubectl`)
     - Executable path (e.g., `/usr/local/bin/kubectl`)
     - Full command equality or prefix (`match + " "` / `match + "\t"`)
     - Plain `*` wildcard globs (e.g., `*.sh`, `git*push`)

### Multi-Rule Accumulation
All matching rules apply sequentially:
- `command:` rewrites update the active command args for subsequent rules.
- `envs:` items are appended in order (later rules override earlier variables).
- `templates:` deliveries are appended in order.

---

## Command Rewriting (`command:`)

When `command:` is set on a matching rule, `cloakenv` expands capture groups from `match:` and re-parses the command into argument tokens:

- **Numbered captures**: `\1`..`\9`, `$1`..`$N`, `${1}`..`${N}` (where `$0` is the full match).
- **Named captures**: `(?P<env>prod|staging)` -> `$env` or `${env}`.
- **Literal `$`**: Write `$$`.
- **Quote-context-aware escaping**: Captured substrings are automatically escaped according to whether they appear unquoted, inside `'...'`, or inside `"..."` in `command:`.

### Example: Rewriting with Named Captures
```yaml
autoload:
  - match: '^litellm\s+(?P<args>.*)$'
    envs:
      - litellm_env
    templates:
      - template: litellm_config
        env_path: LITELLM_CONFIG_PATH
    command: 'sh -c ''exec litellm --config "$LITELLM_CONFIG_PATH" "$@"'' -- $args'
```

> [!NOTE]
> `autoload` executes binaries directly without a shell and only expands regex capture groups from `match:`. Wrap the command in `sh -c` when referencing environment variables such as `$LITELLM_CONFIG_PATH`, or use an explicit `path:` delivery target when invoking the binary directly.

---

## Combining Named `envs:` and Inline Maps

Each item in `autoload[].envs[]` can be either:
1. **A named `envs:` key** (e.g., `"app_env"`), or
2. **An inline `KEY: VALUE` map**.

Inline map values may use Go `text/template` expressions referencing any variable already resolved by earlier `envs:` items in the same command:

```yaml
autoload:
  - match: '^api-server$'
    envs:
      - app_env
      - PORT: "8080"
        PUBLIC_URL: 'http://127.0.0.1:{{ default "8080" .PORT }}'
```

---

## Verbatim Child Command Pass-Through

Child command arguments (`<command> [args]`) are passed through **verbatim** to the child process without Go template evaluation or secret expansion. Foreign templates such as `docker inspect --format '{{.State.Status}}'` are never modified by `cloakenv`.
