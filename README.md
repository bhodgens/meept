<div align="center">
  <img src="meept.jpg" alt="Meept" width="200"/>
  <h1>Meept</h1>
  <p><strong>Self-executing autonomous agent daemon with multi-agent orchestration, hybrid memory, and skill-based task execution.</strong></p>
</div>

---

Meept is a Go-based daemon that runs AI agents as background processes. It supports multi-agent collaboration, persistent memory, tool execution, and multiple frontends (TUI, Telegram, web). Unlike single-session CLI tools, Meept runs continuously as a daemon, enabling persistent context, job scheduling, and multi-agent workflows.

## What Makes Meept Different

| Tool | Approach | Meept's Advantage |
|------|----------|-------------------|
| **Claude Code** | Single-session CLI | **Daemon architecture** with persistent memory and job scheduling |
| **OpenCode** | Terminal-only execution | **Multi-agent collaboration** with specialist agents routing work |
| **Cursor** | IDE integration | **Background processing** with continuous learning and self-improvement |

Meept combines the best features from these tools while adding unique capabilities:
- **Daemon-first**: Runs continuously with persistent state
- **Multi-agent**: 8 specialist agents that collaborate and delegate
- **Hybrid memory**: memvid + SQLite fallback for robust context retention
- **Self-improvement**: Automated code improvement and learning
- **Skill ecosystem**: Third-party skills via ClawSkills registry

## But What Is It, Really?

Meept is my feeble attempt at, "This irritates me, I could do better than this." I've been playing with chatbots since Python was new and ELIZA was barely known, even on IRC. Could I make something convincing?

Instead of a chatbot, I need a useful tool, and I wanted to see if I could borrow the features I liked from the different tools, while exploring my apparent love for complex data queuing and routing, and perhaps stretching some of my performance engineering experience in the process.

So I figured I'd do it in Golang, because the world needs more beauty - especially since Python somehow ended up popular.

## Quick Start

### 1. Build from Source

```bash
git clone https://github.com/caimlas/meept.git
cd meept
make go-build-all
```

### 2. Configure API Keys

```bash
mkdir -p ~/.meept
cp config/meept.toml ~/.meept/meept.toml
cp config/models.json5 ~/.meept/models.json5

# Edit ~/.meept/models.json5 to add your API keys
# The daemon REQUIRES a valid models.json5 to start
```

### 3. Run Meept

```bash
# Terminal 1: Start daemon
./bin/meept-daemon -f

# Terminal 2: Interactive chat
./bin/meept chat

# Or single message
./bin/meept chat "What files are in this directory?"
```

**Need more help?** See [Getting Started](docs/getting-started/) for detailed setup instructions.

## Feature Highlights

| Feature | Status | Description | Docs |
|---------|--------|-------------|------|
| **Daemon Core** | ✅ Working | Full lifecycle, config, RPC server | [Architecture](docs/concepts/architecture.md) |
| **Multi-Agent System** | ✅ Working | 8 specialist agents with routing | [Multi-Agent](docs/concepts/multi-agent.md) |
| **Memory Management** | ✅ Working | Hybrid memvid + SQLite memory | [Memory](docs/concepts/memory.md) |
| **Tool Execution** | ✅ Working | File ops, shell, web fetch, memory | [Tools](docs/concepts/tools.md) |
| **Interactive TUI** | ✅ Working | Vim keybindings, markdown rendering | [CLI Reference](docs/reference/cli.md) |
| **Models CLI** | ✅ Working | Interactive provider/model management | [Models CLI](docs/reference/models-cli.md) |
| **Job Scheduling** | ✅ Working | SQLite-backed queue with priorities | [Workflows](docs/workflows/) |
| **Skills System** | 🔄 Partial | Discovery works; execution in progress | [Skills](docs/concepts/skills.md) |
| **Security Engine** | 🔄 Partial | Permission system; sanitizers not wired | [Configuration](docs/configuration/) |
| **Self-Improvement** | 🚧 Stubbed | Detection works; full cycle planned | [Workflows](docs/workflows/) |

## Project Status

Meept is **actively developed** with a working core system. The daemon, multi-agent orchestration, memory system, and basic tools are fully functional. Several advanced features are in progress or planned.

**What's working:** Daemon lifecycle, agent loop, 8 specialist agents, hybrid memory, job queue, interactive TUI, tool execution, configuration system

**In progress:** Skills execution, security integration, external integrations (Telegram, web)

