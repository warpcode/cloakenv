package main

import (
	"fmt"
	"io"
	"os"

	"github.com/warpcode/cloakenv/internal/cmd"
	"github.com/warpcode/cloakenv/internal/config"
)

func main() {
	os.Exit(run(os.Args, os.Stderr))
}

// run parses arguments and configuration, executes the requested
// subcommand, and returns its exit code.
func run(args []string, stderr io.Writer) int {
	if len(args) == 0 {
		cmd.PrintUsage()
		return 1
	}

	var customConfigPath string
	parsedArgs := []string{args[0]}
	i := 1
	for i < len(args) {
		if args[i] == "--" {
			parsedArgs = append(parsedArgs, args[i:]...)
			break
		}
		if args[i] == "-c" {
			if i+1 >= len(args) {
				fmt.Fprintln(stderr, "Error: -c flag requires a config path argument")
				return 1
			}
			customConfigPath = args[i+1]
			i += 2
			continue
		}
		parsedArgs = append(parsedArgs, args[i])
		i++
	}

	if len(parsedArgs) < 2 {
		cmd.PrintUsage()
		return 1
	}

	if parsedArgs[1] == "--help" || parsedArgs[1] == "-h" {
		cmd.PrintUsageStdout()
		return 0
	}

	cfg, err := loadConfig(customConfigPath)
	if err != nil {
		fmt.Fprintf(stderr, "Config error: %v\n", err)
		return 1
	}

	// Router logic dispatches to internal/cmd subcommands
	switch parsedArgs[1] {
	case "run":
		return cmd.Run(parsedArgs[2:], cfg)
	case "get":
		return cmd.Get(parsedArgs[2:], cfg)
	case "set":
		return cmd.Set(parsedArgs[2:], cfg)
	case "delete":
		return cmd.Delete(parsedArgs[2:], cfg)
	case "cache":
		return cmd.Cache(parsedArgs[2:], cfg)
	case "show":
		return cmd.Show(parsedArgs[2:], cfg)
	case "search":
		return cmd.Search(parsedArgs[2:], cfg)
	case "auth":
		return cmd.Auth(parsedArgs[2:], cfg)
	case "internal":
		return cmd.Internal(parsedArgs[2:], cfg)
	default:
		fmt.Fprintf(stderr, "Unknown command: %s\n", parsedArgs[1])
		cmd.PrintUsage()
		return 1
	}
}

func loadConfig(configPath string) (*config.Config, error) {
	path := configPath
	if path == "" {
		var err error
		path, err = config.DefaultConfigPath()
		if err != nil {
			return nil, err
		}
	}
	return config.Load(path)
}
