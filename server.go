package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// registerTools adds all MCP tools to the server and binds their handlers.
func registerTools(s *server.MCPServer, mgr *Manager) {
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
		),
		handleOutput(mgr),
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

func handleStart(mgr *Manager) server.ToolHandlerFunc {
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

func handleStop(mgr *Manager) server.ToolHandlerFunc {
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
		})
	}
}

func handleList(mgr *Manager) server.ToolHandlerFunc {
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

func handleOutput(mgr *Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id, err := req.RequireString("id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		tail := req.GetInt("tail", 0)

		p, err := mgr.Get(id)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		return jsonResult(map[string]any{
			"stdout": strings.Join(p.Stdout.Lines(tail), "\n"),
			"stderr": strings.Join(p.Stderr.Lines(tail), "\n"),
		})
	}
}

func handleSend(mgr *Manager) server.ToolHandlerFunc {
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

func handleStatus(mgr *Manager) server.ToolHandlerFunc {
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
