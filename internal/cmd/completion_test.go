package cmd

import (
	"bytes"
	"os/exec"
	"regexp"
	"strings"
	"testing"
)

func TestCompletion(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantExit   int
		wantSubstr string
		wantErrSub string
	}{
		{
			name:     "no args",
			args:     []string{},
			wantExit: 1,
		},
		{
			name:       "help flag",
			args:       []string{"--help"},
			wantExit:   0,
			wantSubstr: "cloakenv completion",
		},
		{
			name:       "bash",
			args:       []string{"bash"},
			wantExit:   0,
			wantSubstr: "_cloakenv_complete()",
		},
		{
			name:       "zsh",
			args:       []string{"zsh"},
			wantExit:   0,
			wantSubstr: "#compdef cloakenv",
		},
		{
			name:       "fish",
			args:       []string{"fish"},
			wantExit:   0,
			wantSubstr: "# fish completion for cloakenv",
		},
		{
			name:       "powershell",
			args:       []string{"powershell"},
			wantExit:   0,
			wantSubstr: "Register-ArgumentCompleter",
		},
		{
			name:       "pwsh alias",
			args:       []string{"pwsh"},
			wantExit:   0,
			wantSubstr: "Register-ArgumentCompleter",
		},
		{
			name:       "unsupported",
			args:       []string{"unknown_shell"},
			wantExit:   1,
			wantErrSub: "unsupported shell",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := Completion(tt.args, &stdout, &stderr)
			if code != tt.wantExit {
				t.Fatalf("Completion(%v) exit code = %d, want %d (stderr: %s)", tt.args, code, tt.wantExit, stderr.String())
			}
			if tt.wantSubstr != "" && !strings.Contains(stdout.String(), tt.wantSubstr) {
				t.Errorf("expected stdout to contain %q, got: %s", tt.wantSubstr, stdout.String())
			}
			if tt.wantErrSub != "" && !strings.Contains(stderr.String(), tt.wantErrSub) {
				t.Errorf("expected stderr to contain %q, got: %s", tt.wantErrSub, stderr.String())
			}
		})
	}
}

func TestCompletion_ReflectsLockedDownCLI(t *testing.T) {
	for _, shell := range []string{"bash", "zsh", "fish", "powershell"} {
		t.Run(shell, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := Completion([]string{shell}, &stdout, &stderr); code != 0 {
				t.Fatalf("Completion(%s) failed: %d %s", shell, code, stderr.String())
			}
			out := stdout.String()
			if !strings.Contains(out, "inspect") {
				t.Errorf("%s completion missing 'inspect' subcommand", shell)
			}
			for _, removed := range []string{"--show-meta", "--ttl", "--raw", "--key-format"} {
				if strings.Contains(out, removed) {
					t.Errorf("%s completion should not contain removed flag %q", shell, removed)
				}
			}
			for _, removedCmd := range []string{"get", "show", "search", "set", "delete", "sync"} {
				re := regexp.MustCompile(`\b` + removedCmd + `\b`)
				if re.MatchString(out) {
					t.Errorf("%s completion should not contain removed command %q", shell, removedCmd)
				}
			}
			// Strip shell-builtin flags (compgen -f, complete ... -f, _describe -t) before checking removed run flags
			cleaned := strings.ReplaceAll(out, "compgen -f", "compgen")
			cleaned = strings.ReplaceAll(cleaned, "complete -c cloakenv -f", "complete -c cloakenv")
			cleaned = strings.ReplaceAll(cleaned, "_describe -t", "_describe")
			for _, removedRunFlag := range []string{"-e", "-m", "-t", "-i", "-f"} {
				re := regexp.MustCompile(`(?:^|[^a-zA-Z0-9_-])` + regexp.QuoteMeta(removedRunFlag) + `(?:[^a-zA-Z0-9_-]|$)`)
				if re.MatchString(cleaned) {
					t.Errorf("%s completion should not contain removed run flag %q", shell, removedRunFlag)
				}
			}
		})
	}
}

func TestBashCompletionSyntax(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Completion([]string{"bash"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("unexpected exit code: %d", code)
	}
	cmd := exec.Command("bash", "-n")
	cmd.Stdin = &stdout
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("bash -n failed on generated completion script: %v (output: %s)", err, string(out))
	}
}
