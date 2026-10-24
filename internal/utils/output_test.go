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
			name:           "render JSON successfully",
			data:           sampleData{Key: "foo", Count: 42},
			asJSON:         true,
			errorLabel:     "sample data",
			expectedOutput: "{\n  \"key\": \"foo\",\n  \"count\": 42\n}\n",
			wantErr:        false,
		},
		{
			name:           "render YAML successfully",
			data:           sampleData{Key: "bar", Count: 100},
			asJSON:         false,
			errorLabel:     "sample data",
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
		name     string
		input    string
		expected string
	}{
		{
			name:     "hyphenated key",
			input:    "my-key",
			expected: "MY_KEY",
		},
		{
			name:     "dot separated key",
			input:    "my.key.name",
			expected: "MY_KEY_NAME",
		},
		{
			name:     "consecutive hyphens",
			input:    "my--key",
			expected: "MY_KEY",
		},
		{
			name:     "leading double underscores",
			input:    "__foo",
			expected: "_FOO",
		},
		{
			name:     "trailing double underscores",
			input:    "foo__",
			expected: "FOO_",
		},
		{
			name:     "multiple consecutive underscores in middle",
			input:    "multiple___underscores",
			expected: "MULTIPLE_UNDERSCORES",
		},
		{
			name:     "alphanumeric with hyphen",
			input:    "api-v2-key",
			expected: "API_V2_KEY",
		},
		{
			name:     "already uppercase formatted key",
			input:    "KEY_A",
			expected: "KEY_A",
		},
		{
			name:     "mixed case already formatted with underscore",
			input:    "already_Format_Key",
			expected: "ALREADY_FORMAT_KEY",
		},
		{
			name:     "special characters",
			input:    "special$#@char",
			expected: "SPECIAL_CHAR",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "single lowercase char",
			input:    "a",
			expected: "A",
		},
		{
			name:     "single uppercase char",
			input:    "A",
			expected: "A",
		},
		{
			name:     "single digit char",
			input:    "1",
			expected: "1",
		},
		{
			name:     "single non-alphanumeric char",
			input:    "-",
			expected: "_",
		},
		{
			name:     "leading and trailing spaces",
			input:    "  leading_and_trailing  ",
			expected: "_LEADING_AND_TRAILING_",
		},
		{
			name:     "only non-alphanumeric chars",
			input:    "---",
			expected: "_",
		},
		{
			name:     "mixed spaces dots dashes and numbers",
			input:    "foo.bar-baz_qux 123",
			expected: "FOO_BAR_BAZ_QUX_123",
		},
		{
			name:     "uppercase with digits and single underscores",
			input:    "FOO_BAR_123",
			expected: "FOO_BAR_123",
		},
		{
			name:     "leading special characters",
			input:    "!key",
			expected: "_KEY",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := FormatKey(tc.input)
			if got != tc.expected {
				t.Errorf("FormatKey(%q) = %q; expected %q", tc.input, got, tc.expected)
			}
		})
	}
}

func TestFormatKeyWithFormat(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		format   string
		expected string
	}{
		// none format (passes through original key)
		{"none_empty", "", KeyFormatNone, ""},
		{"none_kebab", "api-key", KeyFormatNone, "api-key"},
		{"none_dot", "database.user_name", KeyFormatNone, "database.user_name"},
		{"none_mixed_case", "UserName", KeyFormatNone, "UserName"},
		{"none_underscores", "multiple___underscores", KeyFormatNone, "multiple___underscores"},
		{"none_leading_special", "__foo", KeyFormatNone, "__foo"},
		{"none_blank_string", "any-key", "", "any-key"},

		// env format (uppercase with underscores)
		{"env_kebab", "api-key", KeyFormatEnv, "API_KEY"},
		{"env_dot", "database.user_name", KeyFormatEnv, "DATABASE_USER_NAME"},
		{"env_mixed_case", "UserName", KeyFormatEnv, "USERNAME"},
		{"env_consecutive_hyphens", "my--key", KeyFormatEnv, "MY_KEY"},
		{"env_leading_trailing", "  foo_bar  ", KeyFormatEnv, "_FOO_BAR_"},
		{"env_already_env", "API_KEY", KeyFormatEnv, "API_KEY"},

		// snake format (lowercase with underscores)
		{"snake_kebab", "api-key", KeyFormatSnake, "api_key"},
		{"snake_dot", "database.user_name", KeyFormatSnake, "database_user_name"},
		{"snake_mixed_case", "UserName", KeyFormatSnake, "username"},
		{"snake_pascal_case_multi", "already_Format_Key", KeyFormatSnake, "already_format_key"},
		{"snake_consecutive_hyphens", "my--key", KeyFormatSnake, "my_key"},
		{"snake_leading_underscores", "__foo", KeyFormatSnake, "_foo"},
		{"snake_trailing_underscores", "foo__", KeyFormatSnake, "foo_"},
		{"snake_uppercase_input", "API_KEY_123", KeyFormatSnake, "api_key_123"},
		{"snake_special_chars", "special$#@char", KeyFormatSnake, "special_char"},

		// kebab format (lowercase with hyphens)
		{"kebab_kebab", "api-key", KeyFormatKebab, "api-key"},
		{"kebab_dot", "database.user_name", KeyFormatKebab, "database-user-name"},
		{"kebab_mixed_case", "UserName", KeyFormatKebab, "username"},
		{"kebab_pascal_case_multi", "already_Format_Key", KeyFormatKebab, "already-format-key"},
		{"kebab_consecutive_hyphens", "my--key", KeyFormatKebab, "my-key"},
		{"kebab_leading_underscores", "__foo", KeyFormatKebab, "-foo"},
		{"kebab_trailing_underscores", "foo__", KeyFormatKebab, "foo-"},
		{"kebab_uppercase_input", "API_KEY_123", KeyFormatKebab, "api-key-123"},
		{"kebab_special_chars", "special$#@char", KeyFormatKebab, "special-char"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := FormatKeyWithFormat(tc.input, tc.format)
			if got != tc.expected {
				t.Errorf("FormatKeyWithFormat(%q, %q) = %q; expected %q", tc.input, tc.format, got, tc.expected)
			}
		})
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
