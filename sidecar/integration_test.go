package sidecar

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// testConfig returns a Config suitable for integration tests.
func testConfig() *Config {
	return &Config{
		MaxProcesses:  5,
		BufferSize:    1024 * 1024, // 1 MB
		KillTimeout:   5 * time.Second,
		CleanupAfter:  0, // disabled in tests
		MaxOutputSize: 0, // unlimited in tests
	}
}

// longRunningCommand returns a platform-appropriate command that runs for a
// long time (used for tests that need to stop a running process).
func longRunningCommand() string {
	if runtime.GOOS == "windows" {
		return "ping -n 100 127.0.0.1"
	}
	return "sleep 100"
}

func TestIntegration_StartAndOutput(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	mgr := NewManager(testConfig(), nil, nil)
	t.Cleanup(func() { mgr.StopAll() })

	p, err := mgr.Start("echo hello", "echo-test", "", nil)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	if p.ID == "" {
		t.Fatal("process ID is empty")
	}
	if p.Pid == 0 {
		t.Fatal("process PID is 0")
	}

	// Wait for the process to exit (echo is instant).
	<-p.done

	// Verify output contains "hello".
	stdout := strings.Join(p.Stdout.Lines(0), "\n")
	if !strings.Contains(stdout, "hello") {
		t.Errorf("stdout = %q, want it to contain 'hello'", stdout)
	}

	// Verify state is exited (not failed/killed).
	if p.State != StateExited {
		t.Errorf("state = %q, want %q", p.State, StateExited)
	}
}

func TestIntegration_StartAndStop(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	mgr := NewManager(testConfig(), nil, nil)
	t.Cleanup(func() { mgr.StopAll() })

	p, err := mgr.Start(longRunningCommand(), "long-proc", "", nil)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	if !p.IsRunning() {
		t.Fatal("process should be running after start")
	}

	_, err = mgr.Stop(p.ID)
	if err != nil {
		t.Fatalf("Stop failed: %v", err)
	}

	if p.IsRunning() {
		t.Error("process should not be running after stop")
	}
	if p.State != StateKilled {
		t.Errorf("state = %q, want %q", p.State, StateKilled)
	}
}

func TestIntegration_MaxProcessLimit(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	cfg := testConfig()
	cfg.MaxProcesses = 2
	mgr := NewManager(cfg, nil, nil)
	t.Cleanup(func() { mgr.StopAll() })

	_, err := mgr.Start(longRunningCommand(), "proc-1", "", nil)
	if err != nil {
		t.Fatalf("Start 1 failed: %v", err)
	}

	_, err = mgr.Start(longRunningCommand(), "proc-2", "", nil)
	if err != nil {
		t.Fatalf("Start 2 failed: %v", err)
	}

	// Third should fail.
	_, err = mgr.Start(longRunningCommand(), "proc-3", "", nil)
	if err == nil {
		t.Fatal("expected error when exceeding max processes, got nil")
	}
	if !strings.Contains(err.Error(), "max processes") {
		t.Errorf("error = %q, want it to mention 'max processes'", err.Error())
	}
}

func TestIntegration_StopAll(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	mgr := NewManager(testConfig(), nil, nil)

	for i := 0; i < 3; i++ {
		_, err := mgr.Start(longRunningCommand(), "", "", nil)
		if err != nil {
			t.Fatalf("Start %d failed: %v", i, err)
		}
	}

	procs := mgr.List()
	if len(procs) != 3 {
		t.Fatalf("List() = %d processes, want 3", len(procs))
	}

	mgr.StopAll()

	for _, p := range procs {
		if p.IsRunning() {
			t.Errorf("process %s still running after StopAll", p.ID)
		}
	}
}

func TestIntegration_EnvVarPropagation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	mgr := NewManager(testConfig(), nil, nil)
	t.Cleanup(func() { mgr.StopAll() })

	env := map[string]string{"SIDECAR_TEST_VAR": "hello-from-env"}

	var cmd string
	if runtime.GOOS == "windows" {
		cmd = "echo %SIDECAR_TEST_VAR%"
	} else {
		cmd = "echo $SIDECAR_TEST_VAR"
	}

	p, err := mgr.Start(cmd, "env-test", "", env)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	<-p.done

	stdout := strings.Join(p.Stdout.Lines(0), "\n")
	if !strings.Contains(stdout, "hello-from-env") {
		t.Errorf("stdout = %q, want it to contain 'hello-from-env'", stdout)
	}
}

