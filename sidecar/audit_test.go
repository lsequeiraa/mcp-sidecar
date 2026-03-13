package sidecar

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAuditLogger_Nil(t *testing.T) {
	var l *AuditLogger

	// All methods should be safe to call on nil.
	l.LogStart("id", "cmd", "/tmp")
	l.LogStop("id", 0, time.Second)
	l.LogBlocked("cmd", "reason")
	if err := l.Close(); err != nil {
		t.Errorf("Close on nil logger: %v", err)
	}
}

func TestAuditLogger_Disabled(t *testing.T) {
	l, err := NewAuditLogger("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if l != nil {
		t.Error("expected nil logger for empty path")
	}
}

func TestAuditLogger_LogStart(t *testing.T) {
	var buf bytes.Buffer
	l := newAuditLoggerWriter(&buf)

	l.LogStart("sc-abc123", "dotnet run", "/app")

	var entry auditEntry
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("invalid JSON: %v\nraw: %s", err, buf.String())
	}

	if entry.Event != "start" {
		t.Errorf("event = %q, want 'start'", entry.Event)
	}
	if entry.ID != "sc-abc123" {
		t.Errorf("id = %q, want 'sc-abc123'", entry.ID)
	}
	if entry.Command != "dotnet run" {
		t.Errorf("command = %q, want 'dotnet run'", entry.Command)
	}
	if entry.Cwd != "/app" {
		t.Errorf("cwd = %q, want '/app'", entry.Cwd)
	}
	if entry.Timestamp == "" {
		t.Error("timestamp is empty")
	}
}

func TestAuditLogger_LogStop(t *testing.T) {
	var buf bytes.Buffer
	l := newAuditLoggerWriter(&buf)

	l.LogStop("sc-abc123", 42, 3*time.Second)

	var entry auditEntry
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if entry.Event != "stop" {
		t.Errorf("event = %q, want 'stop'", entry.Event)
	}
	if entry.ExitCode == nil || *entry.ExitCode != 42 {
		t.Errorf("exit_code = %v, want 42", entry.ExitCode)
	}
	if entry.Duration != "3s" {
		t.Errorf("duration = %q, want '3s'", entry.Duration)
	}
}

func TestAuditLogger_LogBlocked(t *testing.T) {
	var buf bytes.Buffer
	l := newAuditLoggerWriter(&buf)

	l.LogBlocked("rm -rf /", "executable \"rm\" not in allowed list")

	var entry auditEntry
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if entry.Event != "blocked" {
		t.Errorf("event = %q, want 'blocked'", entry.Event)
	}
	if entry.Command != "rm -rf /" {
		t.Errorf("command = %q, want 'rm -rf /'", entry.Command)
	}
	if !strings.Contains(entry.Reason, "not in allowed list") {
		t.Errorf("reason = %q, want it to contain 'not in allowed list'", entry.Reason)
	}
}

func TestAuditLogger_JSONL(t *testing.T) {
	var buf bytes.Buffer
	l := newAuditLoggerWriter(&buf)

	l.LogStart("sc-1", "cmd1", "/a")
	l.LogStart("sc-2", "cmd2", "/b")
	l.LogBlocked("bad", "blocked")

	// Each line should be valid JSON.
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d", len(lines))
	}
	for i, line := range lines {
		var entry auditEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Errorf("line %d invalid JSON: %v\nraw: %s", i, err, line)
		}
	}
}

func TestAuditLogger_FileOutput(t *testing.T) {
	dir := t.TempDir()

	l, err := NewAuditLogger(dir)
	if err != nil {
		t.Fatalf("NewAuditLogger: %v", err)
	}
	defer l.Close()

	l.LogStart("sc-file", "echo test", "/tmp")
	l.LogStop("sc-file", 0, time.Second)

	// Close to flush.
	l.Close()

	expectedPath := filepath.Join(dir, "sidecar-audit.jsonl")
	data, err := os.ReadFile(expectedPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines in file, got %d", len(lines))
	}
}

func TestAuditLogger_StopExitCodeZero(t *testing.T) {
	var buf bytes.Buffer
	l := newAuditLoggerWriter(&buf)

	// exit_code 0 should still appear in the JSON (not omitted).
	l.LogStop("sc-zero", 0, time.Second)

	var entry auditEntry
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if entry.ExitCode == nil {
		t.Error("exit_code should not be omitted for 0")
	}
	if *entry.ExitCode != 0 {
		t.Errorf("exit_code = %d, want 0", *entry.ExitCode)
	}
}

func TestAuditLogger_TrueValue(t *testing.T) {
	// "true" should write to ./sidecar-audit.jsonl in the cwd.
	// We can't easily change cwd in tests, so just verify it creates
	// a non-nil logger and the file exists. Clean up afterward.
	l, err := NewAuditLogger("true")
	if err != nil {
		t.Fatalf("NewAuditLogger(\"true\"): %v", err)
	}
	if l == nil {
		t.Fatal("expected non-nil logger for \"true\"")
	}

	l.LogStart("sc-true", "echo test", ".")
	l.Close()

	expectedPath := filepath.Join(".", "sidecar-audit.jsonl")
	t.Cleanup(func() { os.Remove(expectedPath) })

	if _, err := os.Stat(expectedPath); os.IsNotExist(err) {
		t.Errorf("expected file %q to exist", expectedPath)
	}
}

func TestAuditLogger_TempValue(t *testing.T) {
	l, err := NewAuditLogger("temp")
	if err != nil {
		t.Fatalf("NewAuditLogger(\"temp\"): %v", err)
	}
	if l == nil {
		t.Fatal("expected non-nil logger for \"temp\"")
	}

	l.LogStart("sc-temp", "echo test", ".")
	l.Close()

	expectedPath := filepath.Join(os.TempDir(), "sidecar-audit.jsonl")
	t.Cleanup(func() { os.Remove(expectedPath) })

	data, err := os.ReadFile(expectedPath)
	if err != nil {
		t.Fatalf("ReadFile(%q): %v", expectedPath, err)
	}

	var entry auditEntry
	if err := json.Unmarshal(bytes.TrimSpace(data), &entry); err != nil {
		t.Fatalf("invalid JSON: %v\nraw: %s", err, data)
	}
	if entry.Event != "start" {
		t.Errorf("event = %q, want 'start'", entry.Event)
	}
}

func TestAuditLogger_CreatesDirectory(t *testing.T) {
	// Pass a non-existent subdirectory; NewAuditLogger should create it.
	dir := filepath.Join(t.TempDir(), "nonexistent", "sub")

	l, err := NewAuditLogger(dir)
	if err != nil {
		t.Fatalf("NewAuditLogger(%q): %v", dir, err)
	}
	defer l.Close()

	l.LogBlocked("bad cmd", "test reason")
	l.Close()

	expectedPath := filepath.Join(dir, "sidecar-audit.jsonl")
	data, err := os.ReadFile(expectedPath)
	if err != nil {
		t.Fatalf("ReadFile(%q): %v", expectedPath, err)
	}

	var entry auditEntry
	if err := json.Unmarshal(bytes.TrimSpace(data), &entry); err != nil {
		t.Fatalf("invalid JSON: %v\nraw: %s", err, data)
	}
	if entry.Event != "blocked" {
		t.Errorf("event = %q, want 'blocked'", entry.Event)
	}
}
