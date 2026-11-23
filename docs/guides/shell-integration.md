# Guide: Shell Integration & Auto-Wrapping

Instead of typing `cloakenv run --` before every command, you can combine `cloakenv completion` with `cloakenv internal match-alias` to automatically wrap commands that match your `autoload:` rules.

---

## How `cloakenv internal match-alias` Works

```bash
cloakenv internal match-alias -- aws s3 ls
```
- **Exit code `0`**: At least one `autoload:` rule matches the command.
- **Exit code `1`**: No rule matches.
- Add `--json` to inspect the matched rule, rewritten command, `envs`, and `templates`.

---

## Zsh Integration (Interactive `accept-line` Wrapper)

Add this snippet to `~/.zshrc` to transparently wrap any command matching `autoload:` when you press Enter:

```zsh
_cloakenv_accept_line() {
  # Skip empty lines or commands already invoking cloakenv
  if [[ -n "${BUFFER// }" && "$BUFFER" != cloakenv\ * ]]; then
    local -a words
    words=(${(z)BUFFER})
    if cloakenv internal match-alias -- "${words[@]}" >/dev/null 2>&1; then
      BUFFER="cloakenv run -- ${BUFFER}"
    fi
  fi
  zle .accept-line
}
zle -N accept-line _cloakenv_accept_line
```

---

## Explicit Function Wrappers (Bash / Zsh)

If you prefer wrapping only specific commands rather than intercepting every command line:

```bash
# In ~/.bashrc or ~/.zshrc
_cloakenv_wrap() {
  if cloakenv internal match-alias -- "$@" >/dev/null 2>&1; then
    cloakenv run -- "$@"
  else
    command "$@"
  fi
}

aws()     { _cloakenv_wrap aws "$@"; }
kubectl() { _cloakenv_wrap kubectl "$@"; }
litellm() { _cloakenv_wrap litellm "$@"; }
```

---

## Fish Integration

Add to `~/.config/fish/config.fish`:

```fish
function _cloakenv_wrap --argument-names cmd
    if cloakenv internal match-alias -- $argv >/dev/null 2>&1
        cloakenv run -- $argv
    else
        command $argv
    end
end

function aws;     _cloakenv_wrap aws $argv; end
function kubectl; _cloakenv_wrap kubectl $argv; end
```

---

## Enable Tab Completion

See [Installation: Shell Autocompletion](../getting-started/installation.md#shell-autocompletion) to enable tab completion for `cloakenv` subcommands and vault names.