func TestIntegration_WorkingDirectory(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	mgr := NewManager(testConfig(), nil, nil)
	t.Cleanup(func() { mgr.StopAll() })

	tmpDir := t.TempDir()

	var cmd string
	if runtime.GOOS == "windows" {
		cmd = "cd"
	} else {
		cmd = "pwd"
	}

	p, err := mgr.Start(cmd, "cwd-test", tmpDir, nil)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	<-p.done

	stdout := strings.TrimSpace(strings.Join(p.Stdout.Lines(0), "\n"))

	// Full-path comparison is unreliable across platforms:
	//   - Windows: cmd.exe may return 8.3 short paths (e.g. USERNA~1 vs username)
	//   - macOS:   /tmp is a symlink to /private/tmp
	// Instead, verify the output contains the unique test directory name,
	// which is stable across all path representations.
	testDirName := filepath.Base(filepath.Dir(tmpDir))
	if !strings.Contains(stdout, testDirName) {
		t.Errorf("cwd output %q does not contain test dir name %q", stdout, testDirName)
	}
}

func TestIntegration_ExitCode(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	mgr := NewManager(testConfig(), nil, nil)
	t.Cleanup(func() { mgr.StopAll() })

	p, err := mgr.Start("exit 42", "exit-test", "", nil)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	<-p.done

	// Non-zero exit code: cmd.Wait() returns error -> StateFailed.
	if p.State != StateFailed {
		t.Errorf("state = %q, want %q", p.State, StateFailed)
	}
	if p.ExitCode != 42 {
		t.Errorf("exit code = %d, want 42", p.ExitCode)
	}
}

