package sidecar

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// RegisterTools adds all MCP tools to the server and binds their handlers.
// cfg is used to read global limits such as MaxOutputSize.
func RegisterTools(s *server.MCPServer, mgr ProcessManager, cfg *Config) {
	// -- start --
	s.AddTool(
		mcp.NewTool("start",
			mcp.WithDescription("Spawn a background process"),
			mcp.WithString("command", mcp.Required(), mcp.Description("Shell command to execute")),
			mcp.WithString("name", mcp.Description("Human-friendly name for the process")),
			mcp.WithString("cwd", mcp.Description("Working directory for the process")),
			mcp.WithObject("env",
				mcp.Description("Additional environment variables"),
				mcp.AdditionalProperties(map[string]any{"type": "string"}),
			),
		),
		handleStart(mgr),
	)

	// -- stop --
	s.AddTool(
		mcp.NewTool("stop",
			mcp.WithDescription("Terminate a managed process (graceful, then force)"),
			mcp.WithString("id", mcp.Required(), mcp.Description("Process ID to stop")),
		),
		handleStop(mgr),
	)

	// -- list --
	s.AddTool(
		mcp.NewTool("list",
			mcp.WithDescription("List all managed processes"),
		),
		handleList(mgr),
	)

	// -- output --
	s.AddTool(
		mcp.NewTool("output",
			mcp.WithDescription("Get buffered stdout and stderr of a process"),
			mcp.WithString("id", mcp.Required(), mcp.Description("Process ID")),
			mcp.WithNumber("tail", mcp.Description("Return only the last N lines (0 = all)")),
			mcp.WithNumber("maxBytes", mcp.Description("Max total bytes for stdout+stderr combined (0 = unlimited). Keeps the most recent output. Stderr is prioritized.")),
		),
		handleOutput(mgr, cfg),
	)

	// -- send --
	s.AddTool(
		mcp.NewTool("send",
			mcp.WithDescription("Write to a process's stdin"),
			mcp.WithString("id", mcp.Required(), mcp.Description("Process ID")),
			mcp.WithString("input", mcp.Required(), mcp.Description("Text to send (a trailing newline is added automatically)")),
		),
		handleSend(mgr),
	)

	// -- status --
	s.AddTool(
		mcp.NewTool("status",
			mcp.WithDescription("Get detailed state of a single process"),
			mcp.WithString("id", mcp.Required(), mcp.Description("Process ID")),
		),
		handleStatus(mgr),
	)
}

// --- handlers ---

func handleStart(mgr ProcessManager) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		command, err := req.RequireString("command")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		name := req.GetString("name", "")
		cwd := req.GetString("cwd", "")

		var env map[string]string
		if raw, ok := req.GetArguments()["env"]; ok && raw != nil {
			if m, ok := raw.(map[string]any); ok {
				env = make(map[string]string, len(m))
				for k, v := range m {
					env[k] = fmt.Sprintf("%v", v)
				}
			}
		}

		p, err := mgr.Start(command, name, cwd, env)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		return jsonResult(map[string]any{
			"id":   p.ID,
			"name": p.Name,
			"pid":  p.Pid,
		})
	}
}

func handleStop(mgr ProcessManager) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id, err := req.RequireString("id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		p, err := mgr.Stop(id)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		return jsonResult(map[string]any{
			"id":       p.ID,
			"exitCode": p.ExitCode,
			"uptime":   p.Uptime().String(),
		})
	}
}

func handleList(mgr ProcessManager) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		procs := mgr.List()
		items := make([]map[string]any, len(procs))
		for i, p := range procs {
			items[i] = map[string]any{
				"id":     p.ID,
				"name":   p.Name,
				"pid":    p.Pid,
				"state":  p.State,
				"uptime": p.Uptime().String(),
			}
		}
		return jsonResult(items)
	}
}

func handleOutput(mgr ProcessManager, cfg *Config) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id, err := req.RequireString("id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		tail := req.GetInt("tail", 0)
		reqMax := req.GetInt("maxBytes", 0)

		p, err := mgr.Get(id)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		stdoutStr := strings.Join(p.Stdout.Lines(tail), "\n")
		stderrStr := strings.Join(p.Stderr.Lines(tail), "\n")

		limit := effectiveLimit(cfg.MaxOutputSize, reqMax)

		result := map[string]any{
			"uptime": p.Uptime().String(),
		}

		if limit > 0 {
			stdoutOut, stderrOut, total, truncated := truncateOutput(stdoutStr, stderrStr, limit)
			result["stdout"] = stdoutOut
			result["stderr"] = stderrOut
			if truncated {
				result["truncated"] = true
				result["totalBytes"] = total
			}
		} else {
			result["stdout"] = stdoutStr
			result["stderr"] = stderrStr
		}

		return jsonResult(result)
	}
}

func handleSend(mgr ProcessManager) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id, err := req.RequireString("id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		input, err := req.RequireString("input")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		p, err := mgr.Get(id)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		if err := p.Send(input + "\n"); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		return jsonResult(map[string]any{"ok": true})
	}
}

func handleStatus(mgr ProcessManager) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id, err := req.RequireString("id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		p, err := mgr.Get(id)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		return jsonResult(map[string]any{
			"id":         p.ID,
			"name":       p.Name,
			"pid":        p.Pid,
			"state":      p.State,
			"exitCode":   p.ExitCode,
			"uptime":     p.Uptime().String(),
			"outputSize": p.Stdout.Len() + p.Stderr.Len(),
		})
	}
}

// jsonResult marshals v to JSON and returns it as an MCP text result.
func jsonResult(v any) (*mcp.CallToolResult, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("json marshal: %v", err)), nil
	}
	return mcp.NewToolResultText(string(b)), nil
}

// effectiveLimit returns the active byte cap given a global config value and
// a per-request value. Zero means unlimited; when both are set the smaller
// non-zero value wins.
func effectiveLimit(global, request int) int {
	switch {
	case global > 0 && request > 0:
		if global < request {
			return global
		}
		return request
	case global > 0:
		return global
	default:
		return request
	}
}

// truncateOutput caps the combined size of stdout and stderr to maxBytes.
// When truncation is needed, stderr is prioritized (it usually contains
// error information). Each field keeps its most recent content (tail) and
// is cut on a newline boundary to avoid partial lines.
// Returns the (possibly truncated) stdout, stderr, total original bytes,
// and whether truncation occurred.
func truncateOutput(stdout, stderr string, maxBytes int) (string, string, int, bool) {
	total := len(stdout) + len(stderr)
	if total <= maxBytes {
		return stdout, stderr, total, false
	}

	half := maxBytes / 2

	switch {
	case len(stderr) <= half:
		// stderr fits in half; give stdout the remainder.
		stdout = keepTail(stdout, maxBytes-len(stderr))
	case len(stdout) <= half:
		// stdout fits in half; give stderr the remainder.
		stderr = keepTail(stderr, maxBytes-len(stdout))
	default:
		// Both exceed half; split evenly (stderr gets the rounding byte).
		stdout = keepTail(stdout, half)
		stderr = keepTail(stderr, maxBytes-half)
	}

	return stdout, stderr, total, true
}

// keepTail returns the last maxBytes of s, cutting at the first newline
// after the cut point to avoid returning partial lines. If s already fits,
// it is returned unchanged.
func keepTail(s string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	if len(s) <= maxBytes {
		return s
	}
	truncated := s[len(s)-maxBytes:]
	if idx := strings.Index(truncated, "\n"); idx != -1 {
		return truncated[idx+1:]
	}
	return truncated
}
