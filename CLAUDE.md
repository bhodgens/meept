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

## Skills Discovery

Three-tier with priority shadowing:
```
.meept/skills/              # Project-local (highest)
~/.meept/skills/            # User-global
~/.config/meept/skills/     # System-wide
~/.meept/clawskills/        # Third-party (claw: prefix)
```

## Code Conventions

- Go 1.22+
- Standard library preferred where possible
- `log/slog` for structured logging
- Context propagation throughout
- Interfaces for testability
- Table-driven tests

## Project Structure

```
cmd/
  meept/           # CLI application
  meept-daemon/    # Daemon application
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
tests/             # Integration tests
archive/           # Legacy Python code (reference only)
```

## Legacy Python

The original Python implementation is archived in `archive/python-legacy/` for reference. It is no longer maintained or used.
