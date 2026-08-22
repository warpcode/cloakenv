package utils

import "testing"

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
