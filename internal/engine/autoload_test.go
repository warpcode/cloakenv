package engine

import (
	"testing"

	"github.com/warpcode/cloakenv/internal/config"
)

func TestMatchCommand(t *testing.T) {
	tests := []struct {
		name        string
		ruleCommand string
		cmdArgs     []string
		wantMatch   bool
	}{
		{
			name:        "empty rule or args",
			ruleCommand: "",
			cmdArgs:     []string{"aws"},
			wantMatch:   false,
		},
		{
			name:        "nil cmdArgs",
			ruleCommand: "aws",
			cmdArgs:     nil,
			wantMatch:   false,
		},
		{
			name:        "exact executable name",
			ruleCommand: "aws",
			cmdArgs:     []string{"aws", "s3", "ls"},
			wantMatch:   true,
		},
		{
			name:        "full executable path basename",
			ruleCommand: "aws",
			cmdArgs:     []string{"/usr/local/bin/aws", "ec2", "describe-instances"},
			wantMatch:   true,
		},
		{
			name:        "exact path match",
			ruleCommand: "/usr/bin/python3",
			cmdArgs:     []string{"/usr/bin/python3", "script.py"},
			wantMatch:   true,
		},
		{
			name:        "case insensitive executable match",
			ruleCommand: "AWS",
			cmdArgs:     []string{"aws", "s3"},
			wantMatch:   true,
		},
		{
			name:        "subcommand prefix match",
			ruleCommand: "git push",
			cmdArgs:     []string{"git", "push", "origin", "main"},
			wantMatch:   true,
		},
		{
			name:        "subcommand prefix mismatch",
			ruleCommand: "git push",
			cmdArgs:     []string{"git", "status"},
			wantMatch:   false,
		},
		{
			name:        "glob pattern executable match",
			ruleCommand: "kubectl*",
			cmdArgs:     []string{"kubectl-prod", "get", "pods"},
			wantMatch:   true,
		},
		{
			name:        "glob pattern script match",
			ruleCommand: "*.sh",
			cmdArgs:     []string{"./deploy.sh", "staging"},
			wantMatch:   true,
		},
		{
			name:        "glob pattern full command match",
			ruleCommand: "npm run *",
			cmdArgs:     []string{"npm", "run", "build"},
			wantMatch:   true,
		},
		{
			name:        "non matching executable",
			ruleCommand: "terraform",
			cmdArgs:     []string{"helm", "install"},
			wantMatch:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MatchCommand(tt.ruleCommand, tt.cmdArgs)
			if got != tt.wantMatch {
				t.Errorf("MatchCommand(%q, %v) = %v, want %v", tt.ruleCommand, tt.cmdArgs, got, tt.wantMatch)
			}
		})
	}
}

func TestSplitCommand(t *testing.T) {
	tests := []struct {
		input   string
		want    []string
		wantErr bool
	}{
		{
			input:   "uvx --with 'litellm[proxy]' --with 'fastapi<0.116' litellm --config config.yaml",
			want:    []string{"uvx", "--with", "litellm[proxy]", "--with", "fastapi<0.116", "litellm", "--config", "config.yaml"},
			wantErr: false,
		},
		{
			input:   `echo "hello world" 'foo bar'`,
			want:    []string{"echo", "hello world", "foo bar"},
			wantErr: false,
		},
		{
			input:   `C:\Users\runner\AppData\Local\Temp\tool.exe arg1 arg2`,
			want:    []string{`C:\Users\runner\AppData\Local\Temp\tool.exe`, "arg1", "arg2"},
			wantErr: false,
		},
		{
			input:   `"C:\Program Files\Tool\tool.exe" --flag "value"`,
			want:    []string{`C:\Program Files\Tool\tool.exe`, "--flag", "value"},
			wantErr: false,
		},
		{
			input:   `tool\ name arg`,
			want:    []string{"tool name", "arg"},
			wantErr: false,
		},
		{
			input:   `cmd 'unclosed quote`,
			want:    nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := splitCommand(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("splitCommand(%q) err = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if !tt.wantErr {
				if len(got) != len(tt.want) {
					t.Fatalf("splitCommand(%q) len = %d, want %d", tt.input, len(got), len(tt.want))
				}
				for i := range got {
					if got[i] != tt.want[i] {
						t.Errorf("token %d = %q, want %q", i, got[i], tt.want[i])
					}
				}
			}
		})
	}
}

