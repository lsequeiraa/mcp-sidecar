package sidecar

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
)

// --- test helpers ---

type nopWriteCloser struct{ io.Writer }

func (nopWriteCloser) Close() error { return nil }

// mockManager implements ProcessManager for handler unit tests.
type mockManager struct {
	startFn func(command, name, cwd string, env map[string]string) (*Process, error)
	stopFn  func(id string) (*Process, error)
	listFn  func() []*Process
	getFn   func(id string) (*Process, error)
}

func (m *mockManager) Start(command, name, cwd string, env map[string]string) (*Process, error) {
	return m.startFn(command, name, cwd, env)
}
func (m *mockManager) Stop(id string) (*Process, error) { return m.stopFn(id) }
func (m *mockManager) List() []*Process                 { return m.listFn() }
func (m *mockManager) Get(id string) (*Process, error)  { return m.getFn(id) }

// newTestProcess creates a Process with exported and unexported fields set for
// testing. No real OS process is involved.
func newTestProcess(id, name string, state ProcessState) *Process {
	return &Process{
		ID:        id,
		Name:      name,
		Command:   "test-cmd",
		Pid:       12345,
		State:     state,
		StartTime: time.Now(),
		Stdout:    NewLineBuffer(1024),
		Stderr:    NewLineBuffer(1024),
		done:      make(chan struct{}),
	}
}

// callToolRequest builds a CallToolRequest with the given tool name and arguments.
func callToolRequest(name string, args map[string]any) mcp.CallToolRequest {
	req := mcp.CallToolRequest{}
	req.Params.Name = name
	req.Params.Arguments = args
	return req
}

// resultJSON extracts the JSON text from a successful CallToolResult and
// unmarshals it into dest.
func resultJSON(t *testing.T, result *mcp.CallToolResult, dest any) {
	t.Helper()
	if result.IsError {
		t.Fatalf("expected success result, got error")
	}
	if len(result.Content) == 0 {
		t.Fatal("result has no content")
	}
	text, ok := result.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatalf("content[0] is %T, want mcp.TextContent", result.Content[0])
	}
	if err := json.Unmarshal([]byte(text.Text), dest); err != nil {
		t.Fatalf("failed to unmarshal result JSON: %v\nraw: %s", err, text.Text)
	}
}

// --- jsonResult tests ---

func TestJsonResult_Map(t *testing.T) {
	result, err := jsonResult(map[string]any{"key": "value", "num": 42})
	if err != nil {
		t.Fatalf("jsonResult returned error: %v", err)
	}
	if result.IsError {
		t.Error("result.IsError is true, want false")
	}

	var got map[string]any
	resultJSON(t, result, &got)
	if got["key"] != "value" {
		t.Errorf("key = %v, want 'value'", got["key"])
	}
}

func TestJsonResult_Slice(t *testing.T) {
	result, err := jsonResult([]string{"a", "b", "c"})
	if err != nil {
		t.Fatalf("jsonResult returned error: %v", err)
	}

	var got []string
	resultJSON(t, result, &got)
	if len(got) != 3 || got[0] != "a" {
		t.Errorf("got %v, want [a b c]", got)
	}
}

func TestJsonResult_EmptySlice(t *testing.T) {
	result, err := jsonResult([]string{})
	if err != nil {
		t.Fatalf("jsonResult returned error: %v", err)
	}

	var got []string
	resultJSON(t, result, &got)
	if len(got) != 0 {
		t.Errorf("got %v, want []", got)
	}
}

// --- handler tests ---

func TestHandleStart_Success(t *testing.T) {
	mock := &mockManager{
		startFn: func(command, name, cwd string, env map[string]string) (*Process, error) {
			if command != "echo hello" {
				t.Errorf("command = %q, want 'echo hello'", command)
			}
			if name != "my-proc" {
				t.Errorf("name = %q, want 'my-proc'", name)
			}
			if cwd != "/tmp" {
				t.Errorf("cwd = %q, want '/tmp'", cwd)
			}
			if env["FOO"] != "bar" {
				t.Errorf("env[FOO] = %q, want 'bar'", env["FOO"])
			}
			return newTestProcess("sc-abc123", "my-proc", StateRunning), nil
		},
	}

	handler := handleStart(mock)
	req := callToolRequest("start", map[string]any{
		"command": "echo hello",
		"name":    "my-proc",
		"cwd":     "/tmp",
		"env":     map[string]any{"FOO": "bar"},
	})

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	var got map[string]any
	resultJSON(t, result, &got)
	if got["id"] != "sc-abc123" {
		t.Errorf("id = %v, want 'sc-abc123'", got["id"])
	}
	if got["name"] != "my-proc" {
		t.Errorf("name = %v, want 'my-proc'", got["name"])
	}
}

