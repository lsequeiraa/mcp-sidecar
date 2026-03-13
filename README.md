# mcp-sidecar

A lightweight, cross-platform MCP server for managing background processes. Built in Go for single-binary distribution with zero runtime dependencies.

## Why?

AI coding agents (Claude Code, Gemini CLI, Cursor, etc.) lack the ability to run long-lived processes while continuing to interact with them. Common workflows affected:

- Start an API server, wait for it to be ready, then run HTTP requests against it
- Run a build in watch mode while editing code
- Start a database, seed it, run tests, tear it down

`mcp-sidecar` solves this by exposing process lifecycle management as MCP tools over stdio transport.

## Features

- **Cross-platform** -- Windows, Linux, macOS from a single codebase
- **Zero dependencies** -- Single Go binary, no runtime required
- **Minimal surface** -- 6 tools, no unnecessary complexity
- **Reliable cleanup** -- All child processes are terminated when the server exits; exited processes are auto-removed after a configurable TTL
- **Output control** -- Per-request `maxBytes` and global `SIDECAR_MAX_OUTPUT_SIZE` to cap output returned to the LLM
- **Command security** -- Optional executable allowlist, blocked patterns, and audit logging

## Installation

### Quick Setup (any agent)

The fastest way to install `mcp-sidecar` into any supported coding agent:

```bash
# Interactive -- detects installed agents and lets you pick
npx add-mcp mcp-sidecar

# Install to a specific agent
npx add-mcp mcp-sidecar -a claude-code
npx add-mcp mcp-sidecar -a cursor
npx add-mcp mcp-sidecar -a vscode

# Install to all detected agents
npx add-mcp mcp-sidecar --all
```

