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
# Interactive mode (opens to most recent session)
meept chat

# Single message (uses oneshot_responses session)
meept chat "What's the weather like?"

# Send to specific session
meept chat --session session-abc123 "continue implementing that feature"

# Shorthand for --session


# Open TUI to specific session
meept chat --session session-abc123

# From stdin
echo "Hello world" | meept chat -
```

**Options:**
- `--session` - Target specific session by ID (canonical format: `session-XXXXXXXXXXXXXXXX`)
- `--project` - Bind session to named project
- `--nofence` - Disable path fencing for this session

**Behavior:**
- `meept chat` (no args) - Opens TUI to most recent session
- `meept chat "msg"` (no --session) - Sends to `oneshot_responses` session, prints response, exits
- `meept chat --session <id> "msg"` - Sends to existing session, prints response, exits (errors if session not found)
- `meept chat --session <id>` (no message) - Opens TUI targeted to that session

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

### `meept session` - Session Management

List and manage chat sessions. (`meept sessions` works as an alias.)

```bash
# List sessions
meept session list

# Create new session
meept session create

# Attach to existing session
meept session attach <session-id>

# Inspect
meept session get <session-id>
meept session messages <session-id>
meept session trace <session-id>

# Thread management within a session (see also: meept thread)
meept session needs-attention
```

### `meept jobs` - Scheduled Jobs

List scheduled jobs. (`meept tasks` is an alias.) Job scheduling is configured in `meept.json5` or via AI employees; there are no `jobs run/status/cancel` subcommands — use `meept queue status` for queue state.

```bash
# List scheduled jobs
meept jobs

# Queue state
meept queue status
```

### `meept memory` - Memory Operations

Search and manage long-term memory.

```bash
# List recent memories
meept memory

# Search memories
meept memory "authentication patterns"

# Vector search operations
meept memory vector --help

# Epistemic review workflow (auto-claims)
meept memory review          # list pending claims
meept memory promote <id>    # promote an auto-claim to confirmed
meept memory reject <id>     # reject an auto-claim
meept memory supersede <id>  # mark a claim superseded by a newer one

# Export
meept memory export
```

Note: there is no `memory stats` subcommand; statistics appear via the analytics commands.

### `meept task` - Task Management

Manage background tasks. (`meept tasks` is an alias for `meept jobs`, which lists scheduled jobs — not the same thing.)

```bash
# List tasks
meept task list

# Create task
meept task create --name "Fix bug" --description "Fix authentication bug"

# Get task details
meept task get <task-id>

# Delete / link / unlink
meept task delete <task-id>
meept task link <task-id> <session-id>
meept task unlink <task-id>
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

- `ctrl+x` — enter command mode.
- `ctrl+d` — open the pending changes review modal (j/k navigate, v view diff, a accept, r reject, esc close). See [change review](../workflows/change-review.md).
- `esc` — close the active menu or overlay (double `esc`/`ctrl+c` quits).

**Slash Commands** (type `/` in the input for autocomplete; full list via `/help`):

Core built-ins include: `/help`, `/new`, `/clear`, `/retry`, `/undo`, `/usage`, `/stop`, `/status`, `/vim`, `/session`, `/task`, `/tasks`, `/cancel`, `/amend`, `/interrupt`, `/diff`, `/model`, `/compact`, `/edit`, `/plan`, `/review`, `/project`, `/mcp`, `/skill`. Skills are also invocable as slash commands by name.

Note: MCP server enable/disable is managed through the interactive config editor (`meept config`, section "mcp servers") or HTTP — not a TUI menu.

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

# Filter by project
meept plans list --project my-app

# JSON output
meept plans list --json

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

### `meept tools` - Tool Management (removed)

The `meept tools` CLI command has been removed. To inspect available tools:

- TUI: `/help` lists slash commands; tool activity appears inline during agent runs.
- MCP: run `meept mcp-chat-server` to expose meept's tools to an external agent platform.

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

## Other Commands (Quick Reference)

Verified against the binary. Run `meept <command> --help` for flags.

| Command | Subcommands | Purpose |
|---------|-------------|---------|
| `meept analytics` | errors, export, models, summary | Agent performance and model metrics |
| `meept backup` | list, push | Database backups |
| `meept benchmark` | — | SWE-bench-style regression benchmarks |
| `meept bots` | — | Removed; see `meept agents` |
| `meept branch` | list, navigate, tree, summary | Session branches (disabled by default) |
| `meept cache` | clear, inspect, invalidate, status | Token cache management |
| `meept calendar` | auth, today | Google Calendar integration |
| `meept changes` | list, revert | Pending-change staging review |
| `meept cluster` | debug, init, join, keygen, leave, remote, start, status | P2P cluster mesh |
| `meept config` | get, list, oauth, set, sync | Config editor + dot-notation get/set (`rendering.ui_theme`, `llm.default_model`, …). `config oauth connect <provider>` runs subscription logins — providers: `github-models`, `google-oauth`, `google-calendar`, `xai-oauth` (SuperGrok), `openai-codex` (ChatGPT Plus/Pro), `anthropic-sub` (Claude Pro/Max). See [OAuth Providers](../workflows/auth.md). |
| `meept daemon` | restart, start, status, stop | Daemon lifecycle |
| `meept dispatch` | — | Dispatch tasks to cluster nodes |
| `meept halo` | — | HALO-style trace analysis |
| `meept improvements` | apply, list, skip | Improvement proposal workflow |
| `meept init` | — | Initialize AGENTS.md files for a project |
| `meept instructions` | — | Manage user instructions |
| `meept jobs` | — (aliases: `tasks`) | List scheduled jobs |
| `meept learning` | auto-train, consolidate, dataset-stats, feedback, list, snapshot, status, train | LoRA learning pipeline |
| `meept memory` | export, promote, reject, review, supersede, vector (+ root search) | Memory operations |
| `meept migrate` | — | Migrate local data stores to dual-DB layout |
| `meept mcp-chat-server` | — | Expose meept as an MCP server |
| `meept plans` | approve, confirm, list, reject, show | Plan lifecycle |
| `meept projects` | add, list, remove, status, sync | Project registry and worktrees |
| `meept prompts` | edit, list, show, validate | Prompt templates |
| `meept q` | analyze, status | Q Agent meta-optimization |
| `meept queue` | list, retry, status | Job queue |
| `meept routing` | by-model, recent | Inspect model routing decisions |
| `meept runtime` | restart, start, status, stop | Local LLM runtime processes |
| `meept selfimprove` | analyze, apply, detect, full-cycle, generate-fixes, reject, status, validate | Self-improvement cycle |
| `meept session` | attach, create, delete, detach, get, list, messages, needs-attention, trace | Chat sessions (alias: `sessions`) |
| `meept shadow` | adapters, examples, export, export-db, status | Shadow training |
| `meept skills` | archive, evolve, gaps, history, list, restore, run, show, stats | Skill system + closed-loop evolution |
| `meept status` | — | Daemon health |
| `meept sync` | pull, status | Peer backup sync |
| `meept task` | create, delete, get, link, list, unlink | Background tasks |
| `meept templates` | clear, invoke, list, show | Prompt templates |
| `meept thread` | current, delete, list, new, switch | Conversation threads in a session |
| `meept token` | generate, list, revoke | API tokens |
| `meept tts` | voices | Text-to-speech voices |
| `meept workers` | list, scale, status | Worker pool |

Developer-only: `meept dev` (config/model/test helpers), `meept completion` (shell completions), `meept version`.

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
# List jobs
meept jobs

# Check job status
meept queue status
```

### Memory Search

```bash
# Search for past authentication work
meept memory "authentication"
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