// PrintCompletionHelp prints usage help for the completion subcommand.
package cmd

import (
	"fmt"
	"io"
	"os"
)

// PrintCompletionHelp prints usage help for the completion subcommand to stdout.
func PrintCompletionHelp() {
	PrintCompletionHelpTo(os.Stdout)
}

// PrintCompletionHelpTo prints usage help for the completion subcommand to the provided writer.
func PrintCompletionHelpTo(w io.Writer) {
	fmt.Fprintln(w, `Usage:
  cloakenv completion <bash|zsh|fish|powershell>

Description:
  Generate shell autocompletion script for the specified shell.

Supported shells:
  bash        Bourne Again SHell
  zsh         Z Shell
  fish        Friendly Interactive SHell
  powershell  PowerShell / pwsh

Examples:
  # Bash (eval in ~/.bashrc)
  source <(cloakenv completion bash)

  # Zsh (install into completion directory)
  cloakenv completion zsh > ~/.zsh/completion/_cloakenv

  # Fish
  cloakenv completion fish > ~/.config/fish/completions/cloakenv.fish`)
}