Supported agents: Claude Code, Codex, Cursor, VS Code, Gemini CLI, OpenCode, Zed, Goose, Cline, and more. See [add-mcp](https://github.com/neondatabase/add-mcp) for the full list.

### Manual Configuration

**Claude Code**:
```bash
claude mcp add sidecar -- npx -y mcp-sidecar
```

**opencode** (`~/.config/opencode/opencode.json`):
```json
{
  "mcp": {
    "sidecar": {
      "type": "local",
      "command": ["npx", "-y", "mcp-sidecar"],
      "enabled": true
    }
  }
}
```

**Generic** (`mcp.json`):
```json
{
  "mcpServers": {
    "sidecar": {
      "command": "npx",
      "args": ["-y", "mcp-sidecar"]
    }
  }
}
```

### From Source

```bash
go install github.com/lsequeiraa/mcp-sidecar@latest
```

Pre-built binaries are also available on [GitHub Releases](https://github.com/lsequeiraa/mcp-sidecar/releases) for windows/amd64, linux/amd64, darwin/arm64, and darwin/amd64.

## Tools

| Tool | Description | Parameters | Returns |
|---|---|---|---|
| `start` | Spawn a background process | `command`, `name?`, `cwd?`, `env?` | `{ id, name, pid }` |
| `stop` | Terminate a process (graceful, then force) | `id` | `{ id, exitCode, uptime }` |
| `list` | List all managed processes | -- | `[{ id, name, pid, state, uptime }]` |
| `output` | Get buffered stdout/stderr | `id`, `tail?`, `maxBytes?` | `{ stdout, stderr, uptime }` |
| `send` | Write to a process's stdin | `id`, `input` | `{ ok }` |
| `status` | Get detailed state of one process | `id` | `{ id, name, pid, state, exitCode, uptime, outputSize }` |

The `uptime` field reflects actual runtime: for running processes it's the time since start; for exited processes it's the time the process was alive (not the time since start).

The `output` tool's `maxBytes` parameter caps the total bytes of stdout+stderr combined. When both `tail` and `maxBytes` are provided, `tail` selects lines first, then `maxBytes` caps the byte size of the result. When the output exceeds the limit, the most recent content is kept and stderr is prioritized. Truncation preserves line boundaries. When output is truncated, the response includes `"truncated": true` and `"totalBytes"` indicating the original size.

### Process States

| State | Meaning |
|---|---|
| `running` | Process is alive |
| `exited` | Process terminated normally |
| `failed` | Process terminated with non-zero exit code |
| `killed` | Process was stopped via the `stop` tool |

### Example workflow

A typical session where an agent starts an API server, waits for it to be ready, tests it, and tears it down:

```
1. start  { command: "dotnet run --project MyApi", name: "api" }
   → { id: "sc-a1b2c3", name: "api", pid: 12345 }

2. output { id: "sc-a1b2c3", tail: 5 }
   → { stdout: "Now listening on: http://localhost:5000", stderr: "", uptime: "3s" }

3. (agent runs curl http://localhost:5000/health using its own shell)

4. output { id: "sc-a1b2c3", tail: 20 }
   → { stdout: "...request logs...", stderr: "", uptime: "15s" }

5. stop   { id: "sc-a1b2c3" }
   → { id: "sc-a1b2c3", exitCode: -1, uptime: "18s" }
```

The agent uses its native shell for short-lived commands (curl, build tools, etc.) and `mcp-sidecar` for processes that need to stay alive across multiple tool calls.

## Configuration

All configuration is via environment variables (all optional):

| Variable | Default | Description |
|---|---|---|
| `SIDECAR_MAX_PROCESSES` | `10` | Maximum concurrent processes |
| `SIDECAR_BUFFER_SIZE` | `1048576` (1MB) | Output buffer size per process in bytes |
| `SIDECAR_KILL_TIMEOUT` | `5000` | Milliseconds to wait between SIGTERM and SIGKILL |
| `SIDECAR_CLEANUP_AFTER` | `1800` (30 min) | Seconds before exited processes are auto-removed. `0` = disabled |
| `SIDECAR_MAX_OUTPUT_SIZE` | `0` (unlimited) | Global cap on bytes returned by the `output` tool. `0` = no limit |
| `SIDECAR_ALLOWED_EXECUTABLES` | -- | Comma-separated allowlist of executables (enables secure mode) |
| `SIDECAR_BLOCKED_PATTERNS` | -- | Comma-separated regex patterns to reject commands |
| `SIDECAR_AUDIT_LOG` | -- | Audit log directory, or `true` (cwd) / `temp` (OS temp dir) |

**Auto-cleanup note:** When `SIDECAR_CLEANUP_AFTER` is enabled (the default), exited processes are removed from the manager after the TTL expires. Once removed, calls to `output`, `status`, or `stop` for that process ID will return "not found". Retrieve any output you need within the TTL window (30 minutes by default).

## Security

When `SIDECAR_ALLOWED_EXECUTABLES` is set, mcp-sidecar switches from shell mode to **secure mode**:

| | Shell mode (default) | Secure mode |
|---|---|---|
| **Execution** | Via `sh -c` / `cmd /C` | Direct exec (no shell) |
| **Allowlist** | None | Only listed executables can run |
| **Metacharacters** | Allowed (shell interprets them) | Rejected (`\|`, `&`, `;`, `>`, `<`, `$`, `` ` ``, `(`, `)`) |
| **Blocked patterns** | None | Regex patterns matched against full command |
| **Audit logging** | None | Optional JSONL log of all start/stop/blocked events |

Metacharacters inside quotes are allowed -- `grep "error|warning" file.txt` works because the `|` is inside double quotes and won't be interpreted by a shell.

Backslash (`\`) is intentionally **not** treated as a metacharacter so Windows paths like `C:\Users\me\app.exe` work without escaping.

### Configuration examples

**Claude Desktop / Cursor** (`mcp.json`):
```json
{
  "mcpServers": {
    "sidecar": {
      "command": "npx",
      "args": ["-y", "mcp-sidecar"],
      "env": {
        "SIDECAR_ALLOWED_EXECUTABLES": "dotnet,npm,node,git,python",
        "SIDECAR_BLOCKED_PATTERNS": "rm\\s+-rf,--force,--no-verify",
        "SIDECAR_AUDIT_LOG": "./logs"
      }
    }
  }
}
```

**Claude Code**:
```bash
claude mcp add sidecar \
  -e SIDECAR_ALLOWED_EXECUTABLES=dotnet,npm,node,git,python \
  -e SIDECAR_BLOCKED_PATTERNS="rm\\s+-rf,--force,--no-verify" \
  -e SIDECAR_AUDIT_LOG=true \
  -- npx -y mcp-sidecar
```

### Audit log format

The audit log is always written to a file named `sidecar-audit.jsonl` inside the configured directory. The `SIDECAR_AUDIT_LOG` variable controls where:

| Value | Log file location |
|---|---|
| `true` | `./sidecar-audit.jsonl` (current working directory) |
| `temp` | `<OS temp dir>/sidecar-audit.jsonl` |
| `./logs` | `./logs/sidecar-audit.jsonl` (directory auto-created) |

Each line is a JSON object with one of three event types:

```jsonl
{"ts":"2025-03-12T10:00:00Z","event":"start","id":"abc123","command":"dotnet run","cwd":"/app"}
{"ts":"2025-03-12T10:00:05Z","event":"stop","id":"abc123","exit_code":0,"duration":"5s"}
{"ts":"2025-03-12T10:00:06Z","event":"blocked","command":"rm -rf /","reason":"executable \"rm\" is not in allowed list"}
```

### Executable matching

The allowlist supports three matching modes:

| Allowlist entry | Matches |
|---|---|
| `dotnet` | `dotnet` anywhere (basename match) |
| `./build.sh` | Only `./build.sh` (exact match) |
| `/usr/bin/python3` | Only `/usr/bin/python3`, or any `python3` resolved via PATH |

## Distribution

| Channel | Usage |
|---|---|
| **npm** | `npx -y mcp-sidecar` (platform binary via `@mcp-sidecar/*` optional packages) |
| **GitHub Releases** | Pre-built binaries attached to each release |
| **MCP Registry** | `io.github.lsequeiraa/mcp-sidecar` |

## License

MIT
