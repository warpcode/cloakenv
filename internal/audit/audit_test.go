package audit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestLoggerWritesJSONL(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "audit.jsonl")
	logger, err := NewLogger(Config{Enabled: true, LogPath: logPath, Format: "jsonl"})
	if err != nil {
		t.Fatalf("NewLogger failed: %v", err)
	}
	defer func() { _ = logger.Close() }()

	if err := logger.Log("keyring://prod/api_token", "SUCCESS"); err != nil {
		t.Fatalf("Log failed: %v", err)
	}

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected 1 log line, got %d: %q", len(lines), string(data))
	}

	var event Event
	if err := json.Unmarshal([]byte(lines[0]), &event); err != nil {
		t.Fatalf("invalid JSON log line: %v", err)
	}
	if event.URI != "keyring://prod/api_token" {
		t.Fatalf("unexpected uri: %q", event.URI)
	}
	if event.Outcome != "SUCCESS" {
		t.Fatalf("unexpected outcome: %q", event.Outcome)
	}
	if event.Target == "" || event.Target != event.URI {
		t.Fatal("expected target to match uri")
	}
	if event.Caller == "" {
		t.Fatal("expected caller binary name")
	}
	if event.PID == 0 {
		t.Fatal("expected pid to be set")
	}
	if event.Subcommand == "" {
		t.Fatal("expected subcommand to be present")
	}
	if runtime.GOOS != "windows" {
		if info, statErr := os.Stat(logPath); statErr != nil {
			t.Fatalf("Stat failed: %v", statErr)
		} else if info.Mode().Perm() != 0o600 {
			t.Fatalf("expected file mode 0600, got %v", info.Mode().Perm())
		}
	}
}
