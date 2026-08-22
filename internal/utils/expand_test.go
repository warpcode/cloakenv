package utils_test

import (
	"errors"
	"testing"

	"github.com/warpcode/cloakenv/internal/utils"
)

func TestExpandString(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		configKey string
		resolve   func(string) (string, error)
		want      string
		wantErr   bool
	}{
		{
			name:  "no expansion",
			input: "plain string",
			want:  "plain string",
		},
		{
			name:  "simple expansion",
			input: "hello ${world}",
			resolve: func(u string) (string, error) {
				if u == "world" {
					return "earth", nil
				}
				return "", errors.New("not found")
			},
			want: "hello earth",
		},
		{
			name:  "escaped dollar",
			input: "cost is $$10",
			want:  "cost is $10",
		},
		{
			name:    "unclosed brace",
			input:   "hello ${world",
			wantErr: true,
		},
		{
			name:    "nested brace",
			input:   "hello ${outer${inner}}",
			wantErr: true,
		},
		{
			name:  "resolve error",
			input: "hello ${err}",
			resolve: func(u string) (string, error) {
				return "", errors.New("fail")
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := utils.ExpandString(tt.input, tt.configKey, tt.resolve)
			if (err != nil) != tt.wantErr {
				t.Errorf("ExpandString() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("ExpandString() = %v, want %v", got, tt.want)
			}
		})
	}
}
