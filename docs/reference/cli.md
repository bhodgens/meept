# CLI Reference

Meept provides a comprehensive command-line interface for interacting with the daemon and managing various aspects of the system.

## Overview

The CLI binary is `./bin/meept` and communicates with the daemon via Unix socket JSON-RPC. Running `meept` without arguments launches the interactive TUI.

## Global Flags

All commands support these global flags:

```bash
--socket, -s     Unix socket path (default: ~/.meept/meept.sock)
--state-dir, -d  State directory (default: ~/.meept)
--debug          Enable debug output (--debug or --debug=file, use '-' for stderr)
```

## Commands

### `meept chat` - Interactive Chat

Launch interactive chat interface or send a single message.

```bash
# Interactive mode
meept chat

# Single message
meept chat "What's the weather like?"

# From stdin
echo "Hello world" | meept chat
```

**Options:**
- `--stdin` - Read message from stdin
- `--session-id` - Use specific session ID
- `--agent-id` - Target specific agent (e.g., coder, planner)

### `meept status` - Daemon Status

Check daemon status and health.

```bash
meept status
```

**Returns:**
- Daemon status (running/stopped)
- Version information
- Uptime
- Registered RPC methods
- Bus statistics

### `meept sessions` - Session Management

List and manage chat sessions.

```bash
# List sessions
meept sessions list

# Create new session
meept sessions create

# Attach to existing session
meept sessions attach <session-id>
```

### `meept jobs` - Job Management

Manage scheduled and background jobs.

```bash
# List jobs
meept jobs list

# Get job status
meept jobs status <job-id>

# Run job immediately
meept jobs run <job-id>

# Cancel job
meept jobs cancel <job-id>
```

### `meept memory` - Memory Operations

Search and manage long-term memory.

```bash
# Search memories
meept memory search "authentication patterns"

# Memory statistics
meept memory stats

# Store memory
meept memory store --content "Important decision" --type episodic
```

### `meept tasks` - Task Management

Manage background tasks.

```bash
# List tasks
meept tasks list

# Create task
meept tasks create --name "Fix bug" --description "Fix authentication bug"

# Get task details
meept tasks get <task-id>

# Update task
meept tasks update <task-id> --status completed
```

### `meept selfimprove` - Self-Improvement System

Run automated code improvement cycles.

```bash
# Detect issues
meept selfimprove detect

# Run full improvement cycle
meept selfimprove full-cycle

# Check improvement status
meept selfimprove status
```

### `meept config` - Configuration Management

Interactive configuration editor and get/set operations. This replaces the old `meept models` command.

```bash
# Open interactive config editor TUI
meept config

# Open TUI at a specific section
meept config <section>

# List config file paths and status
meept config list

# Get a config value
meept config get <keypath>

# Set a config value
meept config set <keypath> <value>
```

**Sections:** daemon, transport, llm, models, agents, memory, security, mcp, client/tui, scheduler, stt (primary), plus ~20 advanced sections.

### `meept` TUI - Interactive Mode

Running `meept chat` with no message argument opens the interactive TUI. In addition to typing messages, several keybindings and slash commands open management menus.

**Keybindings:**

- `ctl-x o` — open the mcp servers menu (same as the `/mcp` slash command).
- `ctl-x m` — open the memory menu.
- `esc` — close the active menu or overlay.

**Slash Commands:**

- `/mcp` — open the mcp servers menu. Columns shown: `en` (enabled toggle glyph), `server`, `status`, `reqs`, `errors`, `description`. Press `e` to toggle enabled on the selected row, `r` to force a refresh from the daemon, arrow keys to move selection, and `esc` to close.
- `/memory` — open the memory menu.

