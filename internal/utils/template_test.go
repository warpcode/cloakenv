package utils

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseTemplateFile(t *testing.T) {
	tempDir := t.TempDir()
	t.Chdir(tempDir)

	tests := []struct {
		name      string
		content   string
		setupPath func(t *testing.T, baseDir string) (clean, dirty string)
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
			setupPath: func(t *testing.T, baseDir string) (string, string) {
				p := filepath.Join(baseDir, "test.env")
				return p, p
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
			setupPath: func(t *testing.T, baseDir string) (string, string) {
				p := filepath.Join(baseDir, "test.env")
				return p, p
			},
			expectErr: false,
		},
		{
			name:    "value containing equal signs",
			content: "KEY=foo=bar=baz\n",
			want: map[string]string{
				"KEY": "foo=bar=baz",
			},
			setupPath: func(t *testing.T, baseDir string) (string, string) {
				p := filepath.Join(baseDir, "test_equals.env")
				return p, p
			},
			expectErr: false,
		},
		{
			name:    "mismatched quotes",
			content: `KEY="value'` + "\n",
			want: map[string]string{
				"KEY": `"value'`,
			},
			setupPath: func(t *testing.T, baseDir string) (string, string) {
				p := filepath.Join(baseDir, "test_mismatched.env")
				return p, p
			},
			expectErr: false,
		},
		{
			name:    "empty file",
			content: "",
			want:    map[string]string{},
			setupPath: func(t *testing.T, baseDir string) (string, string) {
				p := filepath.Join(baseDir, "empty.env")
				return p, p
			},
			expectErr: false,
		},
		{
			name:    "invalid format (missing equal)",
			content: "INVALID_LINE\n",
			want:    nil,
			setupPath: func(t *testing.T, baseDir string) (string, string) {
				p := filepath.Join(baseDir, "test.env")
				return p, p
			},
			expectErr: true,
		},
		{
			name:    "empty key",
			content: "=value\n",
			want:    nil,
			setupPath: func(t *testing.T, baseDir string) (string, string) {
				p := filepath.Join(baseDir, "test.env")
				return p, p
			},
			expectErr: true,
		},
		{
			name:    "empty value",
			content: "KEY=\n",
			want:    nil,
			setupPath: func(t *testing.T, baseDir string) (string, string) {
				p := filepath.Join(baseDir, "test.env")
				return p, p
			},
			expectErr: true,
		},
		{
			name:    "empty value with spaces",
			content: "KEY=   \n",
			want:    nil,
			setupPath: func(t *testing.T, baseDir string) (string, string) {
				p := filepath.Join(baseDir, "test.env")
				return p, p
			},
			expectErr: true,
		},
		{
			name:    "quoted empty value double quotes",
			content: `KEY=""` + "\n",
			want:    nil,
			setupPath: func(t *testing.T, baseDir string) (string, string) {
				p := filepath.Join(baseDir, "test_empty_dq.env")
				return p, p
			},
			expectErr: true,
		},
		{
			name:    "quoted empty value single quotes",
			content: "KEY=''\n",
			want:    nil,
			setupPath: func(t *testing.T, baseDir string) (string, string) {
				p := filepath.Join(baseDir, "test_empty_sq.env")
				return p, p
			},
			expectErr: true,
		},
		{
			name:    "scanner read error due to line exceeding max token size",
			content: strings.Repeat("A", bufio.MaxScanTokenSize+1) + "\n",
			want:    nil,
			setupPath: func(t *testing.T, baseDir string) (string, string) {
				p := filepath.Join(baseDir, "test_long_line.env")
				return p, p
			},
			expectErr: true,
		},
		{
			name: "redundant path segments",
			content: `
K=V
`,
			want: map[string]string{
				"K": "V",
			},
			setupPath: func(t *testing.T, baseDir string) (string, string) {
				dummyDir := filepath.Join(baseDir, "dummy")
				if err := os.Mkdir(dummyDir, 0755); err != nil && !os.IsExist(err) {
					t.Fatalf("failed to create dummy dir: %v", err)
				}
				cleanTmpFile := filepath.Join(baseDir, "test.env")
				dirtyTmpFile := filepath.Join(baseDir, "dummy", "..", "test.env")
				return cleanTmpFile, dirtyTmpFile
			},
			expectErr: false,
		},
		{
			name:    "non-existent file",
			content: "",
			want:    nil,
			setupPath: func(t *testing.T, baseDir string) (string, string) {
				return "", filepath.Join(baseDir, "does_not_exist.env")
			},
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cleanTmpFile, dirtyTmpFile := tt.setupPath(t, tempDir)
			if cleanTmpFile != "" {
				if err := os.WriteFile(cleanTmpFile, []byte(tt.content), 0644); err != nil {
					t.Fatalf("failed to write test file: %v", err)
				}
			}

			got, err := ParseTemplateFile(dirtyTmpFile)
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
