package runner

import (
	"bytes"
	"io"
	"os"
	"testing"
)

func TestValidateCommand(t *testing.T) {
	tests := []struct {
		name       string
		cmdArgs    []string
		env        []string
		wantCode   int
		wantStderr string
	}{
		{
			name:       "empty args",
			cmdArgs:    []string{},
			env:        []string{},
			wantCode:   1,
			wantStderr: "Command missing\n",
		},
		{
			name:       "empty command name",
			cmdArgs:    []string{""},
			env:        []string{},
			wantCode:   1,
			wantStderr: "Invalid command: \"\"\n",
		},
		{
			name:       "dot command",
			cmdArgs:    []string{"."},
			env:        []string{},
			wantCode:   1,
			wantStderr: "Invalid command: \".\"\n",
		},
		{
			name:       "dot dot command",
			cmdArgs:    []string{".."},
			env:        []string{},
			wantCode:   1,
			wantStderr: "Invalid command: \"..\"\n",
		},
		{
			name:       "null byte in command name",
			cmdArgs:    []string{"echo\x00bar"},
			env:        []string{},
			wantCode:   1,
			wantStderr: "Invalid argument at index 0: contains null byte\n",
		},
		{
			name:       "null byte in argument",
			cmdArgs:    []string{"echo", "hello\x00world"},
			env:        []string{},
			wantCode:   1,
			wantStderr: "Invalid argument at index 1: contains null byte\n",
		},
		{
			name:       "null byte in env variable",
			cmdArgs:    []string{"echo", "hello"},
			env:        []string{"BAD_VAR=foo\x00bar"},
			wantCode:   1,
			wantStderr: "Invalid environment variable \"BAD_VAR\" at index 0: contains null byte\n",
		},
		{
			name:       "env variable missing equals",
			cmdArgs:    []string{"echo", "hello"},
			env:        []string{"KEY_WITHOUT_EQUALS"},
			wantCode:   1,
			wantStderr: "Invalid environment variable at index 0: missing '=' or key is empty\n",
		},
		{
			name:       "env variable empty key",
			cmdArgs:    []string{"echo", "hello"},
			env:        []string{"=value"},
			wantCode:   1,
			wantStderr: "Invalid environment variable at index 0: missing '=' or key is empty\n",
		},
		{
			name:       "env variable empty string",
			cmdArgs:    []string{"echo", "hello"},
			env:        []string{"EMPTY_VAR=val", ""},
			wantCode:   1,
			wantStderr: "Invalid environment variable at index 1: missing '=' or key is empty\n",
		},
		{
			name:       "valid env variable with empty value",
			cmdArgs:    []string{"echo", "hello"},
			env:        []string{"EMPTY_VAL="},
			wantCode:   0,
			wantStderr: "",
		},
		{
			name:       "valid env variable with multiple equals",
			cmdArgs:    []string{"echo", "hello"},
			env:        []string{"URL=https://example.com?a=1&b=2"},
			wantCode:   0,
			wantStderr: "",
		},
		{
			name:       "valid windows drive env variable",
			cmdArgs:    []string{"echo", "hello"},
			env:        []string{"=C:=C:\\Users"},
			wantCode:   0,
			wantStderr: "",
		},
		{
			name:       "valid command and env",
			cmdArgs:    []string{"echo", "hello"},
			env:        []string{"GOOD_VAR=foo"},
			wantCode:   0,
			wantStderr: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			oldStderr := os.Stderr
			r, w, err := os.Pipe()
			if err != nil {
				t.Fatalf("Failed to create pipe: %v", err)
			}
			defer func() { _ = r.Close() }()
			defer func() { _ = w.Close() }()
			os.Stderr = w
			t.Cleanup(func() {
				os.Stderr = oldStderr
			})

			gotCode := validateCommand(tt.cmdArgs, tt.env)

			_ = w.Close()
			os.Stderr = oldStderr

			var buf bytes.Buffer
			if _, err := io.Copy(&buf, r); err != nil {
				t.Fatalf("Failed to read from pipe: %v", err)
			}
			gotStderr := buf.String()

			if gotCode != tt.wantCode {
				t.Errorf("validateCommand() code = %v, want %v", gotCode, tt.wantCode)
			}

			if gotStderr != tt.wantStderr {
				t.Errorf("validateCommand() stderr = %q, want %q", gotStderr, tt.wantStderr)
			}
		})
	}
}