func TestHandleStart_MissingCommand(t *testing.T) {
	mock := &mockManager{
		startFn: func(command, name, cwd string, env map[string]string) (*Process, error) {
			t.Fatal("Start should not be called when command is missing")
			return nil, nil
		},
	}

	handler := handleStart(mock)
	req := callToolRequest("start", map[string]any{
		"name": "my-proc",
	})

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error result for missing command")
	}
}

func TestHandleStart_ManagerError(t *testing.T) {
	mock := &mockManager{
		startFn: func(command, name, cwd string, env map[string]string) (*Process, error) {
			return nil, fmt.Errorf("max processes reached (10)")
		},
	}

	handler := handleStart(mock)
	req := callToolRequest("start", map[string]any{"command": "echo test"})

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error result when manager returns error")
	}
}

func TestHandleStop_Success(t *testing.T) {
	p := newTestProcess("sc-test1", "test", StateKilled)
	p.ExitCode = -1

	mock := &mockManager{
		stopFn: func(id string) (*Process, error) {
			if id != "sc-test1" {
				t.Errorf("id = %q, want 'sc-test1'", id)
			}
			return p, nil
		},
	}

	handler := handleStop(mock)
	req := callToolRequest("stop", map[string]any{"id": "sc-test1"})

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	var got map[string]any
	resultJSON(t, result, &got)
	if got["id"] != "sc-test1" {
		t.Errorf("id = %v, want 'sc-test1'", got["id"])
	}
}

func TestHandleStop_NotFound(t *testing.T) {
	mock := &mockManager{
		stopFn: func(id string) (*Process, error) {
			return nil, fmt.Errorf("process %q not found", id)
		},
	}

	handler := handleStop(mock)
	req := callToolRequest("stop", map[string]any{"id": "nonexistent"})

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error result for unknown process")
	}
}

func TestHandleList_Empty(t *testing.T) {
	mock := &mockManager{
		listFn: func() []*Process { return nil },
	}

	handler := handleList(mock)
	req := callToolRequest("list", nil)

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	var got []map[string]any
	resultJSON(t, result, &got)
	if len(got) != 0 {
		t.Errorf("expected empty list, got %d items", len(got))
	}
}

func TestHandleList_WithProcesses(t *testing.T) {
	procs := []*Process{
		newTestProcess("sc-a", "alpha", StateRunning),
		newTestProcess("sc-b", "beta", StateExited),
	}

	mock := &mockManager{
		listFn: func() []*Process { return procs },
	}

	handler := handleList(mock)
	req := callToolRequest("list", nil)

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	var got []map[string]any
	resultJSON(t, result, &got)
	if len(got) != 2 {
		t.Fatalf("expected 2 items, got %d", len(got))
	}

	// Verify fields are present.
	for _, item := range got {
		if item["id"] == nil || item["name"] == nil || item["state"] == nil {
			t.Errorf("list item missing fields: %v", item)
		}
	}
}

func TestHandleOutput_Success(t *testing.T) {
	p := newTestProcess("sc-out1", "output-test", StateRunning)
	p.Stdout.Write([]byte("line1\nline2\nline3\n"))
	p.Stderr.Write([]byte("err1\n"))

	mock := &mockManager{
		getFn: func(id string) (*Process, error) { return p, nil },
	}

	handler := handleOutput(mock, &Config{})
	req := callToolRequest("output", map[string]any{"id": "sc-out1", "tail": float64(2)})

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	var got map[string]any
	resultJSON(t, result, &got)

	stdout := got["stdout"].(string)
	if stdout != "line2\nline3" {
		t.Errorf("stdout = %q, want 'line2\\nline3'", stdout)
	}
	stderr := got["stderr"].(string)
	if stderr != "err1" {
		t.Errorf("stderr = %q, want 'err1'", stderr)
	}
}

func TestHandleOutput_AllLines(t *testing.T) {
	p := newTestProcess("sc-out2", "output-all", StateRunning)
	p.Stdout.Write([]byte("a\nb\nc\n"))

	mock := &mockManager{
		getFn: func(id string) (*Process, error) { return p, nil },
	}

	handler := handleOutput(mock, &Config{})
	req := callToolRequest("output", map[string]any{"id": "sc-out2"}) // no tail

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	var got map[string]any
	resultJSON(t, result, &got)

	stdout := got["stdout"].(string)
	if stdout != "a\nb\nc" {
		t.Errorf("stdout = %q, want 'a\\nb\\nc'", stdout)
	}
}

