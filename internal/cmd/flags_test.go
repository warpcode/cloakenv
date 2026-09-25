package cmd

import (
	"errors"
	"reflect"
	"testing"
)

func TestNewFlagParser(t *testing.T) {
	fp := NewFlagParser()
	if fp == nil {
		t.Fatal("NewFlagParser() returned nil")
	}
	if !fp.StopOnDashDash {
		t.Errorf("NewFlagParser().StopOnDashDash = false, want true")
	}
}

func TestFlagParser_Bool(t *testing.T) {
	fp := NewFlagParser()
	var verbose bool
	fp.Bool([]string{"-v", "--verbose"}, &verbose)

	remaining, err := fp.Parse([]string{"-v"})
	if err != nil {
		t.Fatalf("unexpected error parsing -v: %v", err)
	}
	if !verbose {
		t.Errorf("expected verbose to be true after -v, got false")
	}
	if len(remaining) != 0 {
		t.Errorf("expected remaining to be empty, got %v", remaining)
	}

	verbose = false
	remaining, err = fp.Parse([]string{"--verbose"})
	if err != nil {
		t.Fatalf("unexpected error parsing --verbose: %v", err)
	}
	if !verbose {
		t.Errorf("expected verbose to be true after --verbose, got false")
	}
	if len(remaining) != 0 {
		t.Errorf("expected remaining to be empty, got %v", remaining)
	}
}

