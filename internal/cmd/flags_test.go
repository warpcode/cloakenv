package cmd

import (
	"testing"
)

func TestFlagParser_Bool(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		want    bool
		wantErr bool
	}{
		{
			name: "flag present",
			args: []string{"--bool-flag"},
			want: true,
		},
		{
			name: "short flag present",
			args: []string{"-b"},
			want: true,
		},
		{
			name: "flag missing",
			args: []string{"--other-flag"},
			want: false,
		},
		{
			name: "flag present multiple times",
			args: []string{"--bool-flag", "-b"},
			want: true,
		},
		{
			name: "no arguments",
			args: []string{},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fp := NewFlagParser()
			fp.UnknownFlagErr = func(flag string) error { return nil } // Ignore other flags

			var target bool
			fp.Bool([]string{"--bool-flag", "-b"}, &target)

			_, err := fp.Parse(tt.args)
			if (err != nil) != tt.wantErr {
				t.Errorf("FlagParser.Parse() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if target != tt.want {
				t.Errorf("FlagParser.Bool() target = %v, want %v", target, tt.want)
			}
		})
	}
}

func TestFlagParser_StringSlice(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		want    []string
		wantErr bool
	}{
		{
			name: "flag present",
			args: []string{"--slice-flag", "value1"},
			want: []string{"value1"},
		},
		{
			name: "flag missing",
			args: []string{"--other-flag"},
			want: nil,
		},
		{
			name: "flag present multiple times",
			args: []string{"--slice-flag", "value1", "-s", "value2"},
			want: []string{"value1", "value2"},
		},
		{
			name: "missing value",
			args: []string{"--slice-flag"},
			want: nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fp := NewFlagParser()
			fp.UnknownFlagErr = func(flag string) error { return nil } // Ignore other flags

			var target []string
			fp.StringSlice([]string{"--slice-flag", "-s"}, &target, "missing value err")

			_, err := fp.Parse(tt.args)
			if (err != nil) != tt.wantErr {
				t.Errorf("FlagParser.Parse() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr {
				return
			}

			if len(target) != len(tt.want) {
				t.Errorf("FlagParser.StringSlice() len(target) = %v, want %v", len(target), len(tt.want))
			} else {
				for i := range target {
					if target[i] != tt.want[i] {
						t.Errorf("FlagParser.StringSlice() target[%d] = %v, want %v", i, target[i], tt.want[i])
					}
				}
			}
		})
	}
}

func TestFlagParser_Var(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		want    string
		wantErr bool
	}{
		{
			name: "flag present",
			args: []string{"--var-flag", "value1"},
			want: "value1",
		},
		{
			name: "flag missing",
			args: []string{"--other-flag"},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fp := NewFlagParser()
			fp.UnknownFlagErr = func(flag string) error { return nil } // Ignore other flags

			var target string
			fp.Var([]string{"--var-flag"}, true, "missing value err", func(name, val string) error {
				target = val
				return nil
			})

			_, err := fp.Parse(tt.args)
			if (err != nil) != tt.wantErr {
				t.Errorf("FlagParser.Parse() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if target != tt.want {
				t.Errorf("FlagParser.Var() target = %v, want %v", target, tt.want)
			}
		})
	}
}
