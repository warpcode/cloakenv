package cmd

import (
	"fmt"
	"io"
	"strings"

	"github.com/warpcode/cloakenv/internal/utils"
)

const bashCompletionScript = `# bash completion for cloakenv
_cloakenv_complete() {
    local cur prev words cword
    cur="${COMP_WORDS[COMP_CWORD]}"
    prev="${COMP_WORDS[COMP_CWORD-1]}"
    words=("${COMP_WORDS[@]}")
    cword=$COMP_CWORD

    local subcommands="run inspect auth cache doctor completion version internal"

    if [[ $cword -eq 1 ]]; then
        if [[ "$cur" == -* ]]; then
            COMPREPLY=( $(compgen -W "-c -h --help -v --version" -- "$cur") )
        else
            COMPREPLY=( $(compgen -W "${subcommands}" -- "$cur") )
        fi
        return 0
    fi

    if [[ "$prev" == "-c" ]]; then
        COMPREPLY=( $(compgen -f -- "$cur") )
        return 0
    fi

    local cmd=""
    local i=1
    while [[ $i -lt $cword ]]; do
        if [[ "${words[$i]}" == "-c" ]]; then
            i=$((i + 2))
            continue
        fi
        if [[ "${words[$i]}" != -* ]]; then
            cmd="${words[$i]}"
            break
        fi
        i=$((i + 1))
    done

    if [[ -z "$cmd" ]]; then
        COMPREPLY=( $(compgen -W "${subcommands}" -- "$cur") )
        return 0
    fi

    case "$cmd" in
        run)
            if [[ "$cur" == -* ]]; then
                COMPREPLY=( $(compgen -W "-E --no-autoload --dry-run --help -h" -- "$cur") )
            fi
            ;;
        inspect)
            if [[ "$cur" == -* ]]; then
                COMPREPLY=( $(compgen -W "-o --output --help -h" -- "$cur") )
            elif [[ $cword -eq 2 || ($cword -eq 4 && "${words[1]}" == "-c") ]]; then
                COMPREPLY=( $(compgen -W "env template run" -- "$cur") )
            fi
            ;;
        cache)
            COMPREPLY=( $(compgen -W "clear prune --help -h" -- "$cur") )
            ;;
        auth)
            if [[ $cword -eq 2 || ($cword -eq 4 && "${words[1]}" == "-c") ]]; then
                COMPREPLY=( $(compgen -W "login forget status --help -h" -- "$cur") )
            elif [[ $cword -eq 3 || ($cword -eq 5 && "${words[1]}" == "-c") ]]; then
                local vaults=$(cloakenv internal list-vaults 2>/dev/null)
                COMPREPLY=( $(compgen -W "${vaults}" -- "$cur") )
            fi
            ;;
        doctor)
            COMPREPLY=( $(compgen -W "--strict --help -h" -- "$cur") )
            ;;
        completion)
            COMPREPLY=( $(compgen -W "bash zsh fish powershell --help -h" -- "$cur") )
            ;;
        internal)
            if [[ $cword -eq 2 || ($cword -eq 4 && "${words[1]}" == "-c") ]]; then
                COMPREPLY=( $(compgen -W "match-alias list-vaults --help -h" -- "$cur") )
            fi
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
        'run:Wrap a binary with config-governed injected environment variables and templates'
        'inspect:Preview matched vaults, envs, templates, and autoload rules (always redacted)'
        'auth:Manage vault authentication'
        'cache:Manage local encrypted cache'
        'doctor:Diagnose environment, validate config, and check vault reachability'
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
                inspect)
                    _values 'target' env template run
                    ;;
                completion)
                    _values 'shell' bash zsh fish powershell
                    ;;
                cache)
                    _values 'subcommand' clear prune
                    ;;
                auth)
                    _values 'subcommand' login forget status
                    ;;
                internal)
                    _values 'subcommand' match-alias list-vaults
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
complete -c cloakenv -n "__fish_use_subcommand" -a run -d "Wrap a binary with config-governed injected environment variables"
complete -c cloakenv -n "__fish_use_subcommand" -a inspect -d "Preview matched envs, templates, and autoload rules (redacted)"
complete -c cloakenv -n "__fish_use_subcommand" -a auth -d "Manage vault authentication"
complete -c cloakenv -n "__fish_use_subcommand" -a cache -d "Manage local encrypted cache"
complete -c cloakenv -n "__fish_use_subcommand" -a doctor -d "Diagnose environment, validate config, and check vault reachability"
complete -c cloakenv -n "__fish_use_subcommand" -a completion -d "Generate shell completion script"
complete -c cloakenv -n "__fish_use_subcommand" -a version -d "Print version information"
complete -c cloakenv -n "__fish_use_subcommand" -a internal -d "Internal helper commands"

# run flags
complete -c cloakenv -n "__fish_seen_subcommand_from run" -s E -d "Start with an empty environment"
complete -c cloakenv -n "__fish_seen_subcommand_from run" -l no-autoload -d "Disable config autoload rules"
complete -c cloakenv -n "__fish_seen_subcommand_from run" -l dry-run -d "Preview execution without running"

# inspect subcommands & flags
complete -c cloakenv -n "__fish_seen_subcommand_from inspect" -a "env template run"
complete -c cloakenv -n "__fish_seen_subcommand_from inspect" -s o -l output -d "Output format (text, json, yaml)" -r

# completion arguments
complete -c cloakenv -n "__fish_seen_subcommand_from completion" -a "bash zsh fish powershell"

# cache subcommands
complete -c cloakenv -n "__fish_seen_subcommand_from cache" -a "clear prune"

# auth subcommands
complete -c cloakenv -n "__fish_seen_subcommand_from auth" -a "login forget status"

# doctor flags
complete -c cloakenv -n "__fish_seen_subcommand_from doctor" -l strict -d "Enforce strict schema and warning checks"

# internal subcommands
complete -c cloakenv -n "__fish_seen_subcommand_from internal" -a "match-alias list-vaults"
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
        'run', 'inspect', 'auth', 'cache',
        'doctor', 'completion', 'version', 'internal'
    )

    if (-not $command) {
        $subcommands | Where-Object { $_ -like "$wordToComplete*" } | ForEach-Object {
            [System.Management.Automation.CompletionResult]::new($_, $_, 'ParameterValue', $_)
        }
        return
    }

    switch ($command) {
        'inspect' {
            @('env', 'template', 'run') | Where-Object { $_ -like "$wordToComplete*" } | ForEach-Object {
                [System.Management.Automation.CompletionResult]::new($_, $_, 'ParameterValue', $_)
            }
        }
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
        'internal' {
            @('match-alias', 'list-vaults') | Where-Object { $_ -like "$wordToComplete*" } | ForEach-Object {
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
