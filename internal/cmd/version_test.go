package cmd

import (
	"bytes"
	"strings"
	"testing"
)

func TestVersionCmd(t *testing.T) {
	var buf bytes.Buffer
	exitCode := VersionCmd(nil, &buf)
	if exitCode != 0 {
		t.Fatalf("VersionCmd returned %d, want 0", exitCode)
	}

	out := buf.String()
	if !strings.HasPrefix(out, "cloakenv version ") {
		t.Errorf("expected version output to start with 'cloakenv version ', got: %q", out)
	}
	if !strings.Contains(out, "commit:") {
		t.Errorf("expected commit info, got: %q", out)
	}
	if !strings.Contains(out, "runtime:") {
		t.Errorf("expected runtime info, got: %q", out)
	}
}

func TestGetVersionInfo(t *testing.T) {
	info := GetVersionInfo()
	if info.Version == "" {
		t.Error("expected non-empty version")
	}
	if info.GoVersion == "" {
		t.Error("expected non-empty GoVersion")
	}
	if info.Platform == "" {
		t.Error("expected non-empty Platform")
	}
}
