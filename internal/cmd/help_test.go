package cmd

import (
	"strings"
	"testing"
)

func TestHelpOutputs(t *testing.T) {
	tests := []struct {
		name     string
		printFn  func()
		checkErr bool
		contains string
	}{
		{"PrintUsage", PrintUsage, true, "cloakenv — pluggable secret orchestrator"},
		{"PrintUsage_Inspect", PrintUsage, true, "inspect     Preview matched vaults, envs, templates, and autoload rules"},
		{"PrintUsage_Internal", PrintUsage, true, "internal    Developer & hook helpers (subcommands: match-alias, list-vaults)"},
		{"PrintUsage_Doctor", PrintUsage, true, "doctor      Diagnose environment, validate config, and check vault reachability"},
		{"PrintUsage_Completion", PrintUsage, true, "completion  Generate shell autocompletion script"},
		{"PrintUsageStdout", PrintUsageStdout, false, "cloakenv — pluggable secret orchestrator"},
		{"PrintUsageStdout_Inspect", PrintUsageStdout, false, "inspect     Preview matched vaults, envs, templates, and autoload rules"},
		{"PrintUsageStdout_Internal", PrintUsageStdout, false, "internal    Developer & hook helpers (subcommands: match-alias, list-vaults)"},
		{"PrintUsageStdout_Doctor", PrintUsageStdout, false, "doctor      Diagnose environment, validate config, and check vault reachability"},
		{"PrintUsageStdout_Completion", PrintUsageStdout, false, "completion  Generate shell autocompletion script"},
		{"PrintDoctorHelp", PrintDoctorHelp, false, "cloakenv doctor [--strict]"},
		{"PrintRunHelp", PrintRunHelp, false, "cloakenv run"},
		{"PrintInspectHelp", PrintInspectHelp, false, "cloakenv inspect"},
		{"PrintCacheHelp", PrintCacheHelp, false, "cloakenv cache"},
		{"PrintCacheClearHelp", PrintCacheClearHelp, false, "cloakenv cache clear"},
		{"PrintCachePruneHelp", PrintCachePruneHelp, false, "cloakenv cache prune"},
		{"PrintAuthHelp", PrintAuthHelp, false, "cloakenv auth"},
		{"PrintAuthLoginHelp", PrintAuthLoginHelp, false, "cloakenv auth login"},
		{"PrintAuthForgetHelp", PrintAuthForgetHelp, false, "cloakenv auth forget"},
		{"PrintAuthStatusHelp", PrintAuthStatusHelp, false, "cloakenv auth status"},
		{"PrintInternalHelp", PrintInternalHelp, false, "cloakenv internal"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout, stderr := captureOutput(t, tt.printFn)
			output := stdout
			if tt.checkErr {
				output = stderr
			}
			if output == "" {
				t.Errorf("%s() produced no output", tt.name)
			}
			if !strings.Contains(output, tt.contains) {
				t.Errorf("%s() output did not contain %q\nOutput:\n%s", tt.name, tt.contains, output)
			}
		})
	}

	t.Run("PrintUsage_OmitsRemovedCommands", func(t *testing.T) {
		stdout, _ := captureOutput(t, PrintUsageStdout)
		for _, removed := range []string{"cloakenv [-c config_path] get", "cloakenv [-c config_path] show", "cloakenv [-c config_path] search", "cloakenv [-c config_path] set", "cloakenv [-c config_path] delete", "cloakenv [-c config_path] sync"} {
			if strings.Contains(stdout, removed) {
				t.Errorf("PrintUsageStdout() should not mention removed command %q:\n%s", removed, stdout)
			}
		}
	})

	t.Run("PrintRunHelp_OmitDeprecatedAndRemovedFlags", func(t *testing.T) {
		stdout, _ := captureOutput(t, PrintRunHelp)
		for _, omitted := range []string{"--skip-autoload", "-e KEY=uri", "-t template", "-m entry-uri", "-i KEY", "--key-format"} {
			if strings.Contains(stdout, omitted) {
				t.Errorf("PrintRunHelp() should not contain %q, got:\n%s", omitted, stdout)
			}
		}
		if !strings.Contains(stdout, "--no-autoload") {
			t.Errorf("PrintRunHelp() output should contain primary flag --no-autoload, got:\n%s", stdout)
		}
	})
}
