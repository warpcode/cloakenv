package utils

import (
	"bytes"
	"testing"
)

func TestZeroBytes(t *testing.T) {
	tests := []struct {
		name  string
		input []byte
	}{
		{
			name:  "nil slice",
			input: nil,
		},
		{
			name:  "empty slice",
			input: []byte{},
		},
		{
			name:  "populated slice",
			input: []byte("secret_password_123"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := make([]byte, len(tt.input))
			copy(b, tt.input)

			ZeroBytes(b)

			expected := make([]byte, len(tt.input))
			if !bytes.Equal(b, expected) {
				t.Errorf("ZeroBytes() failed, got %v, want %v", b, expected)
			}
		})
	}
}
