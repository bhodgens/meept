<div align="center">
  <img src="meept.jpg" alt="Meept" width="200"/>
  <h1>Meept</h1>
  <p><strong>Self-executing autonomous agent daemon with multi-agent orchestration, hybrid memory, and skill-based task execution.</strong></p>
</div>

---

Meept is a Go-based daemon that runs AI agents as background processes. It supports multi-agent collaboration, persistent memory, tool execution, and multiple frontends (TUI, Telegram, web). Agents can decompose complex tasks, route work to specialists, and maintain context across sessions and tries to retain as much compatibility as I found useful from eg. opencode or claude. 

## But What Is It, Really?

Meept is my feeble attempt at, "This irritates me, I could do better than this." I've been playing with chatbots since Python was new and ELIZA was barely known, even on IRC. Could I make something convincing? 

Instead of a chatbot, I need a useful tool, and I wanted to see if I could borrow the features I liked from the different tools, while exploring my apparent love for complex data queuing and routing, and perhaps stretching some of my performance engineering experience in the process. 

So I figured I'd do it in Golang, because the world needs more beauty - especially since Python somehow ended up popular. 
   
## Project Status

### What Works

| Component | Status | Notes |
|-----------|--------|-------|
| **Daemon Core** | Working | Full lifecycle, config, RPC server |
| **Agent Loop** | Working | Tool use, reasoning, iteration limits |
| **Multi-Agent** | Working | 8 specialist agents, routing, delegation |
| **CLI/TUI** | Working | Interactive chat, vim mode, markdown rendering |
| **LLM Client** | Working | OpenAI-compatible API, retry logic, budget tracking |
| **Tools** | Working | File ops, shell, web fetch, memory, tasks, platform |
| **Memory** | Working | Memvid + SQLite fallback, episodic/task/personality |
| **Job Queue** | Working | SQLite-backed, agent routing, priorities |
| **Sessions** | Working | Persistent sessions, LLM summarization |
| **Shadow Training** | Working | Interaction capture, few-shot injection |

### What's Partial or Stubbed

| Component | Status | Notes |
|-----------|--------|-------|
| **Security** | Partial | Permission system works; sanitizers exist but not integrated into agent loop |
| **Skills** | Partial | Discovery works; execution not wired up |
| **MCP Tools** | Stubbed | Protocol implemented; no runtime integration |
| **Self-Improve** | Stubbed | Detection works; full cycle not implemented |
| **Telegram** | Stubbed | Bot scaffolding only |
| **Web Server** | Stubbed | Basic structure; many endpoints TODO |
| **Calendar** | Stubbed | File exists; no integration |

## Quick Start

### Build from Source

```bash
# Clone and build
git clone https://github.com/caimlas/meept.git
cd meept
make go-build-all

# Or build individually
go build -o bin/meept-daemon ./cmd/meept-daemon
go build -o bin/meept ./cmd/meept
```

### Configuration

```bash
# Create config directory
mkdir -p ~/.meept

# Copy default configs
cp config/meept.toml ~/.meept/meept.toml
cp config/models.json5 ~/.meept/models.json5

# Edit models.json5 to add your API keys
# The daemon REQUIRES a valid models.json5 to start
```

### Run

```bash
# Terminal 1: Start daemon (foreground)
./bin/meept-daemon -f

# Terminal 2: Interactive chat
./bin/meept chat

# Or single message
./bin/meept chat "What files are in this directory?"

# Check status
./bin/meept status
```

## Architecture

```
                          meept (TUI)         Telegram         Web UI
                              |                  |                |
                              v                  v                v
                        +-----------+      +-----------+   +----------+
                        | RPC Client|      | Bot (stub)|   | HTTP(stub)
                        +-----------+      +-----------+   +----------+
                              \                  |              /
                               \                 |             /
                         Unix Socket JSON-RPC    |            /
                                \                |           /
                                 +-------> MessageBus <-----+
                                            (pub/sub)
                                               |
                     +-------------------------+-------------------------+
                     |                         |                         |
                     v                         v                         v
              Agent Registry             Job Queue              Memory Manager
              (8 specialists)           (SQLite)              (memvid + SQLite)
                     |                         |                         |
                     v                         v                         v
               Agent Loop               Worker Pool              Episodic/Task
              /     |     \                    |                   Memory
        LLM Client Tools Security         Job Processor
                    |
              Tool Registry
             /      |      \
       Builtin   Memory   Platform
       (file,    (store,  (agents,
        shell,   search)  delegate)
        web)
```

