package sidecar

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const auditFileName = "sidecar-audit.jsonl"

// AuditLogger writes structured JSON-Lines entries for process lifecycle
// events. All methods are safe to call on a nil receiver (no-op).
type AuditLogger struct {
	mu sync.Mutex
	w  io.Writer
	f  *os.File // non-nil when writing to a file (for Close)
}

// NewAuditLogger creates a logger that writes to sidecar-audit.jsonl in
// the directory determined by value:
//
//   - ""      → disabled (returns nil)
//   - "true"  → current working directory
//   - "temp"  → os.TempDir()
//   - other   → treated as a directory path (created if it doesn't exist)
func NewAuditLogger(value string) (*AuditLogger, error) {
	if value == "" {
		return nil, nil
	}

	var dir string
	switch value {
	case "true":
		dir = "."
	case "temp":
		dir = os.TempDir()
	default:
		dir = value
	}

	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}

	f, err := os.OpenFile(filepath.Join(dir, auditFileName), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, err
	}

	return &AuditLogger{w: f, f: f}, nil
}

// newAuditLoggerWriter creates a logger that writes to any io.Writer.
// Used by tests.
func newAuditLoggerWriter(w io.Writer) *AuditLogger {
	return &AuditLogger{w: w}
}

// Close releases resources held by the logger.
func (l *AuditLogger) Close() error {
	if l == nil || l.f == nil {
		return nil
	}
	return l.f.Close()
}

// auditEntry is the JSON structure written for each event.
type auditEntry struct {
	Timestamp string `json:"ts"`
	Event     string `json:"event"`
	ID        string `json:"id,omitempty"`
	Command   string `json:"command,omitempty"`
	Cwd       string `json:"cwd,omitempty"`
	ExitCode  *int   `json:"exit_code,omitempty"`
	Duration  string `json:"duration,omitempty"`
	Reason    string `json:"reason,omitempty"`
}

// LogStart records a process start event.
func (l *AuditLogger) LogStart(id, command, cwd string) {
	l.write(auditEntry{
		Event:   "start",
		ID:      id,
		Command: command,
		Cwd:     cwd,
	})
}

// LogStop records a process stop event.
func (l *AuditLogger) LogStop(id string, exitCode int, duration time.Duration) {
	l.write(auditEntry{
		Event:    "stop",
		ID:       id,
		ExitCode: &exitCode,
		Duration: duration.Truncate(time.Millisecond).String(),
	})
}

// LogBlocked records a command that was rejected by security policy.
func (l *AuditLogger) LogBlocked(command, reason string) {
	l.write(auditEntry{
		Event:   "blocked",
		Command: command,
		Reason:  reason,
	})
}

func (l *AuditLogger) write(e auditEntry) {
	if l == nil {
		return
	}

	e.Timestamp = time.Now().UTC().Format(time.RFC3339)

	l.mu.Lock()
	defer l.mu.Unlock()

	b, err := json.Marshal(e)
	if err != nil {
		return
	}
	b = append(b, '\n')
	_, _ = l.w.Write(b)
}
