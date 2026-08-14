package cmd

import (
	"strings"
	"testing"
)

func TestHelpOutputs(t *testing.T) {
	tests := []struct {
		name     string
		printFn  func()
		checkErr bool // if true, check stderr, otherwise check stdout
		contains string
	}{
		{"PrintUsage", PrintUsage, true, "cloakenv — pluggable secret orchestrator"},
		{"PrintUsageStdout", PrintUsageStdout, false, "cloakenv — pluggable secret orchestrator"},
		{"PrintRunHelp", PrintRunHelp, false, "cloakenv run"},
		{"PrintGetHelp", PrintGetHelp, false, "cloakenv get"},
		{"PrintSetHelp", PrintSetHelp, false, "cloakenv set"},
		{"PrintDeleteHelp", PrintDeleteHelp, false, "cloakenv delete"},
		{"PrintCacheHelp", PrintCacheHelp, false, "cloakenv cache"},
		{"PrintCacheClearHelp", PrintCacheClearHelp, false, "cloakenv cache clear"},
		{"PrintShowHelp", PrintShowHelp, false, "cloakenv show"},
		{"PrintSearchHelp", PrintSearchHelp, false, "cloakenv search"},
		{"PrintAuthHelp", PrintAuthHelp, false, "cloakenv auth"},
		{"PrintAuthLoginHelp", PrintAuthLoginHelp, false, "cloakenv auth login"},
		{"PrintAuthForgetHelp", PrintAuthForgetHelp, false, "cloakenv auth forget"},
		{"PrintAuthStatusHelp", PrintAuthStatusHelp, false, "cloakenv auth status"},
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
}
