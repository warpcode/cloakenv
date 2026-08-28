package utils

import (
	"bytes"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
)

type errMarshaler struct{}

func (errMarshaler) MarshalJSON() ([]byte, error) {
	return nil, errors.New("marshal json error")
}

func (errMarshaler) MarshalYAML() (any, error) {
	return nil, errors.New("marshal yaml error")
}

func TestRenderOutput(t *testing.T) {
	type sampleData struct {
		Key   string `json:"key" yaml:"key"`
		Count int    `json:"count" yaml:"count"`
	}

	tests := []struct {
		name           string
		data           any
		asJSON         bool
		errorLabel     string
		expectedOutput string
		wantErr        bool
		errContains    string
	}{
		{
			name:       "render JSON successfully",
			data:       sampleData{Key: "foo", Count: 42},
			asJSON:     true,
			errorLabel: "sample data",
			expectedOutput: "{\n  \"key\": \"foo\",\n  \"count\": 42\n}\n",
			wantErr:        false,
		},
		{
			name:       "render YAML successfully",
			data:       sampleData{Key: "bar", Count: 100},
			asJSON:     false,
			errorLabel: "sample data",
			expectedOutput: "key: bar\ncount: 100\n",
			wantErr:        false,
		},
		{
			name:        "JSON serialization error",
			data:        errMarshaler{},
			asJSON:      true,
			errorLabel:  "invalid JSON data",
			wantErr:     true,
			errContains: "failed to serialize invalid JSON data to JSON",
		},
		{
			name:        "YAML serialization error",
			data:        errMarshaler{},
			asJSON:      false,
			errorLabel:  "invalid YAML data",
			wantErr:     true,
			errContains: "failed to serialize invalid YAML data to YAML",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			oldStdout := os.Stdout
			r, w, err := os.Pipe()
			if err != nil {
				t.Fatalf("failed to create pipe: %v", err)
			}
			os.Stdout = w

			renderErr := RenderOutput(tt.data, tt.asJSON, tt.errorLabel)

			_ = w.Close()
			os.Stdout = oldStdout

			var buf bytes.Buffer
			if _, err := io.Copy(&buf, r); err != nil {
				t.Fatalf("failed to read captured stdout: %v", err)
			}
			_ = r.Close()

			if (renderErr != nil) != tt.wantErr {
				t.Fatalf("RenderOutput() error = %v, wantErr %v", renderErr, tt.wantErr)
			}

			if tt.wantErr {
				if !strings.Contains(renderErr.Error(), tt.errContains) {
					t.Errorf("RenderOutput() error = %q, want containing %q", renderErr.Error(), tt.errContains)
				}
			} else {
				gotOutput := strings.ReplaceAll(buf.String(), "\r\n", "\n")
				if gotOutput != tt.expectedOutput {
					t.Errorf("RenderOutput() output = %q, want %q", gotOutput, tt.expectedOutput)
				}
			}
		})
	}
}

func TestFormatKey(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"my-key", "MY_KEY"},
		{"my.key.name", "MY_KEY_NAME"},
		{"my--key", "MY_KEY"},
		{"__foo", "_FOO"},
		{"foo__", "FOO_"},
		{"multiple___underscores", "MULTIPLE_UNDERSCORES"},
		{"api-v2-key", "API_V2_KEY"},
		{"KEY_A", "KEY_A"},
		{"already_Format_Key", "ALREADY_FORMAT_KEY"},
		{"special$#@char", "SPECIAL_CHAR"},
	}

	for _, tc := range tests {
		got := FormatKey(tc.input)
		if got != tc.expected {
			t.Errorf("FormatKey(%q) = %q; expected %q", tc.input, got, tc.expected)
		}
	}
}

func TestSerializeAttrValue(t *testing.T) {
	tests := []struct {
		name     string
		input    any
		expected string
		wantErr  bool
	}{
		{
			name:     "string value",
			input:    "secret_value",
			expected: "secret_value",
		},
		{
			name:     "slice of string",
			input:    []string{"item1", "item2"},
			expected: "- item1\n- item2",
		},
		{
			name:     "slice of any",
			input:    []any{"alpha", 100, true},
			expected: "- alpha\n- 100\n- true",
		},
		{
			name:     "map of string to any",
			input:    map[string]any{"port": 8080, "host": "localhost"},
			expected: "host: localhost\nport: 8080",
		},
		{
			name:     "map of any to any",
			input:    map[any]any{"key": "val"},
			expected: "key: val",
		},
		{
			name:     "integer primitive",
			input:    42,
			expected: "42",
		},
		{
			name:     "boolean primitive",
			input:    true,
			expected: "true",
		},
		{
			name:     "nil value",
			input:    nil,
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := SerializeAttrValue(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("SerializeAttrValue(%v) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if got != tt.expected {
				t.Errorf("SerializeAttrValue(%v) = %q; want %q", tt.input, got, tt.expected)
			}
		})
	}
}