## Multi-Agent System

Meept runs 8 specialist agents that can discover and delegate to each other:

| Agent | Role | Tools |
|-------|------|-------|
| `dispatcher` | Intake, classify, route | All baseline |
| `chat` | General conversation | Baseline only |
| `coder` | File ops, shell, coding | file_*, shell_execute |
| `debugger` | Troubleshooting | file_read, shell_execute |
| `planner` | Task decomposition | task_*, memory_* |
| `analyst` | Research, summarization | web_fetch, memory_* |
| `committer` | Git operations | shell_execute (git) |
| `scheduler` | Job scheduling | scheduler tools |

### Agent Customization (AGENT.md)

Agents can be overridden or extended with `AGENT.md` files using YAML frontmatter — same pattern as skills. Discovery hierarchy (highest priority first):

```
.meept/agents/<id>/AGENT.md    # project-local
~/.meept/agents/<id>/AGENT.md  # user-global
~/.config/meept/agents/        # system-wide
config/agents/                 # bundled defaults
```

Example override for the coder agent (`~/.meept/agents/coder/AGENT.md`):
```yaml
---
id: coder
temperature: 0.1
max_iterations: 20
---
# Custom Coder Instructions
Always add type annotations. Prefer functional style.
```

Non-empty fields override programmatic defaults; tools are merged (union). A global `RULES.md` can inject behavior requirements into all agents, with structured JSON report parsing built into the dispatcher.

### Agent Discovery

Agents can discover coworkers via platform tools:

```
platform_agents  - List available agents and capabilities
platform_status  - Get system health
delegate_task    - Route task to specific agent
```

### Job Queue Routing

Jobs can target specific agents:
- Jobs with `agent_id` set are only claimable by that agent
- Unassigned jobs can be claimed by any capable agent
- Priority levels: low, normal, high, urgent

## Configuration

### Main Config: `~/.meept/meept.toml`

```toml
[daemon]
socket_path = "~/.meept/meept.sock"
log_level = "INFO"
data_dir = "~/.meept"

[llm.budget]
hourly_token_limit = 100000
daily_token_limit = 1000000

[memory]
enabled = true
data_dir = "~/.meept/memory"

[security]
allowed_paths = ["~/*"]
blocked_paths = ["~/.ssh/*", "~/.gnupg/*"]

# Optional integrations (currently stubs)
[telegram]
enabled = false

[web]
enabled = false
```

### Models Config: `~/.meept/models.json5` (REQUIRED)

```json5
{
  "model": "ollama/llama3.2",  // Default model
  "small_model": "ollama/llama3.2",

  "providers": {
    "ollama": {
      "api": "openai",
      "options": {
        "baseURL": "http://localhost:11434/v1"
      },
      "models": {
        "llama3.2": {
          "capabilities": ["code", "tool_use", "reasoning"],
          "input_cost": 0.0,
          "output_cost": 0.0,
          "context_limit": 128000
        }
      }
    },

    // Example: OpenRouter
    "openrouter": {
      "api": "openai",
      "options": {
        "baseURL": "https://openrouter.ai/api/v1",
        "apiKey": "${OPENROUTER_API_KEY}"
      },
      "models": {
        "claude-3-sonnet": {
          "name": "anthropic/claude-3-sonnet",
          "capabilities": ["code", "reasoning", "tool_use"],
          "input_cost": 3.0,
          "output_cost": 15.0,
          "context_limit": 200000
        }
      }
    }
  }
}
```

## CLI Commands

```bash
# Chat
./bin/meept chat              # Interactive TUI
./bin/meept chat "message"    # Single message
./bin/meept chat -            # Read from stdin

# Daemon
./bin/meept status            # Check daemon status

# Sessions
./bin/meept sessions list
./bin/meept sessions attach <id>

# Jobs
./bin/meept jobs list
./bin/meept jobs status <id>

# Memory
./bin/meept memory search "query"

# Tasks
./bin/meept tasks list
./bin/meept tasks create "title" "description"
```

## TUI Features

