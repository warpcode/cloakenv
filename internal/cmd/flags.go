package cmd

import (
	"fmt"
	"strings"
)

type flagHandler struct {
	names         []string
	takesValue    bool
	missingValErr string
	fn            func(name, val string) error
}

// FlagParser provides standardized CLI flag parsing for cloakenv commands.
type FlagParser struct {
	handlers          []*flagHandler
	UnknownFlagErr    func(flag string) error
	StopAtNonFlag     bool
	StopOnDashDash    bool
	PositionalHandler func(arg string) error
}

// NewFlagParser constructs a new FlagParser configured with default settings.
func NewFlagParser() *FlagParser {
	return &FlagParser{
		StopOnDashDash: true,
	}
}

// Bool registers a boolean flag that sets target to true when present.
func (fp *FlagParser) Bool(names []string, target *bool) {
	fp.handlers = append(fp.handlers, &flagHandler{
		names:      names,
		takesValue: false,
		fn: func(name, val string) error {
			*target = true
			return nil
		},
	})
}

// StringSlice registers a flag that appends its value to target slice.
func (fp *FlagParser) StringSlice(names []string, target *[]string, missingValErr string) {
	fp.handlers = append(fp.handlers, &flagHandler{
		names:         names,
		takesValue:    true,
		missingValErr: missingValErr,
		fn: func(name, val string) error {
			*target = append(*target, val)
			return nil
		},
	})
}

// Var registers a custom flag handler.
func (fp *FlagParser) Var(names []string, takesValue bool, missingValErr string, fn func(name, val string) error) {
	fp.handlers = append(fp.handlers, &flagHandler{
		names:         names,
		takesValue:    takesValue,
		missingValErr: missingValErr,
		fn:            fn,
	})
}

// Parse processes args sequentially according to registered flags and options.
func (fp *FlagParser) Parse(args []string) (remaining []string, err error) {
	i := 0
	for i < len(args) {
		arg := args[i]
		if arg == "--" && fp.StopOnDashDash {
			return args[i+1:], nil
		}

		if strings.HasPrefix(arg, "-") && arg != "-" {
			var matched *flagHandler
			for _, h := range fp.handlers {
				for _, name := range h.names {
					if name == arg {
						matched = h
						break
					}
				}
				if matched != nil {
					break
				}
			}

			if matched == nil {
				if fp.StopAtNonFlag {
					return args[i:], nil
				}
				if fp.UnknownFlagErr != nil {
					return nil, fp.UnknownFlagErr(arg)
				}
				return nil, fmt.Errorf("Unknown flag: %s", arg)
			}

			val := ""
			if matched.takesValue {
				if i+1 >= len(args) {
					if matched.missingValErr != "" {
						return nil, fmt.Errorf("%s", matched.missingValErr)
					}
					return nil, fmt.Errorf("flag %s requires an argument", arg)
				}
				i++
				val = args[i]
			}

			if err := matched.fn(arg, val); err != nil {
				return nil, err
			}
			i++
			continue
		}

		// Non-flag argument
		if fp.StopAtNonFlag {
			return args[i:], nil
		}

		if fp.PositionalHandler != nil {
			if err := fp.PositionalHandler(arg); err != nil {
				return nil, err
			}
		} else {
			remaining = append(remaining, arg)
		}
		i++
	}

	return remaining, nil
}
