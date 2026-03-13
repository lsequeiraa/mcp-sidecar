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
- **Reliable cleanup** -- All child processes are terminated when the server exits
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
| `stop` | Terminate a process (graceful, then force) | `id` | `{ id, exitCode }` |
| `list` | List all managed processes | -- | `[{ id, name, pid, state, uptime }]` |
| `output` | Get buffered stdout/stderr | `id`, `tail?` | `{ stdout, stderr }` |
| `send` | Write to a process's stdin | `id`, `input` | `{ ok }` |
| `status` | Get detailed state of one process | `id` | `{ id, name, pid, state, exitCode, uptime, outputSize }` |

### Process States

| State | Meaning |
|---|---|
| `running` | Process is alive |
| `exited` | Process terminated normally |
| `failed` | Process terminated with non-zero exit code |
| `killed` | Process was stopped via the `stop` tool |

## Configuration

All configuration is via environment variables (all optional):

| Variable | Default | Description |
|---|---|---|
| `SIDECAR_MAX_PROCESSES` | `10` | Maximum concurrent processes |
| `SIDECAR_BUFFER_SIZE` | `1048576` (1MB) | Output buffer size per process in bytes |
| `SIDECAR_KILL_TIMEOUT` | `5000` | Milliseconds to wait between SIGTERM and SIGKILL |
| `SIDECAR_ALLOWED_EXECUTABLES` | -- | Comma-separated allowlist of executables (enables secure mode) |
| `SIDECAR_BLOCKED_PATTERNS` | -- | Comma-separated regex patterns to reject commands |
| `SIDECAR_AUDIT_LOG` | -- | Audit log directory, or `true` (cwd) / `temp` (OS temp dir) |

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
| **npm** | `npx -y mcp-sidecar` (downloads platform binary on install) |
| **GitHub Releases** | Pre-built binaries attached to each release |
| **MCP Registry** | `io.github.lsequeiraa/mcp-sidecar` |

## License

MIT
