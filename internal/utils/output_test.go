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
