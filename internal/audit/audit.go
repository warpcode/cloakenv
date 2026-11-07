package audit

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Event is a single structured audit record for a secret access attempt.
type Event struct {
	Timestamp  string `json:"timestamp"`
	URI        string `json:"uri"`
	Target     string `json:"target,omitempty"`
	Caller     string `json:"caller"`
	PID        int    `json:"pid"`
	UID        int    `json:"uid,omitempty"`
	Subcommand string `json:"subcommand,omitempty"`
	Outcome    string `json:"outcome"`
}

// Config contains audit logging configuration.
type Config struct {
	Enabled bool
	LogPath string
	Format  string
}

// Logger writes JSON-lines access events to a dedicated file.
type Logger struct {
	mu   sync.Mutex
	file *os.File
	cfg  Config
}

func defaultLogPath() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Join(os.TempDir(), "cloakenv-audit.jsonl")
	}
	return filepath.Join(home, ".config", "cloakenv", "audit.jsonl")
}

func normalizeFormat(format string) string {
	switch strings.ToLower(format) {
	case "", "jsonl":
		return "jsonl"
	case "json":
		return "json"
	default:
		return ""
	}
}

func currentBinary() string {
	if len(os.Args) > 0 && os.Args[0] != "" {
		return filepath.Base(os.Args[0])
	}
	if exe, err := os.Executable(); err == nil {
		return filepath.Base(exe)
	}
	return "unknown"
}

func currentSubcommand() string {
	for _, arg := range os.Args[1:] {
		if strings.TrimSpace(arg) == "" || strings.HasPrefix(arg, "-") {
			continue
		}
		return arg
	}
	return "unknown"
}

// NewLogger creates a new audit logger for the provided config.
func NewLogger(cfg Config) (*Logger, error) {
	if !cfg.Enabled {
		return &Logger{cfg: cfg}, nil
	}
	format := normalizeFormat(cfg.Format)
	if format == "" {
		return nil, fmt.Errorf("invalid audit format %q (must be json or jsonl)", cfg.Format)
	}
	logPath := cfg.LogPath
	if logPath == "" {
		logPath = defaultLogPath()
	}
	logDir := filepath.Dir(logPath)
	if logDir != "" && logDir != "." {
		if err := os.MkdirAll(logDir, 0o700); err != nil {
			return nil, fmt.Errorf("failed to create audit log directory %q: %w", logDir, err)
		}
	}
	//nolint:gosec // operator-configured log path must be writable for compliance logging
	file, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, fmt.Errorf("failed to open audit log %q: %w", logPath, err)
	}
	if err := os.Chmod(logPath, 0o600); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("failed to set audit log permissions on %q: %w", logPath, err)
	}
	return &Logger{file: file, cfg: Config{Enabled: true, LogPath: logPath, Format: format}}, nil
}

func (l *Logger) Enabled() bool {
	return l != nil && l.file != nil
}

// Log writes a single JSON-lines audit event without logging any secret payloads.
func (l *Logger) Log(uri, outcome string) error {
	if l == nil || l.file == nil {
		return nil
	}
	outcome = strings.ToUpper(strings.TrimSpace(outcome))
	switch outcome {
	case "SUCCESS", "DENIED", "ERROR":
		// valid
	default:
		outcome = "ERROR"
	}
	event := Event{
		Timestamp:  time.Now().UTC().Format(time.RFC3339Nano),
		URI:        uri,
		Target:     uri,
		Caller:     currentBinary(),
		PID:        os.Getpid(),
		UID:        currentUID(),
		Subcommand: currentSubcommand(),
		Outcome:    outcome,
	}
	payload, err := json.Marshal(event)
	if err != nil {
		return err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, err := l.file.Write(append(payload, '\n')); err != nil {
		return err
	}
	return l.file.Sync()
}

// Close flushes and closes the underlying log file.
func (l *Logger) Close() error {
	if l == nil || l.file == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	file := l.file
	l.file = nil
	return file.Close()
}