func TestIntegration_MCP_RoundTrip(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	mgr := NewManager(testConfig(), nil, nil)
	t.Cleanup(func() { mgr.StopAll() })

	cfg := testConfig()
	s := server.NewMCPServer("test-sidecar", "0.1.0", server.WithToolCapabilities(true))
	RegisterTools(s, mgr, cfg)

	c, err := client.NewInProcessClient(s)
	if err != nil {
		t.Fatalf("create client: %v", err)
	}
	defer c.Close()

	ctx := context.Background()

	if err := c.Start(ctx); err != nil {
		t.Fatalf("start client: %v", err)
	}

	initReq := mcp.InitializeRequest{}
	initReq.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initReq.Params.ClientInfo = mcp.Implementation{Name: "test-client", Version: "1.0.0"}
	if _, err := c.Initialize(ctx, initReq); err != nil {
		t.Fatalf("initialize: %v", err)
	}

	// --- start a process ---
	startReq := mcp.CallToolRequest{}
	startReq.Params.Name = "start"
	startReq.Params.Arguments = map[string]any{
		"command": "echo integration-test",
		"name":    "mcp-echo",
	}

	startResult, err := c.CallTool(ctx, startReq)
	if err != nil {
		t.Fatalf("call start: %v", err)
	}
	if startResult.IsError {
		t.Fatalf("start returned error: %v", startResult.Content)
	}

	// Parse start response to get process ID.
	var startResp map[string]any
	text := startResult.Content[0].(mcp.TextContent).Text
	if err := json.Unmarshal([]byte(text), &startResp); err != nil {
		t.Fatalf("parse start response: %v", err)
	}
	id := startResp["id"].(string)

	// Poll status until the process exits (robust alternative to time.Sleep).
	for i := 0; i < 50; i++ {
		pollReq := mcp.CallToolRequest{}
		pollReq.Params.Name = "status"
		pollReq.Params.Arguments = map[string]any{"id": id}

		pollResult, pollErr := c.CallTool(ctx, pollReq)
		if pollErr != nil {
			t.Fatalf("poll status: %v", pollErr)
		}

		var pollResp map[string]any
		pollText := pollResult.Content[0].(mcp.TextContent).Text
		if err := json.Unmarshal([]byte(pollText), &pollResp); err != nil {
			t.Fatalf("parse poll response: %v", err)
		}
		if pollResp["state"] != "running" {
			break
		}
		if i == 49 {
			t.Fatal("echo process did not exit within polling window")
		}
		time.Sleep(100 * time.Millisecond)
	}

	// --- list processes ---
	listReq := mcp.CallToolRequest{}
	listReq.Params.Name = "list"

	listResult, err := c.CallTool(ctx, listReq)
	if err != nil {
		t.Fatalf("call list: %v", err)
	}

	var listResp []map[string]any
	text = listResult.Content[0].(mcp.TextContent).Text
	if err := json.Unmarshal([]byte(text), &listResp); err != nil {
		t.Fatalf("parse list response: %v", err)
	}
	if len(listResp) != 1 {
		t.Fatalf("list returned %d processes, want 1", len(listResp))
	}

	// --- get output ---
	outputReq := mcp.CallToolRequest{}
	outputReq.Params.Name = "output"
	outputReq.Params.Arguments = map[string]any{"id": id}

	outputResult, err := c.CallTool(ctx, outputReq)
	if err != nil {
		t.Fatalf("call output: %v", err)
	}

	var outputResp map[string]any
	text = outputResult.Content[0].(mcp.TextContent).Text
	if err := json.Unmarshal([]byte(text), &outputResp); err != nil {
		t.Fatalf("parse output response: %v", err)
	}

	stdout := outputResp["stdout"].(string)
	if !strings.Contains(stdout, "integration-test") {
		t.Errorf("stdout = %q, want it to contain 'integration-test'", stdout)
	}

	// --- get status ---
	statusReq := mcp.CallToolRequest{}
	statusReq.Params.Name = "status"
	statusReq.Params.Arguments = map[string]any{"id": id}

	statusResult, err := c.CallTool(ctx, statusReq)
	if err != nil {
		t.Fatalf("call status: %v", err)
	}

	var statusResp map[string]any
	text = statusResult.Content[0].(mcp.TextContent).Text
	if err := json.Unmarshal([]byte(text), &statusResp); err != nil {
		t.Fatalf("parse status response: %v", err)
	}
	if statusResp["name"] != "mcp-echo" {
		t.Errorf("status name = %v, want 'mcp-echo'", statusResp["name"])
	}
}

// --- Security integration tests ---

// secureEchoCommand returns a platform-appropriate echo executable name
// and the command string for use in secure (direct exec) mode.
func secureEchoCommand() (executable, command string) {
	if runtime.GOOS == "windows" {
		// On Windows, cmd.exe built-in "echo" isn't a standalone exe.
		// Use "cmd" as the allowed executable and run via /C echo.
		// But that defeats the point of direct exec. Instead, use
		// Go's own binary which is guaranteed in PATH.
		return "go", "go version"
	}
	return "echo", "echo secure-mode-test"
}

func TestIntegration_SecureMode_AllowedCommand(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	exe, command := secureEchoCommand()
	sec := &SecurityValidator{allowedExecutables: []string{exe}}
	mgr := NewManager(testConfig(), sec, nil)
	t.Cleanup(func() { mgr.StopAll() })

	p, err := mgr.Start(command, "secure-test", "", nil)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Wait for the process to exit.
	<-p.done

	if p.State != StateExited {
		t.Errorf("state = %q, want %q", p.State, StateExited)
	}

	stdout := strings.Join(p.Stdout.Lines(0), "\n")
	if runtime.GOOS == "windows" {
		if !strings.Contains(stdout, "go version") {
			t.Errorf("stdout = %q, want it to contain 'go version'", stdout)
		}
	} else {
		if !strings.Contains(stdout, "secure-mode-test") {
			t.Errorf("stdout = %q, want it to contain 'secure-mode-test'", stdout)
		}
	}
}

