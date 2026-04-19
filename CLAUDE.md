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
User Input (CLI/Telegram/Web)
    → CommServer (Unix socket JSON-RPC)
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
- **MCP servers**: `~/.meept/mcp_servers.json`

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
  config/          # Configuration loading
  daemon/          # Daemon lifecycle
  llm/             # LLM client and resolution
  memory/          # Memory management (SQLite+FTS5)
  registry/        # Service registry
  rpc/             # JSON-RPC server
  scheduler/       # Job scheduling
  security/        # Security engine, sanitizer, tirith
  selfimprove/     # Self-improvement system
  skills/          # Skill discovery and parsing
  tools/           # Tool registry and builtins
config/            # Configuration templates
docs/              # MkDocs documentation site
tests/             # Integration tests
archive/           # Legacy Python code (reference only)
```

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