func TestHandleSend_Success(t *testing.T) {
	stdinBuf := &bytes.Buffer{}
	p := newTestProcess("sc-send1", "send-test", StateRunning)
	p.stdin = nopWriteCloser{stdinBuf}

	mock := &mockManager{
		getFn: func(id string) (*Process, error) { return p, nil },
	}

	handler := handleSend(mock)
	req := callToolRequest("send", map[string]any{"id": "sc-send1", "input": "hello world"})

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	var got map[string]any
	resultJSON(t, result, &got)
	if got["ok"] != true {
		t.Errorf("ok = %v, want true", got["ok"])
	}

	// Verify the input was written to stdin with trailing newline.
	if stdinBuf.String() != "hello world\n" {
		t.Errorf("stdin received %q, want 'hello world\\n'", stdinBuf.String())
	}
}

func TestHandleSend_ProcessNotRunning(t *testing.T) {
	p := newTestProcess("sc-send2", "send-stopped", StateExited)
	p.stdin = nopWriteCloser{&bytes.Buffer{}}

	mock := &mockManager{
		getFn: func(id string) (*Process, error) { return p, nil },
	}

	handler := handleSend(mock)
	req := callToolRequest("send", map[string]any{"id": "sc-send2", "input": "hello"})

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error result when process is not running")
	}
}

func TestHandleStatus_Success(t *testing.T) {
	p := newTestProcess("sc-stat1", "status-test", StateRunning)
	p.Stdout.Write([]byte("some output\n"))
	p.Stderr.Write([]byte("some error\n"))

	mock := &mockManager{
		getFn: func(id string) (*Process, error) { return p, nil },
	}

	handler := handleStatus(mock)
	req := callToolRequest("status", map[string]any{"id": "sc-stat1"})

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	var got map[string]any
	resultJSON(t, result, &got)
	if got["id"] != "sc-stat1" {
		t.Errorf("id = %v, want 'sc-stat1'", got["id"])
	}
	if got["name"] != "status-test" {
		t.Errorf("name = %v, want 'status-test'", got["name"])
	}
	if got["state"] != "running" {
		t.Errorf("state = %v, want 'running'", got["state"])
	}
	// outputSize = stdout(11) + stderr(10) = 21
	outputSize := got["outputSize"].(float64)
	if outputSize != 21 {
		t.Errorf("outputSize = %v, want 21", outputSize)
	}
}

func TestHandleStatus_NotFound(t *testing.T) {
	mock := &mockManager{
		getFn: func(id string) (*Process, error) {
			return nil, fmt.Errorf("process %q not found", id)
		},
	}

	handler := handleStatus(mock)
	req := callToolRequest("status", map[string]any{"id": "nonexistent"})

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error result for unknown process")
	}
}

// --- missing required params tests ---

func TestHandleStop_MissingID(t *testing.T) {
	mock := &mockManager{
		stopFn: func(id string) (*Process, error) {
			t.Fatal("Stop should not be called when id is missing")
			return nil, nil
		},
	}

	handler := handleStop(mock)
	req := callToolRequest("stop", map[string]any{})

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error result for missing id")
	}
}

func TestHandleOutput_MissingID(t *testing.T) {
	mock := &mockManager{
		getFn: func(id string) (*Process, error) {
			t.Fatal("Get should not be called when id is missing")
			return nil, nil
		},
	}

	handler := handleOutput(mock, &Config{})
	req := callToolRequest("output", map[string]any{})

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error result for missing id")
	}
}

func TestHandleSend_MissingID(t *testing.T) {
	mock := &mockManager{
		getFn: func(id string) (*Process, error) {
			t.Fatal("Get should not be called when id is missing")
			return nil, nil
		},
	}

	handler := handleSend(mock)
	req := callToolRequest("send", map[string]any{"input": "hello"})

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error result for missing id")
	}
}

func TestHandleSend_MissingInput(t *testing.T) {
	mock := &mockManager{
		getFn: func(id string) (*Process, error) {
			t.Fatal("Get should not be called when input is missing")
			return nil, nil
		},
	}

	handler := handleSend(mock)
	req := callToolRequest("send", map[string]any{"id": "sc-test1"})

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error result for missing input")
	}
}

func TestHandleStatus_MissingID(t *testing.T) {
	mock := &mockManager{
		getFn: func(id string) (*Process, error) {
			t.Fatal("Get should not be called when id is missing")
			return nil, nil
		},
	}

	handler := handleStatus(mock)
	req := callToolRequest("status", map[string]any{})

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error result for missing id")
	}
}

// --- output tool uptime field ---