See [tool routing: mcp default catalog](../workflows/tool-routing.md#mcp-default-catalog) for details on the catalog the menu manages.

**Examples:**
```bash
# Open models section (replaces old `meept models`)
meept config models

# Get the default model
meept config get llm.default_model

# Set a config value
meept config set llm.default_model "claude-opus-4-6"

# List all config files
meept config list

# Open STT configuration section
meept config stt

# Check if STT is enabled
meept config get stt.enabled

# Change STT engine
meept config set stt.engine "native"
```

### `meept agents` - AI Employee Management

The unified `meept agents` namespace manages AI employees — persistent, constitution-bound autonomous agents. This replaces the legacy `meept bots` commands (hard cutover). See [AI Employees](../workflows/employees.md) for the full feature spec.

#### Lifecycle

```bash
meept agents list                              # all employees, status, tier, drift score
meept agents show <id>                         # full definition: constitution, state, goals, recent findings
meept agents create <definition.json5>         # validates constitution; refuses without one
meept agents update <id> <definition.json5>
meept agents delete <id>                       # stops + deletes; confirms unless --force
meept agents pause <id>                        # operator pause
meept agents resume <id>                       # operator resume (only un-pause path)
meept agents amend <id> --field=<key> <value>  # propose constitution amendment (routes to Plan signoff)
```

#### Migration

```bash
meept agents migrate                           # scans ~/.meept/bots/*.json
meept agents migrate --apply <id>              # write proposed constitution to disk
```

#### Goals

```bash
meept agents goals [--employee=<id>]           # list goals with health (red/yellow/green)
meept agents goal <goal-id>                    # goal detail + active plan + history
meept agents goal <goal-id> --approve <plan-id>
meept agents goal <goal-id> --reject <plan-id> --reason="..."
```

#### Audit

```bash
meept agents audit <id> [--since=<dur>]        # recent findings, severity, resolution
meept agents audit <id> --resolve <finding-id> --as=false_positive
```

Legacy `meept bots` commands are removed. Scripts that call `meept bots` get an error pointing to `meept agents --help` and [AI Employees](../workflows/employees.md).

### `meept plans` - Plan Management

Manage plans through their lifecycle: creation, approval, execution tracking, and sign-off.

```bash
# List all plans
meept plans list

# Filter by state
meept plans list --state pending_approval

# Filter by project
meept plans list --project my-app

# Show plan details
meept plans show plan-a1b2c3d4
meept plans show plan-a1b2c3d4 --verbose

# Approve a pending plan
meept plans approve plan-a1b2c3d4
meept plans approve plan-a1b2c3d4 --comment "Looks good, proceed"

# Reject a pending plan
meept plans reject plan-a1b2c3d4
meept plans reject plan-a1b2c3d4 --comment "Needs more detail on phase 2"

# Confirm sign-off on a completed plan
meept plans confirm plan-a1b2c3d4
meept plans confirm plan-a1b2c3d4 --comment "All deliverables verified"
```

**Subcommands:**
- `list` - List plans, optionally filtered by `--state` or `--project`
- `show <id>` - Display plan details with phases and progress
- `approve <id>` - Approve a pending plan (triggers task synthesis)
- `reject <id>` - Reject a pending plan with optional `--comment`
- `confirm <id>` - Confirm sign-off on a completed plan

### `meept tools` - Tool Management

List registered tools.

```bash
meept tools
```

**Shows:**
- Tool names and descriptions
- Parameter schemas
- Risk levels
- Agent access

### `meept daemon` - Daemon Management

Start and stop the daemon process.

```bash
# Start daemon (foreground)
meept daemon start

# Start daemon (background)
meept daemon start --daemon

# Stop daemon
meept daemon stop

# Restart daemon
meept daemon restart
```

### `meept runtime` - Local LLM Runtime Management

Manage local LLM runtime subprocesses (llama.cpp, MLX). Providers must have a `lifecycle` block in `config/models.json5` and a loopback `baseURL` to be eligible.

```bash
# Status (default provider is "local")
meept runtime status
meept runtime status local
meept runtime status local --format json

# Start (waits for health by default)
meept runtime start [provider]
meept runtime start [provider] --wait=false

# Stop / restart
meept runtime stop [provider]
meept runtime restart [provider]
```

Status output (text):

```
Runtime local: running (PID: 12345)
  Health endpoint:  http://127.0.0.1:8080/health
  PID file:         /Users/me/.meept/run/local.pid
  Process group:    llama-cpp:127.0.0.1:8080
  In-use models:    lfm-code, lfm-thinking-claude
  Would start:      true
```

Status output (JSON) adds three fields:

| Field | Description |
|-------|-------------|
| `process_group` | Endpoint key (`<runtime>:<host>:<port>`). Shared across all providers on the same port |
| `in_use_models` | Subset of the provider's models referenced by enabled agents, model slots, or aliases |
| `would_start` | `true` when `auto_start: true` and at least one in-use model is present |

Use `--format json` for scripting; the text form is for humans.

### `meept queue` - Queue Management

View and manage job queue.

```bash
# Queue status
meept queue status

# List queued jobs
meept queue list

# Retry failed job
meept queue retry <job-id>
```

### `meept workers` - Worker Management

Manage worker pool.

```bash
# Worker status
meept workers

# Scale workers
meept workers scale 5
```

### `meept version` - Version Information

Display version information.

```bash
meept version
```

### `meept help` - Help System

Get help for any command.

```bash
# General help
meept help

# Command-specific help
meept help chat
meept help status
```

## Examples

### Interactive Development Session

```bash
# Start daemon
meept daemon start --daemon

# Check status
meept status

# Start coding session
meept chat "Please help me implement authentication middleware"
```

### Scheduled Task

```bash
# Create scheduled backup job
meept jobs create --name "Daily backup" --schedule "0 2 * * *" --type shell --command "/usr/bin/backup.sh"

# Check job status
meept jobs list
```

### Memory Search

```bash
# Search for past authentication work
meept memory search "authentication" --type task --limit 10
```

## Exit Codes

- `0` - Success
- `1` - General error
- `2` - Daemon not running
- `3` - Invalid command or arguments
- `4` - Permission denied
- `5` - Network/connection error

## Configuration

The CLI reads configuration from:
- `~/.meept/meept.toml` - Main configuration
- `~/.meept/cli.toml` - CLI-specific settings

Key CLI configuration options:

```toml
[cli]
default_socket = "~/.meept/meept.sock"
default_state_dir = "~/.meept"
log_level = "info"
color_output = true

[cli.chat]
default_agent = "chat"
auto_attach_session = true

[cli.memory]
default_search_limit = 10
search_timeout = 30
```