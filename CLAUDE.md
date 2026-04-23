# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build & Development Commands

```bash
# Build daemon
go build -o bin/meept-daemon ./cmd/meept-daemon

# Build CLI
go build -o bin/meept ./cmd/meept

# Run all tests
go test ./... -v

# Run specific package tests
go test ./internal/agent/... -v
go test ./internal/clawskills/... -v

# Run tests with coverage
go test ./... -coverprofile=coverage.out
go tool cover -html=coverage.out

# Run tests with race detection
go test -race ./...

# TUI testing with agent-tui (https://lib.rs/crates/agent-tui)
agent-tui ./bin/meept chat

# Start daemon (foreground)
./bin/meept-daemon -f

# Or use make targets
make go-daemon         # Build and start daemon
make go-daemon-debug   # Build and start with debug logging

# CLI commands
./bin/meept status
./bin/meept chat "What's the weather like?"
./bin/meept chat  # Interactive TUI mode

# macOS MenuBar app (macOS only)
make menubar                   # Build menubar app (release)
make menubar-install           # Build and install to /Applications
make menubar-clean             # Clean menubar build artifacts
# cd menubar
# swift build                    # Build menubar app
# swift run         # Run menubar app
# cp -r  # Install

# Models management
./bin/meept models                    # Interactive model management
./bin/meept models list               # List configured models
./bin/meept models providers          # List available providers
./bin/meept models add                # Add new provider/model
./bin/meept models remove <ref>       # Remove model
./bin/meept models set-default <ref>  # Set default model
./bin/meept models credentials        # Manage API credentials

# ClawSkills commands
./bin/meept clawskills search "query"
./bin/meept clawskills install <slug>
./bin/meept clawskills list

# Self-improvement commands
./bin/meept selfimprove detect
./bin/meept selfimprove full-cycle
./bin/meept selfimprove status
```

## Architecture Overview

Meept is a **Go daemon** with skill-based task orchestration, LLM integration, memory management, and external integrations.

### Request Flow

```
User Input (CLI/Telegram/Web/MenuBar)
    → CommServer (Unix socket JSON-RPC) OR HTTP REST API
    → MessageBus (pub/sub)
    → AgentLoop
        → Planner.Decompose() → TaskSteps
        → CollaborativePlanner (review/approval workflow)
        → WorkspaceManager (git-backed task tracking)
        → SecurityEngine.Check()
        → Tool execution
        → Memory injection
    → Response
```

### Key Components