func TestHandleOutput_IncludesUptime(t *testing.T) {
	p := newTestProcess("sc-up1", "uptime-test", StateRunning)
	p.Stdout.Write([]byte("data\n"))

	mock := &mockManager{
		getFn: func(id string) (*Process, error) { return p, nil },
	}

	handler := handleOutput(mock, &Config{})
	req := callToolRequest("output", map[string]any{"id": "sc-up1"})

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	var got map[string]any
	resultJSON(t, result, &got)
	if got["uptime"] == nil {
		t.Error("output response should include 'uptime' field")
	}
}

// --- stop tool uptime field ---

func TestHandleStop_IncludesUptime(t *testing.T) {
	p := newTestProcess("sc-stop-up", "stop-uptime", StateKilled)
	p.ExitCode = -1
	p.EndTime = p.StartTime.Add(5 * time.Second)

	mock := &mockManager{
		stopFn: func(id string) (*Process, error) { return p, nil },
	}

	handler := handleStop(mock)
	req := callToolRequest("stop", map[string]any{"id": "sc-stop-up"})

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	var got map[string]any
	resultJSON(t, result, &got)
	if got["uptime"] == nil {
		t.Error("stop response should include 'uptime' field")
	}
}

// --- effectiveLimit ---

func TestEffectiveLimit(t *testing.T) {
	tests := []struct {
		name    string
		global  int
		request int
		want    int
	}{
		{"both zero", 0, 0, 0},
		{"global only", 1000, 0, 1000},
		{"request only", 0, 500, 500},
		{"both set, global smaller", 500, 1000, 500},
		{"both set, request smaller", 1000, 500, 500},
		{"both set, equal", 500, 500, 500},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := effectiveLimit(tt.global, tt.request)
			if got != tt.want {
				t.Errorf("effectiveLimit(%d, %d) = %d, want %d", tt.global, tt.request, got, tt.want)
			}
		})
	}
}

// --- keepTail ---

func TestKeepTail(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		maxBytes int
		want     string
	}{
		{"fits entirely", "abc", 10, "abc"},
		{"exact fit", "abc", 3, "abc"},
		{"truncates on newline", "line1\nline2\nline3", 12, "line2\nline3"},
		{"no newline in tail", "abcdefghij", 5, "fghij"},
		{"zero maxBytes", "abc", 0, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := keepTail(tt.input, tt.maxBytes)
			if got != tt.want {
				t.Errorf("keepTail(%q, %d) = %q, want %q", tt.input, tt.maxBytes, got, tt.want)
			}
		})
	}
}

// --- truncateOutput ---

func TestTruncateOutput_NoTruncation(t *testing.T) {
	stdout, stderr, total, truncated := truncateOutput("hello", "err", 100)

	if truncated {
		t.Error("should not be truncated")
	}
	if stdout != "hello" {
		t.Errorf("stdout = %q, want 'hello'", stdout)
	}
	if stderr != "err" {
		t.Errorf("stderr = %q, want 'err'", stderr)
	}
	if total != 8 {
		t.Errorf("total = %d, want 8", total)
	}
}

func TestTruncateOutput_StderrSmall_StdoutTruncated(t *testing.T) {
	// stderr is small (3 bytes), fits in half of 20 (10).
	// stdout gets remainder: 20 - 3 = 17 bytes.
	stdout := "line1\nline2\nline3\nline4\nline5" // 29 bytes
	stderr := "err"                               // 3 bytes
	outStdout, outStderr, total, truncated := truncateOutput(stdout, stderr, 20)

	if !truncated {
		t.Error("should be truncated")
	}
	if total != 32 {
		t.Errorf("total = %d, want 32", total)
	}
	if outStderr != "err" {
		t.Errorf("stderr should be unchanged, got %q", outStderr)
	}
	if len(outStdout) > 17 {
		t.Errorf("stdout should be <= 17 bytes, got %d bytes: %q", len(outStdout), outStdout)
	}
}

func TestTruncateOutput_StdoutSmall_StderrTruncated(t *testing.T) {
	stdout := "ok"                                     // 2 bytes
	stderr := "error1\nerror2\nerror3\nerror4\nerror5" // 34 bytes
	outStdout, outStderr, total, truncated := truncateOutput(stdout, stderr, 20)

	if !truncated {
		t.Error("should be truncated")
	}
	if total != 36 {
		t.Errorf("total = %d, want 36", total)
	}
	if outStdout != "ok" {
		t.Errorf("stdout should be unchanged, got %q", outStdout)
	}
	// stderr budget = 20 - 2 = 18
	if len(outStderr) > 18 {
		t.Errorf("stderr should be <= 18 bytes, got %d bytes: %q", len(outStderr), outStderr)
	}
}