**Planned:** Self-improvement system, MCP tool integration, calendar integration

See [Concepts](docs/concepts/) for detailed architecture documentation.

## Architecture Overview

Meept uses a multi-layer architecture with specialist agents:

```
User Interfaces → RPC Server → Message Bus → Agent Registry → Tool Execution
    (TUI/CLI)     (JSON-RPC)   (pub/sub)    (8 specialists)   (file/shell/web)
```

**Key components:**
- **Daemon**: Background process with RPC server
- **Message Bus**: Pub/sub for internal communication
- **Agent Registry**: 8 specialist agents with routing
- **Memory System**: Hybrid memvid + SQLite storage
- **Tool Registry**: Built-in and extensible tools
- **Job Queue**: SQLite-backed task scheduling

For detailed architecture diagrams and component descriptions, see [Architecture](docs/concepts/architecture.md).

## Multi-Agent System

Meept runs 8 specialist agents that collaborate on complex tasks:

| Agent | Role | Specialization |
|-------|------|---------------|
| `dispatcher` | Intake & routing | Task classification and delegation |
| `chat` | General conversation | Natural language interaction |
| `coder` | Code operations | File editing, shell commands, programming |
| `debugger` | Troubleshooting | Problem diagnosis and fixing |
| `planner` | Task decomposition | Breaking down complex problems |
| `analyst` | Research & analysis | Data gathering and summarization |
| `committer` | Git operations | Version control and collaboration |
| `scheduler` | Job management | Task scheduling and monitoring |

Agents can discover each other using platform tools (`platform_agents`, `platform_status`) and delegate work using `delegate_task`. Each agent has customized instructions and tool access based on its specialization.

Learn more: [Multi-Agent System](docs/concepts/multi-agent.md)

## Configuration

Meept uses a hierarchical configuration system:

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
```

### Models Config: `~/.meept/models.json5` (REQUIRED)

**Quick Setup:** Use `meept models setup` for interactive configuration instead of manual editing.
Meept requires a valid models configuration with API keys. See the template in `config/models.json5`.

### Agent Customization
Agents can be customized using `AGENT.md` files with YAML frontmatter. Discovery hierarchy:
- `.meept/agents/<id>/AGENT.md` (project-local)
- `~/.meept/agents/<id>/AGENT.md` (user-global)
- `~/.config/meept/agents/` (system-wide)
- `config/agents/` (bundled defaults)

Full configuration details: [Configuration Guide](docs/configuration/)

## CLI Commands

```bash
# Chat and interaction
./bin/meept chat              # Interactive TUI
./bin/meept chat "message"    # Single message
./bin/meept status            # Daemon status

# Session management
./bin/meept sessions list     # List sessions
./bin/meept sessions attach <id>  # Attach to session

# Job and task management
./bin/meept jobs list         # List jobs
./bin/meept tasks list        # List tasks

# Memory operations
./bin/meept memory search "query"  # Search memories

# Skills system
./bin/meept clawskills list   # List available skills
./bin/meept clawskills search "query"  # Search skills

# Self-improvement
./bin/meept selfimprove detect  # Detect improvement opportunities
```

Complete reference: [CLI Reference](docs/reference/cli.md)

## Development

```bash
# Run tests
go test ./... -v

# Run with race detection
go test -race ./...

# Build debug version
go build -gcflags="all=-N -l" -o bin/meept ./cmd/meept

# TUI testing with agent-tui (https://lib.rs/crates/agent-tui)
agent-tui ./bin/meept chat
```

## Documentation

- **[Getting Started](docs/getting-started/)**: Installation and first steps
- **[Concepts](docs/concepts/)**: Architecture, multi-agent system, memory, tools
- **[Configuration](docs/configuration/)**: Setup and customization guide
- **[Workflows](docs/workflows/)**: Common usage patterns and features
- **[Reference](docs/reference/cli.md)**: CLI commands and API documentation

## Project Structure

```
cmd/meept/           # CLI application
cmd/meept-daemon/    # Daemon application
internal/
  agent/             # Agent loop, planning, execution
  bus/               # Message bus (pub/sub)
  llm/               # LLM client and resolution
  memory/            # Memory management
  tools/             # Tool registry and builtins
  security/          # Security engine
  skills/            # Skill system
  selfimprove/       # Self-improvement system
config/              # Configuration templates
```

## Contributing

Meept is an open-source project. Contributions are welcome! Please see the contributing guidelines for details.

## License

MIT