| Layer | Go Packages |
|-------|-------------|
| **Server Core** | `cmd/meept-daemon`, `internal/daemon`, `internal/rpc`, `internal/bus` |
| **Agent** | `internal/agent` (loop, orchestrator, planner, collaborative, workspace) |
| **Security** | `internal/security` (engine, sanitizer, tirith, tls) |
| **LLM** | `internal/llm` (client, resolver, budget, providers) |
| **Skills** | `internal/skills` (discovery, registry, parser, models) |
| **Memory** | `internal/memory` (manager, episodic, task, consolidation) |
| **Tools** | `internal/tools` (registry, builtin/*, mcp) |
| **Code Intel** | `internal/code/ast` (tree-sitter parser + symbol extraction), `internal/code/lsp` (LSP client/manager), `internal/code/tools` (ast_* and lsp_* agent tools) |
| **ClawSkills** | `internal/clawskills` (client, installer, security, index) |
| **Self-Improve** | `internal/selfimprove` (controller, detector, analyzer, generator, validator, applier) |
| **Scheduler** | `internal/scheduler` (scheduler, jobs) |
| **External** | `internal/comm/telegram`, `internal/comm/web`, `internal/calendar` |
| **CLI** | `cmd/meept` (chat, status, jobs, memory, clawskills, selfimprove) |
| **MenuBar** | `menubar/` (SwiftUI app), `internal/comm/http` (REST API) |
| **Metrics** | `internal/metrics` (store, collector) |

### Skill/Model Resolution

Skills declare `requires: [code, reasoning]` in YAML frontmatter; models declare `capabilities: [code, tool_use]` in `config/models.json5`. The resolver (`internal/llm/resolver.go`) finds the cheapest model satisfying requirements.

### Security Layers

1. **InputSanitizer**: Prompt injection pattern detection
2. **SecurityEngine**: SQLite-backed permission checks, tool gating, audit log
3. **Tirith**: Pre-execution shell command scanning
4. **TLS**: Server/client TLS configuration helpers

## Configuration

- **Main config**: `~/.meept/meept.toml` (see `config/meept.toml` for template)
- **Models**: `config/models.json5` (JSON5 format with capability tags)
- **Client**: `~/.meept/client.json5` (menubar app settings)
- **Presets**: `config/presets.json5` (model presets), `~/.meept/presets.json5` (user overrides)
- **MCP servers**: `~/.meept/mcp_servers.json`
- **Metrics DB**: `~/.meept/metrics.db` (SQLite time-series storage)
- **launchd Plist**: `~/Library/LaunchAgents/com.caimlas.meept-daemon.plist` (macOS)

### Programmatic component options

- `ShellExecuteTool.SetKnownSafeCommands([]string)` — base command names treated
  as `RiskMedium` instead of the default `RiskHigh` for unknown commands. Use for
  project-local CLIs (`mytool`, `mycli`) that the operator has vetted.
- `LLMClassifierConfig.Timeout time.Duration` — per-classification HTTP timeout;
  defaults to 5 s when unset.
- `ContextFirewall.Stats()` returns `FirewallStats` with counters for
  `SummarizationFailures`, `DroppedMessages`, and `DropEvents` so operators
  can monitor context-pressure indicators.

## Skills Discovery

Three-tier with priority shadowing:
```
.meept/skills/              # Project-local (highest)
~/.meept/skills/            # User-global
~/.config/meept/skills/     # System-wide
~/.meept/clawskills/        # Third-party (claw: prefix)
```

## Multi-Agent Architecture

Meept uses a multi-agent architecture where specialist agents handle different types of tasks:

| Agent ID | Role | Purpose |
|----------|------|---------|
| `dispatcher` | Dispatcher | Intake, classify, route to specialists |
| `chat` | Executor | General conversation |
| `coder` | Executor | File ops, shell, coding tasks |
| `debugger` | Executor | Troubleshooting, bug fixing |
| `planner` | Executor | Task decomposition, planning |
| `analyst` | Executor | Research, data analysis |
| `committer` | Executor | Git operations |
| `scheduler` | Executor | Job scheduling |

### Coworker Awareness

Agents discover and delegate to each other using platform tools:
- `platform_agents`: List available agents and their capabilities
- `platform_status`: Get platform health status
- `platform_tools`: List registered tools
- `delegate_task`: Route a task to a specific agent

### Job Queue Routing

Jobs can be targeted to specific agents via `agent_id`:
- If `agent_id` is set, only that agent can claim the job
- If `agent_id` is empty, any agent with matching capabilities can claim
- Jobs are prioritized by: targeted agent match > priority > creation time

## Code Conventions

- Go 1.22+
- Standard library preferred where possible
- `log/slog` for structured logging
- Context propagation throughout
- Interfaces for testability
- Table-driven tests

## UI Conventions

- **All UI element text must be explicitly lowercase** (e.g., "switch" not "Switch", "ok" not "OK")
- This applies to: button labels, menu items, tooltips, status messages, dialog titles, hints
- for TUI, use bubblezone library to help with positioning.
- Default to making elements clickable for switching context, if possible.

## Coding Practices

- Always implement complete wired features, do not leave stub code or partial implementations
- Always check your work

## Project Structure

```
cmd/
  meept/           # CLI application
  meept-daemon/    # Daemon application
  gendoc/          # Documentation generator tool
internal/
  agent/           # Agent loop, planner, workspace, collaborative
  bus/             # Message bus (pub/sub)
  calendar/        # Google Calendar integration
  clawskills/      # Third-party skill registry
  comm/
    telegram/      # Telegram bot
    web/           # HTTP API server
    http/          # REST API for menubar app (NEW)
  config/          # Configuration loading
  daemon/          # Daemon lifecycle
  llm/             # LLM client and resolution
  memory/          # Memory management (SQLite+FTS5)
  metrics/         # Metrics storage and collection (NEW)
  registry/        # Service registry
  rpc/             # JSON-RPC server
  scheduler/       # Job scheduling
  security/        # Security engine, sanitizer, tirith
  selfimprove/     # Self-improvement system
  skills/          # Skill discovery and parsing
  tools/           # Tool registry and builtins
config/            # Configuration templates
  presets.json5    # Model presets (NEW)
docs/              # MkDocs documentation site
menubar/           # macOS MenuBar app (NEW)
  MeeptMenuBar/    # Main SwiftUI app
  Views/           # Menu, Settings, Analytics views
  Services/        # APIClient, DaemonController, ConfigService
  Models/          # Data models and presets
tests/             # Integration tests
archive/           # Legacy Python code (reference only)
```

## macOS MenuBar App

The menubar app provides native macOS integration for monitoring and controlling Meept:

### Features
- **Status menu**: Shows daemon running/stopped state with uptime
- **Daemon control**: Start, stop, restart via launchd
- **Settings**: Edit client.json5, models.json5, manage agents
- **Analytics**: Live metrics dashboard with historical charts

### Architecture
```
MenuBar App (SwiftUI)
    ↓ HTTP REST (localhost:8081)
internal/comm/http/
    ↓
Daemon components
```

### HTTP API Endpoints

**Config:**
- `GET/POST /api/v1/config/client` - Client configuration
- `GET/POST /api/v1/config/models` - Models configuration
- `GET/POST/DELETE /api/v1/config/agents/:id` - Agent management

**Daemon:**
- `GET /api/v1/daemon/status` - Running state, PID, uptime
- `POST /api/v1/daemon/restart` - Graceful restart

**Metrics:**
- `GET /api/v1/metrics/live` - Current metrics snapshot
- `GET /api/v1/metrics/historical?from=&to=&resolution=` - Historical data

### Model Presets

Built-in presets for common tasks:
- `development` (temp: 0.3) - Balanced for coding
- `debugging` (temp: 0.2) - Methodical troubleshooting
- `planning` (temp: 0.4) - Structured thinking
- `creative` (temp: 0.9) - High creativity
- `research` (temp: 0.5) - Analytical tasks
- `fast` - Quick responses
- `detailed` - Comprehensive answers

## Documentation

Meept uses MkDocs Material for its documentation site. The site is built from markdown files under `docs/`.

### Documentation Commands

```bash
make docs-serve       # Start local dev server (auto-reload)
make docs-build       # Build static site to site/
make docs-generate    # Generate reference docs from Go source
```

### Documentation Structure

| Directory | Purpose |
|-----------|---------|
| `docs/getting-started/` | Installation, quick start, troubleshooting |
| `docs/concepts/` | Architecture, agents, memory, tools, skills |
| `docs/configuration/` | Config reference with examples |
| `docs/workflows/` | Feature specifications (15 features) |
| `docs/reference/` | CLI, RPC API, tools, logging, metrics |
| `docs/reference/generated/` | Auto-generated from Go structs via `cmd/gendoc` |
| `docs/plans/archive/` | Historical implementation plans |

## Documentation Maintenance

**IMPORTANT**: When making changes to the implementation, always update related documentation to stay in sync:

1. **diagram.md**: Update architecture diagrams when adding/modifying components, agents, tools, or data flows
2. **CLAUDE.md**: Update this file when changing architecture patterns, adding new agents, or modifying key behaviors
3. **Code comments**: Keep inline documentation accurate when modifying function signatures or behavior
4. **features.md**: Always update features as they are implemented or changed to match the code.

### Documentation Rules

- All features must have a feature spec in `docs/workflows/` using the standard template
- Struct changes in `internal/config/schema.go` require running `make docs-generate`
- New CLI commands require updating `docs/reference/cli.md`
- New agents require updating `docs/concepts/multi-agent.md`
- All doc pages must be linked from `mkdocs.yml` nav
- README.md feature list must stay in sync with `docs/workflows/`

Documentation should always reflect the current implementation state. If you add a new agent, tool, or architectural component, document it immediately.