func TestTruncateOutput_BothExceedHalf(t *testing.T) {
	stdout := "aaaaaaaaaa\nbbbbbbbbbb" // 21 bytes
	stderr := "cccccccccc\ndddddddddd" // 21 bytes
	outStdout, outStderr, total, truncated := truncateOutput(stdout, stderr, 20)

	if !truncated {
		t.Error("should be truncated")
	}
	if total != 42 {
		t.Errorf("total = %d, want 42", total)
	}
	// Each gets ~10 bytes (half of 20).
	if len(outStdout) > 10 {
		t.Errorf("stdout should be <= 10 bytes, got %d: %q", len(outStdout), outStdout)
	}
	if len(outStderr) > 10 {
		t.Errorf("stderr should be <= 10 bytes, got %d: %q", len(outStderr), outStderr)
	}
}

// --- handleOutput with maxBytes ---

func TestHandleOutput_MaxBytes_Truncates(t *testing.T) {
	p := newTestProcess("sc-trunc", "trunc-test", StateRunning)
	// Write enough output to exceed the limit.
	for i := 0; i < 100; i++ {
		p.Stdout.Write([]byte(fmt.Sprintf("stdout-line-%03d\n", i)))
	}
	p.Stderr.Write([]byte("small-error\n"))

	mock := &mockManager{
		getFn: func(id string) (*Process, error) { return p, nil },
	}

	handler := handleOutput(mock, &Config{})
	req := callToolRequest("output", map[string]any{
		"id":       "sc-trunc",
		"maxBytes": float64(200),
	})

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	var got map[string]any
	resultJSON(t, result, &got)

	if got["truncated"] != true {
		t.Error("expected truncated=true")
	}
	totalBytes, ok := got["totalBytes"].(float64)
	if !ok || totalBytes < 200 {
		t.Errorf("totalBytes = %v, want > 200", got["totalBytes"])
	}

	stdout := got["stdout"].(string)
	stderr := got["stderr"].(string)
	if len(stdout)+len(stderr) > 200 {
		t.Errorf("combined output = %d bytes, want <= 200", len(stdout)+len(stderr))
	}
}

func TestHandleOutput_MaxBytes_NoTruncation(t *testing.T) {
	p := newTestProcess("sc-notrunc", "notrunc-test", StateRunning)
	p.Stdout.Write([]byte("short\n"))
	p.Stderr.Write([]byte("err\n"))

	mock := &mockManager{
		getFn: func(id string) (*Process, error) { return p, nil },
	}

	handler := handleOutput(mock, &Config{})
	req := callToolRequest("output", map[string]any{
		"id":       "sc-notrunc",
		"maxBytes": float64(10000),
	})

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	var got map[string]any
	resultJSON(t, result, &got)

	if got["truncated"] != nil {
		t.Errorf("expected no truncated field, got %v", got["truncated"])
	}
}

func TestHandleOutput_GlobalMaxOutputSize(t *testing.T) {
	p := newTestProcess("sc-global", "global-test", StateRunning)
	for i := 0; i < 100; i++ {
		p.Stdout.Write([]byte(fmt.Sprintf("line-%03d\n", i)))
	}

	mock := &mockManager{
		getFn: func(id string) (*Process, error) { return p, nil },
	}

	// Global config limit, no per-request maxBytes.
	handler := handleOutput(mock, &Config{MaxOutputSize: 100})
	req := callToolRequest("output", map[string]any{"id": "sc-global"})

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	var got map[string]any
	resultJSON(t, result, &got)

	if got["truncated"] != true {
		t.Error("expected truncated=true with global MaxOutputSize")
	}
}

func TestHandleOutput_GlobalAndRequest_UsesSmaller(t *testing.T) {
	p := newTestProcess("sc-both", "both-test", StateRunning)
	for i := 0; i < 100; i++ {
		p.Stdout.Write([]byte(fmt.Sprintf("line-%03d\n", i)))
	}

	mock := &mockManager{
		getFn: func(id string) (*Process, error) { return p, nil },
	}

	// Global = 500, request = 200 — should use 200.
	handler := handleOutput(mock, &Config{MaxOutputSize: 500})
	req := callToolRequest("output", map[string]any{
		"id":       "sc-both",
		"maxBytes": float64(200),
	})

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	var got map[string]any
	resultJSON(t, result, &got)

	stdout := got["stdout"].(string)
	stderr := got["stderr"].(string)
	if len(stdout)+len(stderr) > 200 {
		t.Errorf("combined output = %d bytes, want <= 200 (smaller of global/request)", len(stdout)+len(stderr))
	}
}
