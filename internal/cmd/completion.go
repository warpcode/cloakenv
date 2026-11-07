package cmd

import (
	"fmt"
	"io"
	"strings"

	"github.com/warpcode/cloakenv/internal/utils"
)

const bashCompletionScript = `#!/usr/bin/env bash
# Bash completion script for cloakenv

_cloakenv_complete() {
    local cur prev words cword
    _init_completion || return

    local commands="run get set delete cache show search auth sync internal config doctor completion version"

    if [[ $cword -eq 1 ]]; then
        COMPREPLY=( $(compgen -W "${commands} -c -h --help -v --version" -- "$cur") )
        return 0
    fi

    local cmd="${words[1]}"
    local i=1
    # Check if -c was used before subcommand
    if [[ "${words[1]}" == "-c" ]]; then
        if [[ $cword -eq 2 ]]; then
            _filedir
            return 0
        fi
        cmd="${words[3]}"
    fi

    case "$cmd" in
        run)
            if [[ "$cur" == -* ]]; then
                COMPREPLY=( $(compgen -W "-E -e -t -m -i -f --key-format --no-autoload --dry-run --help" -- "$cur") )
            fi
            ;;
        get)
            if [[ "$cur" == -* ]]; then
                COMPREPLY=( $(compgen -W "--help -h" -- "$cur") )
            else
                local schemes="keyring:// env:// cache:// search://"
                local vaults=$(cloakenv internal list-vaults 2>/dev/null)
                local vault_schemes=""
                for v in $vaults; do
                    vault_schemes+="${v}:// "
                done
                COMPREPLY=( $(compgen -W "${schemes} ${vault_schemes}" -- "$cur") )
            fi
            ;;
        set|delete)
            if [[ "$cur" == -* ]]; then
                COMPREPLY=( $(compgen -W "--ttl --help -h" -- "$cur") )
            else
                local schemes="keyring:// cache://"
                COMPREPLY=( $(compgen -W "${schemes}" -- "$cur") )
            fi
            ;;
        cache)
            if [[ $cword -eq 2 || ($cword -eq 4 && "${words[1]}" == "-c") ]]; then
                COMPREPLY=( $(compgen -W "clear prune --help -h" -- "$cur") )
            fi
            ;;
        show)
            if [[ "$cur" == -* ]]; then
                COMPREPLY=( $(compgen -W "-m -e -t -i -f --key-format --show-meta -o --output --help -h" -- "$cur") )
            fi
            ;;
        search)
            if [[ "$cur" == -* ]]; then
                COMPREPLY=( $(compgen -W "--vault -i -f --key-format --show-meta -o --output --help -h" -- "$cur") )
            fi
            ;;
        auth)
            if [[ $cword -eq 2 || ($cword -eq 4 && "${words[1]}" == "-c") ]]; then
                COMPREPLY=( $(compgen -W "login forget status --help -h" -- "$cur") )
            elif [[ $cword -eq 3 || ($cword -eq 5 && "${words[1]}" == "-c") ]]; then
                local vaults=$(cloakenv internal list-vaults 2>/dev/null)
                COMPREPLY=( $(compgen -W "${vaults}" -- "$cur") )
            fi
            ;;
        sync)
            if [[ "$cur" == -* ]]; then
                COMPREPLY=( $(compgen -W "--dry-run --tags --prefix -r --regex --help -h" -- "$cur") )
            fi
            ;;
        config)
            if [[ $cword -eq 2 || ($cword -eq 4 && "${words[1]}" == "-c") ]]; then
                COMPREPLY=( $(compgen -W "check --help -h" -- "$cur") )
            elif [[ "${words[2]}" == "check" || "${words[4]}" == "check" ]]; then
                COMPREPLY=( $(compgen -W "--strict --help -h" -- "$cur") )
            fi
            ;;
        doctor)
            COMPREPLY=( $(compgen -W "--help -h" -- "$cur") )
            ;;
        completion)
            COMPREPLY=( $(compgen -W "bash zsh fish powershell --help -h" -- "$cur") )
            ;;
        *)
            ;;
    esac
    return 0
}

complete -F _cloakenv_complete cloakenv
`

const zshCompletionScript = `#compdef cloakenv

_cloakenv() {
    local -a commands
    commands=(
        'run:Wrap a binary with injected environment variables'
        'get:Retrieve and print a single secret value raw to stdout'
        'set:Store a secret value at a writable URI'
        'delete:Remove a secret from a writable URI'
        'cache:Manage local encrypted cache'
        'show:Retrieve and display a structured entry'
        'search:Search for structured entries'
        'auth:Manage vault authentication'
        'sync:Cross-vault secret synchronization'
        'config:Inspect and validate configuration'
        'doctor:Diagnose environment and vault reachability'
        'completion:Generate shell autocompletion script'
        'version:Print version information'
        'internal:Internal helper commands'
    )

    _arguments -C \
        '-c[Custom configuration file path]:config file:_files' \
        '(-h --help)'{-h,--help}'[Display help information]' \
        '(-v --version)'{-v,--version}'[Display version]' \
        '1: :->command' \
        '*:: :->args'

    case $state in
        command)
            _describe -t commands 'cloakenv command' commands
            ;;
        args)
            case $words[1] in
                completion)
                    _values 'shell' bash zsh fish powershell
                    ;;
                cache)
                    _values 'subcommand' clear prune
                    ;;
                auth)
                    _values 'subcommand' login forget status
                    ;;
                config)
                    _values 'subcommand' check
                    ;;
                *)
                    _files
                    ;;
            esac
            ;;
    esac
}

_cloakenv "$@"
`

