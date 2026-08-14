package cmd

import (
	"strings"
	"testing"
)

func TestPrintUsage(t *testing.T) {
	_, errOutput := captureOutput(t, PrintUsage)
	if !strings.Contains(errOutput, "cloakenv — pluggable secret orchestrator") {
		t.Errorf("PrintUsage() missing expected output, got:\n%s", errOutput)
	}
}

func TestPrintHelpFunctions(t *testing.T) {
	tests := []struct {
		name     string
		f        func()
		expected string
	}{
		{"PrintUsageStdout", PrintUsageStdout, "cloakenv — pluggable secret orchestrator"},
		{"PrintRunHelp", PrintRunHelp, "cloakenv run"},
		{"PrintGetHelp", PrintGetHelp, "cloakenv get"},
		{"PrintSetHelp", PrintSetHelp, "cloakenv set"},
		{"PrintDeleteHelp", PrintDeleteHelp, "cloakenv delete"},
		{"PrintCacheHelp", PrintCacheHelp, "cloakenv cache clear"},
		{"PrintCacheClearHelp", PrintCacheClearHelp, "cloakenv cache clear"},
		{"PrintShowHelp", PrintShowHelp, "cloakenv show"},
		{"PrintSearchHelp", PrintSearchHelp, "cloakenv search"},
		{"PrintAuthHelp", PrintAuthHelp, "cloakenv auth"},
		{"PrintAuthLoginHelp", PrintAuthLoginHelp, "cloakenv auth login"},
		{"PrintAuthForgetHelp", PrintAuthForgetHelp, "cloakenv auth forget"},
		{"PrintAuthStatusHelp", PrintAuthStatusHelp, "cloakenv auth status"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			outOutput, _ := captureOutput(t, tt.f)
			if !strings.Contains(outOutput, tt.expected) {
				t.Errorf("%s() missing expected output %q, got:\n%s", tt.name, tt.expected, outOutput)
			}
		})
	}
}