func TestMatchRunAlias(t *testing.T) {
	cfg := &config.Config{
		Autoload: []config.AutoloadRule{
			{
				Match:   "^litellm\\s+(.*)$",
				Command: "uvx --with 'litellm[proxy]' litellm \\1",
				Vaults:  []string{"work_vault"},
				Env: map[string]string{
					"LITELLM_KEY": "keyring://litellm/master_key",
				},
			},
			{
				Match:  "aws",
				Vaults: []string{"aws_prod"},
			},
		},
	}

	orch, err := NewOrchestrator(cfg)
	if err != nil {
		t.Fatalf("failed to create orchestrator: %v", err)
	}

	t.Run("Match regex rule", func(t *testing.T) {
		cmdArgs := []string{"litellm", "--config", "config.yaml"}

		rule, matched, err := MatchRunAlias(cfg, cmdArgs)
		if err != nil {
			t.Fatalf("MatchRunAlias returned unexpected error: %v", err)
		}
		if !matched {
			t.Fatalf("expected MatchRunAlias to return true")
		}
		if rule.Match != "^litellm\\s+(.*)$" {
			t.Errorf("expected match ^litellm\\s+(.*)$, got %q", rule.Match)
		}

		ruleOrch, matchedOrch, errOrch := orch.MatchRunAlias(cmdArgs)
		if errOrch != nil {
			t.Fatalf("orchestrator MatchRunAlias returned unexpected error: %v", errOrch)
		}
		if !matchedOrch || ruleOrch.Match != rule.Match {
			t.Errorf("orchestrator method mismatch: got (%v, %t)", ruleOrch, matchedOrch)
		}

		if !IsRunAlias(cfg, cmdArgs) || !orch.IsRunAlias(cmdArgs) {
			t.Errorf("expected IsRunAlias to return true")
		}
	})

	t.Run("Match simple rule", func(t *testing.T) {
		cmdArgs := []string{"aws", "s3", "ls"}

		rule, matched, err := MatchRunAlias(cfg, cmdArgs)
		if err != nil {
			t.Fatalf("MatchRunAlias returned unexpected error: %v", err)
		}
		if !matched {
			t.Fatalf("expected MatchRunAlias to return true")
		}
		if rule.Match != "aws" {
			t.Errorf("expected match 'aws', got %q", rule.Match)
		}
	})

	t.Run("No match", func(t *testing.T) {
		cmdArgs := []string{"helm", "status"}

		_, matched, _ := MatchRunAlias(cfg, cmdArgs)
		if matched {
			t.Errorf("expected MatchRunAlias to return false for unmatched command")
		}
		if IsRunAlias(cfg, cmdArgs) || orch.IsRunAlias(cmdArgs) {
			t.Errorf("expected IsRunAlias to return false for unmatched command")
		}
	})

	t.Run("Nil or empty inputs", func(t *testing.T) {
		if IsRunAlias(nil, []string{"aws"}) {
			t.Errorf("expected IsRunAlias(nil, ...) to return false")
		}
		var nilOrch *Orchestrator
		if nilOrch.IsRunAlias([]string{"aws"}) {
			t.Errorf("expected nilOrch.IsRunAlias(...) to return false")
		}
		if IsRunAlias(cfg, nil) || IsRunAlias(cfg, []string{}) {
			t.Errorf("expected IsRunAlias with empty cmdArgs to return false")
		}
	})
}