func TestIntegration_SecureMode_BlockedExecutable(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	sec := &SecurityValidator{allowedExecutables: []string{"git"}}
	mgr := NewManager(testConfig(), sec, nil)
	t.Cleanup(func() { mgr.StopAll() })

	_, err := mgr.Start("rm -rf /", "blocked-test", "", nil)
	if err == nil {
		t.Fatal("expected error for blocked executable, got nil")
	}
	if !strings.Contains(err.Error(), "security:") {
		t.Errorf("error = %q, want it to contain 'security:'", err.Error())
	}
	if !strings.Contains(err.Error(), "not in allowed list") {
		t.Errorf("error = %q, want 'not in allowed list'", err.Error())
	}

	// No process should have been created.
	if len(mgr.List()) != 0 {
		t.Error("expected 0 processes after blocked command")
	}
}

func TestIntegration_SecureMode_BlockedPattern(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	sec := &SecurityValidator{
		allowedExecutables: []string{"git"},
		blockedPatterns:    []*regexp.Regexp{regexp.MustCompile(`git\s+push\s+--force`)},
	}
	mgr := NewManager(testConfig(), sec, nil)

	_, err := mgr.Start("git push --force origin main", "pattern-test", "", nil)
	if err == nil {
		t.Fatal("expected error for blocked pattern, got nil")
	}
	if !strings.Contains(err.Error(), "blocked pattern") {
		t.Errorf("error = %q, want 'blocked pattern'", err.Error())
	}
}

func TestIntegration_SecureMode_ShellMetacharsBlocked(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	sec := &SecurityValidator{allowedExecutables: []string{"echo"}}
	mgr := NewManager(testConfig(), sec, nil)

	_, err := mgr.Start("echo hello; rm -rf /", "injection-test", "", nil)
	if err == nil {
		t.Fatal("expected error for shell metacharacter, got nil")
	}
	if !strings.Contains(err.Error(), "shell metacharacter") {
		t.Errorf("error = %q, want 'shell metacharacter'", err.Error())
	}
}

// --- Manager audit wiring tests ---

func TestIntegration_Audit_StartLogged(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	var buf bytes.Buffer
	audit := newAuditLoggerWriter(&buf)
	mgr := NewManager(testConfig(), nil, audit)
	t.Cleanup(func() { mgr.StopAll() })

	p, err := mgr.Start("echo audit-test", "audit-start", "", nil)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	<-p.done

	var entry auditEntry
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("invalid audit JSON: %v\nraw: %s", err, buf.String())
	}
	if entry.Event != "start" {
		t.Errorf("event = %q, want 'start'", entry.Event)
	}
	if entry.Command != "echo audit-test" {
		t.Errorf("command = %q, want 'echo audit-test'", entry.Command)
	}
}

func TestIntegration_Audit_BlockedLogged(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	var buf bytes.Buffer
	audit := newAuditLoggerWriter(&buf)
	sec := &SecurityValidator{allowedExecutables: []string{"git"}}
	mgr := NewManager(testConfig(), sec, audit)

	_, _ = mgr.Start("rm -rf /", "audit-blocked", "", nil)

	var entry auditEntry
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("invalid audit JSON: %v\nraw: %s", err, buf.String())
	}
	if entry.Event != "blocked" {
		t.Errorf("event = %q, want 'blocked'", entry.Event)
	}
	if entry.Command != "rm -rf /" {
		t.Errorf("command = %q, want 'rm -rf /'", entry.Command)
	}
	if entry.Reason == "" {
		t.Error("reason should not be empty")
	}
}

func TestIntegration_Audit_StopLogged(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	var buf bytes.Buffer
	audit := newAuditLoggerWriter(&buf)
	mgr := NewManager(testConfig(), nil, audit)
	t.Cleanup(func() { mgr.StopAll() })

	p, err := mgr.Start(longRunningCommand(), "audit-stop", "", nil)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	_, err = mgr.Stop(p.ID)
	if err != nil {
		t.Fatalf("Stop failed: %v", err)
	}

	// buf should have two entries: start + stop.
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) < 2 {
		t.Fatalf("expected at least 2 audit entries, got %d:\n%s", len(lines), buf.String())
	}

	// Last line should be the stop event.
	var stopEntry auditEntry
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &stopEntry); err != nil {
		t.Fatalf("invalid stop JSON: %v", err)
	}
	if stopEntry.Event != "stop" {
		t.Errorf("event = %q, want 'stop'", stopEntry.Event)
	}
	if stopEntry.ID != p.ID {
		t.Errorf("id = %q, want %q", stopEntry.ID, p.ID)
	}
	if stopEntry.Duration == "" {
		t.Error("duration should not be empty")
	}
}

