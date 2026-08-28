package provider

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

func TestResolveDotPath(t *testing.T) {
	tests := []struct {
		name       string
		val        any
		path       string
		want       any
		wantErr    bool
		wantErrMsg string
	}{
		{
			name: "empty path",
			val:  map[string]any{"a": 1},
			path: "",
			want: map[string]any{"a": 1},
		},
		{
			name: "path with empty parts ignored",
			val:  map[string]any{"a": map[string]any{"b": 2}},
			path: ".a..b.",
			want: 2,
		},
		{
			name: "valid path map[string]any",
			val:  map[string]any{"a": map[string]any{"b": 3}},
			path: "a.b",
			want: 3,
		},
		{
			name: "valid path map[any]any",
			val:  map[any]any{"a": map[any]any{"b": 4}},
			path: "a.b",
			want: 4,
		},
		{
			name: "valid path map[any]any with non-string keys",
			val:  map[any]any{1: map[any]any{2: 4}},
			path: "1.2",
			want: 4,
		},
		{
			name: "valid path []any",
			val:  []any{10, 20, []any{30, 40}},
			path: "2.1",
			want: 40,
		},
		{
			name:       "invalid key map[string]any",
			val:        map[string]any{"a": 1},
			path:       "b",
			wantErr:    true,
			wantErrMsg: `key "b" not found`,
		},
		{
			name:       "invalid key map[any]any",
			val:        map[any]any{"a": 1},
			path:       "b",
			wantErr:    true,
			wantErrMsg: `key "b" not found`,
		},
		{
			name:       "invalid array index non-integer",
			val:        []any{1, 2},
			path:       "foo",
			wantErr:    true,
			wantErrMsg: `cannot index array with non-integer "foo"`,
		},
		{
			name:       "invalid array index out of bounds",
			val:        []any{1, 2},
			path:       "2",
			wantErr:    true,
			wantErrMsg: "index 2 out of bounds",
		},
		{
			name:       "invalid array index negative",
			val:        []any{1, 2},
			path:       "-1",
			wantErr:    true,
			wantErrMsg: "index -1 out of bounds",
		},
		{
			name:       "traverse unsupported type",
			val:        map[string]any{"a": "string"},
			path:       "a.b",
			wantErr:    true,
			wantErrMsg: `cannot traverse key "b" on value of type string`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := resolveDotPath(tt.val, tt.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("resolveDotPath() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && err != nil && tt.wantErrMsg != "" && !strings.Contains(err.Error(), tt.wantErrMsg) {
				t.Errorf("resolveDotPath() error = %v, want error msg to contain %q", err, tt.wantErrMsg)
			}
			if !tt.wantErr && !reflect.DeepEqual(got, tt.want) {
				t.Errorf("resolveDotPath() got = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestStaticProvider_SetSecret(t *testing.T) {
	p := &staticProvider{scheme: "json"}
	err := p.SetSecret(context.Background(), "key", "value")
	if err == nil {
		t.Error("expected error for SetSecret, got nil")
	}
	expectedMsg := "json provider is read-only"
	if err != nil && err.Error() != expectedMsg {
		t.Errorf("expected error %q, got %q", expectedMsg, err.Error())
	}
}

func TestStaticProvider_DeleteSecret(t *testing.T) {
	p := &staticProvider{scheme: "yaml"}
	err := p.DeleteSecret(context.Background(), "key")
	if err == nil {
		t.Error("expected error for DeleteSecret, got nil")
	}
	expectedMsg := "yaml provider is read-only"
	if err != nil && err.Error() != expectedMsg {
		t.Errorf("expected error %q, got %q", expectedMsg, err.Error())
	}
}