The interactive chat interface includes:

- **Vim keybindings**: `i` insert, `Esc` normal, `:w` send
- **Markdown rendering**: Syntax highlighting in responses
- **Session management**: `Ctrl+N` new, `Ctrl+R` rename
- **Sidebar**: Agent activity, workers, tasks, memory
- **Animation**: Visual dispatch animation (configurable)

## Memory System

Three memory types with memvid (primary) + SQLite (fallback):

| Type | Purpose | Backend |
|------|---------|---------|
| **Episodic** | Conversation history | memvid or SQLite+FTS5 |
| **Task** | Domain knowledge | memvid or SQLite+FTS5 |
| **Personality** | Self-model, preferences | Markdown files |

Memory is automatically injected into agent context before each turn.

## Tools

### Builtin Tools

| Tool | Description |
|------|-------------|
| `file_read` | Read file contents |
| `file_write` | Write/create files |
| `file_delete` | Delete files |
| `list_directory` | List directory contents |
| `shell_execute` | Run shell commands (60s timeout) |
| `web_fetch` | HTTP fetch (30s timeout, 100KB limit) |
| `memory_store` | Store memory |
| `memory_search` | Search memories |
| `memory_get_context` | Get relevant context |
| `task_create` | Create task |
| `task_get` | Get task by ID |
| `task_list` | List tasks |
| `task_update` | Update task |
| `platform_status` | Get system status |
| `platform_agents` | List available agents |
| `platform_tools` | List available tools |
| `delegate_task` | Route task to agent |

## Security

Currently implemented:

- **Path validation**: Allowed/blocked path patterns
- **Risk assessment**: 4-level risk scoring (low/medium/high/critical)
- **Permission checks**: Before tool execution
- **Audit logging**: Permission decisions logged

**Not yet integrated** (code exists but not wired):
- Input sanitization (prompt injection detection)
- Output monitoring
- Shell command scanning (tirith)

## Development

```bash
# Run all tests
go test ./... -v

# Run specific package tests
go test ./internal/agent/... -v
go test ./internal/tui/... -v

# Run with race detection
go test -race ./...

# Build with debug symbols
go build -gcflags="all=-N -l" -o bin/meept ./cmd/meept

# TUI testing
agent-tui ./bin/meept chat
```

## Project Structure

```
cmd/
  meept/           # CLI application
  meept-daemon/    # Daemon application
  animation/       # Standalone animation demo
internal/
  agent/           # Agent loop, specs, executor, conversation
  bus/             # Message bus (pub/sub)
  clawskills/      # Third-party skill client
  comm/
    telegram/      # Telegram bot (stub)
    web/           # HTTP server (stub)
  config/          # Configuration loading
  daemon/          # Daemon lifecycle, components
  llm/             # LLM client, budget, resolver
  memory/          # Memory manager, episodic, task
  queue/           # Job queue (SQLite)
  rpc/             # JSON-RPC server
  scheduler/       # Job scheduling
  security/        # Security engine, sanitizers (partial)
  selfimprove/     # Self-improvement (stub)
  session/         # Session management
  shadow/          # Shadow training
  skills/          # Skill discovery, registry
  task/            # Task registry
  tools/           # Tool registry, builtins
  tui/             # Bubble Tea TUI
  worker/          # Worker pool
pkg/
  security/        # Permission system
config/            # Configuration templates
archive/
  python-legacy/   # Archived Python implementation
```

## Known Issues

1. **Daemon foreground only**: No backgrounding support yet
2. **No hot reload**: Config changes require restart
3. **Security gaps**: Input sanitizers not integrated into agent loop
4. **MCP not active**: Protocol exists but tools not executed
5. **Skills execution**: Discovery works but execution path incomplete
6. **External integrations**: Telegram, web, calendar are stubs

## Dependencies

| Package | Purpose |
|---------|---------|
| `github.com/charmbracelet/bubbletea` | TUI framework |
| `github.com/charmbracelet/lipgloss` | TUI styling |
| `github.com/charmbracelet/glamour` | Markdown rendering |
| `github.com/mattn/go-sqlite3` | SQLite driver |
| `github.com/pelletier/go-toml/v2` | TOML parsing |
| `github.com/tidwall/jsonc` | JSON5 parsing |

## License

MIT