// --- MCP round-trip with security ---

func TestIntegration_MCP_SecureMode_Blocked(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	cfg := testConfig()
	sec := &SecurityValidator{allowedExecutables: []string{"git"}}
	mgr := NewManager(cfg, sec, nil)
	t.Cleanup(func() { mgr.StopAll() })

	s := server.NewMCPServer("test-sidecar", "0.1.0", server.WithToolCapabilities(true))
	RegisterTools(s, mgr, cfg)

	c, err := client.NewInProcessClient(s)
	if err != nil {
		t.Fatalf("create client: %v", err)
	}
	defer c.Close()

	ctx := context.Background()
	if err := c.Start(ctx); err != nil {
		t.Fatalf("start client: %v", err)
	}

	initReq := mcp.InitializeRequest{}
	initReq.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initReq.Params.ClientInfo = mcp.Implementation{Name: "test-client", Version: "1.0.0"}
	if _, err := c.Initialize(ctx, initReq); err != nil {
		t.Fatalf("initialize: %v", err)
	}

	// Attempt to start a blocked command.
	startReq := mcp.CallToolRequest{}
	startReq.Params.Name = "start"
	startReq.Params.Arguments = map[string]any{
		"command": "rm -rf /",
		"name":    "mcp-blocked",
	}

	result, err := c.CallTool(ctx, startReq)
	if err != nil {
		t.Fatalf("call start: %v", err)
	}

	if !result.IsError {
		t.Fatal("expected IsError=true for blocked command")
	}

	text := result.Content[0].(mcp.TextContent).Text
	if !strings.Contains(text, "security:") {
		t.Errorf("error text = %q, want it to contain 'security:'", text)
	}
	if !strings.Contains(text, "not in allowed list") {
		t.Errorf("error text = %q, want it to contain 'not in allowed list'", text)
	}
}