const fishCompletionScript = `# fish completion for cloakenv

# Disable file completion for subcommands
complete -c cloakenv -f

# Global flags
complete -c cloakenv -s c -d "Custom configuration file path" -r
complete -c cloakenv -s h -l help -d "Display help information"
complete -c cloakenv -s v -l version -d "Display version"

# Subcommands
complete -c cloakenv -n "__fish_use_subcommand" -a run -d "Wrap a binary with injected environment variables"
complete -c cloakenv -n "__fish_use_subcommand" -a get -d "Retrieve and print a single secret value"
complete -c cloakenv -n "__fish_use_subcommand" -a set -d "Store a secret value at a writable URI"
complete -c cloakenv -n "__fish_use_subcommand" -a delete -d "Remove a secret from a writable URI"
complete -c cloakenv -n "__fish_use_subcommand" -a cache -d "Manage local encrypted cache"
complete -c cloakenv -n "__fish_use_subcommand" -a show -d "Retrieve and display a structured entry"
complete -c cloakenv -n "__fish_use_subcommand" -a search -d "Search for structured entries"
complete -c cloakenv -n "__fish_use_subcommand" -a auth -d "Manage vault authentication"
complete -c cloakenv -n "__fish_use_subcommand" -a sync -d "Cross-vault secret synchronization"
complete -c cloakenv -n "__fish_use_subcommand" -a config -d "Inspect and validate configuration"
complete -c cloakenv -n "__fish_use_subcommand" -a doctor -d "Diagnose environment and vault reachability"
complete -c cloakenv -n "__fish_use_subcommand" -a completion -d "Generate shell completion script"
complete -c cloakenv -n "__fish_use_subcommand" -a version -d "Print version information"

# completion arguments
complete -c cloakenv -n "__fish_seen_subcommand_from completion" -a "bash zsh fish powershell"

# cache subcommands
complete -c cloakenv -n "__fish_seen_subcommand_from cache" -a "clear prune"

# auth subcommands
complete -c cloakenv -n "__fish_seen_subcommand_from auth" -a "login forget status"

# config subcommands
complete -c cloakenv -n "__fish_seen_subcommand_from config" -a "check"
complete -c cloakenv -n "__fish_seen_subcommand_from check" -l strict -d "Enforce strict schema validation"
`

const powershellCompletionScript = `Register-ArgumentCompleter -Native -CommandName cloakenv -ScriptBlock {
    param($wordToComplete, $commandAst, $cursorPosition)

    $commandElements = $commandAst.Elements
    $command = @(
        for ($i = 1; $i -lt $commandElements.Count; $i++) {
            $element = $commandElements[$i]
            if ($element -is [System.Management.Automation.Language.StringConstantExpressionAst] -and
                $element.StringConstantType -eq [System.Management.Automation.Language.StringConstantType]::BareWord -and
                -$not ($element.Value.StartsWith("-"))) {
                $element.Value
                break
            }
        }
    )[0]

    $subcommands = @(
        'run', 'get', 'set', 'delete', 'cache',
        'show', 'search', 'auth', 'sync', 'config',
        'doctor', 'completion', 'version'
    )

    if (-not $command) {
        $subcommands | Where-Object { $_ -like "$wordToComplete*" } | ForEach-Object {
            [System.Management.Automation.CompletionResult]::new($_, $_, 'ParameterValue', $_)
        }
        return
    }

    switch ($command) {
        'completion' {
            @('bash', 'zsh', 'fish', 'powershell') | Where-Object { $_ -like "$wordToComplete*" } | ForEach-Object {
                [System.Management.Automation.CompletionResult]::new($_, $_, 'ParameterValue', $_)
            }
        }
        'cache' {
            @('clear', 'prune') | Where-Object { $_ -like "$wordToComplete*" } | ForEach-Object {
                [System.Management.Automation.CompletionResult]::new($_, $_, 'ParameterValue', $_)
            }
        }
        'auth' {
            @('login', 'forget', 'status') | Where-Object { $_ -like "$wordToComplete*" } | ForEach-Object {
                [System.Management.Automation.CompletionResult]::new($_, $_, 'ParameterValue', $_)
            }
        }
        'config' {
            @('check') | Where-Object { $_ -like "$wordToComplete*" } | ForEach-Object {
                [System.Management.Automation.CompletionResult]::new($_, $_, 'ParameterValue', $_)
            }
        }
    }
}
`

// Completion handles the "cloakenv completion [bash|zsh|fish|powershell]" command.
func Completion(args []string, stdout, stderr io.Writer) int {
	if utils.HasHelpFlag(args) || len(args) == 0 {
		PrintCompletionHelpTo(stdout)
		if len(args) == 0 {
			return 1
		}
		return 0
	}

	shell := strings.ToLower(args[0])
	switch shell {
	case "bash":
		fmt.Fprint(stdout, bashCompletionScript)
		return 0
	case "zsh":
		fmt.Fprint(stdout, zshCompletionScript)
		return 0
	case "fish":
		fmt.Fprint(stdout, fishCompletionScript)
		return 0
	case "powershell", "pwsh":
		fmt.Fprint(stdout, powershellCompletionScript)
		return 0
	default:
		fmt.Fprintf(stderr, "Error: unsupported shell %q (supported: bash, zsh, fish, powershell)\n", shell)
		return 1
	}
}
