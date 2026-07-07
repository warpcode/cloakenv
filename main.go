package main

import (
	"fmt"
	"os"

	"github.com/warpcode/cloakenv/internal/cmd"
	"github.com/warpcode/cloakenv/internal/config"
)

var customConfigPath string

func main() {
	// Parse global flags first (e.g. -c <config_path>)
	args := os.Args
	var parsedArgs []string
	parsedArgs = append(parsedArgs, args[0])

	i := 1
	for i < len(args) {
		if args[i] == "--" {
			parsedArgs = append(parsedArgs, args[i:]...)
			break
		}
		if args[i] == "-c" {
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, "Error: -c flag requires a config path argument")
				os.Exit(1)
			}
			customConfigPath = args[i+1]
			i += 2
			continue
		}
		parsedArgs = append(parsedArgs, args[i])
		i++
	}
	os.Args = parsedArgs

	if len(os.Args) < 2 {
		cmd.PrintUsage()
		os.Exit(1)
	}

	if os.Args[1] == "--help" || os.Args[1] == "-h" {
		cmd.PrintUsageStdout()
		os.Exit(0)
	}

	cfg, err := loadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Config error: %v\n", err)
		os.Exit(1)
	}

	// Router logic dispatches to internal/cmd subcommands
	switch os.Args[1] {
	case "run":
		os.Exit(cmd.Run(os.Args[2:], cfg))
	case "get":
		os.Exit(cmd.Get(os.Args[2:], cfg))
	case "set":
		os.Exit(cmd.Set(os.Args[2:], cfg))
	case "delete":
		os.Exit(cmd.Delete(os.Args[2:], cfg))
	case "cache":
		os.Exit(cmd.Cache(os.Args[2:], cfg))
	case "show":
		os.Exit(cmd.Show(os.Args[2:], cfg))
	case "search":
		os.Exit(cmd.Search(os.Args[2:], cfg))
	case "auth":
		os.Exit(cmd.Auth(os.Args[2:], cfg))
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", os.Args[1])
		cmd.PrintUsage()
		os.Exit(1)
	}
}

func loadConfig() (*config.Config, error) {
	path := customConfigPath
	if path == "" {
		var err error
		path, err = config.DefaultConfigPath()
		if err != nil {
			return nil, err
		}
	}
	return config.Load(path)
}