func TestIntegration_MCP_SecureMode_Allowed(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	cfg := testConfig()
	exe, command := secureEchoCommand()
	sec := &SecurityValidator{allowedExecutables: []string{exe}}
	mgr := NewManager(cfg, sec, nil)
	t.Cleanup(func() { mgr.StopAll() })

	s := server.NewMCPServer("test-sidecar", "0.1.0", server.WithToolCapabilities(true))
	RegisterTools(s, mgr, cfg)

	c, err := client.NewInProcessClient(s)
	if err != nil {
		t.Fatalf("create client: %v", err)
	}
	defer c.Close()

	ctx := context.Background()
	if err := c.Start(ctx); err != nil {
		t.Fatalf("start client: %v", err)
	}

	initReq := mcp.InitializeRequest{}
	initReq.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initReq.Params.ClientInfo = mcp.Implementation{Name: "test-client", Version: "1.0.0"}
	if _, err := c.Initialize(ctx, initReq); err != nil {
		t.Fatalf("initialize: %v", err)
	}

	// Start an allowed command.
	startReq := mcp.CallToolRequest{}
	startReq.Params.Name = "start"
	startReq.Params.Arguments = map[string]any{
		"command": command,
		"name":    "mcp-secure",
	}

	startResult, err := c.CallTool(ctx, startReq)
	if err != nil {
		t.Fatalf("call start: %v", err)
	}
	if startResult.IsError {
		t.Fatalf("start returned error: %v", startResult.Content)
	}

	// Extract process ID.
	var startResp map[string]any
	text := startResult.Content[0].(mcp.TextContent).Text
	if err := json.Unmarshal([]byte(text), &startResp); err != nil {
		t.Fatalf("parse start response: %v", err)
	}
	id := startResp["id"].(string)

	// Poll until the process exits.
	for i := 0; i < 50; i++ {
		pollReq := mcp.CallToolRequest{}
		pollReq.Params.Name = "status"
		pollReq.Params.Arguments = map[string]any{"id": id}

		pollResult, pollErr := c.CallTool(ctx, pollReq)
		if pollErr != nil {
			t.Fatalf("poll status: %v", pollErr)
		}

		var pollResp map[string]any
		pollText := pollResult.Content[0].(mcp.TextContent).Text
		if err := json.Unmarshal([]byte(pollText), &pollResp); err != nil {
			t.Fatalf("parse poll response: %v", err)
		}
		if pollResp["state"] != "running" {
			break
		}
		if i == 49 {
			t.Fatal("process did not exit within polling window")
		}
		time.Sleep(100 * time.Millisecond)
	}

	// Verify output via MCP.
	outputReq := mcp.CallToolRequest{}
	outputReq.Params.Name = "output"
	outputReq.Params.Arguments = map[string]any{"id": id}

	outputResult, err := c.CallTool(ctx, outputReq)
	if err != nil {
		t.Fatalf("call output: %v", err)
	}

	var outputResp map[string]any
	outputText := outputResult.Content[0].(mcp.TextContent).Text
	if err := json.Unmarshal([]byte(outputText), &outputResp); err != nil {
		t.Fatalf("parse output response: %v", err)
	}

	stdout := outputResp["stdout"].(string)
	if runtime.GOOS == "windows" {
		if !strings.Contains(stdout, "go version") {
			t.Errorf("stdout = %q, want it to contain 'go version'", stdout)
		}
	} else {
		if !strings.Contains(stdout, "secure-mode-test") {
			t.Errorf("stdout = %q, want it to contain 'secure-mode-test'", stdout)
		}
	}
}

// --- StopAll audit logging ---

func TestIntegration_Audit_StopAllLogged(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	var buf bytes.Buffer
	audit := newAuditLoggerWriter(&buf)
	mgr := NewManager(testConfig(), nil, audit)

	// Start 2 long-running processes.
	for i := 0; i < 2; i++ {
		_, err := mgr.Start(longRunningCommand(), "", "", nil)
		if err != nil {
			t.Fatalf("Start %d failed: %v", i, err)
		}
	}

	mgr.StopAll()

	// Expect 4 audit entries: 2 starts + 2 stops.
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 4 {
		t.Fatalf("expected 4 audit entries, got %d:\n%s", len(lines), buf.String())
	}

	stopCount := 0
	for _, line := range lines {
		var entry auditEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("invalid JSON line: %v\nraw: %s", err, line)
		}
		if entry.Event == "stop" {
			stopCount++
			if entry.ID == "" {
				t.Error("stop entry should have a process ID")
			}
			if entry.Duration == "" {
				t.Error("stop entry should have a duration")
			}
		}
	}
	if stopCount != 2 {
		t.Errorf("expected 2 stop events, got %d", stopCount)
	}
}

// --- Secure mode with cwd and env ---

func TestIntegration_SecureMode_CwdAndEnv(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	sec := &SecurityValidator{allowedExecutables: []string{"go"}}
	mgr := NewManager(testConfig(), sec, nil)
	t.Cleanup(func() { mgr.StopAll() })

	tmpDir := t.TempDir()
	customGopath := "/custom/test-gopath"
	if runtime.GOOS == "windows" {
		customGopath = `C:\custom\test-gopath`
	}

	p, err := mgr.Start(
		"go env GOPATH",
		"secure-cwd-env",
		tmpDir,
		map[string]string{"GOPATH": customGopath},
	)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	<-p.done

	if p.State != StateExited {
		t.Errorf("state = %q, want %q", p.State, StateExited)
	}

	stdout := strings.TrimSpace(strings.Join(p.Stdout.Lines(0), "\n"))
	if !strings.Contains(stdout, customGopath) {
		t.Errorf("stdout = %q, want it to contain custom GOPATH %q", stdout, customGopath)
	}
}
