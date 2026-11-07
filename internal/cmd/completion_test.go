package cmd

import (
	"bytes"
	"os/exec"
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
			name:       "no args",
			args:       []string{},
			wantExit:   1,
			wantErrSub: "",
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