func TestFlagParser_StringSlice(t *testing.T) {
	t.Run("short flag appends value", func(t *testing.T) {
		fp := NewFlagParser()
		var tags []string
		fp.StringSlice([]string{"-t", "--tag"}, &tags, "tag value missing")

		remaining, err := fp.Parse([]string{"-t", "v1"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		wantTags := []string{"v1"}
		if !reflect.DeepEqual(tags, wantTags) {
			t.Errorf("tags = %v, want %v", tags, wantTags)
		}
		if len(remaining) != 0 {
			t.Errorf("expected remaining to be empty, got %v", remaining)
		}
	})

	t.Run("long flag appends value", func(t *testing.T) {
		fp := NewFlagParser()
		var tags []string
		fp.StringSlice([]string{"-t", "--tag"}, &tags, "tag value missing")

		remaining, err := fp.Parse([]string{"--tag", "v2"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		wantTags := []string{"v2"}
		if !reflect.DeepEqual(tags, wantTags) {
			t.Errorf("tags = %v, want %v", tags, wantTags)
		}
		if len(remaining) != 0 {
			t.Errorf("expected remaining to be empty, got %v", remaining)
		}
	})

	t.Run("appends to existing slice", func(t *testing.T) {
		fp := NewFlagParser()
		tags := []string{"existing"}
		fp.StringSlice([]string{"-t"}, &tags, "")

		remaining, err := fp.Parse([]string{"-t", "new"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		wantTags := []string{"existing", "new"}
		if !reflect.DeepEqual(tags, wantTags) {
			t.Errorf("tags = %v, want %v", tags, wantTags)
		}
		if len(remaining) != 0 {
			t.Errorf("expected remaining to be empty, got %v", remaining)
		}
	})

	t.Run("no flags leaves slice unchanged", func(t *testing.T) {
		fp := NewFlagParser()
		var tags []string
		fp.StringSlice([]string{"-t"}, &tags, "")

		remaining, err := fp.Parse([]string{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if tags != nil {
			t.Errorf("tags = %v, want nil", tags)
		}
		if len(remaining) != 0 {
			t.Errorf("expected remaining to be empty, got %v", remaining)
		}
	})
}

func TestFlagParser_Var(t *testing.T) {
	t.Run("without value", func(t *testing.T) {
		fp := NewFlagParser()
		var count int
		var capturedName string
		fp.Var([]string{"--inc"}, false, "", func(name, val string) error {
			count++
			capturedName = name
			return nil
		})

		remaining, err := fp.Parse([]string{"--inc", "--inc"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if count != 2 {
			t.Errorf("count = %d, want 2", count)
		}
		if capturedName != "--inc" {
			t.Errorf("captured name = %q, want %q", capturedName, "--inc")
		}
		if len(remaining) != 0 {
			t.Errorf("expected remaining to be empty, got %v", remaining)
		}
	})

	t.Run("with value", func(t *testing.T) {
		fp := NewFlagParser()
		var items []string
		fp.Var([]string{"--item"}, true, "missing item", func(name, val string) error {
			items = append(items, val)
			return nil
		})

		remaining, err := fp.Parse([]string{"--item", "apple", "--item", "banana"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		wantItems := []string{"apple", "banana"}
		if !reflect.DeepEqual(items, wantItems) {
			t.Errorf("items = %v, want %v", items, wantItems)
		}
		if len(remaining) != 0 {
			t.Errorf("expected remaining to be empty, got %v", remaining)
		}
	})

	t.Run("with value - missing arg", func(t *testing.T) {
		fp := NewFlagParser()
		fp.Var([]string{"--item"}, true, "missing item", func(name, val string) error {
			return nil
		})

		_, err := fp.Parse([]string{"--item"})
		if err == nil {
			t.Fatalf("expected error, got nil")
		}
		if err.Error() != "missing item" {
			t.Errorf("error = %q, want %q", err.Error(), "missing item")
		}
	})
}

func TestFlagParser_Parse(t *testing.T) {
	tests := []struct {
		name          string
		setup         func() *FlagParser
		args          []string
		wantRemaining []string
		wantErr       string
	}{
		{
			name: "dash dash separator stops parsing when StopOnDashDash is true",
			setup: func() *FlagParser {
				fp := NewFlagParser()
				var verbose bool
				fp.Bool([]string{"-v"}, &verbose)
				return fp
			},
			args:          []string{"-v", "--", "-v", "extra"},
			wantRemaining: []string{"-v", "extra"},
		},
		{
			name: "dash dash separator treated as flag when StopOnDashDash is false",
			setup: func() *FlagParser {
				fp := NewFlagParser()
				fp.StopOnDashDash = false
				return fp
			},
			args:    []string{"--", "foo"},
			wantErr: "unknown flag: --",
		},
		{
			name: "dash dash handled as registered flag when StopOnDashDash is false",
			setup: func() *FlagParser {
				fp := NewFlagParser()
				fp.StopOnDashDash = false
				var dash bool
				fp.Bool([]string{"--"}, &dash)
				return fp
			},
			args:          []string{"--", "foo"},
			wantRemaining: []string{"foo"},
		},
		{
			name: "flag taking value treats dash dash as value before StopOnDashDash check",
			setup: func() *FlagParser {
				fp := NewFlagParser()
				var vals []string
				fp.StringSlice([]string{"--opt"}, &vals, "")
				return fp
			},
			args:          []string{"--opt", "--", "extra"},
			wantRemaining: []string{"extra"},
		},
		{
			name: "single dash is treated as positional argument",
			setup: func() *FlagParser {
				return NewFlagParser()
			},
			args:          []string{"-", "file.txt"},
			wantRemaining: []string{"-", "file.txt"},
		},
		{
			name: "unknown flag default error",
			setup: func() *FlagParser {
				return NewFlagParser()
			},
			args:    []string{"--unknown"},
			wantErr: "unknown flag: --unknown",
		},
		{
			name: "unknown flag custom error handler",
			setup: func() *FlagParser {
				fp := NewFlagParser()
				fp.UnknownFlagErr = func(flag string) error {
					return errors.New("custom unknown: " + flag)
				}
				return fp
			},
			args:    []string{"--foo"},
			wantErr: "custom unknown: --foo",
		},
		{
			name: "unknown flag with StopAtNonFlag returns remaining from flag",
			setup: func() *FlagParser {
				fp := NewFlagParser()
				fp.StopAtNonFlag = true
				return fp
			},
			args:          []string{"--unknown", "rest"},
			wantRemaining: []string{"--unknown", "rest"},
		},
		{
			name: "missing argument default error",
			setup: func() *FlagParser {
				fp := NewFlagParser()
				var vals []string
				fp.StringSlice([]string{"--opt"}, &vals, "")
				return fp
			},
			args:    []string{"--opt"},
			wantErr: "flag --opt requires an argument",
		},
		{
			name: "missing argument custom missingValErr",
			setup: func() *FlagParser {
				fp := NewFlagParser()
				var vals []string
				fp.StringSlice([]string{"--opt"}, &vals, "custom missing msg")
				return fp
			},
			args:    []string{"--opt"},
			wantErr: "custom missing msg",
		},
		{
			name: "handler fn returns error",
			setup: func() *FlagParser {
				fp := NewFlagParser()
				fp.Var([]string{"--fail"}, false, "", func(name, val string) error {
					return errors.New("handler error")
				})
				return fp
			},
			args:    []string{"--fail"},
			wantErr: "handler error",
		},
		{
			name: "non-flag stops parsing when StopAtNonFlag is true",
			setup: func() *FlagParser {
				fp := NewFlagParser()
				fp.StopAtNonFlag = true
				var boolVal bool
				fp.Bool([]string{"-b"}, &boolVal)
				return fp
			},
			args:          []string{"-b", "positional", "-b"},
			wantRemaining: []string{"positional", "-b"},
		},
		{
			name: "positional handler success",
			setup: func() *FlagParser {
				fp := NewFlagParser()
				var positionals []string
				fp.PositionalHandler = func(arg string) error {
					positionals = append(positionals, arg)
					return nil
				}
				return fp
			},
			args:          []string{"arg1", "arg2"},
			wantRemaining: nil,
		},
		{
			name: "positional handler error",
			setup: func() *FlagParser {
				fp := NewFlagParser()
				fp.PositionalHandler = func(arg string) error {
					return errors.New("invalid positional: " + arg)
				}
				return fp
			},
			args:    []string{"bad_arg"},
			wantErr: "invalid positional: bad_arg",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fp := tc.setup()
			remaining, err := fp.Parse(tc.args)

			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tc.wantErr)
				}
				if err.Error() != tc.wantErr {
					t.Errorf("error = %q, want %q", err.Error(), tc.wantErr)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if !reflect.DeepEqual(remaining, tc.wantRemaining) {
				t.Errorf("remaining = %v, want %v", remaining, tc.wantRemaining)
			}
		})
	}
}
