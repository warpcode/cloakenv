package utils

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseTemplateFile(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "cloakenv-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	tests := []struct {
		name      string
		content   string
		want      map[string]string
		expectErr bool
	}{
		{
			name: "valid entries",
			content: `# This is a comment
KEY1=val1
KEY2 = val2  
KEY3 = "val3"
KEY4='val4'
`,
			want: map[string]string{
				"KEY1": "val1",
				"KEY2": "val2",
				"KEY3": "val3",
				"KEY4": "val4",
			},
			expectErr: false,
		},
		{
			name: "comments and blank lines",
			content: `
# comment 1
   # comment 2
A=B

C=D
`,
			want: map[string]string{
				"A": "B",
				"C": "D",
			},
			expectErr: false,
		},
		{
			name:      "invalid format (missing equal)",
			content:   "INVALID_LINE\n",
			want:      nil,
			expectErr: true,
		},
		{
			name:      "empty key",
			content:   "=value\n",
			want:      nil,
			expectErr: true,
		},
		{
			name:      "empty value",
			content:   "KEY=\n",
			want:      nil,
			expectErr: true,
		},
		{
			name:      "empty value with spaces",
			content:   "KEY=   \n",
			want:      nil,
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpFile := filepath.Join(tempDir, "test.env")
			if err := os.WriteFile(tmpFile, []byte(tt.content), 0644); err != nil {
				t.Fatalf("failed to write test file: %v", err)
			}

			got, err := ParseTemplateFile(tmpFile)
			if (err != nil) != tt.expectErr {
				t.Errorf("ParseTemplateFile() error = %v, expectErr = %v", err, tt.expectErr)
				return
			}

			if !tt.expectErr {
				if len(got) != len(tt.want) {
					t.Errorf("ParseTemplateFile() length mismatch: got %d, want %d", len(got), len(tt.want))
				}
				for k, wantVal := range tt.want {
					gotVal, ok := got[k]
					if !ok {
						t.Errorf("ParseTemplateFile() missing key %q", k)
					} else if gotVal != wantVal {
						t.Errorf("ParseTemplateFile() for key %q got %q, want %q", k, gotVal, wantVal)
					}
				}
			}
		})
	}
}
